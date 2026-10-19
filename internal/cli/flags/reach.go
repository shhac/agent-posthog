package flags

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
	"github.com/shhac/agent-posthog/internal/featureflag"
	"github.com/shhac/agent-posthog/internal/output"
)

func reachCommand(globals *shared.GlobalFlags) *cobra.Command {
	var condition int
	cmd := &cobra.Command{
		Use:   "reach <id-or-key>",
		Short: "Count users each condition's filters match (ignores rollout; conditions overlap)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			only := -1
			if cmd.Flags().Changed("condition") {
				only = condition
			}
			return WithProject(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				flag, err := p.Fetch(ctx, args[0])
				if err != nil {
					return err
				}
				reaches, err := p.Reach(ctx, flag, only)
				if err != nil {
					return err
				}
				lines := make([]any, 0, len(reaches))
				for _, r := range reaches {
					line := output.Ordered{}
					line = append(line, output.Field{Key: "index", Value: r.Index})
					line = append(line, output.Field{Key: "matching", Value: r.Matching})
					line = append(line, output.Field{Key: "total", Value: r.Total})
					line = append(line, output.Field{Key: "rollout", Value: r.Rollout})
					line = append(line, output.Field{Key: "aggregation_group_type_index", Value: r.Aggregation})
					lines = append(lines, line)
				}
				note := "matching counts users whose properties pass a condition's filters, before its rollout; conditions overlap, so do not sum them"
				return shared.WriteExactList(lines, map[string]any{"@note": note}, globals.Format)
			})
		},
	}
	cmd.Flags().IntVar(&condition, "condition", 0, "Only this condition index")
	return cmd
}
