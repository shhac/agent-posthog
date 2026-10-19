package cli

import (
	"strings"
	"testing"
)

func TestStagingProjectWorkflow(t *testing.T) {
	s := stagingSession(t)
	s.mustRun("--project", "staging", "flags", "apply", "--spec-json", gateSpec, "--yes")
	out, errOut, err := s.run("--project", "staging", "flags", "test", "beta-gate", "--cases", writeTemp(t, "cases.yaml", gateCases))
	if err != nil {
		t.Fatalf("staging suite: %v\n%s\n%s", err, out, errOut)
	}

	diff := s.mustRun("--project", "staging", "flags", "diff", "beta-gate", "--against", "prod")
	if diff["exists_in_against"] != false || diff["identical"] != false {
		t.Fatalf("diff = %v", diff)
	}

	plan := s.mustRun("--project", "staging", "flags", "copy", "beta-gate", "--to", "prod")
	if plan["outcome"] != "planned" || !strings.Contains(mustJSON(plan["targets"]), `"action":"create"`) {
		t.Fatalf("plan = %v", plan)
	}
	if out, _, _ := s.run("--project", "prod", "flags", "get", "beta-gate"); !strings.Contains(out, "No feature flag matched") {
		t.Fatalf("plan copied: %s", out)
	}
	if r := s.mustRun("--project", "staging", "flags", "copy", "beta-gate", "--to", "prod", "--yes"); r["outcome"] != "applied" {
		t.Fatalf("copy = %v", r)
	}
	if diff := s.mustRun("--project", "staging", "flags", "diff", "beta-gate", "--against", "prod"); diff["identical"] != true {
		t.Fatalf("after copy diff = %v", diff)
	}

	s.mustRun("--project", "staging", "flags", "conditions", "set", "beta-gate", "2", "--rollout", "0", "--if-version", "1", "--yes")
	plan = s.mustRun("--project", "staging", "flags", "copy", "beta-gate", "--to", "prod")
	if !strings.Contains(mustJSON(plan["targets"]), `"action":"overwrite"`) || !strings.Contains(plan["hint"].(string), "--overwrite") {
		t.Fatalf("plan = %v", plan)
	}
	if !strings.Contains(mustJSON(plan["targets"]), `"path":"conditions[2].rollout","source":0,"target":100`) {
		t.Fatalf("plan differences = %v", plan["targets"])
	}
	if errOut := s.mustFail("--project", "staging", "flags", "copy", "beta-gate", "--to", "prod", "--yes"); !strings.Contains(errOut, "--overwrite") {
		t.Fatalf("stderr = %s", errOut)
	}
	if r := s.mustRun("--project", "staging", "flags", "copy", "beta-gate", "--to", "prod", "--overwrite", "--yes"); r["outcome"] != "applied" {
		t.Fatalf("overwrite = %v", r)
	}
}

func TestCopyReportsApprovalAndRefusesSource(t *testing.T) {
	s := stagingSession(t)
	if errOut := s.mustFail("flags", "copy", "checkout-v2", "--to", "prod"); !strings.Contains(errOut, "source project") {
		t.Fatalf("stderr = %s", errOut)
	}
	out, errOut, err := s.run("flags", "copy", "checkout-v2", "--to", "125", "--yes", "--format", "json")
	if err == nil || !strings.Contains(errOut, `"fixable_by":"human"`) || !strings.Contains(out, `"outcome": "failed"`) || !strings.Contains(out, "cr_copy") {
		t.Fatalf("err=%v\nstdout=%s\nstderr=%s", err, out, errOut)
	}
}

func TestDiffNamesFlagDependenciesByKey(t *testing.T) {
	s := stagingSession(t)
	gate := `{"key":"dependent-gate","conditions":[{"where":["flag:base-gate = true","cohort in 5"]},{"where":[{"key":"base-gate","type":"flag","operator":"flag_evaluates_to","value":true}]}]}`
	for _, project := range []string{"staging", "prod"} {
		if project == "prod" {
			s.mustRun("--project", project, "flags", "create", "padding-flag", "--yes")
		}
		s.mustRun("--project", project, "flags", "create", "base-gate", "--yes")
		s.mustRun("--project", project, "flags", "apply", "--spec-json", gate, "--yes")
	}
	if dependencyID(t, s, "staging") == dependencyID(t, s, "prod") {
		t.Fatal("fixture should give the dependency different IDs in each project")
	}
	diff := s.mustRun("--project", "staging", "flags", "diff", "dependent-gate", "--against", "prod")
	if diff["identical"] != true {
		t.Fatalf("same dependency by key should compare equal across projects: %v", diff)
	}
	warnings := mustJSON(diff["warnings"])
	if strings.Count(warnings, "cohort IDs differ") != 1 {
		t.Fatalf("warnings = %s", warnings)
	}
}

func dependencyID(t *testing.T, s *cliSession, project string) any {
	t.Helper()
	flag := s.mustRun("--project", project, "flags", "get", "dependent-gate")["data"].([]any)[0].(map[string]any)
	group := flag["filters"].(map[string]any)["groups"].([]any)[0].(map[string]any)
	return group["properties"].([]any)[0].(map[string]any)["key"]
}

func TestCopyReportsPartialSuccess(t *testing.T) {
	s := stagingSession(t)
	out, errOut, err := s.run("flags", "copy", "checkout-v2", "--to", "staging", "--to", "125", "--yes", "--format", "json")
	if err == nil || !strings.Contains(out, `"outcome": "partial"`) || !strings.Contains(errOut, "1 target(s) were not copied") {
		t.Fatalf("err=%v\nstdout=%s\nstderr=%s", err, out, errOut)
	}
	if s.mustRun("--project", "staging", "flags", "get", "checkout-v2")["data"] == nil {
		t.Fatal("the successful target should have the copy")
	}
}

func TestCopyCanIncludeSchedules(t *testing.T) {
	s := stagingSession(t)
	plan := s.mustRun("flags", "copy", "checkout-v2", "--to", "staging", "--copy-schedule")
	if !strings.Contains(mustJSON(plan["warnings"]), "Scheduled changes are copied") {
		t.Fatalf("plan = %v", plan)
	}
	r := s.mustRun("flags", "copy", "checkout-v2", "--to", "staging", "--copy-schedule", "--yes")
	if r["outcome"] != "applied" || !strings.Contains(mustJSON(r["succeeded"]), "schedule_copy_warning") {
		t.Fatalf("copy = %v", r)
	}
	plain := s.mustRun("flags", "copy", "checkout-v2", "--to", "staging", "--overwrite", "--yes")
	if strings.Contains(mustJSON(plain["succeeded"]), "schedule_copy_warning") {
		t.Fatalf("schedules copied without --copy-schedule: %v", plain)
	}
}

func TestCopyPlanNamesAliasesAndWarnsAboutSideEffects(t *testing.T) {
	s := stagingSession(t)
	plan := s.mustRun("flags", "copy", "checkout-v2", "--to", "staging", "--disable", "--copy-dependencies")
	targets, warnings := mustJSON(plan["targets"]), mustJSON(plan["warnings"])
	if !strings.Contains(targets, `"alias":"staging"`) || !strings.Contains(targets, `"project":124`) {
		t.Fatalf("targets = %s", targets)
	}
	for _, want := range []string{"cohorts are matched by name", "advisory", "--disable applies to this flag only"} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("warnings missing %q: %s", want, warnings)
		}
	}
	if plan["dependencies"] == nil {
		t.Fatalf("--copy-dependencies should preflight dependency requirements: %v", plan)
	}
}
