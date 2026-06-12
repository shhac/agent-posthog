//go:build darwin

package credential

import (
	"fmt"
	"os/exec"
	"strings"
)

const keychainService = "app.paulie.agent-posthog"

// KEYCHAIN-MIGRATION: Remove this legacy service constant after the migration window.
const legacyKeychainService = "agent-posthog"

// KEYCHAIN-MIGRATION: Service-specific helpers let the temporary migration code read/write both names.
var (
	keychainStoreForService  = platformKeychainStore
	keychainGetForService    = platformKeychainGet
	keychainDeleteForService = platformKeychainDelete
)

func keychainStore(name, apiKey string) error {
	_ = keychainDelete(name)
	return keychainStoreForService(keychainService, name, apiKey)
}

func platformKeychainStore(service, name, apiKey string) error {
	cmd := exec.Command("security", "add-generic-password", "-a", name, "-s", service, "-w", apiKey, "-U")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("store credential in Keychain: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func keychainGet(name string) (string, error) {
	return keychainGetForService(keychainService, name)
}

func platformKeychainGet(service, name string) (string, error) {
	cmd := exec.Command("security", "find-generic-password", "-a", name, "-s", service, "-w")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read credential from Keychain: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func keychainDelete(name string) error {
	return keychainDeleteForService(keychainService, name)
}

func platformKeychainDelete(service, name string) error {
	cmd := exec.Command("security", "delete-generic-password", "-a", name, "-s", service)
	_ = cmd.Run()
	return nil
}
