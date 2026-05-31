//go:build darwin

package credential

import (
	"fmt"
	"os/exec"
	"strings"
)

const serviceName = "agent-posthog"

func keychainStore(name, apiKey string) error {
	_ = keychainDelete(name)
	cmd := exec.Command("security", "add-generic-password", "-a", name, "-s", serviceName, "-w", apiKey, "-U")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("store credential in Keychain: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func keychainGet(name string) (string, error) {
	cmd := exec.Command("security", "find-generic-password", "-a", name, "-s", serviceName, "-w")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read credential from Keychain: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func keychainDelete(name string) error {
	cmd := exec.Command("security", "delete-generic-password", "-a", name, "-s", serviceName)
	_ = cmd.Run()
	return nil
}
