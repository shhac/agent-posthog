package flagspec

import "testing"

func TestConfigVersionOnlyAcceptsNumericOneAndTwo(t *testing.T) {
	for raw, want := range map[string]int{`{}`: 1, `{"version":null}`: 1, `{"version":1}`: 1, `{"version":2}`: 2} {
		filters, _ := DecodeObject([]byte(raw))
		if got, err := ConfigVersion(filters); err != nil || got != want {
			t.Fatalf("%s: %d, %v", raw, got, err)
		}
	}
	for _, raw := range []string{`{"version":"1"}`, `{"version":true}`, `{"version":3}`} {
		filters, _ := DecodeObject([]byte(raw))
		if _, err := ConfigVersion(filters); err == nil {
			t.Fatalf("%s should be unsupported", raw)
		}
	}
}
