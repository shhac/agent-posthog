package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/secret"
)

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient Doer
	MaxRetries int
	Debug      bool
	DebugOut   io.Writer
	// Sleep, when set, replaces the context-aware wait between retries (tests).
	Sleep func(time.Duration)
	// Guard, when set, runs before every state-changing request and can refuse
	// it, so a write policy holds for every command that writes.
	Guard func() error
}

type Page struct {
	Next    string            `json:"next"`
	Results []json.RawMessage `json:"results"`
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Token:      token,
		HTTPClient: http.DefaultClient,
		MaxRetries: 2,
	}
}

// WithBase returns a client with the same transport, retry, and debug
// settings for another host and token.
func (c *Client) WithBase(baseURL, token string) *Client {
	clone := *c
	clone.BaseURL = strings.TrimRight(baseURL, "/")
	clone.Token = token
	return &clone
}

// ListAll reads a paginated collection, following next links for at most
// maxPages pages (0 for no limit). It returns the results and the next link
// left unfollowed, empty once the collection is exhausted.
func (c *Client) ListAll(ctx context.Context, path string, query url.Values, maxPages int) ([]json.RawMessage, string, error) {
	var all []json.RawMessage
	for pages := 1; ; pages++ {
		page, err := c.List(ctx, path, query)
		if err != nil {
			return nil, "", err
		}
		all = append(all, page.Results...)
		if page.Next == "" || (maxPages > 0 && pages >= maxPages) {
			return all, page.Next, nil
		}
		path, query = page.Next, nil
	}
}

func (c *Client) Get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query, nil, true)
}

// Post is for POSTs that are safe to repeat, such as query endpoints. State-changing
// POSTs go through Mutate.
func (c *Client) Post(ctx context.Context, path string, query url.Values, body any) (json.RawMessage, error) {
	return c.do(ctx, http.MethodPost, path, query, body, true)
}

func (c *Client) Patch(ctx context.Context, path string, query url.Values, body any) (json.RawMessage, error) {
	return c.Mutate(ctx, http.MethodPatch, path, query, body)
}

func (c *Client) Delete(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.Mutate(ctx, http.MethodDelete, path, query, nil)
}

// Mutate sends a state-changing request. Only 429s are retried: a lost response
// or a 5xx may follow a committed write, and replaying it could create or
// overwrite twice.
func (c *Client) Mutate(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	if c.Guard != nil {
		if err := c.Guard(); err != nil {
			return nil, err
		}
	}
	return c.do(ctx, method, path, query, body, false)
}

func (c *Client) List(ctx context.Context, path string, query url.Values) (*Page, error) {
	raw, err := c.Get(ctx, path, query)
	if err != nil {
		return nil, err
	}
	var page Page
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("PostHog returned an unexpected list response shape")
	}
	return &page, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, idempotent bool) (json.RawMessage, error) {
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
		payload = encoded
	}

	attempts := c.MaxRetries + 1
	for attempt := range attempts {
		lastAttempt := attempt == attempts-1
		req, err := c.newRequest(ctx, method, path, query, payload)
		if err != nil {
			return nil, err
		}
		status, header, raw, err := c.send(req)
		if err != nil {
			if retry, failure := transportFailure(ctx, err, idempotent, lastAttempt); !retry {
				return nil, failure
			}
			if err := c.wait(ctx, backoff(attempt, 0)); err != nil {
				return nil, timedOut(err)
			}
			continue
		}
		after := retryAfter(header.Get("Retry-After"))
		if shouldRetry(status, idempotent) && !lastAttempt && after <= maxRetryWait {
			if err := c.wait(ctx, backoff(attempt, after)); err != nil {
				return nil, timedOut(err)
			}
			continue
		}
		return responseResult(status, raw, after, idempotent)
	}
	return nil, agenterrors.New("request failed after retries", agenterrors.FixableByRetry)
}

// send performs one request and reads the whole response.
func (c *Client) send(req *http.Request) (int, http.Header, []byte, error) {
	c.debug(map[string]any{"debug": "request", "method": req.Method, "url": secret.RedactURL(req.URL.String())})
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, nil, agenterrors.Wrap(err, agenterrors.FixableByRetry)
	}
	c.debug(map[string]any{"debug": "response", "status": resp.StatusCode, "url": secret.RedactURL(req.URL.String())})
	return resp.StatusCode, resp.Header, raw, nil
}

func (c *Client) debug(fields map[string]any) {
	if c.Debug && c.DebugOut != nil {
		_ = json.NewEncoder(c.DebugOut).Encode(fields)
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values, payload []byte) (*http.Request, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent).WithHint("Check the PostHog host or AGENT_POSTHOG_BASE_URL")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		next, err := url.Parse(path)
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
		// Absolute URLs come from PostHog's next links; the personal API key
		// only ever goes to the configured host.
		if next.Scheme != u.Scheme || next.Host != u.Host {
			return nil, agenterrors.New("PostHog pointed at "+next.Scheme+"://"+next.Host+", not the configured "+u.Scheme+"://"+u.Host+"; not sending the API key there", agenterrors.FixableByHuman).
				WithHint("Check the PostHog host setting matches the URL PostHog reports for itself (SITE_URL on self-hosted instances).")
		}
		u = next
	} else {
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		u.Path = strings.TrimRight(u.Path, "/") + path
	}
	q := u.Query()
	for key, values := range query {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	u.RawQuery = q.Encode()

	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return req, nil
}
