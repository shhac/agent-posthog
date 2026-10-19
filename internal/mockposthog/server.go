package mockposthog

import (
	"net/http"
	"strings"
)

// NewServer returns a mock with its own flag store, so each test server starts
// from the same fixtures.
func NewServer() http.Handler {
	store := newFlagStore()
	exact := map[string]http.HandlerFunc{}
	for _, rt := range exactRoutes(store) {
		exact[rt.method+" "+rt.path] = rt.serve
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, store, exact)
	})
	return mux
}

func handle(w http.ResponseWriter, r *http.Request, store *flagStore, exact map[string]http.HandlerFunc) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodPost && r.URL.Path == "/flags" {
		store.handlePublicFlags(w, r)
		return
	}
	if handlePreflight(w, r) || store.handle(w, r) {
		return
	}
	if serve, ok := exact[r.Method+" "+r.URL.Path]; ok {
		serve(w, r)
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/environments/456/query/") {
		writeQueryStatus(w, r)
		return
	}
	notFound(w, "No mock route for "+r.Method+" "+r.URL.Path)
}

func handlePreflight(w http.ResponseWriter, r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if auth == "" || strings.Contains(auth, "phx_invalid") {
		writeStatus(w, http.StatusUnauthorized, map[string]any{
			"type":   "authentication_error",
			"code":   "invalid_personal_api_key",
			"detail": "Invalid Personal API key.",
		})
		return true
	}

	path := r.URL.Path
	if strings.Contains(auth, "phx_no_scope") && path != "/api/users/@me/" {
		writeStatus(w, http.StatusForbidden, map[string]any{
			"type":   "permission_denied",
			"code":   "permission_denied",
			"detail": "API key is missing the required scope for this endpoint.",
		})
		return true
	}
	if strings.Contains(path, "rate_limit") {
		w.Header().Set("Retry-After", "0")
		writeStatus(w, http.StatusTooManyRequests, map[string]any{
			"type":   "throttled_error",
			"code":   "throttled",
			"detail": "Rate limit exceeded.",
		})
		return true
	}
	if path == "/api/mock/validation_error" {
		writeStatus(w, http.StatusBadRequest, map[string]any{
			"type":   "validation_error",
			"code":   "invalid_input",
			"attr":   "query",
			"detail": "This field is required.",
		})
		return true
	}
	return false
}
