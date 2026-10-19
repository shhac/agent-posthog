package output

import (
	"fmt"
	"strings"
	"testing"
)

func TestCleanRedactsSecretsAndDropsNulls(t *testing.T) {
	cleaned, _ := Clean(map[string]any{
		"api_key": "secret-value",
		"note":    "phc_should_be_redacted",
		"token":   nil,
		"safe":    "keep",
	}).(map[string]any)
	if cleaned["api_key"] != "[REDACTED]" || cleaned["note"] != "[REDACTED]" || cleaned["safe"] != "keep" {
		t.Fatalf("cleaned = %v", cleaned)
	}
	if _, ok := cleaned["token"]; ok {
		t.Fatalf("a null field should be dropped before masking: %v", cleaned)
	}
	if notes := cleaned["@redacted"]; strings.Contains(fmt.Sprint(notes), "token") {
		t.Fatalf("a dropped null must not leave a redaction note: %v", notes)
	}
}

func TestCompactKeepsTheUsualFields(t *testing.T) {
	record := map[string]any{"id": 1.0, "name": "n", "timezone": "UTC"}
	compact, _ := cleaner(Compact)(record).(map[string]any)
	if compact["name"] != "n" || compact["timezone"] != nil {
		t.Fatalf("compact = %v", compact)
	}
	full, _ := cleaner(Full)(record).(map[string]any)
	if full["timezone"] != "UTC" {
		t.Fatalf("full = %v", full)
	}
}
