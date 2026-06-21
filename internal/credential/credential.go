package credential

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shhac/agent-posthog/internal/config"
)

// keychainSentinel is stored in the index in place of the real key when the
// secret lives in the macOS keychain. When the keychain is unavailable (non-
// macOS, or opted out via AGENT_POSTHOG_NO_KEYCHAIN / LIB_AGENT_NO_KEYCHAIN),
// the raw key is kept in the 0600 index file instead.
const keychainSentinel = "__KEYCHAIN__"

type credentialEntry struct {
	APIKey          string `json:"api_key,omitempty"`
	KeychainManaged bool   `json:"keychain_managed"`
}

type NotFoundError struct {
	Name string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("profile credential %q not found", e.Name)
}

func credentialsPath() string {
	return filepath.Join(config.ConfigDir(), "credentials.json")
}

// Store persists a credential. It prefers the macOS keychain; when the keychain
// is unavailable (non-macOS, or opted out), it keeps the raw key in the 0600
// index file instead. Returns "keychain" or "file" so the caller can surface the
// choice.
func Store(name, apiKey string) (string, error) {
	index, err := readIndex()
	if err != nil {
		return "", err
	}

	entry := credentialEntry{APIKey: apiKey}
	storage := "file"
	if err := keychainStore(name, apiKey); err == nil {
		entry.APIKey = keychainSentinel
		entry.KeychainManaged = true
		storage = "keychain"
	}

	index[name] = entry
	if err := writeIndex(index); err != nil {
		return "", err
	}
	return storage, nil
}

func Get(name string) (string, error) {
	index, err := readIndex()
	if err != nil {
		return "", err
	}
	entry, ok := index[name]
	if !ok {
		return "", &NotFoundError{Name: name}
	}
	if entry.KeychainManaged {
		return keychainGet(name)
	}
	return entry.APIKey, nil
}

func Remove(name string) error {
	index, err := readIndex()
	if err != nil {
		return err
	}
	entry, ok := index[name]
	if !ok {
		return &NotFoundError{Name: name}
	}
	if entry.KeychainManaged {
		_ = keychainDelete(name)
	}
	delete(index, name)
	return writeIndex(index)
}

func List() ([]string, error) {
	index, err := readIndex()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(index))
	for name := range index {
		names = append(names, name)
	}
	return names, nil
}

func readIndex() (map[string]credentialEntry, error) {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]credentialEntry), nil
		}
		return nil, err
	}
	var index map[string]credentialEntry
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}
	if index == nil {
		index = make(map[string]credentialEntry)
	}
	return index, nil
}

func writeIndex(index map[string]credentialEntry) error {
	dir := config.ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(credentialsPath(), append(data, '\n'), 0o600)
}
