package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOrgsListAgainstMock(t *testing.T) {
	out := runOK(t, "orgs", "list")
	if !strings.Contains(out, `"id":"org_1"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestMissingEnvironmentHasHint(t *testing.T) {
	// Environment mirrors the project when unset, so the missing-environment
	// hint only surfaces when the project is unset too. Clear both.
	s := newMockSession(t, "phx_mock", "")
	t.Setenv("AGENT_POSTHOG_PROJECT_ID", "")
	errOut := s.mustFail("persons", "list")
	if !strings.Contains(errOut, `"fixable_by":"agent"`) || !strings.Contains(errOut, "environments list") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestPaginationMetaFromMock(t *testing.T) {
	out := runOK(t, "orgs", "list", "--limit", "1")
	if !strings.Contains(out, `"id":"org_1"`) || !strings.Contains(out, `"@pagination"`) || !strings.Contains(out, `"has_more":true`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestListEnvelopeMatchesTheFamily(t *testing.T) {
	out := runOK(t, "orgs", "list", "--limit", "1", "--format", "json")
	var envelope struct {
		Data       []map[string]any `json:"data"`
		Pagination map[string]any   `json:"@pagination"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, out)
	}
	if len(envelope.Data) != 1 || envelope.Data[0]["id"] != "org_1" || envelope.Pagination["has_more"] != true || envelope.Pagination["next_url"] == nil {
		t.Fatalf("stdout = %s", out)
	}
}

func TestPaginationAllFollowsNextURL(t *testing.T) {
	out := runOK(t, "orgs", "list", "--limit", "1", "--all", "--page-limit", "2")
	if !strings.Contains(out, `"id":"org_1"`) || !strings.Contains(out, `"id":"org_2"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestPaginationPageLimitLeavesCursor(t *testing.T) {
	out := runOK(t, "orgs", "list", "--limit", "1", "--all", "--page-limit", "1")
	if strings.Contains(out, `"id":"org_2"`) || !strings.Contains(out, `"@pagination"`) {
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

func TestRecordingSharingRedactsAccessToken(t *testing.T) {
	out := runOK(t, "api", "get", "/api/projects/123/session_recordings/rec_1/sharing/")
	if strings.Contains(out, "phs_recording_share_secret") || !strings.Contains(out, `"access_token": "[REDACTED]"`) {
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
			out := runOK(t, args...)
			if strings.TrimSpace(out) == "" {
				t.Fatal("expected stdout")
			}
		})
	}
}

func TestGetCommandsRedactSecrets(t *testing.T) {
	out := runOK(t, "projects", "get", "123")
	if strings.Contains(out, "phc_mock_project_123") || !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("get must redact like list does: %s", out)
	}
	exposed, _ := runCLI(t, "--expose", "api_token", "projects", "get", "123")
	if !strings.Contains(exposed, "phc_mock_project_123") {
		t.Fatalf("--expose should reveal the field: %s", exposed)
	}
}

func TestLocalCommandsHonorFormat(t *testing.T) {
	s := newMockSession(t, "phx_mock", "456")
	for _, args := range [][]string{{"config", "path"}, {"usage"}, {"config", "get", "mcp_writes"}} {
		out, errOut, err := s.run(append(args, "--format", "yaml")...)
		if err != nil || strings.HasPrefix(strings.TrimSpace(out), "{") {
			t.Fatalf("%v --format yaml printed JSON: %v %s\n%s", args, err, errOut, out)
		}
	}
}

func TestGetCommandsFailFastWhenTheScopeIsMissing(t *testing.T) {
	s := newMockSession(t, "phx_mock", "456")
	t.Setenv("AGENT_POSTHOG_PROJECT_ID", "")
	t.Setenv("AGENT_POSTHOG_ENVIRONMENT_ID", "")
	out, errOut, err := s.run("flags", "get", "checkout-v2", "other")
	if err == nil || out != "" || !strings.Contains(errOut, "missing project id") {
		t.Fatalf("a missing project is a command-level failure, like list: err=%v out=%s err=%s", err, out, errOut)
	}
	if out, _, _ := s.run("--project", "123", "flags", "get", "missing-flag"); !strings.Contains(out, "@unresolved") {
		t.Fatalf("an unknown key stays an item-level miss: %s", out)
	}
}

func TestListCommandsRedactSecrets(t *testing.T) {
	for _, args := range [][]string{
		{"projects", "list", "--full"},
		{"projects", "list", "--full", "--format", "json"},
		{"projects", "list", "--full", "--format", "yaml"},
	} {
		out, errOut := runCLI(t, args...)
		if errOut != "" || strings.Contains(out, "phc_mock_project_123") || !strings.Contains(out, "[REDACTED]") {
			t.Fatalf("%v must redact the project token: %s %s", args, errOut, out)
		}
	}
	exposed, _ := runCLI(t, "--expose", "api_token", "projects", "list", "--full")
	if !strings.Contains(exposed, "phc_mock_project_123") {
		t.Fatalf("--expose should reveal the field: %s", exposed)
	}
}

func TestCompactOutputKeepsTheUsualFields(t *testing.T) {
	for _, args := range [][]string{
		{"projects", "list"},
		{"api", "get", "/api/organizations/org_1/projects/123/"},
	} {
		out, errOut := runCLI(t, args...)
		if errOut != "" || !strings.Contains(out, `"Website"`) || strings.Contains(out, "timezone") {
			t.Fatalf("%v should print the compact record: %s %s", args, errOut, out)
		}
	}
	full, _ := runCLI(t, "projects", "list", "--full")
	if !strings.Contains(full, "timezone") {
		t.Fatalf("--full should keep every field: %s", full)
	}
	got, _ := runCLI(t, "projects", "get", "123")
	if !strings.Contains(got, "timezone") {
		t.Fatalf("gets print the whole record: %s", got)
	}
}
