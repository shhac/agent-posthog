package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shhac/agent-posthog/internal/config"
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
	if !strings.Contains(out, `"key":"checkout-v2"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestMissingEnvironmentHasHint(t *testing.T) {
	_, errOut := runCLIWithEnvironment(t, "", "persons", "list")
	if !strings.Contains(errOut, `"fixable_by":"agent"`) || !strings.Contains(errOut, "environments list") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestPaginationMetaFromMock(t *testing.T) {
	out, errOut := runCLI(t, "orgs", "list", "--limit", "1")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, `"id":"org_1"`) || !strings.Contains(out, `"@pagination"`) || !strings.Contains(out, `"has_more":true`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestPaginationAllFollowsNextURL(t *testing.T) {
	out, errOut := runCLI(t, "orgs", "list", "--limit", "1", "--all", "--page-limit", "2")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, `"id":"org_1"`) || !strings.Contains(out, `"id":"org_2"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestPaginationPageLimitLeavesCursor(t *testing.T) {
	out, errOut := runCLI(t, "orgs", "list", "--limit", "1", "--all", "--page-limit", "1")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if strings.Contains(out, `"id":"org_2"`) || !strings.Contains(out, `"@pagination"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestFlagLookupMissHasHint(t *testing.T) {
	// Item-level misses are now @unresolved records on stdout (not fatal errors on
	// stderr) — matching the family get contract. stderr stays empty; exit is 0.
	out, errOut := runCLI(t, "flags", "get", "missing-flag")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, "No feature flag matched key missing-flag") || !strings.Contains(out, `"fixable_by":"agent"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestFlagLookupAmbiguousHasHint(t *testing.T) {
	// Item-level misses are now @unresolved records on stdout (not fatal errors on
	// stderr) — matching the family get contract. stderr stays empty; exit is 0.
	out, errOut := runCLI(t, "flags", "get", "ambiguous-flag")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, "Multiple feature flags matched key ambiguous-flag") || !strings.Contains(out, "Use the numeric feature flag ID") {
		t.Fatalf("stdout = %s", out)
	}
}

func TestRateLimitIsRetryFixable(t *testing.T) {
	_, errOut := runCLI(t, "--max-retries", "0", "api", "get", "/api/mock/rate_limit")
	if !strings.Contains(errOut, `"fixable_by":"retry"`) || !strings.Contains(errOut, "Rate limited") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestValidationErrorIncludesAttribute(t *testing.T) {
	_, errOut := runCLI(t, "api", "get", "/api/mock/validation_error")
	if !strings.Contains(errOut, `"fixable_by":"agent"`) || !strings.Contains(errOut, "query: This field is required") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestInvalidPersonalAPIKeyIsHumanFixable(t *testing.T) {
	_, errOut := runCLIWithToken(t, "phx_invalid", "orgs", "list")
	if !strings.Contains(errOut, `"fixable_by":"human"`) || !strings.Contains(errOut, "Authentication failed") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestMissingScopeIsHumanFixable(t *testing.T) {
	_, errOut := runCLIWithToken(t, "phx_no_scope", "orgs", "list")
	if !strings.Contains(errOut, `"fixable_by":"human"`) || !strings.Contains(errOut, "Permission denied") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestAsyncQueryFixtures(t *testing.T) {
	bodyPath := filepath.Join(t.TempDir(), "query.json")
	if err := os.WriteFile(bodyPath, []byte(`{"async":true,"query":{"kind":"HogQLQuery","query":"select 1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := runCLI(t, "api", "post", "/api/environments/456/query/", "--body", bodyPath)
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, `"complete": false`) || !strings.Contains(out, `"query_pending"`) {
		t.Fatalf("stdout = %s", out)
	}

	out, errOut = runCLI(t, "query", "get", "query_failed")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, `"error": true`) || !strings.Contains(out, "Syntax error near frobulate") {
		t.Fatalf("stdout = %s", out)
	}

	out, errOut = runCLI(t, "query", "log", "query_complete")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, `"runtime_ms": 812`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestRecordingSharingRedactsAccessToken(t *testing.T) {
	out, errOut := runCLI(t, "api", "get", "/api/projects/123/session_recordings/rec_1/sharing/")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if strings.Contains(out, "phs_recording_share_secret") || !strings.Contains(out, `"access_token": "[REDACTED]"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestRawAPIRejectsUnsafePathsAndPosts(t *testing.T) {
	_, errOut := runCLI(t, "api", "get", "/not-api")
	if !strings.Contains(errOut, "raw API paths must start with /api/") {
		t.Fatalf("stderr = %s", errOut)
	}

	_, errOut = runCLI(t, "api", "post", "/api/projects/123/feature_flags/")
	if !strings.Contains(errOut, "raw POST outside query endpoints requires --yes") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestRawAPIPrintRequestRedactsAuth(t *testing.T) {
	out, errOut := runCLI(t, "api", "get", "/api/organizations/", "--print-request")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if strings.Contains(out, "phx_mock") || !strings.Contains(out, "Bearer phx_...") {
		t.Fatalf("stdout = %s", out)
	}
}

func TestQueryHogQLAsyncReturnsStatus(t *testing.T) {
	out, errOut := runCLI(t, "query", "hogql", "--async", "select 1")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, `"query_pending"`) || !strings.Contains(out, `"complete":false`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestQueryWaitEmitsCompletedRows(t *testing.T) {
	out, errOut := runCLI(t, "query", "wait", "query_complete", "--interval", "1ms", "--max-wait", "10ms")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, `"event":"$pageview"`) || !strings.Contains(out, `"@query"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestInvestigateFlagEmitsEvidence(t *testing.T) {
	out, errOut := runCLI(t, "investigate", "flag", "checkout-v2")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, `"type":"entity"`) || !strings.Contains(out, `"name":"activity"`) || !strings.Contains(out, `"type":"next_step"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestMockSmokeChecklist(t *testing.T) {
	cases := [][]string{
		{"orgs", "list", "--limit", "1"},
		{"schema", "events", "list", "--search", "signup"},
		{"schema", "properties", "list", "--event", "$pageview"},
		{"query", "hogql", "select event, count() from events group by event"},
		{"query", "get", "query_failed"},
		{"query", "log", "query_complete"},
		{"persons", "list", "--email", "user@example.com"},
		{"flags", "get", "checkout-v2"},
		{"dashboards", "run", "66"},
		{"recordings", "list"},
		{"experiments", "list"},
		{"investigate", "user", "--email", "user@example.com"},
		{"investigate", "event", "--event", "$pageview"},
		{"investigate", "flag", "checkout-v2"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, errOut := runCLI(t, args...)
			if errOut != "" {
				t.Fatalf("stderr = %s", errOut)
			}
			if strings.TrimSpace(out) == "" {
				t.Fatal("expected stdout")
			}
		})
	}
}

func TestCompletionScriptsRender(t *testing.T) {
	out, errOut := runCLI(t, "completion", "zsh")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, "#compdef agent-posthog") {
		t.Fatalf("stdout = %s", out)
	}

	out, errOut = runCLI(t, "completion", "bash")
	if errOut != "" {
		t.Fatalf("stderr = %s", errOut)
	}
	if !strings.Contains(out, "bash completion V2 for agent-posthog") {
		t.Fatalf("stdout = %s", out)
	}
}

func runCLI(t *testing.T, args ...string) (string, string) {
	return runCLIWithEnvironment(t, "456", args...)
}

func runCLIWithEnvironment(t *testing.T, envID string, args ...string) (string, string) {
	return runCLIWithTokenAndEnvironment(t, "phx_mock", envID, args...)
}

func runCLIWithToken(t *testing.T, token string, args ...string) (string, string) {
	return runCLIWithTokenAndEnvironment(t, token, "456", args...)
}

func runCLIWithTokenAndEnvironment(t *testing.T, token, envID string, args ...string) (string, string) {
	t.Helper()
	// Isolate from the developer's real on-disk config: resolve() ranks
	// profile.{Org,Project,Environment}ID ahead of the env vars set below, so a
	// configured default profile would otherwise shadow the mock fixtures.
	config.SetConfigDir(t.TempDir())
	t.Cleanup(func() { config.SetConfigDir("") })
	server := httptest.NewServer(mockposthog.NewServer())
	t.Cleanup(server.Close)
	t.Setenv("AGENT_POSTHOG_BASE_URL", server.URL)
	t.Setenv("POSTHOG_PERSONAL_API_KEY", token)
	t.Setenv("AGENT_POSTHOG_ORGANIZATION_ID", "org_1")
	t.Setenv("AGENT_POSTHOG_PROJECT_ID", "123")
	t.Setenv("AGENT_POSTHOG_ENVIRONMENT_ID", envID)

	var stdout, stderr bytes.Buffer
	restore := output.SetWritersForTest(&stdout, &stderr)
	t.Cleanup(restore)

	cmd := newRootCmd("test")
	cmd.SetArgs(args)
	// Mirror libcli.Run: errors bubble out of Execute unrendered, and the single
	// sink renders them once to stderr. We render to the captured writer here so
	// the test sees exactly what Run would emit, exercising the single-sink path.
	if err := cmd.Execute(); err != nil {
		output.WriteError(output.Stderr(), err)
	}
	return stdout.String(), stderr.String()
}
