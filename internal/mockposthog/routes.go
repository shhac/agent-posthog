package mockposthog

import (
	"net/http"
	"sort"
)

// route is one exact-path fixture.
type route struct {
	method, path string
	serve        http.HandlerFunc
}

// exactRoutes are the fixed fixtures, matched on method and exact path.
func exactRoutes(store *flagStore) []route {
	return []route{
		{http.MethodGet, "/api/users/@me/", func(w http.ResponseWriter, r *http.Request) {
			write(w, map[string]any{"id": 7, "uuid": "user-uuid", "email": "agent@example.com", "name": "Agent User"})
		}},
		{http.MethodGet, "/api/organizations/", func(w http.ResponseWriter, r *http.Request) {
			writeOrganizations(w, r)
		}},
		{http.MethodGet, "/api/organizations/org_1/", func(w http.ResponseWriter, r *http.Request) {
			write(w, map[string]any{"id": "org_1", "name": "Acme", "slug": "acme"})
		}},
		{http.MethodGet, "/api/organizations/org_1/projects/", func(w http.ResponseWriter, r *http.Request) {
			writePage(w, r, []any{map[string]any{"id": 123, "name": "Website", "timezone": "UTC", "api_token": projectTokens[123]}})
		}},
		{http.MethodGet, "/api/organizations/org_1/projects/123/", func(w http.ResponseWriter, r *http.Request) {
			write(w, map[string]any{"id": 123, "name": "Website", "timezone": "UTC", "api_token": projectTokens[123]})
		}},
		{http.MethodGet, "/api/projects/123/environments/", func(w http.ResponseWriter, r *http.Request) {
			writePage(w, r, []any{map[string]any{"id": 456, "name": "Production", "type": "production"}})
		}},
		{http.MethodGet, "/api/projects/123/environments/456/", func(w http.ResponseWriter, r *http.Request) {
			write(w, map[string]any{"id": 456, "name": "Production", "type": "production"})
		}},
		{http.MethodGet, "/api/projects/123/event_definitions/", func(w http.ResponseWriter, r *http.Request) {
			writePage(w, r, eventDefinitions())
		}},
		{http.MethodGet, "/api/projects/123/event_definitions/1/", func(w http.ResponseWriter, r *http.Request) {
			write(w, map[string]any{"id": 1, "name": "$pageview"})
		}},
		{http.MethodGet, "/api/projects/123/property_definitions/", func(w http.ResponseWriter, r *http.Request) {
			writePage(w, r, propertyDefinitions())
		}},
		{http.MethodGet, "/api/projects/123/experiments/", func(w http.ResponseWriter, r *http.Request) {
			writePage(w, r, experiments())
		}},
		{http.MethodGet, "/api/projects/123/experiments/33/", func(w http.ResponseWriter, r *http.Request) {
			write(w, experiments()[1])
		}},
		{http.MethodGet, "/api/projects/123/session_recordings/rec_1/sharing/", func(w http.ResponseWriter, r *http.Request) {
			write(w, map[string]any{
				"created_at":        "2026-05-31T12:10:00Z",
				"enabled":           true,
				"access_token":      "phs_recording_share_secret",
				"password_required": true,
				"share_passwords": []map[string]any{
					{"id": 1, "created_at": "2026-05-31T12:11:00Z", "note": "Support escalation", "created_by_email": "agent@example.com", "is_active": true},
				},
			})
		}},
		{http.MethodGet, "/api/environments/456/persons/", func(w http.ResponseWriter, r *http.Request) {
			writePersons(w, r)
		}},
		{http.MethodGet, "/api/environments/456/persons/44/", func(w http.ResponseWriter, r *http.Request) {
			write(w, person())
		}},
		{http.MethodGet, "/api/environments/456/persons/44/activity/", func(w http.ResponseWriter, r *http.Request) {
			writePage(w, r, personActivity())
		}},
		{http.MethodGet, "/api/environments/456/insights/", func(w http.ResponseWriter, r *http.Request) {
			writePage(w, r, []any{map[string]any{"id": 55, "name": "Activation", "query": map[string]any{"kind": "TrendsQuery"}}})
		}},
		{http.MethodGet, "/api/environments/456/insights/55/", func(w http.ResponseWriter, r *http.Request) {
			write(w, map[string]any{"id": 55, "name": "Activation", "query": map[string]any{"kind": "TrendsQuery"}})
		}},
		{http.MethodGet, "/api/environments/456/dashboards/", func(w http.ResponseWriter, r *http.Request) {
			writePage(w, r, []any{map[string]any{"id": 66, "name": "Growth", "tiles": []int{55, 56}}})
		}},
		{http.MethodGet, "/api/environments/456/dashboards/66/", func(w http.ResponseWriter, r *http.Request) {
			write(w, map[string]any{"id": 66, "name": "Growth", "tiles": []int{55, 56}})
		}},
		{http.MethodGet, "/api/environments/456/dashboards/66/run_insights/", func(w http.ResponseWriter, r *http.Request) {
			writePage(w, r, []any{
				map[string]any{"type": "tile_result", "insight_id": 55, "results": []int{1, 2, 3}},
				map[string]any{"type": "tile_error", "insight_id": 56, "error": "Query timed out"},
			})
		}},
		{http.MethodGet, "/api/environments/456/session_recordings/", func(w http.ResponseWriter, r *http.Request) {
			writePage(w, r, []any{recording()})
		}},
		{http.MethodGet, "/api/environments/456/session_recordings/rec_1/", func(w http.ResponseWriter, r *http.Request) {
			write(w, recording())
		}},
		{http.MethodPost, "/api/environments/456/query/", func(w http.ResponseWriter, r *http.Request) {
			writeQueryCreate(w, r)
		}},
		{http.MethodGet, "/api/projects/123/", projectDetail(123)},
		{http.MethodGet, "/api/projects/124/", projectDetail(124)},
		{http.MethodPost, "/api/organizations/org_1/feature_flags/copy_flags/", store.copyFlags},
		{http.MethodPost, "/api/organizations/org_1/feature_flags/copy_flags/dependency_requirements/", func(w http.ResponseWriter, r *http.Request) {
			dependencyRequirements(w)
		}},
	}
}

// patternRoutes are served by handlers that match a path pattern (the flag
// store, async query status, public /flags); they are listed for --routes.
var patternRoutes = []string{
	"GET /api/projects/{project}/feature_flags/",
	"POST /api/projects/{project}/feature_flags/",
	"POST /api/projects/{project}/feature_flags/user_blast_radius/",
	"GET /api/projects/{project}/feature_flags/{id}/",
	"PATCH /api/projects/{project}/feature_flags/{id}/",
	"POST /api/projects/{project}/feature_flags/{id}/set_release_condition_rollout/",
	"POST /api/projects/{project}/feature_flags/{id}/test_evaluation/",
	"GET /api/projects/{project}/feature_flags/{id}/activity/",
	"GET /api/projects/{project}/feature_flags/{id}/dependent_flags/",
	"GET /api/environments/456/query/{id}/",
	"GET /api/environments/456/query/{id}/log/",
	"POST /flags?v=2 (project token in body, no personal key)",
	"GET /api/mock/rate_limit",
	"GET /api/mock/validation_error",
}

// Routes lists every route the mock serves, from the same table it routes by.
func Routes() []string {
	var out []string
	for _, rt := range exactRoutes(nil) {
		out = append(out, rt.method+" "+rt.path)
	}
	out = append(out, patternRoutes...)
	sort.Strings(out)
	return out
}

func projectDetail(id int) http.HandlerFunc {
	names := map[int]string{123: "Website", 124: "Website staging"}
	return func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"id": id, "name": names[id], "api_token": projectTokens[id]})
	}
}
