package cli

import (
	"context"
	"fmt"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/featureflag"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func copyCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	var o featureflag.CopyOptions
	var to []string
	cmd := &cobra.Command{
		Use:   "copy <id-or-key> --to <project>...",
		Short: "Copy a flag to other projects in the organization (e.g. staging → prod) with PostHog's copy",
		Long: "Plans by default: shows, per target project, whether the flag would be created or overwritten and how it " +
			"differs. Overwriting an existing flag needs --overwrite. PostHog's copy cannot check that a target is " +
			"unchanged since the plan, maps cohorts by name, and does not copy tags, evaluation contexts, or archived state.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.ifVersion != featureflag.NoVersion {
				return agenterrors.New("copy does not support --if-version: PostHog's copy cannot check the target's version", agenterrors.FixableByAgent)
			}
			if err := opts.refuseSavePlan("copy"); err != nil {
				return err
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireOrg(resolved); err != nil {
					return err
				}
				p, err := resolved.flagProject()
				if err != nil {
					return err
				}
				targets := make([]featureflag.Target, 0, len(to))
				for _, ref := range to {
					id, err := resolved.projectRef(ref)
					if err != nil {
						return err
					}
					targets = append(targets, featureflag.Target{ID: id, Alias: aliasName(ref)})
				}
				plan, err := p.PlanCopy(ctx, args[0], targets, o)
				if err != nil {
					return err
				}
				return writeCopy(ctx, plan, opts, globals.Format)
			})
		},
	}
	cmd.Flags().StringArrayVar(&to, "to", nil, "Target project ID or alias; repeat for several")
	cmd.Flags().BoolVar(&o.Overwrite, "overwrite", false, "Replace the flag in targets where it already exists")
	cmd.Flags().BoolVar(&o.Disable, "disable", false, "Create the copies disabled regardless of the source")
	cmd.Flags().BoolVar(&o.Dependencies, "copy-dependencies", false, "Also copy flags this flag depends on that targets lack")
	cmd.Flags().BoolVar(&o.Schedules, "copy-schedule", false, "Also copy the flag's scheduled changes (schedules already on a target stay)")
	addWriteFlags(cmd, &opts)
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

// writeCopy prints the copy plan or, with --yes, applies it and prints
// PostHog's per-target report, failing the command if any target failed.
func writeCopy(ctx context.Context, plan *featureflag.CopyPlan, opts writeOptions, format string) error {
	result := flagspec.NewOrdered()
	result.Set("outcome", featureflag.OutcomePlanned)
	result.Set("operation", "copy")
	result.Set("key", plan.Key)
	result.Set("from_project", plan.From)
	targets := make([]any, 0, len(plan.Targets))
	for _, t := range plan.Targets {
		entry := flagspec.NewOrdered()
		entry.Set("project", t.Target.ID)
		if t.Target.Alias != "" {
			entry.Set("alias", t.Target.Alias)
		}
		entry.Set("action", t.Action)
		if t.Action == "overwrite" {
			entry.Set("differences", relabel(t.Differences, "source", "target"))
		}
		targets = append(targets, entry)
	}
	result.Set("targets", targets)
	if plan.Dependencies != nil {
		result.Set("dependencies", plan.Dependencies)
	}
	result.Set("warnings", plan.Warnings)
	hint := copyHint(plan)
	if !opts.apply() {
		result.Set("hint", hint)
		return writeExact(result, format)
	}
	if len(plan.Blocked) > 0 {
		return agenterrors.New("Not copied: "+hint, agenterrors.FixableByAgent)
	}
	applied, err := plan.Apply(ctx)
	if applied == nil {
		return err
	}
	result.Set("outcome", applied.Outcome)
	result.Set("succeeded", applied.Succeeded)
	result.Set("failed", applied.Failed)
	if writeErr := writeExact(result, format); writeErr != nil {
		return writeErr
	}
	return err
}

func copyHint(plan *featureflag.CopyPlan) string {
	if len(plan.Blocked) == 0 {
		return applyHint
	}
	blocked := make([]string, 0, len(plan.Blocked))
	for _, id := range plan.Blocked {
		blocked = append(blocked, fmt.Sprint(id))
	}
	return fmt.Sprintf("Flag %s already exists in project(s) %s; review the differences, then pass --overwrite to replace it.", plan.Key, strings.Join(blocked, ", "))
}
