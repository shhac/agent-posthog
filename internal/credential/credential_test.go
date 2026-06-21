package credential

import (
	"os"
	"path/filepath"
	"strings"
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
