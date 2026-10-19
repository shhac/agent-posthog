package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestQueryWaitFailsOnAFailedQuery(t *testing.T) {
	s := newMockSession(t, "phx_mock", "456")
	out, errOut, err := s.run("query", "wait", "query_failed", "--interval", "1ms", "--max-wait", "10ms")
	if err == nil || !strings.Contains(errOut, "query failed: Syntax error near frobulate") {
		t.Fatalf("a failed query must not exit 0: err=%v stderr=%s", err, errOut)
	}
	if !strings.Contains(out, "Syntax error near frobulate") {
		t.Fatalf("the status should still be on stdout: %s", out)
	}
}

func TestWaitForQueryPollsUntilDoneOrOutOfTime(t *testing.T) {
	statuses := []string{
		`{"query_status":{"complete":false}}`,
		`{"query_status":{"complete":false}}`,
		`{"query_status":{"complete":true,"results":[]}}`,
	}
	calls := 0
	fetch := func(context.Context) (json.RawMessage, error) {
		raw := statuses[min(calls, len(statuses)-1)]
		calls++
		return json.RawMessage(raw), nil
	}
	_, status, err := waitForQuery(context.Background(), fetch, time.Millisecond, time.Second)
	if err != nil || !status.Complete || calls != 3 {
		t.Fatalf("status=%+v calls=%d err=%v", status, calls, err)
	}

	calls = 0
	pending := func(context.Context) (json.RawMessage, error) {
		calls++
		return json.RawMessage(statuses[0]), nil
	}
	if _, _, err := waitForQuery(context.Background(), pending, 5*time.Millisecond, 12*time.Millisecond); err == nil || !strings.Contains(err.Error(), "--max-wait") {
		t.Fatalf("err = %v", err)
	}
	if _, status, _ := waitForQuery(context.Background(), func(context.Context) (json.RawMessage, error) {
		return json.RawMessage(`{"results":[]}`), nil
	}, time.Millisecond, time.Second); status.Known {
		t.Fatal("a payload without query_status is returned as unknown")
	}
}
