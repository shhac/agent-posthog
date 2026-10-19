package mockposthog

import (
	"fmt"
	"net/http"
	"strconv"
)

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
