package featureflag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shhac/agent-posthog/internal/api"
	"github.com/shhac/agent-posthog/internal/flagspec"
	"github.com/shhac/agent-posthog/internal/mockposthog"
)

func mockProject(t *testing.T) Project {
	t.Helper()
	server := httptest.NewServer(mockposthog.NewServer())
	t.Cleanup(server.Close)
	return Project{Client: api.New(server.URL, "phx_mock"), ID: 123, Host: server.URL}
}

func TestKeyLookupFollowsPagesAndRefusesToGuess(t *testing.T) {
	savedLimit, savedPages := keyLookupLimit, keyLookupPages
	t.Cleanup(func() { keyLookupLimit, keyLookupPages = savedLimit, savedPages })
	p := mockProject(t)

	keyLookupLimit, keyLookupPages = 1, 5
	if _, _, err := p.FindByKey(context.Background(), "ambiguous-flag"); err == nil || !strings.Contains(err.Error(), "Multiple feature flags matched key ambiguous-flag") {
		t.Fatalf("a duplicate on page 2 must still be found: %v", err)
	}
	keyLookupPages = 1
	if _, _, err := p.FindByKey(context.Background(), "ambiguous-flag"); err == nil || !strings.Contains(err.Error(), "too many feature flags match") {
		t.Fatalf("running out of pages must not read as not-found (it would create a duplicate): %v", err)
	}
}

func conflictError(t *testing.T, body string) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	_, err := api.New(server.URL, "phx_mock").Patch(context.Background(), "/x/", nil, map[string]any{})
	return err
}

func TestPendingChangeRequestIsForAHuman(t *testing.T) {
	err := classifyWriteError(conflictError(t, `{"code":"change_request_pending","status":"approval_required","detail":"Already pending","change_request_id":"cr_9"}`))
	if !strings.Contains(err.Error(), "change request cr_9") {
		t.Fatalf("err = %v", err)
	}
}

func TestStaleConflictIsForTheAgent(t *testing.T) {
	err := classifyWriteError(conflictError(t, `{"type":"server_error","code":"conflict","detail":"The feature flag was updated by someone"}`))
	if !strings.Contains(err.Error(), "updated by someone") {
		t.Fatalf("err = %v", err)
	}
}

func TestPatchCarriesVersionAndFlagAsRead(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":7,"key":"k","version":4,"active":false}`))
	}))
	t.Cleanup(server.Close)
	p := Project{Client: api.New(server.URL, "phx_mock"), ID: 1}
	flag := map[string]any{"id": json.Number("7"), "key": "k", "version": json.Number("3"), "active": true}
	patch := flagspec.NewPatch()
	patch.Fields["active"] = false
	if _, err := p.patch(context.Background(), flag, patch); err != nil {
		t.Fatal(err)
	}
	original, _ := body["original_flag"].(map[string]any)
	if body["version"] != float64(3) || original["active"] != true || body["active"] != false {
		t.Fatalf("PATCH body = %v", body)
	}
}

func TestCommitWarnsWhenPostHogNormalizes(t *testing.T) {
	patch := flagspec.NewPatch()
	patch.Fields["active"] = true
	patch.Filters["groups"] = []any{map[string]any{"properties": []any{}, "rollout_percentage": json.Number("0")}}
	stored := `{"active":true,"filters":{"groups":[{"properties":[],"rollout_percentage":100}]}}`
	pending := &Pending{Receipt: &Receipt{}, patch: patch, send: func(context.Context) (json.RawMessage, error) {
		return json.RawMessage(stored), nil
	}}
	r, err := pending.Commit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != OutcomeApplied || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "filters.groups") {
		t.Fatalf("receipt = %+v", r)
	}
}
