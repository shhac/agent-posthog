package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
