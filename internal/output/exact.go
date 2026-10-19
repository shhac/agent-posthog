package output

import (
	"bytes"
	"encoding/json"

	out "github.com/shhac/lib-agent-output"
)

// Ordered is a record whose field order is part of how it reads; Field is
// one of its fields. Both are lib-agent-output's, so every writer and the
// YAML encoder keep the order.
type (
	Ordered = out.Ordered
	Field   = out.Field
)

// exactTree prepares a value for the exact YAML writer: ordered documents,
// maps, and slices are walked, JSON scalars (json.Number included) pass, and
// any other type — a struct with json tags, a typed slice — goes through JSON
// with numbers kept exact, so YAML shows the same fields and digits JSON does.
func exactTree(v any) any {
	switch t := v.(type) {
	case nil, string, bool, json.Number, float64, int, int64:
		return v
	case Ordered:
		fields := make(Ordered, len(t))
		for i, f := range t {
			fields[i] = Field{Key: f.Key, Value: exactTree(f.Value)}
		}
		return fields
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, child := range t {
			m[k] = exactTree(child)
		}
		return m
	case []any:
		items := make([]any, len(t))
		for i, child := range t {
			items[i] = exactTree(child)
		}
		return items
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return v
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var decoded any
		if decoder.Decode(&decoded) != nil {
			return v
		}
		return decoded
	}
}
