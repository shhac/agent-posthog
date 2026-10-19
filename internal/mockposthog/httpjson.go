package mockposthog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

func decodeBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		validationError(w, "body", "Invalid JSON body.")
		return nil, false
	}
	return body, true
}

func validationError(w http.ResponseWriter, attr, detail string) {
	writeStatus(w, http.StatusBadRequest, map[string]any{"type": "validation_error", "code": "invalid_input", "attr": attr, "detail": detail})
}

// normalize round-trips through JSON so fixtures built from Go literals compare
// and mutate exactly like decoded request bodies.
func normalize(v map[string]any) map[string]any {
	data, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

func cloneMap(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	return normalize(v)
}

func jsonEqual(a, b any) bool {
	ad, _ := json.Marshal(a)
	bd, _ := json.Marshal(b)
	return bytes.Equal(ad, bd)
}

func asObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		n, _ := strconv.Atoi(fmt.Sprint(v))
		return n
	}
}

func writeStatus(w http.ResponseWriter, status int, payload any) {
	w.WriteHeader(status)
	write(w, payload)
}

func write(w http.ResponseWriter, payload any) {
	_ = json.NewEncoder(w).Encode(payload)
}
