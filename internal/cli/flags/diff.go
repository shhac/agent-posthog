package flags

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func diffCommand(globals *shared.GlobalFlags) *cobra.Command {
	var against string
	cmd := &cobra.Command{
		Use:   "diff <id-or-key> --against <project>",
		Short: "Compare a flag with the same-key flag in another project (ID or alias)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, resolved *shared.Resolved) error {
				p, err := project(resolved)
				if err != nil {
					return err
				}
				other, err := target(resolved, against)
				if err != nil {
					return err
				}
				c, err := p.Compare(ctx, args[0], p.In(other.ID, other.Alias))
				if err != nil {
					return err
				}
				result := flagspec.NewOrdered()
				result.Set("key", c.Key)
				result.Set("project", c.Project)
				result.Set("against", c.Against)
				result.Set("exists_in_against", c.Exists)
				result.Set("identical", c.Identical())
				result.Set("differences", relabel(c.Differences, "project", "against"))
				if len(c.Warnings) > 0 {
					result.Set("warnings", c.Warnings)
				}
				return shared.WriteExact(result, globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&against, "against", "", "Project ID or alias to compare with")
	_ = cmd.MarkFlagRequired("against")
	return cmd
}

// relabel names the two sides of each difference.
func relabel(changes []flagspec.Change, this, other string) []any {
	out := make([]any, 0, len(changes))
	for _, change := range changes {
		entry := flagspec.NewOrdered()
		entry.Set("path", change.Path)
		entry.Set(this, change.Before)
		entry.Set(other, change.After)
		out = append(out, entry)
	}
	return out
}
