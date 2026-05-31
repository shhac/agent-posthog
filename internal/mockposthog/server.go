package mockposthog

import (
	"encoding/json"
	"net/http"
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
		"GET  /api/projects/{project}/experiments/",
		"GET  /api/environments/{env}/persons/",
		"GET  /api/environments/{env}/insights/",
		"GET  /api/environments/{env}/dashboards/",
		"GET  /api/environments/{env}/session_recordings/",
		"POST /api/environments/{env}/query/",
	}
}

func handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") == "" {
		w.WriteHeader(http.StatusUnauthorized)
		write(w, map[string]any{"type": "authentication_error", "code": "invalid_personal_api_key", "detail": "Invalid Personal API key."})
		return
	}
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/api/users/@me/":
		write(w, map[string]any{"id": 7, "uuid": "user-uuid", "email": "agent@example.com", "name": "Agent User"})
	case r.Method == http.MethodGet && path == "/api/organizations/":
		writePage(w, []any{map[string]any{"id": "org_1", "name": "Acme", "slug": "acme"}})
	case r.Method == http.MethodGet && path == "/api/organizations/org_1/":
		write(w, map[string]any{"id": "org_1", "name": "Acme", "slug": "acme"})
	case r.Method == http.MethodGet && path == "/api/organizations/org_1/projects/":
		writePage(w, []any{map[string]any{"id": 123, "name": "Website", "timezone": "UTC"}})
	case r.Method == http.MethodGet && path == "/api/organizations/org_1/projects/123/":
		write(w, map[string]any{"id": 123, "name": "Website", "timezone": "UTC"})
	case r.Method == http.MethodGet && path == "/api/projects/123/environments/":
		writePage(w, []any{map[string]any{"id": 456, "name": "Production", "type": "production"}})
	case r.Method == http.MethodGet && path == "/api/projects/123/environments/456/":
		write(w, map[string]any{"id": 456, "name": "Production", "type": "production"})
	case r.Method == http.MethodGet && path == "/api/projects/123/event_definitions/":
		writePage(w, []any{map[string]any{"id": 1, "name": "$pageview"}, map[string]any{"id": 2, "name": "signup completed"}})
	case r.Method == http.MethodGet && path == "/api/projects/123/event_definitions/1/":
		write(w, map[string]any{"id": 1, "name": "$pageview"})
	case r.Method == http.MethodGet && path == "/api/projects/123/property_definitions/":
		writePage(w, []any{map[string]any{"id": 10, "name": "$browser", "type": "event"}, map[string]any{"id": 11, "name": "email", "type": "person"}})
	case r.Method == http.MethodGet && path == "/api/projects/123/feature_flags/":
		writePage(w, []any{map[string]any{"id": 22, "key": "checkout-v2", "name": "Checkout v2", "active": true}})
	case r.Method == http.MethodGet && path == "/api/projects/123/feature_flags/22/":
		write(w, map[string]any{"id": 22, "key": "checkout-v2", "name": "Checkout v2", "active": true})
	case r.Method == http.MethodGet && path == "/api/projects/123/feature_flags/22/dependent_flags/":
		writePage(w, []any{})
	case r.Method == http.MethodGet && path == "/api/projects/123/feature_flags/22/activity/":
		writePage(w, []any{map[string]any{"type": "activity", "detail": "Flag created"}})
	case r.Method == http.MethodGet && path == "/api/projects/123/experiments/":
		writePage(w, []any{map[string]any{"id": 33, "name": "Signup CTA"}})
	case r.Method == http.MethodGet && path == "/api/projects/123/experiments/33/":
		write(w, map[string]any{"id": 33, "name": "Signup CTA"})
	case r.Method == http.MethodGet && path == "/api/environments/456/persons/":
		writePage(w, []any{map[string]any{"id": 44, "uuid": "person-uuid", "distinct_ids": []string{"user_123"}, "properties": map[string]any{"email": "user@example.com"}}})
	case r.Method == http.MethodGet && path == "/api/environments/456/persons/44/":
		write(w, map[string]any{"id": 44, "uuid": "person-uuid", "distinct_ids": []string{"user_123"}, "properties": map[string]any{"email": "user@example.com"}})
	case r.Method == http.MethodGet && path == "/api/environments/456/persons/44/activity/":
		writePage(w, []any{map[string]any{"event": "$pageview", "timestamp": "2026-05-31T12:00:00Z"}})
	case r.Method == http.MethodGet && path == "/api/environments/456/insights/":
		writePage(w, []any{map[string]any{"id": 55, "name": "Activation"}})
	case r.Method == http.MethodGet && path == "/api/environments/456/insights/55/":
		write(w, map[string]any{"id": 55, "name": "Activation"})
	case r.Method == http.MethodGet && path == "/api/environments/456/dashboards/":
		writePage(w, []any{map[string]any{"id": 66, "name": "Growth"}})
	case r.Method == http.MethodGet && path == "/api/environments/456/dashboards/66/":
		write(w, map[string]any{"id": 66, "name": "Growth"})
	case r.Method == http.MethodGet && path == "/api/environments/456/dashboards/66/run_insights/":
		writePage(w, []any{map[string]any{"type": "tile_result", "insight_id": 55, "results": []int{1, 2, 3}}})
	case r.Method == http.MethodGet && path == "/api/environments/456/session_recordings/":
		writePage(w, []any{map[string]any{"id": "rec_1", "person_id": 44, "viewed": false}})
	case r.Method == http.MethodGet && path == "/api/environments/456/session_recordings/rec_1/":
		write(w, map[string]any{"id": "rec_1", "person_id": 44, "viewed": false})
	case r.Method == http.MethodPost && path == "/api/environments/456/query/":
		write(w, map[string]any{
			"id":      "query_1",
			"columns": []string{"event", "count"},
			"results": [][]any{{"$pageview", 42}, {"signup completed", 7}},
		})
	default:
		if strings.Contains(path, "rate_limit") {
			w.WriteHeader(http.StatusTooManyRequests)
			write(w, map[string]any{"detail": "Rate limited"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		write(w, map[string]any{"detail": "No mock route for " + r.Method + " " + path})
	}
}

func writePage(w http.ResponseWriter, results []any) {
	write(w, map[string]any{"next": nil, "previous": nil, "results": results})
}

func write(w http.ResponseWriter, payload any) {
	_ = json.NewEncoder(w).Encode(payload)
}
