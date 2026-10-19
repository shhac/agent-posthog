package cli

import (
	"strings"
	"testing"
)

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
