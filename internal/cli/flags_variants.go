package cli

import (
	"context"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/featureflag"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func registerFlagVariantCommands(flags *cobra.Command, globals *GlobalFlags) {
	variants := &cobra.Command{Use: "variants", Short: "Edit a multivariate flag's variants"}
	variants.AddCommand(variantsSetCommand(globals))
	flags.AddCommand(variants)
}

func variantsSetCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   "set <id-or-key> <key=percent>...",
		Short: "Replace a multivariate flag's variants (percents sum to 100); payloads of removed variants are dropped",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed, err := parseVariantArgs(args[1:])
			if err != nil {
				return err
			}
			if err := flagspec.ValidateVariants(parsed); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			return withFlags(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				return editFlag(ctx, p, args[0], "variants set", opts, featureflag.SetVariants(parsed), globals.Format)
			})
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}
