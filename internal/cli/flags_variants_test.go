package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVariantsSetDropsRemovedPayloadsAndGuardsOverrides(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "apply", "--yes", "--file", writeTemp(t, "spec.yaml", configSpec))
	errOut := s.mustFail("flags", "variants", "set", "checkout-config", "default=100")
	if !strings.Contains(errOut, `overrides variant \"debug\"`) {
		t.Fatalf("stderr = %s", errOut)
	}
	if errOut := s.mustFail("flags", "variants", "set", "checkout-config", "a=60", "b=60"); !strings.Contains(errOut, "sum to 100") {
		t.Fatalf("stderr = %s", errOut)
	}
	r := s.mustRun("flags", "variants", "set", "checkout-config", "default=50", "debug=0", "extra=50", "--yes")
	warnings, _ := json.Marshal(r["warnings"])
	if r["outcome"] != "applied" || !strings.Contains(string(warnings), "order changed") {
		t.Fatalf("receipt = %v", r)
	}
}
