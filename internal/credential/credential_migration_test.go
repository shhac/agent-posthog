package credential

import (
	"errors"
	"testing"

	"github.com/shhac/agent-posthog/internal/config"
)

func TestKeychainMigrationReadsNewServiceFirst(t *testing.T) {
	store := testKeychain(t)
	store[keychainService]["prod"] = "phx_new"
	store[legacyKeychainService]["prod"] = "phx_legacy"

	token, err := GetWithMigration("prod", true)
	if err != nil {
		t.Fatal(err)
	}
	if token != "phx_new" {
		t.Fatalf("token = %q, want phx_new", token)
	}
}

func TestKeychainMigrationRequiresExplicitMigrationForLegacyOnly(t *testing.T) {
	store := testKeychain(t)
	store[legacyKeychainService]["prod"] = "phx_legacy"

	_, err := GetWithMigration("prod", true)
	var migrationErr *MigrationRequiredError
	if !errors.As(err, &migrationErr) {
		t.Fatalf("err = %v, want MigrationRequiredError", err)
	}
	if migrationErr.Hint() != "Run 'agent-posthog auth --migrate' to migrate stored credentials." {
		t.Fatalf("hint = %q", migrationErr.Hint())
	}
}

func TestKeychainMigrationNoMigrateFallsBackSilently(t *testing.T) {
	store := testKeychain(t)
	store[legacyKeychainService]["prod"] = "phx_legacy"

	token, err := GetWithMigration("prod", false)
	if err != nil {
		t.Fatal(err)
	}
	if token != "phx_legacy" {
		t.Fatalf("token = %q, want phx_legacy", token)
	}
}

func TestKeychainMigrationMovesLegacyCredential(t *testing.T) {
	store := testKeychain(t)
	store[legacyKeychainService]["prod"] = "phx_legacy"
	index := map[string]credentialEntry{"prod": {KeychainManaged: true}}
	if err := writeIndex(index); err != nil {
		t.Fatal(err)
	}

	migrated, err := MigrateLegacyCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if migrated != 1 {
		t.Fatalf("migrated = %d, want 1", migrated)
	}
	if got := store[keychainService]["prod"]; got != "phx_legacy" {
		t.Fatalf("new service token = %q, want phx_legacy", got)
	}
	if _, ok := store[legacyKeychainService]["prod"]; ok {
		t.Fatal("legacy credential was not deleted")
	}
}

func testKeychain(t *testing.T) map[string]map[string]string {
	t.Helper()
	t.Cleanup(func() {
		config.SetConfigDir("")
		config.ClearCache()
		keychainStoreForService = platformKeychainStore
		keychainGetForService = platformKeychainGet
		keychainDeleteForService = platformKeychainDelete
	})
	config.SetConfigDir(t.TempDir())
	store := map[string]map[string]string{
		keychainService:       {},
		legacyKeychainService: {},
	}
	keychainStoreForService = func(service, name, token string) error {
		if store[service] == nil {
			store[service] = map[string]string{}
		}
		store[service][name] = token
		return nil
	}
	keychainGetForService = func(service, name string) (string, error) {
		if token, ok := store[service][name]; ok {
			return token, nil
		}
		return "", errors.New("not found")
	}
	keychainDeleteForService = func(service, name string) error {
		delete(store[service], name)
		return nil
	}
	return store
}
