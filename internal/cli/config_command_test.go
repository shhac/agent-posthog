package cli

import (
	"strings"
	"testing"

	"github.com/shhac/agent-posthog/internal/cli/shared"
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

func TestMCPWritesTurnBackOffWithUnset(t *testing.T) {
	s := flagSession(t)
	s.mustRun("config", "set", "mcp_writes", "on")
	if got := s.mustRun("config", "get", "mcp_writes"); got["value"] != "on" || got["set"] != true {
		t.Fatalf("get after set = %v", got)
	}
	s.mustRun("config", "unset", "mcp_writes")
	if got := s.mustRun("config", "get", "mcp_writes"); got["value"] != "off" {
		t.Fatalf("get after unset = %v", got)
	}
	if errOut := s.mustFail("config", "set", "mcp_writes", "true"); !strings.Contains(errOut, "on or off") {
		t.Fatalf("stderr = %s", errOut)
	}
	t.Setenv(shared.ViaMCPEnv, "1")
	if errOut := s.mustFail("flags", "disable", "checkout-v2", "--yes"); !strings.Contains(errOut, "mcp_writes") {
		t.Fatalf("after unset, MCP writes must be refused again: %s", errOut)
	}
}
