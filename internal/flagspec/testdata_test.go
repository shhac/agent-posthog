package flagspec

import (
	"encoding/json"
	"testing"
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

func mustJSONString(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
