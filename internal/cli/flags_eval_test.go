package cli

import (
	"strings"
	"testing"

	"github.com/shhac/agent-posthog/internal/flagspec"
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

func TestInterpretFlagsNeverTurnsFailureIntoFalse(t *testing.T) {
	active := map[string]any{"key": "gate", "active": true}
	cases := []struct {
		name, resp string
		flag       map[string]any
		status     string
		enabled    bool
	}{
		{"disabled flag omitted", `{"flags":{}}`, map[string]any{"key": "gate", "active": false}, "ok", false},
		{"active flag omitted", `{"flags":{}}`, active, "error", false},
		{"omitted with errors", `{"flags":{},"errorsWhileComputingFlags":true}`, active, "error", false},
		{"failed", `{"flags":{"gate":{"enabled":false,"failed":true,"reason":{"description":"timeout"}}}}`, active, "error", false},
		{"evaluated false", `{"flags":{"gate":{"enabled":false,"reason":{"code":"no_condition_match"}}}}`, active, "ok", false},
		{"evaluated true", `{"flags":{"gate":{"enabled":true,"reason":{"code":"condition_match","condition_index":0},"metadata":{"payload":"{\"a\":1}"}}}}`, active, "ok", true},
	}
	for _, tc := range cases {
		resp, _ := flagspec.DecodeObject([]byte(tc.resp))
		r := interpretFlags(resp, tc.flag)
		if r.Status != tc.status || r.Enabled != tc.enabled {
			t.Fatalf("%s: %+v", tc.name, r)
		}
		if tc.enabled && mustJSON(r.Payload) != `{"a":1}` {
			t.Fatalf("%s: payload %v", tc.name, r.Payload)
		}
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
