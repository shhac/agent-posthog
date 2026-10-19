package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"
)

// HTTPError carries the raw status and body of a non-2xx response as the Cause
// of the agent-facing error, so callers can classify responses (approval
// policies, version conflicts) that the message alone cannot distinguish.
type HTTPError struct {
	Status int
	Body   json.RawMessage
}

func (e *HTTPError) Error() string { return fmt.Sprintf("PostHog returned HTTP %d", e.Status) }

const outcomeUnknownHint = "The write may or may not have been applied. Re-read the resource before retrying."

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

func timedOut(err error) error {
	return agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("The request ran out of time; retry, or raise --timeout.")
}
