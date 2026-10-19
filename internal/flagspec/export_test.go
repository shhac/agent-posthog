package flagspec

import (
	"bytes"
	"strings"
	"testing"

	_ "github.com/shhac/lib-agent-cli/yaml"
	agentoutput "github.com/shhac/lib-agent-output"
)

func TestExportThenApplyIsUnchanged(t *testing.T) {
	for name, raw := range map[string]string{"boolean": boolFlag, "multivariate": multivariateFlag} {
		t.Run(name, func(t *testing.T) {
			flag := mustFlag(t, raw)
			if name == "multivariate" {
				// The stray "true" payload is pruned on any multivariate apply.
				delete(Object(Object(flag["filters"])["payloads"]), "true")
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
	delete(Object(Object(flag["filters"])["payloads"]), "true")
	spec, err := Export(flag)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := agentoutput.Print(&buf, spec, agentoutput.FormatYAML, nil); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
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
