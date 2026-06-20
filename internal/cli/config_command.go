package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/config"
	agenterrors "github.com/shhac/agent-posthog/internal/errors"
)

func registerConfig(root *cobra.Command) {
	cfg := &cobra.Command{
		Use:   "config",
		Short: "Inspect and update non-secret agent-posthog config",
	}
	cfg.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print config path",
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeItem(map[string]any{"config_path": config.ConfigPath()}, "")
		},
	})
	cfg.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show non-secret config",
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeItem(config.Read(), "")
		},
	})
	cfg.AddCommand(configSetCommand())
	cfg.AddCommand(configUnsetCommand())
	root.AddCommand(cfg)
}

func configSetCommand() *cobra.Command {
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
			return writeItem(map[string]any{"status": "set", "key": args[0], "value": value}, "")
		},
	}
	return cmd
}

func configUnsetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <timeout_ms|max_retries>",
		Short: "Unset a default config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.UnsetDefaultValue(args[0]); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			return writeItem(map[string]any{"status": "unset", "key": args[0]}, "")
		},
	}
}
