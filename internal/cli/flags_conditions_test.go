package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// gateSession creates beta-gate with two conditions at version 1:
// 0: email icontains @contractor.com at 0%, 1: everyone at 100%.
func gateSession(t *testing.T) *mockSession {
	t.Helper()
	s := flagSession(t)
	s.mustRun("flags", "apply", "--yes", "--spec-json", `{"key":"beta-gate","early_exit":true,"conditions":[
		{"where":["email icontains @contractor.com"],"rollout":0},
		{"rollout":100}]}`)
	return s
}

func groupsOf(t *testing.T, s *mockSession, ref string) []any {
	t.Helper()
	return s.flag(ref)["filters"].(map[string]any)["groups"].([]any)
}

func TestConditionsListShowsVersionPerCondition(t *testing.T) {
	s := gateSession(t)
	out, _, err := s.run("flags", "conditions", "list", "beta-gate")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"index":0,"version":1`) || !strings.Contains(lines[0], `"email icontains @contractor.com"`) {
		t.Fatalf("list = %s", out)
	}
}

func TestConditionsAddAppendsAndInsertsWithVersion(t *testing.T) {
	s := gateSession(t)
	s.mustRun("flags", "conditions", "add", "beta-gate", "--where", "app_env = staging", "--yes")
	groups := groupsOf(t, s, "beta-gate")
	if len(groups) != 3 || !strings.Contains(mustJSON(groups[2]), "staging") {
		t.Fatalf("groups = %v", groups)
	}
	if errOut := s.mustFail("flags", "conditions", "add", "beta-gate", "--at", "0"); !strings.Contains(errOut, "--if-version") {
		t.Fatalf("stderr = %s", errOut)
	}
	r := s.mustRun("flags", "conditions", "add", "beta-gate", "--where", "email = qa@letsdothis.com", "--at", "0", "--if-version", "2", "--yes")
	if r["outcome"] != "applied" || !strings.Contains(mustJSON(groupsOf(t, s, "beta-gate")[0]), "qa@letsdothis.com") {
		t.Fatalf("receipt = %v", r)
	}
}

func TestConditionsSetRolloutUsesStrictEndpoint(t *testing.T) {
	s := gateSession(t)
	r := s.mustRun("flags", "conditions", "set", "beta-gate", "1", "--rollout", "0", "--if-version", "1", "--yes")
	if r["outcome"] != "applied" || r["version"] != float64(2) {
		t.Fatalf("receipt = %v", r)
	}
	if groupsOf(t, s, "beta-gate")[1].(map[string]any)["rollout_percentage"] != float64(0) {
		t.Fatal("rollout not set")
	}
	errOut := s.mustFail("flags", "conditions", "set", "beta-gate", "1", "--rollout", "50", "--if-version", "1", "--yes")
	if !strings.Contains(errOut, "not 1") {
		t.Fatalf("stale version stderr = %s", errOut)
	}
}

func TestConditionsSetEditsOnlyNamedParts(t *testing.T) {
	s := gateSession(t)
	s.mustRun("flags", "conditions", "set", "beta-gate", "0", "--where", "email icontains @agency.com", "--if-version", "1", "--yes")
	group := groupsOf(t, s, "beta-gate")[0].(map[string]any)
	if !strings.Contains(mustJSON(group), "@agency.com") || group["rollout_percentage"] != float64(0) {
		t.Fatalf("group = %v", group)
	}
	s.mustRun("flags", "conditions", "set", "beta-gate", "0", "--clear-where", "--if-version", "2", "--yes")
	if props := groupsOf(t, s, "beta-gate")[0].(map[string]any)["properties"].([]any); len(props) != 0 {
		t.Fatalf("properties = %v", props)
	}
	if errOut := s.mustFail("flags", "conditions", "set", "beta-gate", "0", "--if-version", "3"); !strings.Contains(errOut, "nothing to change") {
		t.Fatalf("stderr = %s", errOut)
	}
	if errOut := s.mustFail("flags", "conditions", "set", "beta-gate", "0", "--variant", "x", "--if-version", "3"); !strings.Contains(errOut, "only multivariate") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestConditionsMoveAndRemove(t *testing.T) {
	s := gateSession(t)
	r := s.mustRun("flags", "conditions", "move", "beta-gate", "0", "1", "--if-version", "1", "--yes")
	warnings, _ := json.Marshal(r["warnings"])
	if !strings.Contains(string(warnings), "Condition 0 matches everyone") {
		t.Fatalf("move should warn the catch-all now hides condition 1: %v", r)
	}
	if !strings.Contains(mustJSON(groupsOf(t, s, "beta-gate")[1]), "contractor") {
		t.Fatalf("groups = %v", groupsOf(t, s, "beta-gate"))
	}
	s.mustRun("flags", "conditions", "remove", "beta-gate", "0", "--if-version", "2", "--yes")
	if groups := groupsOf(t, s, "beta-gate"); len(groups) != 1 || !strings.Contains(mustJSON(groups[0]), "contractor") {
		t.Fatalf("groups = %v", groups)
	}
}

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

func TestPayloadSetGetUnset(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "apply", "--yes", "--file", writeTemp(t, "spec.yaml", configSpec))
	if errOut := s.mustFail("flags", "payload", "set", "checkout-config", "--json", `{"a":1}`); !strings.Contains(errOut, "--variant") {
		t.Fatalf("stderr = %s", errOut)
	}
	s.mustRun("flags", "payload", "set", "checkout-config", "--variant", "debug", "--json", `{"logLevel":"trace","max":12345678901234567890}`, "--yes")
	got := s.mustRun("flags", "payload", "get", "checkout-config", "--variant", "debug")
	if got["set"] != true || !strings.Contains(mustJSON(got["payload"]), "trace") {
		t.Fatalf("payload = %v", got)
	}
	out, _, _ := s.run("flags", "payload", "get", "checkout-config", "--variant", "debug", "--format", "json")
	if !strings.Contains(out, "12345678901234567890") {
		t.Fatalf("payload lost precision: %s", out)
	}
	s.mustRun("flags", "payload", "unset", "checkout-config", "--variant", "debug", "--yes")
	if got := s.mustRun("flags", "payload", "get", "checkout-config", "--variant", "debug"); got["set"] != false {
		t.Fatalf("payload = %v", got)
	}

	s.mustRun("flags", "create", "gate", "--yes")
	s.mustRun("flags", "payload", "set", "gate", "--json", `"hello"`, "--yes")
	if got := s.mustRun("flags", "payload", "get", "gate"); got["payload"] != "hello" || got["payload_key"] != "true" {
		t.Fatalf("payload = %v", got)
	}
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestFlagOutputKeepsURLsUnescaped(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "create", "link-gate", "--payload-json", `{"url":"https://x.example/?a=1&b=<2>"}`, "--yes")
	for _, args := range [][]string{
		{"flags", "payload", "get", "link-gate", "--format", "json"},
		{"flags", "export", "link-gate"},
		{"flags", "payload", "set", "link-gate", "--json", `{"url":"https://y.example/?a&b"}`},
	} {
		out, errOut, err := s.run(args...)
		if err != nil {
			t.Fatalf("%v: %v %s", args, err, errOut)
		}
		if strings.Contains(out, `\u0026`) || strings.Contains(out, `\u003c`) {
			t.Fatalf("%v escaped HTML: %q", args, out)
		}
	}
}
