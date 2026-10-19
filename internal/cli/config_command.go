package cli

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/config"
	agenterrors "github.com/shhac/agent-posthog/internal/errors"
)

func registerConfig(root *cobra.Command, globals *GlobalFlags) {
	cfg := &cobra.Command{
		Use:   "config",
		Short: "Inspect and update non-secret agent-posthog config",
	}
	cfg.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print config path",
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeItem(map[string]any{"config_path": config.ConfigPath()}, globals.Format)
		},
	})
	cfg.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show non-secret config",
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeItem(config.Read(), globals.Format)
		},
	})
	cfg.AddCommand(configSetCommand(globals))
	cfg.AddCommand(configUnsetCommand(globals))
	cfg.AddCommand(aliasCommand(globals))
	cfg.AddCommand(mcpWritesCommand(globals))
	root.AddCommand(cfg)
}

func configSetCommand(globals *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <timeout_ms|max_retries> <value>",
		Short: "Set an integer default config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var value int
			if _, err := fmt.Sscanf(args[1], "%d", &value); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			if err := config.SetDefaultValue(args[0], value); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			return writeItem(map[string]any{"status": "set", "key": args[0], "value": value}, globals.Format)
		},
	}
	return cmd
}

func configUnsetCommand(globals *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "unset <timeout_ms|max_retries>",
		Short: "Unset a default config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.UnsetDefaultValue(args[0]); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			return writeItem(map[string]any{"status": "unset", "key": args[0]}, globals.Format)
		},
	}
}

// aliasCommand manages per-profile project aliases, so a separate staging
// project can be addressed as --project staging.
func aliasCommand(globals *GlobalFlags) *cobra.Command {
	alias := &cobra.Command{
		Use:   "alias",
		Short: "Name projects within a profile (e.g. staging, prod) for --project",
	}
	alias.AddCommand(&cobra.Command{
		Use:   "set <name> <project-id>",
		Short: "Name a project ID in the current profile",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, err := aliasProfile(globals)
			if err != nil {
				return err
			}
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
			return writeItem(map[string]any{"status": "set", "profile": profile, "alias": name, "project_id": id}, globals.Format)
		},
	})
	alias.AddCommand(&cobra.Command{
		Use:   "unset <name>",
		Short: "Remove a project alias from the current profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, err := aliasProfile(globals)
			if err != nil {
				return err
			}
			if err := config.UnsetProjectAlias(profile, args[0]); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			return writeItem(map[string]any{"status": "unset", "profile": profile, "alias": args[0]}, globals.Format)
		},
	})
	alias.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the current profile's project aliases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, err := aliasProfile(globals)
			if err != nil {
				return err
			}
			aliases := config.Read().Profiles[profile].ProjectAliases
			if aliases == nil {
				aliases = map[string]int{}
			}
			return writeItem(map[string]any{"profile": profile, "aliases": aliases}, globals.Format)
		},
	})
	return alias
}

func aliasProfile(globals *GlobalFlags) (string, error) {
	cfg := config.Read()
	profile := selectedProfile(globals, cfg)
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

func mcpWritesCommand(globals *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp-writes <on|off|status>",
		Short: "Allow or block PostHog writes from commands run through the MCP server (off by default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "on", "off":
				if err := config.SetMCPWrites(args[0] == "on"); err != nil {
					return agenterrors.Wrap(err, agenterrors.FixableByHuman)
				}
			case "status":
			default:
				return agenterrors.New("mcp-writes takes on, off, or status", agenterrors.FixableByAgent)
			}
			return writeItem(map[string]any{"mcp_writes": config.Read().MCPWrites}, globals.Format)
		},
	}
}
