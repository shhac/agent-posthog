package flags

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
	"github.com/shhac/agent-posthog/internal/featureflag"
)

func applyPlanCommand(globals *shared.GlobalFlags) *cobra.Command {
	var opts writeOptions
	var file string
	cmd := &cobra.Command{
		Use:   "apply-plan --file <plan.json>",
		Short: "Apply a plan saved with --save-plan, only if the flag is unchanged since",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.refuseSavePlan("apply-plan"); err != nil {
				return err
			}
			plan, err := featureflag.ReadPlan(file)
			if err != nil {
				return err
			}
			return opts.run(cmd, globals, func(ctx context.Context, p featureflag.Project) (*featureflag.Pending, error) {
				return p.PlanSaved(ctx, plan)
			})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "Plan file written by --save-plan")
	_ = cmd.MarkFlagRequired("file")
	addWriteFlags(cmd, &opts)
	return cmd
}
