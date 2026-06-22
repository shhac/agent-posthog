package cli

import (
	"encoding/json"
	"testing"
)

func TestResolveByField(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"name":"signup","id":1}`),
		json.RawMessage(`{"name":"login","id":2}`),
		json.RawMessage(`{"name":"login","id":3}`),
		json.RawMessage(`not valid json`),
	}

	t.Run("single match returns the record", func(t *testing.T) {
		got, err := resolveByField(items, "name", "signup", "event")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("got %#v, want map", got)
		}
		if m["name"] != "signup" {
			t.Fatalf("name = %#v, want signup", m["name"])
		}
	})

	t.Run("zero matches returns an error", func(t *testing.T) {
		got, err := resolveByField(items, "name", "logout", "event")
		if err == nil {
			t.Fatalf("expected error for zero matches, got %#v", got)
		}
	})

	t.Run("multiple matches returns an error", func(t *testing.T) {
		got, err := resolveByField(items, "name", "login", "event")
		if err == nil {
			t.Fatalf("expected error for multiple matches, got %#v", got)
		}
	})
}
