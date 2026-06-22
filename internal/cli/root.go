package cli

import (
	"github.com/spf13/cobra"

	libcli "github.com/shhac/lib-agent-cli/cli"
	agentmcp "github.com/shhac/lib-agent-mcp"

	"github.com/shhac/agent-posthog/internal/config"
	"github.com/shhac/agent-posthog/internal/output"
)

// GlobalFlags carries the persistent flags shared by every command. The shared
// --format/--timeout/--debug live in the embedded libcli.Globals; the rest are
// PostHog domain flags (credential profile, org/project/environment scope, the
// instance host, and a direct API-key override).
type GlobalFlags struct {
	libcli.Globals // Format, TimeoutMS, Debug

	Profile       string
	OrgID         string
	ProjectID     int
	EnvironmentID int
	Host          string
	APIKey        string
	MaxRetries    int
	Full          bool
}

func newRootCmd(version string) *cobra.Command {
	globals := &GlobalFlags{}

	var root *cobra.Command
	root = libcli.NewRoot(libcli.Options{
		Use:            "agent-posthog",
		Short:          "PostHog product analytics CLI for AI agents",
		Version:        version,
		Globals:        &globals.Globals,
		DefaultFormat:  output.FormatNDJSON,
		ConfigDefaults: func() { applyConfiguredDefaults(root, globals) },
		UnknownHint:    "run 'agent-posthog usage' to see the available domains",
	})

	pf := root.PersistentFlags()
	pf.StringVarP(&globals.Profile, "profile", "p", "", "Profile alias (or AGENT_POSTHOG_PROFILE)")
	pf.StringVar(&globals.OrgID, "org", "", "PostHog organization ID override")
	pf.IntVar(&globals.ProjectID, "project", 0, "PostHog project ID override")
	pf.IntVar(&globals.EnvironmentID, "env", 0, "PostHog environment ID override")
	pf.StringVar(&globals.Host, "host", "", "PostHog private API host override")
	pf.StringVar(&globals.APIKey, "api-key", "", "Personal API key override; never printed or persisted")
	pf.IntVar(&globals.MaxRetries, "max-retries", 2, "Maximum automatic retries for transient responses")
	pf.BoolVar(&globals.Full, "full", false, "Return fuller API payloads where supported")

	registerUsageCommand(root)
	registerCompletion(root)
	registerConfig(root)
	registerAuth(root, globals)
	registerOrgs(root, globals)
	registerProjects(root, globals)
	registerEnvironments(root, globals)
	registerSchema(root, globals)
	registerQuery(root, globals)
	registerPersons(root, globals)
	registerFlags(root, globals)
	registerGenericDomains(root, globals)
	registerInvestigate(root, globals)
	registerAPI(root, globals)

	installUnknownSubcommandHandlers(root)

	// Expose the whole command tree as an MCP server (added last, so it reflects
	// the complete tree). --color/--expose are output-shaping, irrelevant to a
	// tool call, so hide them from the generated schemas.
	root.AddCommand(agentmcp.Command(root, agentmcp.WithHiddenFlags("color", "expose")))

	return root
}

// installUnknownSubcommandHandlers walks every command group (a command with
// subcommands but no action of its own) and installs the family's structured
// unknown-subcommand handler, so 'agent-posthog <group> bogus' returns a
// fixable_by:agent error listing the valid subcommands instead of plain help.
// The root already has its own handler from NewRoot, so it is skipped.
func installUnknownSubcommandHandlers(root *cobra.Command) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, sub := range cmd.Commands() {
			if !sub.HasSubCommands() {
				continue
			}
			if sub.RunE == nil && sub.Run == nil {
				libcli.HandleUnknownCommand(sub, "run 'agent-posthog usage' to see the available domains")
			}
			walk(sub)
		}
	}
	walk(root)
}

// Run builds the root command and executes it, rendering any bubbled error as
// the family's structured JSON on stderr and exiting non-zero on failure.
func Run(version string) {
	libcli.Run(newRootCmd(version))
}

func applyConfiguredDefaults(root *cobra.Command, globals *GlobalFlags) {
	cfg := config.Read()
	flags := root.PersistentFlags()
	if cfg.Defaults.TimeoutMS != nil && !flags.Changed("timeout") {
		globals.TimeoutMS = *cfg.Defaults.TimeoutMS
	}
	if cfg.Defaults.MaxRetries != nil && !flags.Changed("max-retries") {
		globals.MaxRetries = *cfg.Defaults.MaxRetries
	}
	output.SetExpose(globals.Expose)
}
