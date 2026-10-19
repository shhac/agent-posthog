package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	agenterrors "github.com/shhac/lib-agent-output"
)

// testServer serves handler for the rest of the test and returns its URL.
func testServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

func TestClientListAndAuthHeader(t *testing.T) {
	var gotAuth string
	base := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"next":    nil,
			"results": []map[string]any{{"id": 1}},
		})
	})

	client := New(base, "phx_test")
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

type failingDoer struct{ calls int }

func (d *failingDoer) Do(*http.Request) (*http.Response, error) {
	d.calls++
	return nil, errors.New("connection reset")
}

func TestGuardRefusesWritesButNotReads(t *testing.T) {
	requests := 0
	base := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{}`))
	})
	client := New(base, "phx_test")
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
	base := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	var log strings.Builder
	client := New(base, "phx_realpersonalkey123")
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

func apiErrHint(err error) string {
	var apiErr *agenterrors.Error
	if errors.As(err, &apiErr) {
		return apiErr.Hint
	}
	return ""
}

func TestListAllStopsAtThePageLimit(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		next := ""
		if page != "3" {
			n := 1
			if page != "" {
				n, _ = strconv.Atoi(page)
			}
			next = fmt.Sprintf("%s/api/items/?page=%d", server.URL, n+1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{page}, "next": next})
	}))
	defer server.Close()
	client := New(server.URL, "phx_test")

	items, next, err := client.ListAll(context.Background(), "/api/items/", nil, 2)
	if err != nil || len(items) != 2 || !strings.HasSuffix(next, "page=3") {
		t.Fatalf("limited: items=%s next=%q err=%v", items, next, err)
	}
	items, next, err = client.ListAll(context.Background(), "/api/items/", nil, 0)
	if err != nil || len(items) != 3 || next != "" {
		t.Fatalf("unlimited: items=%s next=%q err=%v", items, next, err)
	}
}

func TestListAllKeepsTheKeyOnTheConfiguredHost(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != ""
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{1}, "next": other.URL + "/api/items/?page=2"})
	}))
	defer server.Close()

	_, _, err := New(server.URL, "phx_test").ListAll(context.Background(), "/api/items/", nil, 0)
	if err == nil || !strings.Contains(err.Error(), "not sending the API key there") {
		t.Fatalf("err = %v", err)
	}
	if leaked {
		t.Fatal("the personal API key was sent to another host")
	}
}
