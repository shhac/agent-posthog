package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/config"
	"github.com/shhac/agent-posthog/internal/output"
)

type GlobalFlags struct {
	Profile       string
	OrgID         string
	ProjectID     int
	EnvironmentID int
	Host          string
	APIKey        string
	Format        string
	Timeout       int
	MaxRetries    int
	Debug         bool
	Full          bool
}

func newRootCmd(version string) *cobra.Command {
	globals := &GlobalFlags{}
	root := &cobra.Command{
		Use:           "agent-posthog",
		Short:         "PostHog product analytics CLI for AI agents",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			applyConfiguredDefaults(cmd, globals)
		},
	}

	root.PersistentFlags().StringVarP(&globals.Profile, "profile", "p", "", "Profile alias (or AGENT_POSTHOG_PROFILE)")
	root.PersistentFlags().StringVar(&globals.OrgID, "org", "", "PostHog organization ID override")
	root.PersistentFlags().IntVar(&globals.ProjectID, "project", 0, "PostHog project ID override")
	root.PersistentFlags().IntVar(&globals.EnvironmentID, "env", 0, "PostHog environment ID override")
	root.PersistentFlags().StringVar(&globals.Host, "host", "", "PostHog private API host override")
	root.PersistentFlags().StringVar(&globals.APIKey, "api-key", "", "Personal API key override; never printed or persisted")
	root.PersistentFlags().StringVarP(&globals.Format, "format", "f", "", "Output format: json, yaml, jsonl")
	root.PersistentFlags().IntVarP(&globals.Timeout, "timeout", "t", 0, "Request timeout in milliseconds")
	root.PersistentFlags().IntVar(&globals.MaxRetries, "max-retries", 2, "Maximum automatic retries for transient responses")
	root.PersistentFlags().BoolVarP(&globals.Debug, "debug", "d", false, "Log redacted HTTP debug records to stderr")
	root.PersistentFlags().BoolVar(&globals.Full, "full", false, "Return fuller API payloads where supported")

	registerUsageCommand(root)
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

	return root
}

func applyConfiguredDefaults(cmd *cobra.Command, globals *GlobalFlags) {
	cfg := config.Read()
	flags := cmd.Root().PersistentFlags()
	if cfg.Defaults.TimeoutMS != nil && !flags.Changed("timeout") {
		globals.Timeout = *cfg.Defaults.TimeoutMS
	}
	if cfg.Defaults.MaxRetries != nil && !flags.Changed("max-retries") {
		globals.MaxRetries = *cfg.Defaults.MaxRetries
	}
}

func Execute(version string) error {
	err := newRootCmd(version).Execute()
	if err != nil {
		_, _ = fmt.Fprintln(output.Stderr(), err)
	}
	return err
}
