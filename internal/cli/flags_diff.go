package cli

import (
	"context"
	"encoding/json"
	"fmt"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

const cohortWarning = "Cohorts are compared by ID, and cohort IDs differ between projects; compare their definitions separately."

// comparableSpec exports a flag for comparison across projects. Flag
// dependencies are renamed from ID to key first, since IDs differ between
// projects.
func comparableSpec(ctx context.Context, resolved *resolvedContext, projectID int, flag map[string]any) (map[string]any, []string, error) {
	named, _ := flagspec.Clone(flag).(map[string]any)
	var warnings []string
	for prop := range flagspec.ConditionProperties(named) {
		switch prop["type"] {
		case flagspec.PropertyCohort:
			warnings = flagspec.AppendOnce(warnings, cohortWarning)
		case flagspec.PropertyFlag:
			key, err := flagKeyByID(ctx, resolved, projectID, fmt.Sprint(prop["key"]))
			if err != nil {
				return nil, nil, err
			}
			prop["key"] = key
		}
	}
	exported, err := flagspec.Export(named)
	if err != nil {
		return nil, nil, agenterrors.Wrap(err, agenterrors.FixableByHuman)
	}
	data, err := json.Marshal(exported)
	if err != nil {
		return nil, nil, err
	}
	spec, err := flagspec.DecodeObject(data)
	if err != nil {
		return nil, nil, err
	}
	delete(spec, "schema_version")
	return spec, warnings, nil
}

// otherSpec is the comparable spec of the same-key flag in another project,
// or an empty spec when that project has none.
func otherSpec(ctx context.Context, resolved *resolvedContext, projectID int, other map[string]any) (map[string]any, []string, error) {
	if other == nil {
		return map[string]any{}, nil, nil
	}
	return comparableSpec(ctx, resolved, projectID, other)
}

func diffCommand(globals *GlobalFlags) *cobra.Command {
	var against string
	cmd := &cobra.Command{
		Use:   "diff <id-or-key> --against <project>",
		Short: "Compare a flag with the same-key flag in another project (ID or alias)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				otherProject, err := resolved.projectRef(against)
				if err != nil {
					return err
				}
				flag, err := fetchFlag(ctx, resolved, resolved.ProjectID, args[0])
				if err != nil {
					return err
				}
				key := fmt.Sprint(flag["key"])
				other, err := flagInProject(ctx, resolved, otherProject, key)
				if err != nil {
					return err
				}
				spec, warnings, err := comparableSpec(ctx, resolved, resolved.ProjectID, flag)
				if err != nil {
					return err
				}
				againstSpec, againstWarnings, err := otherSpec(ctx, resolved, otherProject, other)
				if err != nil {
					return err
				}
				warnings = flagspec.AppendOnce(warnings, againstWarnings...)
				differences := flagspec.Diff("", spec, againstSpec)
				result := flagspec.NewOrdered()
				result.Set("key", key)
				result.Set("project", resolved.ProjectID)
				result.Set("against", otherProject)
				result.Set("exists_in_against", other != nil)
				result.Set("identical", other != nil && len(differences) == 0)
				result.Set("differences", relabel(differences, "project", "against"))
				if len(warnings) > 0 {
					result.Set("warnings", warnings)
				}
				return writeExact(result, globals.Format)
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
