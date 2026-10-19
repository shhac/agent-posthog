package cli

import (
	"strings"
	"testing"
)

func TestFlagEvaluateEngines(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "apply", "--yes", "--spec-json", `{"key":"pro-gate","active":true,"conditions":[{"where":["plan = pro"]}],"payload":{"seats":5}}`)
	public := s.mustRun("flags", "evaluate", "pro-gate", "--distinct-id", "anon", "--props-json", `{"plan":"pro"}`)
	if public["enabled"] != true || public["engine"] != "public" || mustJSON(public["payload"]) != `{"seats":5}` {
		t.Fatalf("public = %v", public)
	}
	if !strings.Contains(mustJSON(public["condition"]), "plan = pro") {
		t.Fatalf("matched condition missing: %v", public)
	}
	diagnostic := s.mustRun("flags", "evaluate", "pro-gate", "--distinct-id", "user_123", "--engine", "diagnostic")
	if diagnostic["enabled"] != true || diagnostic["engine"] != "diagnostic" {
		t.Fatalf("diagnostic = %v", diagnostic)
	}
	if errOut := s.mustFail("flags", "evaluate", "pro-gate"); !strings.Contains(errOut, "--distinct-id is required") {
		t.Fatalf("stderr = %s", errOut)
	}
	if errOut := s.mustFail("flags", "evaluate", "pro-gate", "--distinct-id", "mock-error"); !strings.Contains(errOut, "not a false result") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestFlagEvaluateDisabledFlagWarns(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "create", "off-gate", "--yes")
	r := s.mustRun("flags", "evaluate", "off-gate", "--distinct-id", "anon")
	if r["enabled"] != false || r["reason"] != "flag_disabled" || !strings.Contains(mustJSON(r["warnings"]), "disabled") {
		t.Fatalf("r = %v", r)
	}
}

func TestEvaluateAsOfAPastTime(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "apply", "--yes", "--spec-json", `{"key":"pro-gate","active":true,"conditions":[{"where":["plan = pro"]}]}`)
	r := s.mustRun("flags", "evaluate", "pro-gate", "--distinct-id", "user_123", "--engine", "diagnostic", "--at", "2026-04-29T15:30:00")
	if r["at"] != "2026-04-29T15:30:00Z" || r["reason_description"] != "condition_match as of 2026-04-29T15:30:00Z" {
		t.Fatalf("evaluate = %v", r)
	}
	if errOut := s.mustFail("flags", "evaluate", "pro-gate", "--distinct-id", "user_123", "--at", "2026-04-29"); !strings.Contains(errOut, "--engine diagnostic") {
		t.Fatalf("stderr = %s", errOut)
	}
	if errOut := s.mustFail("flags", "evaluate", "pro-gate", "--distinct-id", "user_123", "--engine", "diagnostic", "--at", "last week"); !strings.Contains(errOut, "ISO 8601") {
		t.Fatalf("stderr = %s", errOut)
	}
	out, errOut, err := s.run("flags", "test", "pro-gate", "--engine", "diagnostic", "--cases-json",
		`[{"name":"before launch","distinct_id":"user_123","at":"2026-01-01","expect":{"enabled":true}}]`)
	if err != nil || !strings.Contains(out, `"at":"2026-01-01T00:00:00Z"`) || !strings.Contains(out, `"pass":1`) {
		t.Fatalf("err=%v\n%s\n%s", err, out, errOut)
	}
}

func TestEvaluationContextsDecideWhetherAFlagIsServed(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "create", "web-only", "--evaluation-context", "web", "--active", "--yes")
	if errOut := s.mustFail("flags", "evaluate", "web-only", "--distinct-id", "u1"); !strings.Contains(errOut, "evaluation runtime or contexts") {
		t.Fatalf("a flag filtered out by context must be an error, not false: %s", errOut)
	}
	if r := s.mustRun("flags", "evaluate", "web-only", "--distinct-id", "u1", "--evaluation-context", "web"); r["enabled"] != true {
		t.Fatalf("evaluate = %v", r)
	}
	if errOut := s.mustFail("flags", "evaluate", "web-only", "--distinct-id", "u1", "--device-id", "d1", "--engine", "diagnostic"); !strings.Contains(errOut, "--engine public") {
		t.Fatalf("stderr = %s", errOut)
	}
}
