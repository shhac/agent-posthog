package credential

import (
	"fmt"
	"path/filepath"

	"github.com/shhac/agent-posthog/internal/config"
	"github.com/shhac/lib-agent-cli/creds"
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

// store is the credential index's file: 0600 writes into a 0700 parent, atomic
// replacement, and Update for a locked read-modify-write. This used to be
// hand-rolled with os.ReadFile/os.WriteFile, which carried a lost-update race —
// two concurrent writers could each build their write from a stale snapshot,
// and the loser's entry vanished while its secret stayed in the keychain,
// unreferenced and un-removable (auth list can't show it, auth remove can't
// look it up).
func store() creds.Store {
	return creds.Store{Path: credentialsPath()}
}

// Store persists a credential. It prefers the macOS keychain; when the keychain
// is unavailable (non-macOS, or opted out), it keeps the raw key in the 0600
// index file instead. Returns "keychain" or "file" so the caller can surface the
// choice.
func Store(name, apiKey string) (string, error) {
	entry := credentialEntry{APIKey: apiKey}
	storage := "file"
	if err := keychainStore(name, apiKey); err == nil {
		entry.APIKey = keychainSentinel
		entry.KeychainManaged = true
		storage = "keychain"
	}

	// The index write is the step that must not race: the keychain already
	// holds the secret by now, so an entry lost to a concurrent writer leaves
	// that secret referenced by nothing.
	if err := updateIndex(func(index map[string]credentialEntry) error {
		index[name] = entry
		return nil
	}); err != nil {
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
	return updateIndex(func(index map[string]credentialEntry) error {
		entry, ok := index[name]
		if !ok {
			return &NotFoundError{Name: name}
		}
		if entry.KeychainManaged {
			_ = keychainDelete(name)
		}
		delete(index, name)
		return nil
	})
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
	index := map[string]credentialEntry{}
	if err := store().Load(&index); err != nil {
		return nil, err
	}
	if index == nil {
		index = map[string]credentialEntry{}
	}
	return index, nil
}

// updateIndex applies mutate to the index under an exclusive lock, so two
// concurrent `auth add`/`auth remove` invocations serialize instead of
// clobbering each other. An error from mutate aborts before anything is
// persisted, which is what keeps Remove of an unknown name a no-op.
func updateIndex(mutate func(index map[string]credentialEntry) error) error {
	index := map[string]credentialEntry{}
	return store().Update(&index, func() error {
		if index == nil {
			index = map[string]credentialEntry{}
		}
		return mutate(index)
	})
}
