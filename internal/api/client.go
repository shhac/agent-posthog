package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"
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

// maxRetryWait is the longest Retry-After the client waits out itself; a longer
// one comes back to the caller as a retry-later error instead of a hang.
const maxRetryWait = 30 * time.Second

// HTTPError carries the raw status and body of a non-2xx response as the Cause
// of the agent-facing error, so callers can classify responses (approval
// policies, version conflicts) that the message alone cannot distinguish.
type HTTPError struct {
	Status int
	Body   json.RawMessage
}

func (e *HTTPError) Error() string { return fmt.Sprintf("PostHog returned HTTP %d", e.Status) }

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
	c.debug(map[string]any{"debug": "request", "method": req.Method, "url": redactURL(req.URL.String())})
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
	c.debug(map[string]any{"debug": "response", "status": resp.StatusCode, "url": redactURL(req.URL.String())})
	return resp.StatusCode, resp.Header, raw, nil
}

func (c *Client) debug(fields map[string]any) {
	if c.Debug && c.DebugOut != nil {
		_ = json.NewEncoder(c.DebugOut).Encode(fields)
	}
}

// wait pauses between retries, ending early when the command's deadline does.
func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		c.Sleep(d)
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// transportFailure decides what a request that got no response means. A
// write may already have happened, so it is never retried.
func transportFailure(ctx context.Context, err error, idempotent, lastAttempt bool) (bool, error) {
	switch {
	case !idempotent:
		return false, agenterrors.Wrap(err, agenterrors.FixableByAgent).WithHint(outcomeUnknownHint)
	case ctx.Err() != nil:
		return false, timedOut(ctx.Err())
	case lastAttempt:
		return false, agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("Network request failed after retries")
	default:
		return true, nil
	}
}

func timedOut(err error) error {
	return agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("The request ran out of time; retry, or raise --timeout.")
}

// shouldRetry: reads retry on 429 and gateway errors; writes only on 429,
// which PostHog sends before doing anything.
func shouldRetry(status int, idempotent bool) bool {
	return retryable(status) && (idempotent || status == http.StatusTooManyRequests)
}

func responseResult(status int, raw []byte, after time.Duration, idempotent bool) (json.RawMessage, error) {
	if status < 200 || status >= 300 {
		mapped := mapError(status, raw, after)
		if !idempotent && status >= 500 {
			mapped = mapped.WithHint(outcomeUnknownHint)
			mapped.FixableBy = agenterrors.FixableByAgent
		}
		return nil, mapped
	}
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(raw), nil
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

const outcomeUnknownHint = "The write may or may not have been applied. Re-read the resource before retrying."

func mapError(status int, raw []byte, retryAfter time.Duration) *agenterrors.Error {
	err := classifyError(status, raw, retryAfter)
	err.Cause = &HTTPError{Status: status, Body: append(json.RawMessage(nil), raw...)}
	return err
}

func classifyError(status int, raw []byte, retryAfter time.Duration) *agenterrors.Error {
	detail := strings.TrimSpace(string(raw))
	var payload struct {
		Type   string `json:"type"`
		Code   string `json:"code"`
		Detail string `json:"detail"`
		Attr   string `json:"attr"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && payload.Detail == "" {
		payload.Detail = payload.Error
	}
	if payload.Detail != "" {
		detail = payload.Detail
		if payload.Attr != "" {
			detail = payload.Attr + ": " + detail
		}
	}
	if detail == "" {
		detail = http.StatusText(status)
	}

	switch {
	case status == http.StatusUnauthorized:
		return agenterrors.New("Authentication failed: "+detail, agenterrors.FixableByHuman).
			WithHint("Run 'agent-posthog auth check <profile>' or update the personal API key with 'agent-posthog auth add <profile> --form'.")
	case status == http.StatusForbidden:
		return agenterrors.New("Permission denied: "+detail, agenterrors.FixableByHuman).
			WithHint("The personal API key may need additional PostHog scopes for this resource.")
	case status == http.StatusNotFound:
		return agenterrors.New("Not found: "+detail, agenterrors.FixableByAgent).
			WithHint("Check the current profile, organization, project, and environment IDs.")
	case status == http.StatusTooManyRequests:
		err := agenterrors.New("Rate limited: "+detail, agenterrors.FixableByRetry).
			WithHint("Wait and retry, or narrow the query/list page size.")
		if retryAfter > 0 {
			err = err.WithRetryAfter(retryAfter)
		}
		return err
	case status >= 500:
		return agenterrors.New(fmt.Sprintf("PostHog server error (%d): %s", status, detail), agenterrors.FixableByRetry)
	default:
		return agenterrors.New(detail, agenterrors.FixableByAgent)
	}
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func retryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		return time.Until(when)
	}
	return 0
}

func backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	ms := int(math.Pow(2, float64(attempt))) * 250
	return time.Duration(ms) * time.Millisecond
}

// secretInURL matches PostHog keys (personal, project, share, and app tokens)
// anywhere in a URL, even URL-encoded inside another parameter, plus the value
// of query parameters named like secrets.
var secretInURL = regexp.MustCompile(`(ph[xcsar]_)[A-Za-z0-9_-]+|((?:^|[?&])(?:token|api_key|personal_api_key|project_token|access_token|secret)=)[^&]*`)

// redactURL keeps keys out of --debug output.
func redactURL(value string) string {
	return secretInURL.ReplaceAllStringFunc(value, func(match string) string {
		m := secretInURL.FindStringSubmatch(match)
		if m[1] != "" {
			return m[1] + "..."
		}
		return m[2] + "..."
	})
}
