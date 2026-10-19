package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"
)

func TestClientListAndAuthHeader(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"next":    nil,
			"results": []map[string]any{{"id": 1}},
		})
	}))
	defer server.Close()

	client := New(server.URL, "phx_test")
	page, err := client.List(context.Background(), "/api/organizations/", nil)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if gotAuth != "Bearer phx_test" {
		t.Fatalf("Authorization header = %q", gotAuth)
	}
	if len(page.Results) != 1 {
		t.Fatalf("len(results) = %d", len(page.Results))
	}
}

func TestClientRetriesRateLimit(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"detail": "slow down"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	client := New(server.URL, "phx_test")
	client.Sleep = func(time.Duration) {}
	_, err := client.Get(context.Background(), "/api/users/@me/", nil)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestClientMapsAuthErrorsToHumanFixable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"detail": "Invalid Personal API key."})
	}))
	defer server.Close()

	client := New(server.URL, "bad")
	_, err := client.Get(context.Background(), "/api/users/@me/", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "Authentication failed: Invalid Personal API key." {
		t.Fatalf("error = %q", got)
	}
}

func TestMutateDoesNotRetryServerErrors(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := New(server.URL, "phx_test")
	client.Sleep = func(time.Duration) {}
	_, err := client.Mutate(context.Background(), http.MethodPatch, "/api/projects/1/feature_flags/2/", nil, map[string]any{"active": true})
	if err == nil {
		t.Fatal("expected error")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	var apiErr *agenterrors.Error
	if !errors.As(err, &apiErr) || apiErr.FixableBy != agenterrors.FixableByAgent || !strings.Contains(apiErr.Hint, "Re-read") {
		t.Fatalf("err = %#v", err)
	}
}

func TestMutateRetriesRateLimit(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	client := New(server.URL, "phx_test")
	client.Sleep = func(time.Duration) {}
	if _, err := client.Mutate(context.Background(), http.MethodPost, "/api/x/", nil, map[string]any{}); err != nil {
		t.Fatalf("Mutate returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestErrorsCarryHTTPStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "approval_required", "detail": "Needs approval", "change_request_id": "cr_1"})
	}))
	defer server.Close()

	client := New(server.URL, "phx_test")
	_, err := client.Mutate(context.Background(), http.MethodPatch, "/api/x/", nil, map[string]any{})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want HTTPError cause", err)
	}
	if httpErr.Status != http.StatusConflict || !strings.Contains(string(httpErr.Body), "cr_1") {
		t.Fatalf("httpErr = %+v", httpErr)
	}
	if err.Error() != "Needs approval" {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestErrorFieldUsedWhenDetailMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "Feature flag to copy does not exist."})
	}))
	defer server.Close()

	_, err := New(server.URL, "phx_test").Get(context.Background(), "/api/x/", nil)
	if err == nil || err.Error() != "Feature flag to copy does not exist." {
		t.Fatalf("err = %v", err)
	}
}

type failingDoer struct{ calls int }

func (d *failingDoer) Do(*http.Request) (*http.Response, error) {
	d.calls++
	return nil, errors.New("connection reset")
}

func TestNetworkErrorsRetryReadsButNeverWrites(t *testing.T) {
	read := &failingDoer{}
	client := New("http://posthog.invalid", "phx_test")
	client.HTTPClient, client.MaxRetries, client.Sleep = read, 3, func(time.Duration) {}
	if _, err := client.Get(context.Background(), "/api/x/", nil); err == nil {
		t.Fatal("expected error")
	}
	if read.calls != 4 {
		t.Fatalf("read attempts = %d, want 4", read.calls)
	}

	write := &failingDoer{}
	client.HTTPClient = write
	_, err := client.Mutate(context.Background(), http.MethodPost, "/api/x/", nil, map[string]any{})
	var apiErr *agenterrors.Error
	if write.calls != 1 || !errors.As(err, &apiErr) || apiErr.FixableBy != agenterrors.FixableByAgent || !strings.Contains(apiErr.Hint, "Re-read") {
		t.Fatalf("write attempts = %d, err = %#v", write.calls, err)
	}
}

func TestExpiredContextStopsRetries(t *testing.T) {
	doer := &failingDoer{}
	client := New("http://posthog.invalid", "phx_test")
	client.HTTPClient, client.MaxRetries = doer, 3
	slept := 0
	client.Sleep = func(time.Duration) { slept++ }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Get(ctx, "/api/x/", nil)
	var apiErr *agenterrors.Error
	if doer.calls != 1 || slept != 0 || !errors.As(err, &apiErr) || !strings.Contains(apiErr.Hint, "--timeout") {
		t.Fatalf("calls = %d, slept = %d, err = %#v", doer.calls, slept, err)
	}
}

func TestGuardRefusesWritesButNotReads(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := New(server.URL, "phx_test")
	client.Guard = func() error { return errors.New("writes are off") }
	if _, err := client.Get(context.Background(), "/api/x/", nil); err != nil {
		t.Fatalf("reads must pass the guard: %v", err)
	}
	for _, write := range []func() error{
		func() error {
			_, err := client.Patch(context.Background(), "/api/x/", nil, map[string]any{})
			return err
		},
		func() error { _, err := client.Delete(context.Background(), "/api/x/", nil); return err },
		func() error {
			_, err := client.Mutate(context.Background(), http.MethodPost, "/api/x/", nil, nil)
			return err
		},
	} {
		if err := write(); err == nil || err.Error() != "writes are off" {
			t.Fatalf("err = %v", err)
		}
	}
	if requests != 1 {
		t.Fatalf("refused writes must not reach the server: %d requests", requests)
	}
}

func TestDebugLogsRedactKeysInURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	var log strings.Builder
	client := New(server.URL, "phx_realpersonalkey123")
	client.Debug, client.DebugOut = true, &log
	query := url.Values{"token": {"opaque-secret"}, "next": {"/api/x/?k=phc_projecttoken456"}, "limit": {"5"}}
	if _, err := client.Get(context.Background(), "/api/x/", query); err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"realpersonalkey123", "opaque-secret", "projecttoken456", "Authorization"} {
		if strings.Contains(log.String(), leaked) {
			t.Fatalf("debug log leaked %q: %s", leaked, log.String())
		}
	}
	if !strings.Contains(log.String(), "limit=5") {
		t.Fatalf("non-secret parameters should stay: %s", log.String())
	}
}

func TestRetryAfterIsCappedAndHonorsTheDeadline(t *testing.T) {
	status := http.StatusTooManyRequests
	retryAfterHeader := "3600"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", retryAfterHeader)
		w.WriteHeader(status)
	}))
	defer server.Close()

	var waits []time.Duration
	client := New(server.URL, "phx_test")
	client.Sleep = func(d time.Duration) { waits = append(waits, d) }
	_, err := client.Get(context.Background(), "/api/x/", nil)
	var apiErr *agenterrors.Error
	if !errors.As(err, &apiErr) || apiErr.FixableBy != agenterrors.FixableByRetry || apiErr.RetryAfterSeconds != 3600 || len(waits) != 0 {
		t.Fatalf("an hour-long Retry-After must come back to the caller, not be slept: err=%#v waits=%v", err, waits)
	}

	retryAfterHeader = "2"
	waits = nil
	_, _ = client.Get(context.Background(), "/api/x/", nil)
	if len(waits) != 2 || waits[0] != 2*time.Second {
		t.Fatalf("a short Retry-After is honored: %v", waits)
	}

	retryAfterHeader = time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	waits = nil
	_, _ = client.Get(context.Background(), "/api/x/", nil)
	if len(waits) == 0 || waits[0] < 8*time.Second || waits[0] > 11*time.Second {
		t.Fatalf("an HTTP-date Retry-After is honored: %v", waits)
	}

	retryAfterHeader = "20"
	client.Sleep = nil
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = client.Get(ctx, "/api/x/", nil)
	if time.Since(start) > time.Second || err == nil || !strings.Contains(apiErrHint(err), "--timeout") {
		t.Fatalf("a wait must end with the deadline: took %v, err=%v", time.Since(start), err)
	}
}

func apiErrHint(err error) string {
	var apiErr *agenterrors.Error
	if errors.As(err, &apiErr) {
		return apiErr.Hint
	}
	return ""
}
