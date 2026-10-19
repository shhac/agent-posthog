package mockposthog

import (
	"encoding/json"
	"net/http"
	"strings"
)

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
		notFound(w, "Query "+id+" not found.")
	}
}
