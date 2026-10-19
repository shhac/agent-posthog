package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// gateSession creates beta-gate with two conditions at version 1:
// 0: email icontains @contractor.com at 0%, 1: everyone at 100%.
func gateSession(t *testing.T) *cliSession {
	t.Helper()
	s := flagSession(t)
	s.mustRun("flags", "apply", "--yes", "--spec-json", `{"key":"beta-gate","early_exit":true,"conditions":[
		{"where":["email icontains @contractor.com"],"rollout":0},
		{"rollout":100}]}`)
	return s
}

func groupsOf(t *testing.T, s *cliSession, ref string) []any {
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

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestMoveGroupLandsAtFinalIndex(t *testing.T) {
	cases := []struct {
		from, to int
		want     string
	}{
		{0, 3, "bcda"}, {3, 0, "dabc"}, {1, 2, "acbd"}, {2, 1, "acbd"}, {2, 2, "abcd"},
	}
	for _, tc := range cases {
		groups := []any{"a", "b", "c", "d"}
		got, err := moveGroup(groups, tc.from, tc.to)
		if err != nil {
			t.Fatal(err)
		}
		joined := ""
		for _, g := range got {
			joined += g.(string)
		}
		if joined != tc.want {
			t.Fatalf("move %d→%d = %s, want %s", tc.from, tc.to, joined, tc.want)
		}
	}
	if _, err := moveGroup([]any{"a"}, 0, 1); err == nil {
		t.Fatal("expected out-of-range error")
	}
}

func TestConditionsSetRolloutPlansAndChecksIndex(t *testing.T) {
	s := gateSession(t)
	plan := s.mustRun("flags", "conditions", "set", "beta-gate", "1", "--rollout", "0", "--if-version", "1")
	change := plan["changes"].([]any)[0].(map[string]any)
	if plan["outcome"] != "planned" || change["path"] != "filters.groups[1].rollout_percentage" || change["before"] != float64(100) || change["after"] != float64(0) {
		t.Fatalf("plan = %v", plan)
	}
	if flag := s.flag("beta-gate"); flag["version"] != float64(1) {
		t.Fatalf("plan wrote: %v", flag)
	}
	if r := s.mustRun("flags", "conditions", "set", "beta-gate", "1", "--rollout", "100", "--if-version", "1", "--yes"); r["outcome"] != "unchanged" {
		t.Fatalf("same rollout = %v", r)
	}
	if errOut := s.mustFail("flags", "conditions", "set", "beta-gate", "5", "--rollout", "10", "--if-version", "1"); !strings.Contains(errOut, "no condition 5 (it has 2)") {
		t.Fatalf("stderr = %s", errOut)
	}
}
