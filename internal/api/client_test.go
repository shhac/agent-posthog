package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
