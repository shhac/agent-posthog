package cli

import (
	"strconv"
	"strings"
	"unicode"

	libcli "github.com/shhac/lib-agent-cli/cli"
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
	"github.com/shhac/agent-posthog/internal/config"
)

// registerConfig builds the config group on the family's ConfigCommand
// (get/set/unset/list over configKeys) and adds the bespoke path, show, and
// alias commands.
func registerConfig(root *cobra.Command, globals *shared.GlobalFlags) {
	cfg := libcli.ConfigCommand(&globals.Globals, configKeys())
	cfg.Short = "Inspect and update non-secret agent-posthog config"
	cfg.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print config path",
		RunE: func(cmd *cobra.Command, args []string) error {
			return shared.WriteItem(map[string]any{"config_path": config.ConfigPath()}, globals.Format)
		},
	})
	cfg.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show non-secret config",
		RunE: func(cmd *cobra.Command, args []string) error {
			return shared.WriteItem(config.Read(), globals.Format)
		},
	})
	cfg.AddCommand(aliasCommand(globals))
	root.AddCommand(cfg)
}

func configKeys() []libcli.ConfigKey {
	return []libcli.ConfigKey{
		intKey("timeout_ms", "Request timeout in milliseconds", func(d config.Defaults) *int { return d.TimeoutMS }),
		intKey("max_retries", "Automatic retries for transient 429/5xx responses", func(d config.Defaults) *int { return d.MaxRetries }),
		{
			Name:        "mcp_writes",
			Description: "Allow PostHog writes from commands run through the MCP server (off by default)",
			Get: func() (string, bool) {
				if config.Read().MCPWrites {
					return "on", true
				}
				return "off", false
			},
			Set: func(raw string) error {
				switch raw {
				case "on":
					return config.SetMCPWrites(true)
				case "off":
					return config.SetMCPWrites(false)
				}
				return agenterrors.New("mcp_writes takes on or off", agenterrors.FixableByAgent)
			},
			Unset: func() error { return config.SetMCPWrites(false) },
		},
	}
}

func intKey(name, description string, current func(config.Defaults) *int) libcli.ConfigKey {
	return libcli.ConfigKey{
		Name:        name,
		Description: description,
		Get: func() (string, bool) {
			value := current(config.Read().Defaults)
			if value == nil {
				return "", false
			}
			return strconv.Itoa(*value), true
		},
		Set: func(raw string) error {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 0 {
				return agenterrors.New(name+" takes a whole number", agenterrors.FixableByAgent)
			}
			return config.SetDefaultValue(name, value)
		},
		Unset: func() error { return config.UnsetDefaultValue(name) },
	}
}

// aliasCommand manages per-profile project aliases, so a separate staging
// project can be addressed as --project staging.
func aliasCommand(globals *shared.GlobalFlags) *cobra.Command {
	alias := &cobra.Command{
		Use:   "alias",
		Short: "Name projects within a profile (e.g. staging, prod) for --project",
	}
	alias.AddCommand(&cobra.Command{
		Use:   "set <name> <project-id>",
		Short: "Name a project ID in the current profile",
		Args:  cobra.ExactArgs(2),
		RunE: withAliasProfile(globals, func(profile string, args []string) error {
			name := args[0]
			if _, err := strconv.Atoi(name); err == nil || name == "" || strings.ContainsFunc(name, unicode.IsSpace) {
				return agenterrors.New("alias names must be non-numeric words without spaces", agenterrors.FixableByAgent)
			}
			id, err := strconv.Atoi(args[1])
			if err != nil || id <= 0 {
				return agenterrors.New("project ID must be a positive number", agenterrors.FixableByAgent)
			}
			if err := config.SetProjectAlias(profile, name, id); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			return shared.WriteItem(map[string]any{"status": "set", "profile": profile, "alias": name, "project_id": id}, globals.Format)
		}),
	})
	alias.AddCommand(&cobra.Command{
		Use:   "unset <name>",
		Short: "Remove a project alias from the current profile",
		Args:  cobra.ExactArgs(1),
		RunE: withAliasProfile(globals, func(profile string, args []string) error {
			if err := config.UnsetProjectAlias(profile, args[0]); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			return shared.WriteItem(map[string]any{"status": "unset", "profile": profile, "alias": args[0]}, globals.Format)
		}),
	})
	alias.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the current profile's project aliases",
		Args:  cobra.NoArgs,
		RunE: withAliasProfile(globals, func(profile string, _ []string) error {
			aliases := config.Read().Profiles[profile].ProjectAliases
			if aliases == nil {
				aliases = map[string]int{}
			}
			return shared.WriteItem(map[string]any{"profile": profile, "aliases": aliases}, globals.Format)
		}),
	})
	return alias
}

// withAliasProfile runs an alias command against the selected profile, which
// must exist, since aliases live inside it.
func withAliasProfile(globals *shared.GlobalFlags, run func(profile string, args []string) error) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, args []string) error {
		profile, err := aliasProfile(globals)
		if err != nil {
			return err
		}
		return run(profile, args)
	}
}

func aliasProfile(globals *shared.GlobalFlags) (string, error) {
	cfg := config.Read()
	profile := shared.SelectedProfile(globals, cfg)
	if profile == "" {
		return "", agenterrors.New("project aliases belong to a profile, and none is selected", agenterrors.FixableByAgent).
			WithHint("Pass -p <profile>, or create one with 'agent-posthog auth add <profile> --form'.")
	}
	if _, ok := cfg.Profiles[profile]; !ok {
		return "", agenterrors.New("profile "+profile+" is not configured", agenterrors.FixableByAgent).
			WithHint("Run 'agent-posthog auth list'.")
	}
	return profile, nil
}
