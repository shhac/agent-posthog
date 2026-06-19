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
	"strconv"
	"strings"
	"time"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
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
	Sleep      func(time.Duration)
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
		Sleep:      time.Sleep,
	}
}

func (c *Client) Get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query, nil)
}

func (c *Client) Post(ctx context.Context, path string, query url.Values, body any) (json.RawMessage, error) {
	return c.do(ctx, http.MethodPost, path, query, body)
}

func (c *Client) Patch(ctx context.Context, path string, query url.Values, body any) (json.RawMessage, error) {
	return c.do(ctx, http.MethodPatch, path, query, body)
}

func (c *Client) Delete(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodDelete, path, query, nil)
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

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}
	if c.Sleep == nil {
		c.Sleep = time.Sleep
	}

	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
	}

	attempts := c.MaxRetries + 1
	for attempt := range attempts {
		req, err := c.newRequest(ctx, method, path, query, payload)
		if err != nil {
			return nil, err
		}
		if c.Debug && c.DebugOut != nil {
			_ = json.NewEncoder(c.DebugOut).Encode(map[string]any{
				"debug":  "request",
				"method": method,
				"url":    redactURL(req.URL.String()),
			})
		}
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			if attempt < attempts-1 {
				c.Sleep(backoff(attempt, 0))
				continue
			}
			return nil, agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("Network request failed after retries")
		}
		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, agenterrors.Wrap(readErr, agenterrors.FixableByRetry)
		}
		if c.Debug && c.DebugOut != nil {
			_ = json.NewEncoder(c.DebugOut).Encode(map[string]any{
				"debug":  "response",
				"status": resp.StatusCode,
				"url":    redactURL(req.URL.String()),
			})
		}
		if retryable(resp.StatusCode) && attempt < attempts-1 {
			c.Sleep(backoff(attempt, retryAfter(resp.Header.Get("Retry-After"))))
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, mapError(resp.StatusCode, raw, retryAfter(resp.Header.Get("Retry-After")))
		}
		if len(raw) == 0 {
			return json.RawMessage(`{}`), nil
		}
		return json.RawMessage(raw), nil
	}
	return nil, agenterrors.New("request failed after retries", agenterrors.FixableByRetry)
}

func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values, payload []byte) (*http.Request, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent).WithHint("Check the PostHog host or AGENT_POSTHOG_BASE_URL")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err = url.Parse(path)
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
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

func mapError(status int, raw []byte, retryAfter time.Duration) error {
	detail := strings.TrimSpace(string(raw))
	var payload struct {
		Type   string `json:"type"`
		Code   string `json:"code"`
		Detail string `json:"detail"`
		Attr   string `json:"attr"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && payload.Detail != "" {
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

func redactURL(value string) string {
	return strings.ReplaceAll(value, "phx_mock", "phx_...")
}
