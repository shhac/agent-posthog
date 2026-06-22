package output

import "testing"

// ParseFormat now comes from lib-agent-output and is intentionally more lenient
// than the pre-migration parser: it accepts "ndjson"/"yml" aliases and is
// case-insensitive. Pin that as intended contract.
func TestParseFormatAliases(t *testing.T) {
	cases := map[string]Format{
		"json":   FormatJSON,
		"JSON":   FormatJSON,
		"yaml":   FormatYAML,
		"yml":    FormatYAML,
		"YAML":   FormatYAML,
		"jsonl":  FormatNDJSON,
		"ndjson": FormatNDJSON,
	}
	for in, want := range cases {
		got, err := ParseFormat(in)
		if err != nil {
			t.Errorf("ParseFormat(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseFormat(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ParseFormat("toml"); err == nil {
		t.Error("ParseFormat(toml) should error")
	}
}

func TestRedactionStillApplies(t *testing.T) {
	cleaned, ok := toCleanAny(map[string]any{
		"api_key": "secret-value",
		"note":    "phc_should_be_redacted",
		"safe":    "keep",
	}, true)
	if !ok {
		t.Fatal("toCleanAny failed")
	}
	m := cleaned.(map[string]any)
	if m["api_key"] != "[REDACTED]" {
		t.Errorf("sensitive key not redacted: %v", m["api_key"])
	}
	if m["note"] != "[REDACTED]" {
		t.Errorf("posthog secret prefix not redacted: %v", m["note"])
	}
	if m["safe"] != "keep" {
		t.Errorf("safe value altered: %v", m["safe"])
	}
}
