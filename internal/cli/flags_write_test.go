package cli

import (
	"strings"
	"testing"
)

func TestFlagWritePlansWithoutYes(t *testing.T) {
	s := flagSession(t)
	r := s.mustRun("flags", "disable", "checkout-v2")
	if r["outcome"] != "planned" || !strings.Contains(r["hint"].(string), "--yes") {
		t.Fatalf("receipt = %v", r)
	}
	changes := r["changes"].([]any)
	if len(changes) != 1 || changes[0].(map[string]any)["path"] != "active" {
		t.Fatalf("changes = %v", changes)
	}
	if s.flag("checkout-v2")["active"] != true {
		t.Fatal("plan wrote the flag")
	}
}

func TestFlagWriteDryRunWinsOverYes(t *testing.T) {
	s := flagSession(t)
	r := s.mustRun("flags", "disable", "checkout-v2", "--yes", "--dry-run")
	if r["outcome"] != "planned" || s.flag("checkout-v2")["active"] != true {
		t.Fatalf("dry-run applied: %v", r)
	}
}

func TestFlagWriteAppliesAndReportsVersion(t *testing.T) {
	s := flagSession(t)
	r := s.mustRun("flags", "disable", "checkout-v2", "--yes")
	if r["outcome"] != "applied" || r["base_version"] != float64(1) || r["version"] != float64(2) {
		t.Fatalf("receipt = %v", r)
	}
	if s.flag("checkout-v2")["active"] != false {
		t.Fatal("flag still active")
	}
	again := s.mustRun("flags", "disable", "checkout-v2", "--yes")
	if again["outcome"] != "unchanged" || s.flag("checkout-v2")["version"] != float64(2) {
		t.Fatalf("no-op wrote: %v", again)
	}
}

func TestFlagWriteIfVersionRefusesStaleRead(t *testing.T) {
	s := flagSession(t)
	errOut := s.mustFail("flags", "disable", "checkout-v2", "--if-version", "7", "--yes")
	if !strings.Contains(errOut, "is at version 1, not 7") || !strings.Contains(errOut, `"fixable_by":"agent"`) {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestFlagWriteClassifiesApprovalAndPolicyErrors(t *testing.T) {
	s := flagSession(t)
	errOut := s.mustFail("flags", "enable", "approval-flag", "--yes")
	if !strings.Contains(errOut, "change request cr_1") || !strings.Contains(errOut, `"fixable_by":"human"`) {
		t.Fatalf("approval stderr = %s", errOut)
	}
	errOut = s.mustFail("flags", "enable", "policy-flag", "--yes")
	if !strings.Contains(errOut, "more than one approval policy") || !strings.Contains(errOut, "separately") {
		t.Fatalf("policy stderr = %s", errOut)
	}
}

func TestFlagArchiveDisablesAndUnarchiveStaysDisabled(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "archive", "checkout-v2", "--yes")
	flag := s.flag("checkout-v2")
	if flag["archived"] != true || flag["active"] != false {
		t.Fatalf("flag = %v", flag)
	}
	s.mustRun("flags", "unarchive", "checkout-v2", "--yes")
	flag = s.flag("checkout-v2")
	if flag["archived"] != false || flag["active"] != false {
		t.Fatalf("flag = %v", flag)
	}
}

func TestFlagDeleteAndRestore(t *testing.T) {
	s := flagSession(t)
	r := s.mustRun("flags", "delete", "checkout-v2", "--yes")
	if r["outcome"] != "applied" || len(r["warnings"].([]any)) == 0 {
		t.Fatalf("receipt = %v", r)
	}
	out, _, _ := s.run("flags", "get", "checkout-v2")
	if !strings.Contains(out, "No feature flag matched") {
		t.Fatalf("deleted flag still resolves: %s", out)
	}
	if r := s.mustRun("flags", "restore", "22", "--yes"); r["outcome"] != "applied" {
		t.Fatalf("restore = %v", r)
	}
	if s.flag("checkout-v2")["deleted"] != false {
		t.Fatal("flag not restored")
	}
	if errOut := s.mustFail("flags", "restore", "checkout-v2"); !strings.Contains(errOut, "numeric flag ID") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestFlagEarlyExit(t *testing.T) {
	s := flagSession(t)
	r := s.mustRun("flags", "early-exit", "checkout-v2", "on", "--yes")
	if r["outcome"] != "applied" || len(r["warnings"].([]any)) == 0 {
		t.Fatalf("receipt = %v", r)
	}
	filters := s.flag("checkout-v2")["filters"].(map[string]any)
	if filters["early_exit"] != true || filters["groups"] == nil {
		t.Fatalf("filters = %v", filters)
	}
}

func TestFlagWriteCommandsExposeYesAndDryRun(t *testing.T) {
	root := newRootCmd("test")
	flags, _, err := root.Find([]string{"flags"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"enable", "disable", "archive", "unarchive", "delete", "restore", "early-exit"} {
		cmd, _, err := flags.Find([]string{name})
		if err != nil || cmd.Name() != name {
			t.Fatalf("missing flags %s", name)
		}
		if cmd.Flags().Lookup("yes") == nil || cmd.Flags().Lookup("dry-run") == nil {
			t.Fatalf("flags %s lacks --yes/--dry-run", name)
		}
	}
}

func TestReceiptNamesHostProjectAndAlias(t *testing.T) {
	s := stagingSession(t)
	r := s.mustRun("--project", "staging", "flags", "create", "aliased", "--yes")
	if r["project_id"] != float64(124) || r["project_alias"] != "staging" || !strings.HasPrefix(r["host"].(string), "http://127.0.0.1") {
		t.Fatalf("receipt = %v", r)
	}
	plan := s.mustRun("--project", "staging", "flags", "enable", "aliased")
	if !strings.Contains(plan["hint"].(string), "--if-version") {
		t.Fatalf("plan hint should explain a re-run re-reads the flag: %v", plan)
	}
}

func TestLifecyclePlansWarnAboutDependencies(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "create", "base", "--active", "--yes")
	s.mustRun("flags", "apply", "--spec-json", `{"key":"needs-base","active":true,"conditions":[{"where":["flag:base = true"]}]}`, "--yes")
	for _, op := range []string{"disable", "archive", "delete"} {
		r := s.mustRun("flags", op, "base")
		if !strings.Contains(mustJSON(r["warnings"]), "Active flags depend on this one (needs-base)") {
			t.Fatalf("%s plan = %v", op, r)
		}
	}
	s.mustRun("flags", "disable", "needs-base", "--yes")
	s.mustRun("flags", "disable", "base", "--yes")
	r := s.mustRun("flags", "enable", "needs-base")
	if !strings.Contains(mustJSON(r["warnings"]), "depends on disabled flag base") {
		t.Fatalf("enable plan = %v", r)
	}
	if r := s.mustRun("flags", "disable", "needs-base"); r["warnings"] != nil && strings.Contains(mustJSON(r["warnings"]), "depend") {
		t.Fatalf("a flag nothing depends on should not warn: %v", r)
	}
}

func TestReceiptsKeepExactValuesInYAML(t *testing.T) {
	s := flagSession(t)
	out, errOut, err := s.run("flags", "disable", "checkout-v2", "--yes", "--format", "yaml")
	if err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	for _, want := range []string{"outcome: applied", "base_version: ", "version: ", "before: true", "after: false"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	s.mustRun("flags", "apply", "--spec-json", `{"key":"yaml-gate","payload":{"price":1.50,"code":"007"}}`, "--yes")
	payload, _, err := s.run("flags", "payload", "get", "yaml-gate", "--format", "yaml")
	if err != nil || !strings.Contains(payload, "price: 1.50") || !strings.Contains(payload, `code: "007"`) {
		t.Fatalf("payload get yaml must keep the number's text and quote number-like strings: %v\n%s", err, payload)
	}
}
