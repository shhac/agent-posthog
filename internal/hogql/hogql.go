// Package hogql builds PostHog query requests and follows async queries to
// completion.
package hogql

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Body builds a HogQL query request. values fill {placeholder}s in sql, so
// user input never has to be escaped into the query text.
func Body(sql string, values map[string]any) map[string]any {
	q := map[string]any{"kind": "HogQLQuery", "query": sql}
	if len(values) > 0 {
		q["values"] = values
	}
	return map[string]any{"query": q}
}

// Status is the query_status of an async query response. Known is false for a
// response without one, such as a finished synchronous result.
type Status struct {
	Known    bool
	Complete bool
	Error    bool
	Message  string
}

// parseStatus reads the query_status of a response.
func parseStatus(raw json.RawMessage) Status {
	var payload struct {
		QueryStatus *struct {
			Complete     bool   `json:"complete"`
			Error        bool   `json:"error"`
			ErrorMessage string `json:"error_message"`
		} `json:"query_status"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.QueryStatus == nil {
		return Status{}
	}
	q := payload.QueryStatus
	return Status{Known: true, Complete: q.Complete, Error: q.Error, Message: q.ErrorMessage}
}

// ErrStillRunning is Wait running out of time before the query finished.
var ErrStillRunning = errors.New("query did not complete in time")

// Wait polls fetch every interval until the status is missing, failed, or
// complete, or maxWait runs out.
func Wait(ctx context.Context, fetch func(context.Context) (json.RawMessage, error), interval, maxWait time.Duration) (json.RawMessage, Status, error) {
	deadline := time.Now().Add(maxWait)
	for {
		raw, err := fetch(ctx)
		if err != nil {
			return nil, Status{}, err
		}
		status := parseStatus(raw)
		if !status.Known || status.Error || status.Complete {
			return raw, status, nil
		}
		if time.Now().Add(interval).After(deadline) {
			return nil, status, ErrStillRunning
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, status, ctx.Err()
		case <-timer.C:
		}
	}
}
