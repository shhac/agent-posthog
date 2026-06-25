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

	// Without flags, the namespaced AGENT_POSTHOG_* env vars win over profile
	// metadata (consistent with AGENT_POSTHOG_BASE_URL beating profile.Host).
	resolved, err = resolve(&GlobalFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Host != "https://base.example" || resolved.OrgID != "env_org" || resolved.ProjectID != 3 || resolved.EnvironmentID != 4 {
		t.Fatalf("env-var-over-profile precedence mismatch: %#v", resolved)
	}

	// With the namespaced env vars cleared, profile metadata applies, and the
	// PostHog-native vars remain a lower-tier fallback below the profile.
	t.Setenv("AGENT_POSTHOG_ORGANIZATION_ID", "")
	t.Setenv("AGENT_POSTHOG_PROJECT_ID", "")
	t.Setenv("AGENT_POSTHOG_ENVIRONMENT_ID", "")
	t.Setenv("POSTHOG_ORGANIZATION_ID", "native_org")
	t.Setenv("POSTHOG_PROJECT_ID", "7")
	t.Setenv("POSTHOG_ENVIRONMENT_ID", "8")
	resolved, err = resolve(&GlobalFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.OrgID != "profile_org" || resolved.ProjectID != 1 || resolved.EnvironmentID != 2 {
		t.Fatalf("profile should outrank PostHog-native vars: %#v", resolved)
	}
}

func TestResolveEnvironmentDefaultsToProject(t *testing.T) {
	t.Cleanup(func() {
		config.SetConfigDir("")
		config.ClearCache()
	})
	config.SetConfigDir(t.TempDir())
	t.Setenv("POSTHOG_PERSONAL_API_KEY", "phx_env")

	// Project set, environment unset: environment mirrors the project (default env).
	resolved, err := resolve(&GlobalFlags{ProjectID: 123})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.EnvironmentID != 123 {
		t.Fatalf("environment should default to project: %#v", resolved)
	}

	// An explicit environment still wins over the project fallback.
	resolved, err = resolve(&GlobalFlags{ProjectID: 123, EnvironmentID: 456})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.EnvironmentID != 456 {
		t.Fatalf("explicit environment should win: %#v", resolved)
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
