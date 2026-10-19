package cli

import (
	"strings"
	"testing"
)

func TestFlagReachPerCondition(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "apply", "--spec-json", gateSpec, "--yes")
	out, _, err := s.run("flags", "reach", "beta-gate")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 || !strings.Contains(lines[0], `"index":0,"matching":25,"total":100,"rollout":0`) || !strings.Contains(lines[3], "@note") {
		t.Fatalf("out = %s", out)
	}
}
