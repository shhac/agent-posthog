package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

const gateSpec = `{"key":"beta-gate","active":true,"early_exit":true,"conditions":[
	{"where":["email icontains @contractor.com"],"rollout":0},
	{"where":["app_env = staging"]},
	{"where":["email icontains @letsdothis.com"]}]}`

const gateCases = `
- name: contractor in staging is denied
  props: {email: pat@contractor.com, app_env: staging}
  expect: {enabled: false, condition_index: 0, reason: out_of_rollout_bound}
- name: anyone in staging
  props: {email: sam@example.com, app_env: staging}
  expect: {enabled: true, condition_index: 1}
- name: employee in production
  props: {email: ana@letsdothis.com, app_env: production}
  expect: {enabled: true, condition_index: 2}
- name: stranger in production
  props: {email: bob@example.com, app_env: production}
  expect: {enabled: false, reason: no_condition_match}
- name: no environment sent
  props: {email: bob@example.com}
  expect: {enabled: false}
`

func TestFlagTestStrictGateWithDenyBranch(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "apply", "--spec-json", gateSpec, "--yes")
	out, errOut, err := s.run("flags", "test", "beta-gate", "--cases", writeTemp(t, "cases.yaml", gateCases))
	if err != nil {
		t.Fatalf("suite failed: %v\n%s\n%s", err, out, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 6 || !strings.Contains(lines[5], `"@summary":{"engine":"public","pass":5,"fail":0,"error":0`) {
		t.Fatalf("out = %s", out)
	}
	if strings.Contains(out, "phc_mock") {
		t.Fatalf("project token leaked: %s", out)
	}
}

func TestFlagTestMultivariateConfigPayloads(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "apply", "--file", writeTemp(t, "spec.yaml", configSpec), "--yes")
	cases := `[
		{"name":"staging gets debug config","props":{"app_env":"staging"},"expect":{"variant":"debug","payload":{"logLevel":"debug"}}},
		{"name":"production gets default config","props":{"app_env":"production"},"expect":{"variant":"default","payload":{"logLevel":"warn"},"condition_index":2}},
		{"name":"contractors get nothing","props":{"email":"x@contractor.com","app_env":"staging"},"expect":{"enabled":false,"payload":null}}
	]`
	out, errOut, err := s.run("flags", "test", "checkout-config", "--cases-json", cases)
	if err != nil {
		t.Fatalf("suite failed: %v\n%s\n%s", err, out, errOut)
	}
	if !strings.Contains(out, `"pass":3`) {
		t.Fatalf("out = %s", out)
	}
}

func TestFlagTestReportsFailuresAndErrors(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "apply", "--spec-json", gateSpec, "--yes")
	cases := `[
		{"name":"wrong","props":{"app_env":"staging"},"expect":{"enabled":false}},
		{"name":"broken","distinct_id":"mock-error","expect":{"enabled":false}}
	]`
	out, errOut, err := s.run("flags", "test", "beta-gate", "--cases-json", cases)
	if err == nil || !strings.Contains(errOut, "2 of 2 cases did not pass") {
		t.Fatalf("err = %v stderr = %s", err, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var first, second map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &first)
	_ = json.Unmarshal([]byte(lines[1]), &second)
	if first["result"] != "fail" || !strings.Contains(mustJSON(first["mismatches"]), `"field":"enabled"`) {
		t.Fatalf("first = %v", first)
	}
	if second["result"] != "error" {
		t.Fatalf("an evaluation error must not read as false: %v", second)
	}
}

func TestFlagTestRejectsBadSuites(t *testing.T) {
	s := flagSession(t)
	for cases, want := range map[string]string{
		`[]`:             "non-empty list",
		`[{"name":"a"}]`: "nothing to expect",
		`[{"name":"a","expect":{"enabld":true}}]`:                                         "unknown expectation",
		`[{"name":"a","expect":{"enabled":true}},{"name":"a","expect":{"enabled":true}}]`: "unique name",
		`[{"name":"a","props":{"x":1},"expect":{"enabled":true}}]`:                        "diagnostic engine",
	} {
		args := []string{"flags", "test", "checkout-v2", "--cases-json", cases}
		if want == "diagnostic engine" {
			args = append(args, "--engine", "diagnostic")
		}
		if errOut := s.mustFail(args...); !strings.Contains(errOut, want) {
			t.Fatalf("%s: stderr = %s", cases, errOut)
		}
	}
}
