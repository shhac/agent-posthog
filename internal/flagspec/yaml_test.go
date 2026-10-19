package flagspec

import (
	"encoding/json"
	"testing"
)

func TestYAMLNumbersBecomeValidJSONNumbers(t *testing.T) {
	cases := map[string]string{
		".5":    "0.5",
		"+1.5":  "1.5",
		"1e1":   "1e1",
		"33.3":  "33.3",
		"0x10":  "16",
		"+7":    "7",
		"-0.25": "-0.25",
	}
	for in, want := range cases {
		doc, err := ParseDocument([]byte("n: " + in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		got := Object(doc)["n"]
		if got != json.Number(want) {
			t.Fatalf("%s → %v, want %s", in, got, want)
		}
		if _, err := json.Marshal(got); err != nil {
			t.Fatalf("%s produced invalid JSON number %v: %v", in, got, err)
		}
	}
	for _, in := range []string{".inf", ".nan"} {
		if _, err := ParseDocument([]byte("n: " + in)); err == nil {
			t.Fatalf("%s should be rejected", in)
		}
	}
	if doc, _ := ParseDocument([]byte("n: ~")); Object(doc)["n"] != nil {
		t.Fatal("~ should be null")
	}
}
