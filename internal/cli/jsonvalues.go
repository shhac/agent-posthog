package cli

import (
	"encoding/json"
	"slices"
)

func asObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func jsonInt(v any) int {
	switch t := v.(type) {
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case float64:
		return int(t)
	case int:
		return t
	default:
		return 0
	}
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func appendOnce(list []string, items ...string) []string {
	for _, item := range items {
		if !slices.Contains(list, item) {
			list = append(list, item)
		}
	}
	return list
}

func stringsToAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
