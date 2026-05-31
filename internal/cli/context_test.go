package cli

import (
	"testing"

	"github.com/shhac/agent-posthog/internal/config"
)

func TestResolvePrecedence(t *testing.T) {
	t.Cleanup(func() {
		config.SetConfigDir("")
		config.ClearCache()
	})
	config.SetConfigDir(t.TempDir())
	if err := config.StoreProfile("prod", config.Profile{Host: "https://profile.example", OrganizationID: "profile_org", ProjectID: 1, EnvironmentID: 2}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POSTHOG_PERSONAL_API_KEY", "phx_env")
	t.Setenv("AGENT_POSTHOG_PROFILE", "prod")
	t.Setenv("AGENT_POSTHOG_BASE_URL", "https://base.example")
	t.Setenv("AGENT_POSTHOG_ORGANIZATION_ID", "env_org")
	t.Setenv("AGENT_POSTHOG_PROJECT_ID", "3")
	t.Setenv("AGENT_POSTHOG_ENVIRONMENT_ID", "4")

	resolved, err := resolve(&GlobalFlags{Host: "https://flag.example", OrgID: "flag_org", ProjectID: 5, EnvironmentID: 6, APIKey: "phx_flag"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Host != "https://flag.example" || resolved.OrgID != "flag_org" || resolved.ProjectID != 5 || resolved.EnvironmentID != 6 {
		t.Fatalf("resolved flags did not win: %#v", resolved)
	}

	resolved, err = resolve(&GlobalFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Host != "https://base.example" || resolved.OrgID != "profile_org" || resolved.ProjectID != 1 || resolved.EnvironmentID != 2 {
		t.Fatalf("profile/default precedence mismatch: %#v", resolved)
	}
}

func TestResolveMissingToken(t *testing.T) {
	t.Cleanup(func() {
		config.SetConfigDir("")
		config.ClearCache()
	})
	config.SetConfigDir(t.TempDir())
	_, err := resolve(&GlobalFlags{})
	if err == nil {
		t.Fatal("expected missing token error")
	}
}
