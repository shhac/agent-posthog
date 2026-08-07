package config

import (
	"fmt"
	"sync"
	"testing"
)

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

// Concurrent StoreProfile calls must not lose each other's entries.
//
// Before updateConfig routed through creds.Store.Update, StoreProfile did
// Read() (from the shared in-memory cache) -> mutate -> Write(). Two
// concurrent CLI invocations — in-process sharing the package cache, or
// across processes sharing config.json — each built their write from a
// snapshot taken before the other's landed, so all but the last writer's
// profile were silently erased.
func TestConcurrentStoreProfileDoesNotLoseEntries(t *testing.T) {
	t.Cleanup(func() {
		SetConfigDir("")
		ClearCache()
	})
	SetConfigDir(t.TempDir())

	const writers = 20
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := StoreProfile(fmt.Sprintf("profile-%02d", i), Profile{ProjectID: i}); err != nil {
				t.Errorf("StoreProfile: %v", err)
			}
		}(i)
	}
	wg.Wait()

	ClearCache()
	cfg := Read()
	if len(cfg.Profiles) != writers {
		t.Fatalf("%d of %d concurrent StoreProfile calls survived — updates were lost", len(cfg.Profiles), writers)
	}
	for i := range writers {
		name := fmt.Sprintf("profile-%02d", i)
		profile, ok := cfg.Profiles[name]
		if !ok {
			t.Errorf("%s was lost from config.json", name)
			continue
		}
		if profile.ProjectID != i {
			t.Errorf("%s round-tripped with ProjectID %d, want %d", name, profile.ProjectID, i)
		}
	}
}

// Concurrent SetDefaultValue calls against DIFFERENT keys must not lose each
// other: both keys live in the same Defaults struct, so an unlocked
// read-modify-write lets the second writer's snapshot erase the first key.
func TestConcurrentSetDefaultValueKeepsBothKeys(t *testing.T) {
	t.Cleanup(func() {
		SetConfigDir("")
		ClearCache()
	})
	SetConfigDir(t.TempDir())

	const rounds = 10
	var wg sync.WaitGroup
	for range rounds {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := SetDefaultValue("timeout_ms", 1000); err != nil {
				t.Errorf("SetDefaultValue(timeout_ms): %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := SetDefaultValue("max_retries", 3); err != nil {
				t.Errorf("SetDefaultValue(max_retries): %v", err)
			}
		}()
	}
	wg.Wait()

	ClearCache()
	cfg := Read()
	if cfg.Defaults.TimeoutMS == nil || *cfg.Defaults.TimeoutMS != 1000 {
		t.Errorf("timeout_ms = %#v, want 1000 — a concurrent max_retries write erased it", cfg.Defaults.TimeoutMS)
	}
	if cfg.Defaults.MaxRetries == nil || *cfg.Defaults.MaxRetries != 3 {
		t.Errorf("max_retries = %#v, want 3 — a concurrent timeout_ms write erased it", cfg.Defaults.MaxRetries)
	}
}
