//go:build !darwin

package credential

import "fmt"

func keychainStore(name, apiKey string) error {
	return fmt.Errorf("Keychain credential storage is only implemented on macOS")
}

func keychainGet(name string) (string, error) {
	return "", fmt.Errorf("Keychain credential storage is only implemented on macOS")
}

func keychainDelete(name string) error {
	return nil
}
