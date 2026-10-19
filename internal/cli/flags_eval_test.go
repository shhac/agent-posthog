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

func TestFlagsHostMapping(t *testing.T) {
	t.Setenv("AGENT_POSTHOG_FLAGS_HOST", "")
	for in, want := range map[string]string{
		"https://us.posthog.com":     "https://us.i.posthog.com",
		"https://eu.posthog.com":     "https://eu.i.posthog.com",
		"https://posthog.example.io": "https://posthog.example.io",
	} {
		if got := flagsHost(in, ""); got != want {
			t.Fatalf("flagsHost(%s) = %s", in, got)
		}
	}
	if got := flagsHost("https://us.posthog.com", "https://proxy.example"); got != "https://proxy.example" {
		t.Fatalf("override ignored: %s", got)
	}
}
