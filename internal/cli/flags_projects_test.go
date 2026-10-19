package cli

import (
	"strings"
	"testing"

	"github.com/shhac/agent-posthog/internal/config"
)

// stagingSession is a mock session with a "work" profile whose aliases name
// project 124 staging and 123 prod.
func stagingSession(t *testing.T) *mockSession {
	t.Helper()
	s := flagSession(t)
	if err := config.StoreProfile("work", config.Profile{Host: "http://unused", OrganizationID: "org_1", ProjectID: 123}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_POSTHOG_PROFILE", "work")
	s.mustRun("config", "alias", "set", "staging", "124")
	s.mustRun("config", "alias", "set", "prod", "123")
	return s
}

func TestConfigAliasValidation(t *testing.T) {
	s := stagingSession(t)
	list := s.mustRun("config", "alias", "list")
	if !strings.Contains(mustJSON(list), `"staging":124`) {
		t.Fatalf("list = %v", list)
	}
	if errOut := s.mustFail("config", "alias", "set", "42", "1"); !strings.Contains(errOut, "non-numeric") {
		t.Fatalf("stderr = %s", errOut)
	}
	if errOut := s.mustFail("--project", "nope", "flags", "list"); !strings.Contains(errOut, `unknown project alias \"nope\" in profile work`) {
		t.Fatalf("stderr = %s", errOut)
	}
	s.mustRun("config", "alias", "unset", "prod")
	if errOut := s.mustFail("config", "alias", "unset", "prod"); !strings.Contains(errOut, "no project alias") {
		t.Fatalf("stderr = %s", errOut)
	}
}

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
