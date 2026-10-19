package cli

import (
	"strings"
	"testing"
)

func TestFlagsGetResolvesKey(t *testing.T) {
	out := runOK(t, "--project", "123", "flags", "get", "checkout-v2")
	if !strings.Contains(out, `"key":"checkout-v2"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestFlagLookupMissHasHint(t *testing.T) {
	// Item-level misses are now @unresolved records on stdout (not fatal errors on
	// stderr) — matching the family get contract. stderr stays empty; exit is 0.
	out := runOK(t, "flags", "get", "missing-flag")
	if !strings.Contains(out, "No feature flag matched key missing-flag") || !strings.Contains(out, `"fixable_by":"agent"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestFlagLookupAmbiguousHasHint(t *testing.T) {
	// Item-level misses are now @unresolved records on stdout (not fatal errors on
	// stderr) — matching the family get contract. stderr stays empty; exit is 0.
	out := runOK(t, "flags", "get", "ambiguous-flag")
	if !strings.Contains(out, "Multiple feature flags matched key ambiguous-flag") || !strings.Contains(out, "Use the numeric feature flag ID") {
		t.Fatalf("stdout = %s", out)
	}
}
