package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/shhac/agent-posthog/internal/cli/auth"
	"github.com/shhac/agent-posthog/internal/credential"
)

// authSession runs against the mock with no ambient token, so commands must
// use the profile's stored credential, kept in the 0600 file (no keychain).
func authSession(t *testing.T) (*cliSession, func() string) {
	t.Helper()
	var mu sync.Mutex
	lastAuth := ""
	s := newWrappedMockSession(t, "", "456", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			lastAuth = r.Header.Get("Authorization")
			mu.Unlock()
			next.ServeHTTP(w, r)
		})
	})
	t.Setenv("AGENT_POSTHOG_NO_KEYCHAIN", "1")
	t.Setenv("POSTHOG_PERSONAL_API_KEY", "")
	t.Setenv("AGENT_POSTHOG_PERSONAL_API_KEY", "")
	return s, func() string { mu.Lock(); defer mu.Unlock(); return lastAuth }
}

func TestStoredProfileCredentialIsUsed(t *testing.T) {
	s, lastAuth := authSession(t)
	added := s.mustRun("auth", "add", "work", "--api-key", "phx_stored", "--org", "org_1", "--project", "123")
	if added["storage"] != "file" || added["default"] != true {
		t.Fatalf("add = %v", added)
	}
	if out, errOut, err := s.run("orgs", "list"); err != nil || !strings.Contains(out, "org_1") {
		t.Fatalf("orgs list with the stored key: %v %s", err, errOut)
	}
	if got := lastAuth(); got != "Bearer phx_stored" {
		t.Fatalf("request carried %q, want the profile's stored key", got)
	}
	list, _, _ := s.run("auth", "list")
	if !strings.Contains(list, `"credential":"file"`) {
		t.Fatalf("auth list must report where the key lives: %s", list)
	}
}

func TestAuthAddFormUsesThePrompt(t *testing.T) {
	s, _ := authSession(t)
	saved := auth.PromptSecret
	t.Cleanup(func() { auth.PromptSecret = saved })
	auth.PromptSecret = func(context.Context, string, string, string) (string, error) { return "phx_from_dialog", nil }
	if r := s.mustRun("auth", "add", "dialog", "--form"); r["status"] != "added" {
		t.Fatalf("add --form = %v", r)
	}
	if errOut := s.mustFail("auth", "add", "nokey"); !strings.Contains(errOut, "--form") {
		t.Fatalf("missing key should point at --form: %s", errOut)
	}
}

func TestAuthRemoveCleansUpAHalfRemovedProfile(t *testing.T) {
	s, _ := authSession(t)
	s.mustRun("auth", "add", "half", "--api-key", "phx_x")
	if err := credential.Remove("half"); err != nil {
		t.Fatal(err)
	}
	if errOut := s.mustFail("--profile", "half", "orgs", "list"); !strings.Contains(errOut, "auth add") {
		t.Fatalf("a profile without a key should point at auth add: %s", errOut)
	}
	if r := s.mustRun("auth", "remove", "half"); r["status"] != "removed" {
		t.Fatalf("a profile whose key is already gone must still be removable: %v", r)
	}
	if errOut := s.mustFail("auth", "remove", "half"); !strings.Contains(errOut, "not found") {
		t.Fatalf("removing a profile that is gone should say so: %s", errOut)
	}
}

func TestAuthUpdateDefaultAndCheck(t *testing.T) {
	s, _ := authSession(t)
	s.mustRun("auth", "add", "work", "--api-key", "phx_stored", "--org", "org_1", "--project", "123", "--env", "456")
	s.mustRun("auth", "add", "other", "--api-key", "phx_other")

	if r := s.mustRun("auth", "update", "work", "--clear-project", "--env", "7"); r["project_id"] != float64(0) || r["environment_id"] != float64(7) {
		t.Fatalf("update = %v", r)
	}
	if errOut := s.mustFail("auth", "update", "work"); !strings.Contains(errOut, "no profile updates requested") {
		t.Fatalf("stderr = %s", errOut)
	}
	if errOut := s.mustFail("auth", "update", "nope", "--env", "1"); !strings.Contains(errOut, "not configured") {
		t.Fatalf("stderr = %s", errOut)
	}

	if errOut := s.mustFail("auth", "default", "nope"); !strings.Contains(errOut, "not configured") {
		t.Fatalf("stderr = %s", errOut)
	}
	if r := s.mustRun("auth", "default", "other"); r["profile"] != "other" {
		t.Fatalf("default = %v", r)
	}
	list, _, _ := s.run("auth", "list")
	var profiles []string
	for _, line := range strings.Split(strings.TrimSpace(list), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("auth list line %q: %v", line, err)
		}
		profiles = append(profiles, row["profile"].(string))
		if row["profile"] == "other" && row["default"] != true {
			t.Fatalf("other should be the default: %v", row)
		}
	}
	if strings.Join(profiles, ",") != "other,work" {
		t.Fatalf("auth list order = %v", profiles)
	}

	if r := s.mustRun("auth", "check", "work"); r["status"] != "ok" || r["profile"] != "work" {
		t.Fatalf("check = %v", r)
	}
}
