package cli

import (
	libcli "github.com/shhac/lib-agent-cli/cli"
)

// GlobalFlags carries the persistent flags shared by every command. The shared
// --format/--timeout/--debug live in the embedded libcli.Globals; the rest are
// PostHog domain flags (credential profile, org/project/environment scope, the
// instance host, and a direct API-key override).
type GlobalFlags struct {
	libcli.Globals // Format, TimeoutMS, Debug

	Profile       string
	OrgID         string
	Project       string
	EnvironmentID int
	Host          string
	APIKey        string
	MaxRetries    int
	Full          bool
}
