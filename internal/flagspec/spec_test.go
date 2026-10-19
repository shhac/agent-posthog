package flagspec

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const boolFlag = `{
  "id": 22, "key": "beta-gate", "name": "Beta gate", "active": true, "version": 4,
  "is_remote_configuration": false,
  "filters": {
    "groups": [
      {"properties": [{"key": "email", "type": "person", "value": "@contractor.com", "operator": "icontains"}], "rollout_percentage": 0, "variant": null},
      {"properties": [{"key": "$browser", "type": "event", "value": "Chrome", "operator": "exact"}], "rollout_percentage": 100, "variant": null, "sort_key": "abc"},
      {"properties": [], "rollout_percentage": null, "variant": null}
    ],
    "payloads": {"true": "{\"limit\": 12345678901234567890, \"ratio\": 0.1}"},
    "early_exit": true,
    "holdout": {"id": 9, "exclusion_percentage": 10}
  }
}`

const multivariateFlag = `{
  "id": 30, "key": "checkout-config", "name": "", "active": true, "version": 2,
  "is_remote_configuration": false,
  "filters": {
    "groups": [
      {"properties": [{"key": "app_env", "type": "person", "value": "staging", "operator": "exact"}], "rollout_percentage": 100, "variant": "debug"},
      {"properties": [], "rollout_percentage": 100, "variant": null}
    ],
    "multivariate": {"variants": [{"key": "debug", "name": "", "rollout_percentage": 0}, {"key": "default", "rollout_percentage": 100}]},
    "payloads": {"debug": "{\"logLevel\":\"debug\"}", "default": "\"warn\"", "true": "1"}
  }
}`

func mustFlag(t *testing.T, raw string) map[string]any {
	t.Helper()
	flag, err := DecodeObject([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return flag
}

func exportJSON(t *testing.T, flag map[string]any) []byte {
	t.Helper()
	spec, err := Export(flag)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExportThenApplyIsUnchanged(t *testing.T) {
	for name, raw := range map[string]string{"boolean": boolFlag, "multivariate": multivariateFlag} {
		t.Run(name, func(t *testing.T) {
			flag := mustFlag(t, raw)
			if name == "multivariate" {
				// The stray "true" payload is pruned on any multivariate apply.
				delete(asMap(asMap(flag["filters"])["payloads"]), "true")
			}
			spec, err := ParseSpec(exportJSON(t, flag))
			if err != nil {
				t.Fatalf("ParseSpec: %v", err)
			}
			patch, err := Build(flag, spec, nil)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			patch.Prune(flag)
			if !patch.Empty() {
				t.Fatalf("round trip changed %v", Changes(flag, patch))
			}
		})
	}
}

func TestExportShape(t *testing.T) {
	data := string(exportJSON(t, mustFlag(t, boolFlag)))
	for _, want := range []string{
		`"schema_version":1`,
		`"key":"beta-gate"`,
		`"type":"boolean"`,
		`"early_exit":true`,
		`"where":["email icontains @contractor.com"],"rollout":0`,
		`"where":[{"key":"$browser"`,
		`"sort_key":"abc"`,
		`"rollout":null`,
		`"payload":{"limit":12345678901234567890,"ratio":0.1}`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("export missing %s\n%s", want, data)
		}
	}
	if strings.Contains(data, "holdout") {
		t.Errorf("export should leave unowned filters alone: %s", data)
	}
}

func TestExportMarksEncryptedPayloadsIncomplete(t *testing.T) {
	flag := mustFlag(t, boolFlag)
	flag["has_encrypted_payloads"] = true
	flag["is_remote_configuration"] = true
	data := string(exportJSON(t, flag))
	if strings.Contains(data, "12345678901234567890") || !strings.Contains(data, `"incomplete":["encrypted payloads omitted"]`) {
		t.Fatalf("export = %s", data)
	}
	if _, err := ParseSpec([]byte(data)); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("ParseSpec err = %v", err)
	}
}

func TestExportYAMLRoundTrips(t *testing.T) {
	flag := mustFlag(t, multivariateFlag)
	delete(asMap(asMap(flag["filters"])["payloads"]), "true")
	spec, err := Export(flag)
	if err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "schema_version: 1\nkey: checkout-config\n") {
		t.Fatalf("yaml order wrong:\n%s", data)
	}
	parsed, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec(yaml): %v\n%s", err, data)
	}
	patch, err := Build(flag, parsed, nil)
	if err != nil {
		t.Fatal(err)
	}
	patch.Prune(flag)
	if !patch.Empty() {
		t.Fatalf("yaml round trip changed %v\n%s", Changes(flag, patch), data)
	}
}

func TestParseSpecRejects(t *testing.T) {
	cases := map[string]string{
		`{"key":"x","bogus":1}`:                             "unknown field(s) bogus",
		`{"name":"x"}`:                                      "key is required",
		`{"key":"x","schema_version":2}`:                    "schema_version",
		`{"key":"x","type":"gate"}`:                         "type must be",
		`{"key":"x","active":"yes"}`:                        "active must be",
		`{"key":"x","payload":1,"payloads":{}}`:             "not both",
		`{"key":"x","variants":[{"key":"a","rollout":60}]}`: "sum to 100",
		`{"key":"x","variants":[{"key":"a","rollout":50},{"key":"a","rollout":50}]}`: "twice",
		`{"key":"x","conditions":[{"where":["email frobs x"]}]}`:                     "unknown operator",
		`{"key":"x","conditions":[{"rollout":150}]}`:                                 "rollout must be",
		"key: x\nkey: y\n":               "duplicate key",
		"key: x\npayloads:\n  true: 1\n": "keys must be strings",
		"key: x\n---\nkey: y\n":          "single YAML document",
	}
	for input, want := range cases {
		t.Run(want, func(t *testing.T) {
			_, err := ParseSpec([]byte(input))
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v, want containing %q", err, want)
			}
		})
	}
}

func TestParseSpecYAMLKeepsTypes(t *testing.T) {
	spec, err := ParseSpec([]byte("key: x\ntype: boolean\npayload:\n  zip: \"00123\"\n  big: 12345678901234567890\n  on: yes\n  when: 2026-01-01\n"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(spec.Get("payload"))
	want := `{"big":12345678901234567890,"on":"yes","when":"2026-01-01","zip":"00123"}`
	if string(data) != want {
		t.Fatalf("payload = %s, want %s", data, want)
	}
}

func buildFrom(t *testing.T, current map[string]any, spec string) (*Patch, error) {
	t.Helper()
	parsed, err := ParseSpec([]byte(spec))
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	patch, err := Build(current, parsed, func(ref string) (string, error) { return "77", nil })
	if err == nil {
		patch.Prune(current)
	}
	return patch, err
}

func TestBuildCreateDefaults(t *testing.T) {
	patch, err := buildFrom(t, nil, `{"key":"new-gate","conditions":[{"where":["email icontains @x.com","flag:other = true"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(patch.Body())
	want := `{"active":false,"filters":{"groups":[{"properties":[{"key":"email","operator":"icontains","type":"person","value":"@x.com"},{"key":"77","operator":"flag_evaluates_to","type":"flag","value":true}],"rollout_percentage":100}]},"is_remote_configuration":false,"key":"new-gate"}`
	if string(body) != want {
		t.Fatalf("body = %s\nwant   %s", body, want)
	}
}

func TestBuildSendsOnlyChangedSections(t *testing.T) {
	flag := mustFlag(t, boolFlag)
	patch, err := buildFrom(t, flag, `{"key":"beta-gate","payload":{"limit":1}}`)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(patch.Body())
	if string(body) != `{"filters":{"payloads":{"true":"{\"limit\":1}"}}}` {
		t.Fatalf("body = %s", body)
	}
}

func TestBuildKeepsUntouchedPayloadStrings(t *testing.T) {
	flag := mustFlag(t, multivariateFlag)
	patch, err := buildFrom(t, flag, `{"key":"checkout-config","payloads":{"debug":{"logLevel":"debug"},"default":"error"}}`)
	if err != nil {
		t.Fatal(err)
	}
	payloads := patch.Filters["payloads"].(map[string]any)
	if payloads["debug"] != `{"logLevel":"debug"}` || payloads["default"] != `"error"` {
		t.Fatalf("payloads = %v", payloads)
	}
}

func TestBuildTypeConversions(t *testing.T) {
	flag := mustFlag(t, multivariateFlag)
	if _, err := buildFrom(t, flag, `{"key":"checkout-config","type":"boolean"}`); err == nil || !strings.Contains(err.Error(), "supply conditions") {
		t.Fatalf("expected override refusal, got %v", err)
	}
	patch, err := buildFrom(t, flag, `{"key":"checkout-config","type":"boolean","conditions":[{"where":["app_env = staging"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := patch.Filters["multivariate"]; !ok || v != nil {
		t.Fatalf("multivariate not cleared: %v", patch.Filters)
	}
	if payloads := patch.Filters["payloads"].(map[string]any); len(payloads) != 1 || payloads["true"] != "1" {
		t.Fatalf("payloads = %v", payloads)
	}
	if _, err := buildFrom(t, mustFlag(t, boolFlag), `{"key":"beta-gate","type":"multivariate"}`); err == nil || !strings.Contains(err.Error(), "need variants") {
		t.Fatalf("expected variants requirement, got %v", err)
	}
	if _, err := buildFrom(t, mustFlag(t, boolFlag), `{"key":"beta-gate","type":"remote_config","conditions":[]}`); err == nil {
		t.Fatal("expected remote_config conditions refusal")
	}
}

func TestBuildRejectsUnknownOverride(t *testing.T) {
	flag := mustFlag(t, multivariateFlag)
	_, err := buildFrom(t, flag, `{"key":"checkout-config","conditions":[{"variant":"nope"}]}`)
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildRefusesV2AndRenames(t *testing.T) {
	flag := mustFlag(t, boolFlag)
	if _, err := buildFrom(t, flag, `{"key":"renamed"}`); err == nil || !strings.Contains(err.Error(), "renaming") {
		t.Fatalf("rename err = %v", err)
	}
	asMap(flag["filters"])["version"] = json.Number("2")
	if _, err := buildFrom(t, flag, `{"key":"beta-gate","active":false}`); err == nil || !strings.Contains(err.Error(), "config version 2") {
		t.Fatalf("v2 err = %v", err)
	}
	asMap(flag["filters"])["version"] = json.Number("1")
	if _, err := buildFrom(t, flag, `{"key":"beta-gate","active":false}`); err != nil {
		t.Fatalf("v1 marker should be accepted: %v", err)
	}
}

func TestChangesReportPaths(t *testing.T) {
	flag := mustFlag(t, boolFlag)
	patch, err := buildFrom(t, flag, `{"key":"beta-gate","active":false,"conditions":[{"where":["email icontains @contractor.com"],"rollout":0},{"where":[{"key":"$browser","type":"event","value":"Chrome","operator":"exact"}],"rollout":50,"sort_key":"abc"},{"rollout":null}]}`)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(Changes(flag, patch))
	want := `[{"path":"active","before":true,"after":false},{"path":"filters.groups[1].rollout_percentage","before":100,"after":50}]`
	if string(got) != want {
		t.Fatalf("changes = %s\nwant      %s", got, want)
	}
}

func TestConditionWarnings(t *testing.T) {
	groups := []any{
		map[string]any{"properties": []any{}, "rollout_percentage": json.Number("100")},
		map[string]any{"properties": []any{map[string]any{"key": "email"}}, "rollout_percentage": json.Number("100")},
	}
	if got := ConditionWarnings(groups, TypeBoolean); len(got) != 1 || !strings.Contains(got[0], "Condition 0") {
		t.Fatalf("warnings = %v", got)
	}
	if got := ConditionWarnings(nil, TypeBoolean); len(got) != 1 || !strings.Contains(got[0], "matches nobody") {
		t.Fatalf("warnings = %v", got)
	}
	if got := ConditionWarnings(groups[1:], TypeBoolean); len(got) != 0 {
		t.Fatalf("warnings = %v", got)
	}
}

func TestResultMergesPatchShallowly(t *testing.T) {
	flag := mustFlag(t, multivariateFlag)
	p := NewPatch()
	p.Fields["is_remote_configuration"] = true
	p.Filters["multivariate"] = nil
	p.Filters["early_exit"] = true
	result := Result(flag, p)
	filters := asMap(result["filters"])
	if _, ok := filters["multivariate"]; ok || filters["early_exit"] != true || filters["groups"] == nil {
		t.Fatalf("filters = %v", filters)
	}
	if TypeOf(result) != TypeRemoteConfig || TypeOf(flag) != TypeMultivariate {
		t.Fatal("Result must not alias or keep the old type")
	}
}

func TestYAMLNumbersBecomeValidJSONNumbers(t *testing.T) {
	cases := map[string]string{
		".5":    "0.5",
		"+1.5":  "1.5",
		"1e1":   "1e1",
		"33.3":  "33.3",
		"0x10":  "16",
		"+7":    "7",
		"-0.25": "-0.25",
	}
	for in, want := range cases {
		doc, err := ParseDocument([]byte("n: " + in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		got := asMap(doc)["n"]
		if got != json.Number(want) {
			t.Fatalf("%s → %v, want %s", in, got, want)
		}
		if _, err := json.Marshal(got); err != nil {
			t.Fatalf("%s produced invalid JSON number %v: %v", in, got, err)
		}
	}
	for _, in := range []string{".inf", ".nan"} {
		if _, err := ParseDocument([]byte("n: " + in)); err == nil {
			t.Fatalf("%s should be rejected", in)
		}
	}
	if doc, _ := ParseDocument([]byte("n: ~")); asMap(doc)["n"] != nil {
		t.Fatal("~ should be null")
	}
}
