package query

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestWaitPollsUntilDoneOrOutOfTime(t *testing.T) {
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
	_, status, err := Wait(context.Background(), fetch, time.Millisecond, time.Second)
	if err != nil || !status.Complete || calls != 3 {
		t.Fatalf("status=%+v calls=%d err=%v", status, calls, err)
	}

	calls = 0
	pending := func(context.Context) (json.RawMessage, error) {
		calls++
		return json.RawMessage(statuses[0]), nil
	}
	if _, _, err := Wait(context.Background(), pending, 5*time.Millisecond, 12*time.Millisecond); !errors.Is(err, ErrStillRunning) {
		t.Fatalf("err = %v", err)
	}
	if _, status, _ := Wait(context.Background(), func(context.Context) (json.RawMessage, error) {
		return json.RawMessage(`{"results":[]}`), nil
	}, time.Millisecond, time.Second); status.Known {
		t.Fatal("a payload without query_status is returned as unknown")
	}
}
