package credential

import (
	"fmt"
	"strings"
)

const migrateCommand = "agent-posthog auth --migrate"

// MigrationRequiredError reports that a profile can only be read from the legacy
// Keychain service name until the user explicitly runs the migration command.
type MigrationRequiredError struct {
	Name string
}

func (e *MigrationRequiredError) Error() string {
	return fmt.Sprintf("credentials for profile %q were found under old Keychain service %q and must be migrated to %q", e.Name, legacyKeychainService, keychainService)
}

func (e *MigrationRequiredError) Hint() string {
	return fmt.Sprintf("Run '%s' to migrate stored credentials.", migrateCommand)
}

// GetWithMigration reads the current service first, then handles legacy-service
// credentials according to requireMigration.
func GetWithMigration(name string, requireMigration bool) (string, error) {
	token, err := keychainGetForService(keychainService, name)
	if validCredential(token) {
		return token, nil
	}

	legacyToken, legacyErr := keychainGetForService(legacyKeychainService, name)
	if !validCredential(legacyToken) {
		if err != nil {
			return "", err
		}
		return "", legacyErr
	}
	if requireMigration {
		return "", &MigrationRequiredError{Name: name}
	}
	return legacyToken, nil
}

// MigrateLegacyCredentials copies legacy-service credentials for every indexed
// profile to the current service and deletes each migrated legacy entry.
func MigrateLegacyCredentials() (int, error) {
	index, err := readIndex()
	if err != nil {
		return 0, err
	}
	migrated := 0
	for name, entry := range index {
		if !entry.KeychainManaged {
			continue
		}
		if token, err := keychainGetForService(keychainService, name); err == nil && validCredential(token) {
			continue
		}
		token, err := keychainGetForService(legacyKeychainService, name)
		if err != nil || !validCredential(token) {
			continue
		}
		if err := keychainStoreForService(keychainService, name, token); err != nil {
			return migrated, err
		}
		if err := keychainDeleteForService(legacyKeychainService, name); err != nil {
			return migrated, err
		}
		migrated++
	}
	return migrated, nil
}

func validCredential(token string) bool {
	return strings.TrimSpace(token) != ""
}
