package cli

import (
	"context"
	"fmt"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

func reachCommand(globals *GlobalFlags) *cobra.Command {
	var condition int
	cmd := &cobra.Command{
		Use:   "reach <id-or-key>",
		Short: "Count users each condition's filters match (ignores rollout; conditions overlap)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				flag, err := currentFlag(ctx, resolved, args[0])
				if err != nil {
					return err
				}
				filters := asObject(flag["filters"])
				groups := asList(filters["groups"])
				indexes := make([]int, 0, len(groups))
				for i := range groups {
					if !cmd.Flags().Changed("condition") || i == condition {
						indexes = append(indexes, i)
					}
				}
				if len(indexes) == 0 {
					return agenterrors.New(fmt.Sprintf("the flag has no condition %d", condition), agenterrors.FixableByAgent)
				}
				for _, i := range indexes {
					group := asObject(groups[i])
					aggregation, ok := group["aggregation_group_type_index"]
					if !ok {
						aggregation = filters["aggregation_group_type_index"]
					}
					body := map[string]any{"condition": group, "group_type_index": aggregation}
					raw, err := resolved.Client.Post(ctx, flagsPath(resolved.ProjectID, "user_blast_radius"), nil, body)
					if err != nil {
						return err
					}
					counts, err := flagspec.DecodeObject(raw)
					if err != nil {
						return agenterrors.Wrap(err, agenterrors.FixableByRetry)
					}
					line := flagspec.NewOrdered()
					line.Set("index", i)
					line.Set("matching", counts["affected"])
					line.Set("total", counts["total"])
					line.Set("rollout", group["rollout_percentage"])
					line.Set("aggregation_group_type_index", aggregation)
					if err := writeExactLine(line); err != nil {
						return err
					}
				}
				return writeExactLine(map[string]string{"@note": "matching counts users whose properties pass a condition's filters, before its rollout; conditions overlap, so do not sum them"})
			})
		},
	}
	cmd.Flags().IntVar(&condition, "condition", 0, "Only this condition index")
	return cmd
}
