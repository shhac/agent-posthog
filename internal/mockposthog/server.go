package mockposthog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func NewServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handle)
	return mux
}

func Routes() []string {
	return []string{
		"GET  /api/users/@me/",
		"GET  /api/organizations/",
		"GET  /api/organizations/{org}/",
		"GET  /api/organizations/{org}/projects/",
		"GET  /api/projects/{project}/environments/",
		"GET  /api/projects/{project}/event_definitions/",
		"GET  /api/projects/{project}/property_definitions/",
		"GET  /api/projects/{project}/feature_flags/",
		"GET  /api/projects/{project}/feature_flags/{id}/",
		"GET  /api/projects/{project}/feature_flags/{id}/activity/",
		"GET  /api/projects/{project}/feature_flags/{id}/dependent_flags/",
		"GET  /api/projects/{project}/experiments/",
		"GET  /api/projects/{project}/experiments/{id}/",
		"GET  /api/projects/{project}/session_recordings/{id}/sharing/",
		"GET  /api/environments/{env}/persons/",
		"GET  /api/environments/{env}/insights/",
		"GET  /api/environments/{env}/dashboards/",
		"GET  /api/environments/{env}/session_recordings/",
		"GET  /api/environments/{env}/query/{id}/",
		"GET  /api/environments/{env}/query/{id}/log/",
		"POST /api/environments/{env}/query/",
		"GET  /api/mock/rate_limit",
		"GET  /api/mock/validation_error",
	}
}

func handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	auth := r.Header.Get("Authorization")
	if auth == "" || strings.Contains(auth, "phx_invalid") {
		writeStatus(w, http.StatusUnauthorized, map[string]any{
			"type":   "authentication_error",
			"code":   "invalid_personal_api_key",
			"detail": "Invalid Personal API key.",
		})
		return
	}

	path := r.URL.Path
	if strings.Contains(auth, "phx_no_scope") && path != "/api/users/@me/" {
		writeStatus(w, http.StatusForbidden, map[string]any{
			"type":   "permission_denied",
			"code":   "permission_denied",
			"detail": "API key is missing the required scope for this endpoint.",
		})
		return
	}
	if path == "/api/mock/rate_limit" || strings.Contains(path, "rate_limit") {
		w.Header().Set("Retry-After", "0")
		writeStatus(w, http.StatusTooManyRequests, map[string]any{
			"type":   "throttled_error",
			"code":   "throttled",
			"detail": "Rate limit exceeded.",
		})
		return
	}
	if path == "/api/mock/validation_error" {
		writeStatus(w, http.StatusBadRequest, map[string]any{
			"type":   "validation_error",
			"code":   "invalid_input",
			"attr":   "query",
			"detail": "This field is required.",
		})
		return
	}

	switch {
	case r.Method == http.MethodGet && path == "/api/users/@me/":
		write(w, map[string]any{"id": 7, "uuid": "user-uuid", "email": "agent@example.com", "name": "Agent User"})
	case r.Method == http.MethodGet && path == "/api/organizations/":
		writeOrganizations(w, r)
	case r.Method == http.MethodGet && path == "/api/organizations/org_1/":
		write(w, map[string]any{"id": "org_1", "name": "Acme", "slug": "acme"})
	case r.Method == http.MethodGet && path == "/api/organizations/org_1/projects/":
		writePage(w, r, []any{map[string]any{"id": 123, "name": "Website", "timezone": "UTC"}})
	case r.Method == http.MethodGet && path == "/api/organizations/org_1/projects/123/":
		write(w, map[string]any{"id": 123, "name": "Website", "timezone": "UTC"})
	case r.Method == http.MethodGet && path == "/api/projects/123/environments/":
		writePage(w, r, []any{map[string]any{"id": 456, "name": "Production", "type": "production"}})
	case r.Method == http.MethodGet && path == "/api/projects/123/environments/456/":
		write(w, map[string]any{"id": 456, "name": "Production", "type": "production"})
	case r.Method == http.MethodGet && path == "/api/projects/123/event_definitions/":
		writePage(w, r, eventDefinitions())
	case r.Method == http.MethodGet && path == "/api/projects/123/event_definitions/1/":
		write(w, map[string]any{"id": 1, "name": "$pageview"})
	case r.Method == http.MethodGet && path == "/api/projects/123/property_definitions/":
		writePage(w, r, propertyDefinitions())
	case r.Method == http.MethodGet && path == "/api/projects/123/feature_flags/":
		writeFeatureFlags(w, r)
	case r.Method == http.MethodGet && path == "/api/projects/123/feature_flags/22/":
		write(w, checkoutFlag())
	case r.Method == http.MethodGet && path == "/api/projects/123/feature_flags/23/":
		write(w, billingFlag(23))
	case r.Method == http.MethodGet && path == "/api/projects/123/feature_flags/24/":
		write(w, billingFlag(24))
	case r.Method == http.MethodGet && path == "/api/projects/123/feature_flags/22/dependent_flags/":
		writePage(w, r, []any{map[string]any{"id": 25, "key": "checkout-v2-dependent", "name": "Checkout dependent", "active": true}})
	case r.Method == http.MethodGet && path == "/api/projects/123/feature_flags/22/activity/":
		writePage(w, r, flagActivity())
	case r.Method == http.MethodGet && path == "/api/projects/123/experiments/":
		writePage(w, r, experiments())
	case r.Method == http.MethodGet && path == "/api/projects/123/experiments/33/":
		write(w, experiments()[1])
	case r.Method == http.MethodGet && path == "/api/projects/123/session_recordings/rec_1/sharing/":
		write(w, map[string]any{
			"created_at":        "2026-05-31T12:10:00Z",
			"enabled":           true,
			"access_token":      "phs_recording_share_secret",
			"password_required": true,
			"share_passwords": []map[string]any{
				{"id": 1, "created_at": "2026-05-31T12:11:00Z", "note": "Support escalation", "created_by_email": "agent@example.com", "is_active": true},
			},
		})
	case r.Method == http.MethodGet && path == "/api/environments/456/persons/":
		writePersons(w, r)
	case r.Method == http.MethodGet && path == "/api/environments/456/persons/44/":
		write(w, person())
	case r.Method == http.MethodGet && path == "/api/environments/456/persons/44/activity/":
		writePage(w, r, personActivity())
	case r.Method == http.MethodGet && path == "/api/environments/456/insights/":
		writePage(w, r, []any{map[string]any{"id": 55, "name": "Activation", "query": map[string]any{"kind": "TrendsQuery"}}})
	case r.Method == http.MethodGet && path == "/api/environments/456/insights/55/":
		write(w, map[string]any{"id": 55, "name": "Activation", "query": map[string]any{"kind": "TrendsQuery"}})
	case r.Method == http.MethodGet && path == "/api/environments/456/dashboards/":
		writePage(w, r, []any{map[string]any{"id": 66, "name": "Growth", "tiles": []int{55, 56}}})
	case r.Method == http.MethodGet && path == "/api/environments/456/dashboards/66/":
		write(w, map[string]any{"id": 66, "name": "Growth", "tiles": []int{55, 56}})
	case r.Method == http.MethodGet && path == "/api/environments/456/dashboards/66/run_insights/":
		writePage(w, r, []any{
			map[string]any{"type": "tile_result", "insight_id": 55, "results": []int{1, 2, 3}},
			map[string]any{"type": "tile_error", "insight_id": 56, "error": "Query timed out"},
		})
	case r.Method == http.MethodGet && path == "/api/environments/456/session_recordings/":
		writePage(w, r, []any{recording()})
	case r.Method == http.MethodGet && path == "/api/environments/456/session_recordings/rec_1/":
		write(w, recording())
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/environments/456/query/"):
		writeQueryStatus(w, r)
	case r.Method == http.MethodPost && path == "/api/environments/456/query/":
		writeQueryCreate(w, r)
	default:
		writeStatus(w, http.StatusNotFound, map[string]any{"detail": "No mock route for " + r.Method + " " + path})
	}
}

func writeOrganizations(w http.ResponseWriter, r *http.Request) {
	writePage(w, r, []any{
		map[string]any{"id": "org_1", "name": "Acme", "slug": "acme"},
		map[string]any{"id": "org_2", "name": "Beta", "slug": "beta"},
	})
}

func writeFeatureFlags(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("search") {
	case "missing-flag":
		writePage(w, r, []any{})
	case "ambiguous-flag":
		writePage(w, r, []any{billingFlag(23), billingFlag(24)})
	default:
		writePage(w, r, []any{checkoutFlag(), billingFlag(23)})
	}
}

func writePersons(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("email") == "missing@example.com" || r.URL.Query().Get("distinct_id") == "missing" {
		writePage(w, r, []any{})
		return
	}
	writePage(w, r, []any{person()})
}

func writeQueryCreate(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body["async"] == true {
		write(w, map[string]any{
			"query_status": map[string]any{
				"id":             "query_pending",
				"query_async":    true,
				"complete":       false,
				"error":          false,
				"query_progress": 0.25,
				"results":        nil,
			},
		})
		return
	}
	write(w, map[string]any{
		"id":      "query_1",
		"columns": []string{"event", "count"},
		"results": [][]any{{"$pageview", 42}, {"signup completed", 7}},
	})
}

func writeQueryStatus(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if strings.HasSuffix(path, "/log") {
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/api/environments/456/query/"), "/log")
		write(w, map[string]any{"id": id, "status": "archived", "runtime_ms": 812, "query": "select event, count() from events"})
		return
	}
	id := strings.TrimPrefix(path, "/api/environments/456/query/")
	switch id {
	case "query_pending":
		write(w, map[string]any{"query_status": map[string]any{"id": id, "query_async": true, "complete": false, "error": false, "query_progress": 0.5, "results": nil}})
	case "query_failed":
		write(w, map[string]any{"query_status": map[string]any{"id": id, "query_async": true, "complete": true, "error": true, "error_message": "Syntax error near frobulate", "results": nil}})
	case "query_complete":
		write(w, map[string]any{"query_status": map[string]any{"id": id, "query_async": true, "complete": true, "error": false, "results": map[string]any{"columns": []string{"event", "count"}, "results": [][]any{{"$pageview", 42}}}}})
	default:
		writeStatus(w, http.StatusNotFound, map[string]any{"detail": "Query " + id + " not found."})
	}
}

func writePage(w http.ResponseWriter, r *http.Request, results []any) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page == 0 {
		page = 1
	}
	if limit <= 0 || limit >= len(results) {
		write(w, map[string]any{"next": nil, "previous": nil, "results": results})
		return
	}
	start := (page - 1) * limit
	if start >= len(results) {
		write(w, map[string]any{"next": nil, "previous": absoluteURL(r, page-1), "results": []any{}})
		return
	}
	end := start + limit
	if end > len(results) {
		end = len(results)
	}
	var next any
	if end < len(results) {
		next = absoluteURL(r, page+1)
	}
	var previous any
	if page > 1 {
		previous = absoluteURL(r, page-1)
	}
	write(w, map[string]any{"next": next, "previous": previous, "results": results[start:end]})
}

func absoluteURL(r *http.Request, page int) string {
	q := r.URL.Query()
	q.Set("page", strconv.Itoa(page))
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s%s?%s", scheme, r.Host, r.URL.Path, q.Encode())
}

func eventDefinitions() []any {
	return []any{
		map[string]any{"id": 1, "name": "$pageview", "last_seen_at": "2026-05-31T12:00:00Z", "volume_30_day": 42000},
		map[string]any{"id": 2, "name": "signup completed", "last_seen_at": "2026-05-31T11:30:00Z", "volume_30_day": 700},
		map[string]any{"id": 3, "name": "checkout started", "last_seen_at": "2026-05-31T11:00:00Z", "volume_30_day": 500},
	}
}

func propertyDefinitions() []any {
	return []any{
		map[string]any{"id": 10, "name": "$browser", "type": "event", "property_type": "String"},
		map[string]any{"id": 11, "name": "$current_url", "type": "event", "property_type": "String"},
		map[string]any{"id": 12, "name": "email", "type": "person", "property_type": "String"},
		map[string]any{"id": 13, "name": "plan", "type": "person", "property_type": "String"},
	}
}

func checkoutFlag() map[string]any {
	return map[string]any{
		"id":                           22,
		"key":                          "checkout-v2",
		"name":                         "Checkout v2",
		"active":                       true,
		"rollout_percentage":           50,
		"filters":                      map[string]any{"groups": []map[string]any{{"properties": []any{}, "rollout_percentage": 50}}},
		"multivariate":                 map[string]any{"variants": []map[string]any{{"key": "control", "rollout_percentage": 50}, {"key": "treatment", "rollout_percentage": 50}}},
		"ensure_experience_continuity": true,
	}
}

func billingFlag(id int) map[string]any {
	return map[string]any{"id": id, "key": "ambiguous-flag", "name": "Ambiguous flag", "active": true}
}

func flagActivity() []any {
	return []any{
		map[string]any{
			"id":       "activity_1",
			"activity": "updated",
			"scope":    "FeatureFlag",
			"item_id":  "22",
			"detail": map[string]any{
				"changes": []map[string]any{
					{"type": "FeatureFlag", "action": "changed", "field": "active", "before": false, "after": true},
				},
			},
		},
	}
}

func experiments() []any {
	return []any{
		map[string]any{"id": 31, "name": "Draft onboarding copy", "feature_flag_key": "onboarding-copy", "archived": false, "start_date": nil, "end_date": nil, "metrics": []any{}},
		map[string]any{"id": 33, "name": "Signup CTA", "feature_flag_key": "checkout-v2", "archived": false, "start_date": "2026-05-01T00:00:00Z", "end_date": nil, "metrics": []map[string]any{{"kind": "ExperimentMetric", "metric_type": "funnel", "name": "signup conversion"}}},
		map[string]any{"id": 34, "name": "Completed pricing page", "feature_flag_key": "pricing-page", "archived": true, "start_date": "2026-01-01T00:00:00Z", "end_date": "2026-02-01T00:00:00Z", "metrics": []map[string]any{{"kind": "ExperimentMetric", "metric_type": "trend", "name": "revenue"}}},
	}
}

func person() map[string]any {
	return map[string]any{"id": 44, "uuid": "person-uuid", "distinct_ids": []string{"user_123"}, "properties": map[string]any{"email": "user@example.com", "plan": "pro"}}
}

func personActivity() []any {
	return []any{
		map[string]any{"event": "$pageview", "timestamp": "2026-05-31T12:00:00Z", "properties": map[string]any{"$current_url": "https://example.com/pricing"}},
		map[string]any{"event": "signup completed", "timestamp": "2026-05-31T12:05:00Z", "properties": map[string]any{"plan": "pro"}},
	}
}

func recording() map[string]any {
	return map[string]any{"id": "rec_1", "person_id": 44, "viewed": false, "recording_duration": 88, "start_time": "2026-05-31T12:00:00Z", "console_error_count": 2}
}

func writeStatus(w http.ResponseWriter, status int, payload any) {
	w.WriteHeader(status)
	write(w, payload)
}

func write(w http.ResponseWriter, payload any) {
	_ = json.NewEncoder(w).Encode(payload)
}
