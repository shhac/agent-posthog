package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shhac/agent-posthog/internal/mockposthog"
	"github.com/shhac/agent-posthog/internal/output"
	"net/http/httptest"
)

func TestOrgsListAgainstMock(t *testing.T) {
	out, errOut := runCLI(t, "orgs", "list")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, `"id":"org_1"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestQueryHogQLOutputsNDJSONRows(t *testing.T) {
	out, errOut := runCLI(t, "--env", "456", "query", "hogql", "select event, count() from events group by event")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, `"@query"`) || !strings.Contains(out, `"$pageview"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestFlagsGetResolvesKey(t *testing.T) {
	out, errOut := runCLI(t, "--project", "123", "flags", "get", "checkout-v2")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, `"key": "checkout-v2"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestMissingEnvironmentHasHint(t *testing.T) {
	_, errOut := runCLIWithEnvironment(t, "", "persons", "list")
	if !strings.Contains(errOut, `"fixable_by":"agent"`) || !strings.Contains(errOut, "environments list") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func runCLI(t *testing.T, args ...string) (string, string) {
	return runCLIWithEnvironment(t, "456", args...)
}

func runCLIWithEnvironment(t *testing.T, envID string, args ...string) (string, string) {
	t.Helper()
	server := httptest.NewServer(mockposthog.NewServer())
	t.Cleanup(server.Close)
	t.Setenv("AGENT_POSTHOG_BASE_URL", server.URL)
	t.Setenv("POSTHOG_PERSONAL_API_KEY", "phx_mock")
	t.Setenv("AGENT_POSTHOG_ORGANIZATION_ID", "org_1")
	t.Setenv("AGENT_POSTHOG_PROJECT_ID", "123")
	t.Setenv("AGENT_POSTHOG_ENVIRONMENT_ID", envID)

	var stdout, stderr bytes.Buffer
	restore := output.SetWritersForTest(&stdout, &stderr)
	t.Cleanup(restore)

	cmd := newRootCmd("test")
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	return stdout.String(), stderr.String()
}
