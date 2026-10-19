package flagspec

import (
	"encoding/json"
	"strings"
	"testing"
)

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
	Object(flag["filters"])["version"] = json.Number("2")
	if _, err := buildFrom(t, flag, `{"key":"beta-gate","active":false}`); err == nil || !strings.Contains(err.Error(), "config version 2") {
		t.Fatalf("v2 err = %v", err)
	}
	Object(flag["filters"])["version"] = json.Number("1")
	if _, err := buildFrom(t, flag, `{"key":"beta-gate","active":false}`); err != nil {
		t.Fatalf("v1 marker should be accepted: %v", err)
	}
}

func TestBuildRefusesEncryptedPayloadChanges(t *testing.T) {
	encrypted := func() map[string]any {
		flag := mustFlag(t, boolFlag)
		flag["has_encrypted_payloads"] = true
		flag["is_remote_configuration"] = true
		return flag
	}
	for _, spec := range []string{
		`{"key":"beta-gate","type":"boolean"}`,
		`{"key":"beta-gate","payload":1}`,
		`{"key":"beta-gate","type":"multivariate","variants":[{"key":"a","rollout":100}],"payloads":{"a":1}}`,
	} {
		parsed, err := ParseSpec([]byte(spec))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Build(encrypted(), parsed, nil); err == nil || !strings.Contains(err.Error(), "encrypted") {
			t.Fatalf("%s: err = %v", spec, err)
		}
	}
	patch, err := buildFrom(t, encrypted(), `{"key":"beta-gate","active":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, touched := patch.Filters["payloads"]; touched {
		t.Fatalf("an unrelated edit must leave encrypted payloads alone: %v", patch.Filters)
	}
}

func TestBuildCreatesRemoteConfigServingEveryone(t *testing.T) {
	patch, err := buildFrom(t, nil, `{"key":"cfg","type":"remote_config","payload":{"a":1}}`)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(patch.Body())
	want := `{"active":false,"filters":{"groups":[{"properties":[],"rollout_percentage":100}],"payloads":{"true":"{\"a\":1}"}},"is_remote_configuration":true,"key":"cfg"}`
	if string(body) != want {
		t.Fatalf("body = %s\nwant   %s", body, want)
	}
}

func TestStoredVersionOneIsNeverEchoed(t *testing.T) {
	flag := mustFlag(t, boolFlag)
	Object(flag["filters"])["version"] = json.Number("1")
	patch, err := buildFrom(t, flag, `{"key":"beta-gate","conditions":[{"rollout":5}],"payload":{"a":1}}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, echoed := patch.Filters["version"]; echoed || strings.Contains(mustJSONString(patch.Body()), `"version"`) {
		t.Fatalf("PostHog rejects an incoming filters.version on v1 writes: %v", patch.Body())
	}
}

func TestAggregationNullClearsAndAbsentLeavesAlone(t *testing.T) {
	flag, _ := DecodeObject([]byte(`{"key":"k","filters":{"groups":[],"aggregation_group_type_index":0}}`))
	for spec, wantChange := range map[string]bool{
		`{"key":"k","aggregation_group_type_index":null}`: true,
		`{"key":"k"}`: false,
		`{"key":"k","aggregation_group_type_index":0}`: false,
	} {
		parsed, err := ParseSpec([]byte(spec))
		if err != nil {
			t.Fatal(err)
		}
		patch, err := Build(flag, parsed, nil)
		if err != nil {
			t.Fatal(err)
		}
		patch.Prune(flag)
		_, changed := patch.Filters["aggregation_group_type_index"]
		if changed != wantChange {
			t.Fatalf("%s: change = %v (%v)", spec, changed, patch.Filters)
		}
	}
}
