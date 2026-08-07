package credential

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shhac/agent-posthog/internal/config"
)

// TestStore_Headless_FileFallback exercises the credential-WRITE path
// non-interactively. The per-CLI keychain opt-out (derived by lib-agent-cli from
// the "app.paulie.agent-posthog" service) makes the keychain report unavailable,
// so Store deterministically keeps the raw key in the 0600 index file on every
// platform — including darwin, where it would otherwise reach the `security` GUI
// prompt. Before the file fallback existed, Store simply failed under the opt-out
// (and on any non-macOS host).
func TestStore_Headless_FileFallback(t *testing.T) {
	t.Setenv("AGENT_POSTHOG_NO_KEYCHAIN", "1")
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	storage, err := Store("headless", "phc-headless-key")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if storage != "file" {
		t.Fatalf("storage=%q, want \"file\" (keychain opt-out should force the file path)", storage)
	}

	path := filepath.Join(dir, "credentials.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("index not written: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("index mode=%o, want 0600", mode)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "phc-headless-key") {
		t.Errorf("file should contain the raw key under opt-out; got %s", data)
	}
	if strings.Contains(string(data), keychainSentinel) {
		t.Errorf("file should NOT contain the keychain sentinel under opt-out; got %s", data)
	}

	got, err := Get("headless")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "phc-headless-key" {
		t.Errorf("Get=%q, want phc-headless-key", got)
	}

	if err := Remove("headless"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := Get("headless"); err == nil {
		t.Error("expected NotFound after Remove")
	}
}

// Concurrent stores must not lose each other's entries.
//
// This is the failure that matters most for THIS index: the keychain write has
// already succeeded by the time the index is written, so an entry lost to a
// racing writer leaves a live secret in the OS keychain that nothing
// references — invisible to `auth list` and unreachable by `auth remove`,
// which looks the name up in the index first.
func TestConcurrentStoresDoNotLoseEntries(t *testing.T) {
	t.Setenv("AGENT_POSTHOG_NO_KEYCHAIN", "1")
	config.SetConfigDir(t.TempDir())
	t.Cleanup(func() { config.SetConfigDir("") })

	const writers = 20
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := Store(fmt.Sprintf("profile-%02d", i), fmt.Sprintf("phc-key-%02d", i)); err != nil {
				t.Errorf("Store: %v", err)
			}
		}(i)
	}
	wg.Wait()

	names, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != writers {
		t.Errorf("%d of %d concurrent Store calls survived — updates were lost", len(names), writers)
	}
	for i := range writers {
		name := fmt.Sprintf("profile-%02d", i)
		got, err := Get(name)
		if err != nil {
			t.Errorf("%s was lost from the index: %v", name, err)
			continue
		}
		if want := fmt.Sprintf("phc-key-%02d", i); got != want {
			t.Errorf("%s round-tripped as %q, want %q", name, got, want)
		}
	}
}
