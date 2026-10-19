package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	agentoutput "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/config"
	"github.com/shhac/agent-posthog/internal/mockposthog"
	"github.com/shhac/agent-posthog/internal/output"
)

func runCLI(t *testing.T, args ...string) (string, string) {
	return runCLIWithEnvironment(t, "456", args...)
}

// runOK runs a command against a fresh mock and fails the test on any
// stderr; it returns stdout.
func runOK(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut := runCLI(t, args...)
	if errOut != "" {
		t.Fatalf("%v: stderr = %s", args, errOut)
	}
	return out
}

func runCLIWithEnvironment(t *testing.T, envID string, args ...string) (string, string) {
	return runCLIWithTokenAndEnvironment(t, "phx_mock", envID, args...)
}

func runCLIWithToken(t *testing.T, token string, args ...string) (string, string) {
	return runCLIWithTokenAndEnvironment(t, token, "456", args...)
}

func runCLIWithTokenAndEnvironment(t *testing.T, token, envID string, args ...string) (string, string) {
	t.Helper()
	session := newMockSession(t, token, envID)
	out, errOut, _ := session.run(args...)
	return out, errOut
}

// cliSession runs several CLI invocations against one PostHog (usually a mock
// from newMockSession), for multi-step flows whose later steps depend on state
// the earlier ones wrote.
type cliSession struct {
	t *testing.T
}

func newMockSession(t *testing.T, token, envID string) *cliSession {
	t.Helper()
	return newWrappedMockSession(t, token, envID, func(h http.Handler) http.Handler { return h })
}

// newWrappedMockSession is newMockSession with the mock behind wrap, for tests
// that need to observe or disturb traffic (dropped responses, concurrency).
func newWrappedMockSession(t *testing.T, token, envID string, wrap func(http.Handler) http.Handler) *cliSession {
	t.Helper()
	// Isolate from the developer's real on-disk config: resolve() ranks
	// profile.{Org,Project,Environment}ID ahead of the env vars set below, so a
	// configured default profile would otherwise shadow the mock fixtures.
	config.SetConfigDir(t.TempDir())
	t.Cleanup(func() { config.SetConfigDir("") })
	server := httptest.NewServer(wrap(mockposthog.NewServer()))
	t.Cleanup(server.Close)
	t.Setenv("AGENT_POSTHOG_BASE_URL", server.URL)
	t.Setenv("POSTHOG_PERSONAL_API_KEY", token)
	t.Setenv("AGENT_POSTHOG_ORGANIZATION_ID", "org_1")
	t.Setenv("AGENT_POSTHOG_PROJECT_ID", "123")
	t.Setenv("AGENT_POSTHOG_ENVIRONMENT_ID", envID)
	return &cliSession{t: t}
}

// run executes one CLI invocation, returning stdout, stderr, and the error
// Execute bubbled (non-nil means the process would exit 1).
func (s *cliSession) run(args ...string) (string, string, error) {
	s.t.Helper()
	var stdout, stderr bytes.Buffer
	restore := output.SetWritersForTest(&stdout, &stderr)
	defer restore()

	cmd := newRootCmd("test")
	cmd.SetArgs(args)
	// Mirror libcli.Run: errors bubble out of Execute unrendered, and the single
	// sink renders them once to stderr. We render to the captured writer here so
	// the test sees exactly what Run would emit, exercising the single-sink path.
	err := cmd.Execute()
	if err != nil {
		agentoutput.WriteError(output.Stderr(), err)
	}
	return stdout.String(), stderr.String(), err
}

func flagSession(t *testing.T) *cliSession {
	t.Helper()
	return newMockSession(t, "phx_mock", "456")
}

// mustRun runs a command that must succeed and decodes its single JSON output.
func (s *cliSession) mustRun(args ...string) map[string]any {
	s.t.Helper()
	out, errOut, err := s.run(append(args, "--format", "json")...)
	if err != nil {
		s.t.Fatalf("%v: err=%v stderr=%s", args, err, errOut)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		s.t.Fatalf("%v: stdout not JSON: %v\n%s", args, err, out)
	}
	return v
}

// mustFail runs a command that must fail and returns its stderr.
func (s *cliSession) mustFail(args ...string) string {
	s.t.Helper()
	_, errOut, err := s.run(args...)
	if err == nil {
		s.t.Fatalf("%v: expected failure", args)
	}
	return errOut
}

func (s *cliSession) flag(ref string) map[string]any {
	s.t.Helper()
	return s.mustRun("flags", "get", ref)["data"].([]any)[0].(map[string]any)
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// stagingSession is a mock session with a "work" profile whose aliases name
// project 124 staging and 123 prod.
func stagingSession(t *testing.T) *cliSession {
	t.Helper()
	s := flagSession(t)
	if err := config.StoreProfile("work", config.Profile{Host: "http://unused", OrganizationID: "org_1", ProjectID: 123}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_POSTHOG_PROFILE", "work")
	s.mustRun("config", "alias", "set", "staging", "124")
	s.mustRun("config", "alias", "set", "prod", "123")
	return s
}
