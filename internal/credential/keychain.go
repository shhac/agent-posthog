package credential

import "github.com/shhac/lib-agent-cli/creds"

// keychainService is the reverse-domain service name this CLI owns in the macOS
// keychain. The shared creds package is service-agnostic; we pass ours in.
const keychainService = "app.paulie.agent-posthog"

// MCPKeychainService is the Keychain service for the MCP server's local-OAuth
// secrets — the CLI's service plus a ".mcp" namespace, separate from the API creds.
func MCPKeychainService() string { return keychainService + ".mcp" }

var keychain = creds.NewKeychain(keychainService)
