package flagspec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseSpecRejects(t *testing.T) {
	cases := map[string]string{
		`{"key":"x","bogus":1}`:                             "unknown field(s) bogus",
		`{"name":"x"}`:                                      "key is required",
		`{"key":"x","schema_version":2}`:                    "schema_version",
		`{"key":"x","type":"gate"}`:                         "type must be",
		`{"key":"x","active":"yes"}`:                        "active must be",
		`{"key":"x","payload":1,"payloads":{}}`:             "not both",
		`{"key":"x","variants":[{"key":"a","rollout":60}]}`: "sum to 100",
		`{"key":"x","variants":[{"key":"a","rollout":50},{"key":"a","rollout":50}]}`: "twice",
		`{"key":"x","conditions":[{"where":["email frobs x"]}]}`:                     "unknown operator",
		`{"key":"x","conditions":[{"rollout":150}]}`:                                 "rollout must be",
		"key: x\nkey: y\n":               "duplicate key",
		"key: x\npayloads:\n  true: 1\n": "keys must be strings",
		"key: x\n---\nkey: y\n":          "single YAML document",
	}
	for input, want := range cases {
		t.Run(want, func(t *testing.T) {
			_, err := ParseSpec([]byte(input))
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v, want containing %q", err, want)
			}
		})
	}
}

func TestParseSpecYAMLKeepsTypes(t *testing.T) {
	spec, err := ParseSpec([]byte("key: x\ntype: boolean\npayload:\n  zip: \"00123\"\n  big: 12345678901234567890\n  on: yes\n  when: 2026-01-01\n"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(spec.Get("payload"))
	want := `{"big":12345678901234567890,"on":"yes","when":"2026-01-01","zip":"00123"}`
	if string(data) != want {
		t.Fatalf("payload = %s, want %s", data, want)
	}
}
