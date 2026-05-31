package credential

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shhac/agent-posthog/internal/config"
)

type credentialEntry struct {
	KeychainManaged bool `json:"keychain_managed"`
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

func Store(name, apiKey string) (string, error) {
	if err := keychainStore(name, apiKey); err != nil {
		return "", err
	}

	index, err := readIndex()
	if err != nil {
		return "", err
	}
	index[name] = credentialEntry{KeychainManaged: true}
	if err := writeIndex(index); err != nil {
		return "", err
	}
	return "keychain", nil
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
	if !entry.KeychainManaged {
		return "", fmt.Errorf("profile %q is not keychain managed", name)
	}
	return keychainGet(name)
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
