package config

import "testing"

func TestProfileLifecycleAndDefaults(t *testing.T) {
	t.Cleanup(func() {
		SetConfigDir("")
		ClearCache()
	})
	SetConfigDir(t.TempDir())

	if err := StoreProfile("prod", Profile{Host: "https://us.posthog.com", OrganizationID: "org_1", ProjectID: 123, EnvironmentID: 456}); err != nil {
		t.Fatal(err)
	}
	cfg := Read()
	if cfg.DefaultProfile != "prod" {
		t.Fatalf("DefaultProfile = %q", cfg.DefaultProfile)
	}
	if got := cfg.Profiles["prod"].EnvironmentID; got != 456 {
		t.Fatalf("EnvironmentID = %d", got)
	}

	if err := UpdateProfile("prod", func(profile Profile) Profile {
		profile.ProjectID = 789
		profile.EnvironmentID = 0
		return profile
	}); err != nil {
		t.Fatal(err)
	}
	if got := Read().Profiles["prod"].ProjectID; got != 789 {
		t.Fatalf("ProjectID = %d", got)
	}
	if got := Read().Profiles["prod"].EnvironmentID; got != 0 {
		t.Fatalf("EnvironmentID = %d", got)
	}

	if err := StoreProfile("dev", Profile{}); err != nil {
		t.Fatal(err)
	}
	if err := SetDefault("dev"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfile("dev"); err != nil {
		t.Fatal(err)
	}
	if Read().DefaultProfile == "dev" {
		t.Fatal("removed profile remained default")
	}
}

func TestDefaultValueLifecycle(t *testing.T) {
	t.Cleanup(func() {
		SetConfigDir("")
		ClearCache()
	})
	SetConfigDir(t.TempDir())

	if err := SetDefaultValue("timeout_ms", 1000); err != nil {
		t.Fatal(err)
	}
	if err := SetDefaultValue("max_retries", 3); err != nil {
		t.Fatal(err)
	}
	cfg := Read()
	if cfg.Defaults.TimeoutMS == nil || *cfg.Defaults.TimeoutMS != 1000 {
		t.Fatalf("TimeoutMS = %#v", cfg.Defaults.TimeoutMS)
	}
	if cfg.Defaults.MaxRetries == nil || *cfg.Defaults.MaxRetries != 3 {
		t.Fatalf("MaxRetries = %#v", cfg.Defaults.MaxRetries)
	}
	if err := UnsetDefaultValue("timeout_ms"); err != nil {
		t.Fatal(err)
	}
	if Read().Defaults.TimeoutMS != nil {
		t.Fatal("timeout_ms was not unset")
	}
	if err := SetDefaultValue("unknown", 1); err == nil {
		t.Fatal("expected unknown config key error")
	}
}
