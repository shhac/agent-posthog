package credential

import (
	"fmt"

	"github.com/shhac/lib-agent-cli/creds"
)

// keychainService is the reverse-domain service name this CLI owns in the macOS
// keychain. The shared creds package is service-agnostic; we pass ours in.
const keychainService = "app.paulie.agent-posthog"

var keychain = creds.NewKeychain(keychainService)

func keychainStore(name, apiKey string) error {
	return keychain.Set(name, apiKey)
}

func keychainGet(name string) (string, error) {
	v, ok := keychain.Get(name)
	if !ok {
		return "", fmt.Errorf("read credential %q from Keychain: not found", name)
	}
	return v, nil
}

func keychainDelete(name string) error {
	_ = keychain.Delete(name)
	return nil
}
