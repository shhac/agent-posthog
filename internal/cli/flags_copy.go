package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

const maxCopyTargets = 50

// copyOptions are the flags of 'flags copy'.
type copyOptions struct {
	targets                                     []string
	overwrite, disable, dependencies, schedules bool
}

func copyCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	var o copyOptions
	cmd := &cobra.Command{
		Use:   "copy <id-or-key> --to <project>...",
		Short: "Copy a flag to other projects in the organization (e.g. staging → prod) with PostHog's copy",
		Long: "Plans by default: shows, per target project, whether the flag would be created or overwritten and how it " +
			"differs. Overwriting an existing flag needs --overwrite. PostHog's copy cannot check that a target is " +
			"unchanged since the plan, maps cohorts by name, and does not copy tags, evaluation contexts, or archived state.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.ifVersion != noVersion {
				return agenterrors.New("copy does not support --if-version: PostHog's copy cannot check the target's version", agenterrors.FixableByAgent)
			}
			if err := opts.refuseSavePlan("copy"); err != nil {
				return err
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				return runCopy(ctx, resolved, globals.Format, args[0], o, opts)
			})
		},
	}
	cmd.Flags().StringArrayVar(&o.targets, "to", nil, "Target project ID or alias; repeat for several")
	cmd.Flags().BoolVar(&o.overwrite, "overwrite", false, "Replace the flag in targets where it already exists")
	cmd.Flags().BoolVar(&o.disable, "disable", false, "Create the copies disabled regardless of the source")
	cmd.Flags().BoolVar(&o.dependencies, "copy-dependencies", false, "Also copy flags this flag depends on that targets lack")
	cmd.Flags().BoolVar(&o.schedules, "copy-schedule", false, "Also copy the flag's scheduled changes (schedules already on a target stay)")
	addWriteFlags(cmd, &opts)
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

// runCopy plans a copy per target and, with --yes, applies it with PostHog's
// copy_flags, which reports each target's outcome separately.
func runCopy(ctx context.Context, resolved *resolvedContext, format, ref string, o copyOptions, opts writeOptions) error {
	if err := requireOrg(resolved); err != nil {
		return err
	}
	targets, targetIDs, err := copyTargets(resolved, o.targets)
	if err != nil {
		return err
	}
	flag, err := currentFlag(ctx, resolved, ref)
	if err != nil {
		return err
	}
	key := fmt.Sprint(flag["key"])
	plan, blocked, warnings, err := planCopy(ctx, resolved, flag, targets, o.overwrite)
	if err != nil {
		return err
	}
	result := flagspec.NewOrdered()
	result.Set("outcome", outcomePlanned)
	result.Set("operation", "copy")
	result.Set("key", key)
	result.Set("from_project", resolved.ProjectID)
	result.Set("targets", plan)
	request := map[string]any{"feature_flag_key": key, "from_project": resolved.ProjectID, "target_project_ids": targetIDs}
	if o.dependencies {
		raw, err := resolved.Client.Post(ctx, copyFlagsPath(resolved.OrgID, "dependency_requirements"), nil, request)
		if err != nil {
			return err
		}
		requirements, _ := flagspec.DecodeObject(raw)
		result.Set("dependencies", requirements)
	}
	result.Set("warnings", append(warnings, o.warnings()...))
	hint := copyHint(key, blocked)
	if !opts.apply() {
		result.Set("hint", hint)
		return writeExact(result, format)
	}
	if len(blocked) > 0 {
		return agenterrors.New("Not copied: "+hint, agenterrors.FixableByAgent)
	}
	request["copy_schedule"] = o.schedules
	request["disable_copied_flag"] = o.disable
	request["copy_dependencies"] = o.dependencies
	raw, err := resolved.Client.Mutate(ctx, http.MethodPost, copyFlagsPath(resolved.OrgID), nil, request)
	if err != nil {
		return classifyWriteError(err)
	}
	return reportCopy(result, raw, format)
}

// warnings are the side effects the chosen options bring.
func (o copyOptions) warnings() []string {
	var out []string
	if o.disable && o.dependencies {
		out = append(out, "--disable applies to this flag only; dependency flags copied alongside it keep their source state.")
	}
	if o.schedules {
		out = append(out, "Scheduled changes are copied too; schedules already on a target stay and may change the copy later.")
	}
	return out
}

func copyHint(key string, blocked []string) string {
	if len(blocked) == 0 {
		return applyHint
	}
	return fmt.Sprintf("Flag %s already exists in project(s) %s; review the differences, then pass --overwrite to replace it.", key, strings.Join(blocked, ", "))
}

func copyFlagsPath(orgID string, segments ...string) string {
	return apiPath(append([]string{"organizations", orgID, "feature_flags", "copy_flags"}, segments...)...)
}

// copyTarget is a resolved destination project and the alias that named it.
type copyTarget struct {
	id    int
	alias string
}

func copyTargets(resolved *resolvedContext, refs []string) ([]copyTarget, []int, error) {
	seen := map[int]bool{}
	var targets []copyTarget
	var ids []int
	for _, ref := range refs {
		id, err := resolved.projectRef(ref)
		if err != nil {
			return nil, nil, err
		}
		if id == resolved.ProjectID {
			return nil, nil, agenterrors.New(fmt.Sprintf("project %s is the source project", ref), agenterrors.FixableByAgent)
		}
		if !seen[id] {
			seen[id] = true
			targets = append(targets, copyTarget{id: id, alias: aliasName(ref)})
			ids = append(ids, id)
		}
	}
	if len(ids) > maxCopyTargets {
		return nil, nil, agenterrors.New(fmt.Sprintf("PostHog copies to at most %d projects at once", maxCopyTargets), agenterrors.FixableByAgent)
	}
	return targets, ids, nil
}

func planCopy(ctx context.Context, resolved *resolvedContext, flag map[string]any, targets []copyTarget, overwrite bool) ([]any, []string, []string, error) {
	key := fmt.Sprint(flag["key"])
	source, warnings, err := comparableSpec(ctx, resolved, resolved.ProjectID, flag)
	if err != nil {
		return nil, nil, nil, err
	}
	warnings = flagspec.AppendOnce([]string{
		"The plan is advisory: PostHog's copy cannot check that a target is unchanged since it was read.",
		"Tags, evaluation contexts, and archived state are not copied; cohorts are matched by name in each target.",
	}, warnings...)
	var plan []any
	var blocked []string
	for _, target := range targets {
		existing, err := flagInProject(ctx, resolved, target.id, key)
		if err != nil {
			return nil, nil, nil, err
		}
		entry := flagspec.NewOrdered()
		entry.Set("project", target.id)
		if target.alias != "" {
			entry.Set("alias", target.alias)
		}
		if existing == nil {
			entry.Set("action", "create")
			plan = append(plan, entry)
			continue
		}
		targetSpec, targetWarnings, err := comparableSpec(ctx, resolved, target.id, existing)
		if err != nil {
			return nil, nil, nil, err
		}
		warnings = flagspec.AppendOnce(warnings, targetWarnings...)
		entry.Set("action", "overwrite")
		entry.Set("differences", relabel(flagspec.Diff("", source, targetSpec), "source", "target"))
		plan = append(plan, entry)
		if !overwrite {
			blocked = append(blocked, fmt.Sprint(target.id))
		}
	}
	return plan, blocked, warnings, nil
}

// reportCopy reports per-target results. PostHog returns 200 even when some
// targets fail or wait for approval, so those are surfaced as an error.
func reportCopy(result *flagspec.Ordered, raw json.RawMessage, format string) error {
	resp, err := flagspec.DecodeObject(raw)
	if err != nil {
		return agenterrors.Wrap(err, agenterrors.FixableByRetry)
	}
	succeeded := flagspec.List(resp["success"])
	failed := flagspec.List(resp["failed"])
	outcome := outcomeApplied
	switch {
	case len(failed) > 0 && len(succeeded) > 0:
		outcome = "partial"
	case len(failed) > 0:
		outcome = "failed"
	}
	result.Set("outcome", outcome)
	result.Set("succeeded", succeeded)
	result.Set("failed", failed)
	if err := writeExact(result, format); err != nil {
		return err
	}
	if len(failed) == 0 {
		return nil
	}
	for _, f := range failed {
		if flagspec.Object(f)["approval_pending"] == true {
			return agenterrors.New(fmt.Sprintf("%d target(s) were not copied; at least one waits on an approval", len(failed)), agenterrors.FixableByHuman).
				WithHint("See failed[] for change request IDs; an approver must review them in PostHog.")
		}
	}
	return agenterrors.New(fmt.Sprintf("%d target(s) were not copied", len(failed)), agenterrors.FixableByAgent).
		WithHint("See failed[] on stdout for each project's reason.")
}
