package cli

import (
	"context"
	"strconv"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/featureflag"
)

func registerFlagWrites(flags *cobra.Command, globals *GlobalFlags) {
	for _, l := range []struct{ use, short, operation string }{
		{"enable <id-or-key>", "Enable a feature flag", "enable"},
		{"disable <id-or-key>", "Disable a feature flag", "disable"},
		{"archive <id-or-key>", "Disable and archive a feature flag", "archive"},
		{"unarchive <id-or-key>", "Unarchive a feature flag (it stays disabled)", "unarchive"},
		{"delete <id-or-key>", "Soft-delete a feature flag (restore it by ID)", "delete"},
	} {
		flags.AddCommand(lifecycleCommand(globals, l.use, l.short, l.operation))
	}
	flags.AddCommand(restoreCommand(globals))
	flags.AddCommand(applyPlanCommand(globals))
	flags.AddCommand(earlyExitCommand(globals))
}

func lifecycleCommand(globals *GlobalFlags, use, short, operation string) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withFlags(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				return editFlag(ctx, p, args[0], operation, opts, p.Lifecycle(ctx, operation), globals.Format)
			})
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}

func restoreCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   "restore <id>",
		Long:  "Restores by numeric ID (or id:<n>); deleted flags cannot be found by key.",
		Short: "Restore a soft-deleted feature flag by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.refuseSavePlan("restore"); err != nil {
				return err
			}
			ref, _, err := featureflag.ParseRef(args[0])
			if err != nil || ref == "" {
				return agenterrors.New("restore needs the numeric flag ID; deleted flags cannot be looked up by key", agenterrors.FixableByAgent)
			}
			id, _ := strconv.Atoi(ref)
			return withFlags(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				return writeFlag(ctx, p.PlanRestore(id), opts, globals.Format)
			})
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}

func earlyExitCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   "early-exit <id-or-key> <on|off>",
		Short: "Stop evaluation at the first condition whose properties match, even if its rollout excludes the user",
		Long: "With early exit on, a condition whose properties match but whose rollout excludes the user " +
			"returns false instead of falling through to later conditions, so a 0% condition works as a deny rule. " +
			"PostHog gates it per organization, and SDK local evaluation honors it only in recent SDK versions.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[1] != "on" && args[1] != "off" {
				return agenterrors.New("early-exit takes on or off", agenterrors.FixableByAgent)
			}
			return withFlags(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				return editFlag(ctx, p, args[0], "early-exit", opts, featureflag.EarlyExit(args[1] == "on"), globals.Format)
			})
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}
