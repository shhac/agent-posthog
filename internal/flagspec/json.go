// Package flagspec converts between PostHog feature flag JSON and the
// agent-facing flag spec, parses the --where condition grammar, builds minimal
// PATCH bodies, and diffs flag definitions. It performs no HTTP.
package flagspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
)

// Decode parses JSON keeping numbers as json.Number so payloads and filters
// round-trip without float64 loss.
func Decode(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after the JSON value")
	}
	return v, nil
}

// DecodeObject parses a JSON object with lossless numbers.
func DecodeObject(raw []byte) (map[string]any, error) {
	v, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected a JSON object")
	}
	return m, nil
}

// Clone deep-copies a decoded JSON value.
func Clone(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			out[k] = Clone(item)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = Clone(item)
		}
		return out
	default:
		return v
	}
}

// Equal compares decoded JSON values. Numbers compare by value, so 5 and 5.0
// are equal.
func Equal(a, b any) bool {
	an, aok := a.(json.Number)
	bn, bok := b.(json.Number)
	if aok && bok {
		if an == bn {
			return true
		}
		af, aerr := an.Float64()
		bf, berr := bn.Float64()
		return aerr == nil && berr == nil && af == bf
	}
	switch at := a.(type) {
	case map[string]any:
		bt, ok := b.(map[string]any)
		if !ok || len(at) != len(bt) {
			return false
		}
		for k, av := range at {
			bv, ok := bt[k]
			if !ok || !Equal(av, bv) {
				return false
			}
		}
		return true
	case []any:
		bt, ok := b.([]any)
		if !ok || len(at) != len(bt) {
			return false
		}
		for i := range at {
			if !Equal(at[i], bt[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

// Object is v as a JSON object, or nil.
func Object(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// List is v as a JSON array, or nil.
func List(v any) []any {
	s, _ := v.([]any)
	return s
}

// Int reads a decoded JSON number (json.Number, float64, or int), or 0.
func Int(v any) int {
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

// Strings is values as a JSON array.
func Strings(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// AppendOnce appends the items not already in list.
func AppendOnce(list []string, items ...string) []string {
	for _, item := range items {
		if !slices.Contains(list, item) {
			list = append(list, item)
		}
	}
	return list
}

func number(n int) json.Number {
	return json.Number(fmt.Sprint(n))
}
