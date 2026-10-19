package cli

import (
	"strings"
	"testing"
)

func TestCompletionScriptsRender(t *testing.T) {
	out := runOK(t, "completion", "zsh")
	if !strings.Contains(out, "#compdef agent-posthog") {
		t.Fatalf("stdout = %s", out)
	}

	out = runOK(t, "completion", "bash")
	if !strings.Contains(out, "bash completion V2 for agent-posthog") {
		t.Fatalf("stdout = %s", out)
	}
}
