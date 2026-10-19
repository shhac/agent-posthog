package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavedPlanAppliesExactlyWhatWasReviewed(t *testing.T) {
	s := gateSession(t)
	plan := filepath.Join(t.TempDir(), "plan.json")
	saved := s.mustRun("flags", "conditions", "add", "beta-gate", "--where", "app_env = staging", "--save-plan", plan)
	if saved["outcome"] != "planned" || !strings.Contains(saved["hint"].(string), "apply-plan") {
		t.Fatalf("save = %v", saved)
	}
	if len(groupsOf(t, s, "beta-gate")) != 2 {
		t.Fatal("saving a plan wrote the flag")
	}
	if r := s.mustRun("flags", "apply-plan", "--file", plan); r["outcome"] != "planned" {
		t.Fatalf("apply-plan without --yes = %v", r)
	}
	r := s.mustRun("flags", "apply-plan", "--file", plan, "--yes")
	if r["outcome"] != "applied" || mustJSON(r["changes"]) != mustJSON(saved["changes"]) {
		t.Fatalf("applied %v, reviewed %v", r["changes"], saved["changes"])
	}
	if groups := groupsOf(t, s, "beta-gate"); len(groups) != 3 || !strings.Contains(mustJSON(groups[2]), "staging") {
		t.Fatalf("groups = %v", groups)
	}
	if errOut := s.mustFail("flags", "apply-plan", "--file", plan, "--yes"); !strings.Contains(errOut, "changed since the plan") {
		t.Fatalf("re-applying an applied plan must be refused: %s", errOut)
	}
}

func TestSavedPlanRefusesStaleOrForeignTargets(t *testing.T) {
	s := stagingSession(t)
	s.mustRun("flags", "apply", "--spec-json", gateSpec, "--yes")
	plan := filepath.Join(t.TempDir(), "plan.json")
	s.mustRun("flags", "disable", "beta-gate", "--save-plan", plan)
	if errOut := s.mustFail("--project", "staging", "flags", "apply-plan", "--file", plan, "--yes"); !strings.Contains(errOut, "the plan is for project 123") {
		t.Fatalf("foreign project: %s", errOut)
	}
	s.mustRun("flags", "early-exit", "beta-gate", "off", "--yes")
	if errOut := s.mustFail("flags", "apply-plan", "--file", plan, "--yes"); !strings.Contains(errOut, "changed since the plan") {
		t.Fatalf("stale plan: %s", errOut)
	}
	if s.flag("beta-gate")["active"] != true {
		t.Fatal("a refused plan wrote the flag")
	}
}

func TestSavePlanRulesAndSecrets(t *testing.T) {
	s := flagSession(t)
	plan := filepath.Join(t.TempDir(), "plan.json")
	if errOut := s.mustFail("flags", "disable", "checkout-v2", "--yes", "--save-plan", plan); !strings.Contains(errOut, "not both") {
		t.Fatalf("stderr = %s", errOut)
	}
	if errOut := s.mustFail("flags", "create", "new-flag", "--save-plan", plan); !strings.Contains(errOut, "not create") {
		t.Fatalf("stderr = %s", errOut)
	}
	s.mustRun("flags", "disable", "secret-config", "--save-plan", plan)
	data, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "s3cret") || !strings.Contains(string(data), `"format": "agent-posthog-plan/1"`) {
		t.Fatalf("plan file = %s", data)
	}
	if info, _ := os.Stat(plan); info.Mode().Perm() != 0o600 {
		t.Fatalf("plan file mode = %v", info.Mode().Perm())
	}
}
