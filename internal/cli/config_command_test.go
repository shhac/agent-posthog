package cli

import (
	"strings"
	"testing"
)

func TestConfigAliasValidation(t *testing.T) {
	s := stagingSession(t)
	list := s.mustRun("config", "alias", "list")
	if !strings.Contains(mustJSON(list), `"staging":124`) {
		t.Fatalf("list = %v", list)
	}
	if errOut := s.mustFail("config", "alias", "set", "42", "1"); !strings.Contains(errOut, "non-numeric") {
		t.Fatalf("stderr = %s", errOut)
	}
	if errOut := s.mustFail("--project", "nope", "flags", "list"); !strings.Contains(errOut, `unknown project alias \"nope\" in profile work`) {
		t.Fatalf("stderr = %s", errOut)
	}
	s.mustRun("config", "alias", "unset", "prod")
	if errOut := s.mustFail("config", "alias", "unset", "prod"); !strings.Contains(errOut, "no project alias") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestConfigSetRefusesPartNumbers(t *testing.T) {
	s := newMockSession(t, "phx_mock", "456")
	if errOut := s.mustFail("config", "set", "timeout_ms", "12abc"); !strings.Contains(errOut, "whole number") {
		t.Fatalf("stderr = %s", errOut)
	}
	if got := s.mustRun("config", "set", "timeout_ms", "1500"); got["value"] != "1500" {
		t.Fatalf("set = %v", got)
	}
}
