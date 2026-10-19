package mockposthog

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func request(t *testing.T, server *httptest.Server, method, path string, body any, auth bool) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, server.URL+path, reader)
	if auth {
		req.Header.Set("Authorization", "Bearer phx_mock")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestPatchMergesFiltersAndBumpsVersion(t *testing.T) {
	server := httptest.NewServer(NewServer())
	defer server.Close()
	status, flag := request(t, server, http.MethodPatch, "/api/projects/123/feature_flags/22/",
		map[string]any{"filters": map[string]any{"early_exit": true}}, true)
	if status != http.StatusOK {
		t.Fatalf("status = %d %v", status, flag)
	}
	filters := flag["filters"].(map[string]any)
	if filters["early_exit"] != true || filters["groups"] == nil || flag["version"] != float64(2) {
		t.Fatalf("flag = %v", flag)
	}
}

func TestPatchDetectsOverlappingStaleWrite(t *testing.T) {
	server := httptest.NewServer(NewServer())
	defer server.Close()
	_, original := request(t, server, http.MethodGet, "/api/projects/123/feature_flags/22/", nil, true)
	request(t, server, http.MethodPatch, "/api/projects/123/feature_flags/22/", map[string]any{"active": false}, true)

	status, _ := request(t, server, http.MethodPatch, "/api/projects/123/feature_flags/22/",
		map[string]any{"active": true, "version": original["version"], "original_flag": original}, true)
	if status != http.StatusOK {
		t.Fatalf("same final value as current should not conflict: %d", status)
	}
	status, _ = request(t, server, http.MethodPatch, "/api/projects/123/feature_flags/22/",
		map[string]any{"name": "Renamed", "version": original["version"], "original_flag": original}, true)
	if status != http.StatusOK {
		t.Fatalf("disjoint field should not conflict: %d", status)
	}
	_, current := request(t, server, http.MethodGet, "/api/projects/123/feature_flags/22/", nil, true)
	status, _ = request(t, server, http.MethodPatch, "/api/projects/123/feature_flags/22/",
		map[string]any{"name": "Mine", "version": original["version"], "original_flag": original}, true)
	if status != http.StatusConflict {
		t.Fatalf("overlapping stale write status = %d (current %v)", status, current["name"])
	}
}

func TestArchiveRequiresInactive(t *testing.T) {
	server := httptest.NewServer(NewServer())
	defer server.Close()
	status, _ := request(t, server, http.MethodPatch, "/api/projects/123/feature_flags/22/", map[string]any{"archived": true}, true)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d", status)
	}
}

func TestPublicFlagsEvaluatesWithoutPersonalKey(t *testing.T) {
	server := httptest.NewServer(NewServer())
	defer server.Close()
	request(t, server, http.MethodPost, "/api/projects/123/feature_flags/", map[string]any{
		"key": "staging-gate", "active": true,
		"filters": map[string]any{"groups": []any{
			map[string]any{"properties": []any{map[string]any{"key": "app_env", "type": "person", "operator": "exact", "value": "staging"}}, "rollout_percentage": 100},
		}},
	}, true)
	status, body := request(t, server, http.MethodPost, "/flags?v=2", map[string]any{
		"api_key": "phc_mock_project_123", "distinct_id": "someone", "person_properties": map[string]any{"app_env": "staging"},
	}, false)
	if status != http.StatusOK {
		t.Fatalf("status = %d %v", status, body)
	}
	gate := body["flags"].(map[string]any)["staging-gate"].(map[string]any)
	if gate["enabled"] != true || gate["reason"].(map[string]any)["condition_index"] != float64(0) {
		t.Fatalf("gate = %v", gate)
	}
}

func TestStoreFillsNullAggregationLikePostHog(t *testing.T) {
	server := httptest.NewServer(NewServer())
	defer server.Close()
	_, flag := request(t, server, http.MethodPost, "/api/projects/123/feature_flags/", map[string]any{
		"key": "plain", "filters": map[string]any{"groups": []any{map[string]any{"properties": []any{}, "rollout_percentage": 100}}},
	}, true)
	group := flag["filters"].(map[string]any)["groups"].([]any)[0].(map[string]any)
	if v, ok := group["aggregation_group_type_index"]; !ok || v != nil {
		t.Fatalf("group = %v", group)
	}
}
