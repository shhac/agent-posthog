package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFlagCreatePlansThenCreatesInactive(t *testing.T) {
	s := flagSession(t)
	args := []string{"flags", "create", "beta-gate", "--where", "email icontains @example.com", "--tag", "beta"}
	plan := s.mustRun(args...)
	if plan["outcome"] != "planned" || plan["operation"] != "create" {
		t.Fatalf("plan = %v", plan)
	}
	if out, _, _ := s.run("flags", "get", "beta-gate"); !strings.Contains(out, "No feature flag matched") {
		t.Fatalf("plan created the flag: %s", out)
	}
	r := s.mustRun(append(args, "--yes")...)
	if r["outcome"] != "applied" || r["id"] == nil {
		t.Fatalf("receipt = %v", r)
	}
	flag := s.flag("beta-gate")
	group := flag["filters"].(map[string]any)["groups"].([]any)[0].(map[string]any)
	prop := group["properties"].([]any)[0].(map[string]any)
	if flag["active"] != false || prop["operator"] != "icontains" || group["rollout_percentage"] != float64(100) {
		t.Fatalf("flag = %v", flag)
	}
	if errOut := s.mustFail(append(args, "--yes")...); !strings.Contains(errOut, "already exists") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestFlagCreateMultivariateNeedsVariants(t *testing.T) {
	s := flagSession(t)
	if errOut := s.mustFail("flags", "create", "cfg", "--type", "multivariate"); !strings.Contains(errOut, "need variants") {
		t.Fatalf("stderr = %s", errOut)
	}
	r := s.mustRun("flags", "create", "cfg", "--type", "multivariate", "--variant", "a=50", "--variant", "b=50", "--yes")
	if r["outcome"] != "applied" {
		t.Fatalf("receipt = %v", r)
	}
}

const configSpec = `
key: checkout-config
type: multivariate
active: true
early_exit: true
conditions:
  - where: ["email icontains @contractor.com"]
    rollout: 0
  - where: ["app_env = staging"]
    variant: debug
  - variant: default
variants:
  - {key: debug, rollout: 0}
  - {key: default, rollout: 100}
payloads:
  debug: {logLevel: debug}
  default: {logLevel: warn}
`

func TestFlagApplyCreatesThenUpdatesOnlyChanges(t *testing.T) {
	s := flagSession(t)
	path := writeTemp(t, "spec.yaml", configSpec)
	if r := s.mustRun("flags", "apply", "--file", path, "--yes"); r["operation"] != "create" || r["outcome"] != "applied" {
		t.Fatalf("create = %v", r)
	}
	if r := s.mustRun("flags", "apply", "--file", path, "--yes"); r["outcome"] != "unchanged" {
		t.Fatalf("re-apply = %v", r)
	}
	r := s.mustRun("flags", "apply", "--spec-json", `{"key":"checkout-config","payloads":{"debug":{"logLevel":"trace"},"default":{"logLevel":"warn"}}}`, "--yes")
	changes, _ := json.Marshal(r["changes"])
	if r["outcome"] != "applied" || !strings.Contains(string(changes), `"path":"filters.payloads.debug"`) || strings.Contains(string(changes), "groups") {
		t.Fatalf("update = %v", r)
	}
	filters := s.flag("checkout-config")["filters"].(map[string]any)
	if len(filters["groups"].([]any)) != 3 || filters["early_exit"] != true {
		t.Fatalf("payload edit disturbed other filters: %v", filters)
	}
}

func TestFlagExportRoundTripsThroughApply(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "apply", "--file", writeTemp(t, "spec.yaml", configSpec), "--yes")
	out, errOut, err := s.run("flags", "export", "checkout-config", "--format", "yaml")
	if err != nil {
		t.Fatalf("export: %v %s", err, errOut)
	}
	if !strings.Contains(out, "- email icontains @contractor.com") || !strings.Contains(out, "logLevel: debug") {
		t.Fatalf("export = %s", out)
	}
	if r := s.mustRun("flags", "apply", "--file", writeTemp(t, "exported.yaml", out), "--yes"); r["outcome"] != "unchanged" {
		t.Fatalf("export → apply changed the flag: %v", r)
	}
}

func TestFlagApplyRejectsBadSpecs(t *testing.T) {
	s := flagSession(t)
	cases := map[string]string{
		`{"key":"checkout-v2","type":"remote_config","conditions":[{}]}`:    "remote_config",
		`{"key":"x","bogus":true}`:                                          "unknown field",
		`{"key":"x","conditions":[{"where":["flag:missing-flag = true"]}]}`: "No feature flag matched key missing-flag",
	}
	for spec, want := range cases {
		if errOut := s.mustFail("flags", "apply", "--spec-json", spec); !strings.Contains(errOut, want) {
			t.Fatalf("%s: stderr = %s", spec, errOut)
		}
	}
	if errOut := s.mustFail("flags", "apply"); !strings.Contains(errOut, "--spec-json or --file") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestFlagApplyWarnsAboutConditionsAfterTypeConversion(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "create", "site-config", "--type", "remote_config", "--payload-json", `{"a":1}`, "--yes")
	r := s.mustRun("flags", "apply", "--spec-json", `{"key":"site-config","type":"boolean","conditions":[{},{"where":["plan = pro"]}]}`)
	if !strings.Contains(mustJSON(r["warnings"]), "Condition 0 matches everyone") {
		t.Fatalf("conversion to boolean should review the new conditions: %v", r)
	}
}

// PostHog stores aggregation_group_type_index: null on every condition a
// request leaves it out of; the mock does the same.
func TestStoredNullDefaultsAreNotChanges(t *testing.T) {
	s := flagSession(t)
	spec := `{"key":"hand-written","conditions":[{"where":["plan = pro"]},{"rollout":0}]}`
	created := s.mustRun("flags", "apply", "--spec-json", spec, "--yes")
	if created["warnings"] != nil && strings.Contains(mustJSON(created["warnings"]), "differently than sent") {
		t.Fatalf("server-filled null defaults are not a normalization: %v", created["warnings"])
	}
	if again := s.mustRun("flags", "apply", "--spec-json", spec, "--yes"); again["outcome"] != "unchanged" {
		t.Fatalf("re-applying the same spec must not plan changes: %v", again)
	}
	if out, _, _ := s.run("flags", "export", "hand-written"); strings.Contains(out, "aggregation_group_type_index") {
		t.Fatalf("export should leave out null defaults: %s", out)
	}
}
