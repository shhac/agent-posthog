package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

const maxCopyTargets = 50

func registerFlagProjectCommands(flags *cobra.Command, globals *GlobalFlags) {
	flags.AddCommand(diffCommand(globals))
	flags.AddCommand(copyCommand(globals))
}

// comparableSpec exports a flag for comparison across projects: flag
// dependencies are named by key, since IDs differ between projects.
func comparableSpec(ctx context.Context, resolved *resolvedContext, projectID int, flag map[string]any) (map[string]any, []string, error) {
	exported, err := flagspec.Export(flag)
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
	var warnings []string
	for _, item := range asList(spec["conditions"]) {
		where := asList(asObject(item)["where"])
		for i, filter := range where {
			prop, isExpr := filterProperty(filter)
			switch prop["type"] {
			case flagspec.PropertyCohort:
				warnings = appendOnce(warnings, "Cohorts are compared by ID, and cohort IDs differ between projects; compare their definitions separately.")
			case flagspec.PropertyFlag:
				key, err := flagKeyByID(ctx, resolved, projectID, fmt.Sprint(prop["key"]))
				if err != nil {
					return nil, nil, err
				}
				prop["key"] = key
				if isExpr {
					where[i], _ = flagspec.FormatWhere(prop)
				} else {
					where[i] = prop
				}
			}
		}
	}
	return spec, warnings, nil
}

func filterProperty(filter any) (map[string]any, bool) {
	if expr, ok := filter.(string); ok {
		prop, err := flagspec.ParseWhere(expr)
		if err != nil {
			return map[string]any{}, true
		}
		return prop, true
	}
	return asObject(filter), false
}

func appendOnce(list []string, item string) []string {
	for _, existing := range list {
		if existing == item {
			return list
		}
	}
	return append(list, item)
}

// projectDiff compares a flag between two projects. A nil other means the
// flag does not exist there.
func projectDiff(ctx context.Context, resolved *resolvedContext, projectID int, flag map[string]any, otherProject int, other map[string]any) ([]any, []string, error) {
	spec, warnings, err := comparableSpec(ctx, resolved, projectID, flag)
	if err != nil {
		return nil, nil, err
	}
	otherSpec := map[string]any{}
	if other != nil {
		var otherWarnings []string
		otherSpec, otherWarnings, err = comparableSpec(ctx, resolved, otherProject, other)
		if err != nil {
			return nil, nil, err
		}
		for _, w := range otherWarnings {
			warnings = appendOnce(warnings, w)
		}
	}
	differences := []any{}
	for _, change := range flagspec.Diff("", spec, otherSpec) {
		differences = append(differences, map[string]any{
			"path":  strings.TrimPrefix(change.Path, "."),
			"this":  change.Before,
			"other": change.After,
		})
	}
	return differences, warnings, nil
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
				differences, warnings, err := projectDiff(ctx, resolved, resolved.ProjectID, flag, otherProject, other)
				if err != nil {
					return err
				}
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
func relabel(differences []any, this, other string) []any {
	out := make([]any, 0, len(differences))
	for _, d := range differences {
		m := asObject(d)
		entry := flagspec.NewOrdered()
		entry.Set("path", m["path"])
		entry.Set(this, m["this"])
		entry.Set(other, m["other"])
		out = append(out, entry)
	}
	return out
}

func copyCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	var targets []string
	var overwrite, disable, copyDependencies bool
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
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				if err := requireOrg(resolved); err != nil {
					return err
				}
				targetIDs, err := copyTargets(resolved, targets)
				if err != nil {
					return err
				}
				flag, err := fetchFlag(ctx, resolved, resolved.ProjectID, args[0])
				if err != nil {
					return err
				}
				key := fmt.Sprint(flag["key"])
				plan, blocked, warnings, err := planCopy(ctx, resolved, flag, targetIDs, overwrite)
				if err != nil {
					return err
				}
				result := flagspec.NewOrdered()
				result.Set("outcome", outcomePlanned)
				result.Set("operation", "copy")
				result.Set("key", key)
				result.Set("from_project", resolved.ProjectID)
				result.Set("targets", plan)
				body := map[string]any{
					"feature_flag_key": key, "from_project": resolved.ProjectID, "target_project_ids": targetIDs,
					"copy_schedule": false, "disable_copied_flag": disable, "copy_dependencies": copyDependencies,
				}
				if copyDependencies {
					raw, err := resolved.Client.Post(ctx, "/api/organizations/"+resolved.OrgID+"/feature_flags/copy_flags/dependency_requirements/", nil,
						map[string]any{"feature_flag_key": key, "from_project": resolved.ProjectID, "target_project_ids": targetIDs})
					if err != nil {
						return err
					}
					requirements, _ := flagspec.DecodeObject(raw)
					result.Set("dependencies", requirements)
				}
				result.Set("warnings", warnings)
				overwriteHint := fmt.Sprintf("Flag %s already exists in project(s) %s; review the differences, then pass --overwrite to replace it.", key, strings.Join(blocked, ", "))
				if !opts.apply() {
					result.Set("hint", applyHint)
					if len(blocked) > 0 {
						result.Set("hint", overwriteHint)
					}
					return writeExact(result, globals.Format)
				}
				if len(blocked) > 0 {
					return agenterrors.New("Not copied: "+overwriteHint, agenterrors.FixableByAgent)
				}
				raw, err := resolved.Client.Mutate(ctx, http.MethodPost, "/api/organizations/"+resolved.OrgID+"/feature_flags/copy_flags/", nil, body)
				if err != nil {
					return classifyWriteError(err)
				}
				return reportCopy(result, raw, globals.Format)
			})
		},
	}
	cmd.Flags().StringArrayVar(&targets, "to", nil, "Target project ID or alias; repeat for several")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Replace the flag in targets where it already exists")
	cmd.Flags().BoolVar(&disable, "disable", false, "Create the copies disabled regardless of the source")
	cmd.Flags().BoolVar(&copyDependencies, "copy-dependencies", false, "Also copy flags this flag depends on that targets lack")
	addWriteFlags(cmd, &opts)
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func copyTargets(resolved *resolvedContext, targets []string) ([]int, error) {
	seen := map[int]bool{}
	var ids []int
	for _, target := range targets {
		id, err := resolved.projectRef(target)
		if err != nil {
			return nil, err
		}
		if id == resolved.ProjectID {
			return nil, agenterrors.New(fmt.Sprintf("project %s is the source project", target), agenterrors.FixableByAgent)
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > maxCopyTargets {
		return nil, agenterrors.New(fmt.Sprintf("PostHog copies to at most %d projects at once", maxCopyTargets), agenterrors.FixableByAgent)
	}
	return ids, nil
}

func planCopy(ctx context.Context, resolved *resolvedContext, flag map[string]any, targetIDs []int, overwrite bool) ([]any, []string, []string, error) {
	key := fmt.Sprint(flag["key"])
	warnings := []string{
		"The plan is advisory: PostHog's copy cannot check that a target is unchanged since it was read.",
		"Tags, evaluation contexts, and archived state are not copied; cohorts are matched by name in each target.",
	}
	var plan []any
	var blocked []string
	for _, target := range targetIDs {
		existing, err := flagInProject(ctx, resolved, target, key)
		if err != nil {
			return nil, nil, nil, err
		}
		entry := flagspec.NewOrdered()
		entry.Set("project", target)
		if existing == nil {
			entry.Set("action", "create")
			plan = append(plan, entry)
			continue
		}
		entry.Set("action", "overwrite")
		differences, diffWarnings, err := projectDiff(ctx, resolved, resolved.ProjectID, flag, target, existing)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, w := range diffWarnings {
			warnings = appendOnce(warnings, w)
		}
		entry.Set("differences", relabel(differences, "source", "target"))
		plan = append(plan, entry)
		if !overwrite {
			blocked = append(blocked, fmt.Sprint(target))
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
	succeeded := asList(resp["success"])
	failed := asList(resp["failed"])
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
		if asObject(f)["approval_pending"] == true {
			return agenterrors.New(fmt.Sprintf("%d target(s) were not copied; at least one waits on an approval", len(failed)), agenterrors.FixableByHuman).
				WithHint("See failed[] for change request IDs; an approver must review them in PostHog.")
		}
	}
	return agenterrors.New(fmt.Sprintf("%d target(s) were not copied", len(failed)), agenterrors.FixableByAgent).
		WithHint("See failed[] on stdout for each project's reason.")
}
