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

const cohortWarning = "Cohorts are compared by ID, and cohort IDs differ between projects; compare their definitions separately."

// comparableSpec exports a flag for comparison across projects. Flag
// dependencies are renamed from ID to key first, since IDs differ between
// projects.
func comparableSpec(ctx context.Context, resolved *resolvedContext, projectID int, flag map[string]any) (map[string]any, []string, error) {
	named, _ := flagspec.Clone(flag).(map[string]any)
	var warnings []string
	for _, group := range asList(asObject(named["filters"])["groups"]) {
		for _, item := range asList(asObject(group)["properties"]) {
			prop := asObject(item)
			switch prop["type"] {
			case flagspec.PropertyCohort:
				warnings = appendOnce(warnings, cohortWarning)
			case flagspec.PropertyFlag:
				key, err := flagKeyByID(ctx, resolved, projectID, fmt.Sprint(prop["key"]))
				if err != nil {
					return nil, nil, err
				}
				prop["key"] = key
			}
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
				warnings = appendOnce(warnings, againstWarnings...)
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

func copyCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	var targets []string
	var overwrite, disable, copyDependencies, copySchedule bool
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
				copyTo, targetIDs, err := copyTargets(resolved, targets)
				if err != nil {
					return err
				}
				flag, err := fetchFlag(ctx, resolved, resolved.ProjectID, args[0])
				if err != nil {
					return err
				}
				key := fmt.Sprint(flag["key"])
				plan, blocked, warnings, err := planCopy(ctx, resolved, flag, copyTo, overwrite)
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
				if copyDependencies {
					raw, err := resolved.Client.Post(ctx, copyFlagsPath(resolved.OrgID)+"dependency_requirements/", nil, request)
					if err != nil {
						return err
					}
					requirements, _ := flagspec.DecodeObject(raw)
					result.Set("dependencies", requirements)
				}
				if disable && copyDependencies {
					warnings = append(warnings, "--disable applies to this flag only; dependency flags copied alongside it keep their source state.")
				}
				if copySchedule {
					warnings = append(warnings, "Scheduled changes are copied too; schedules already on a target stay and may change the copy later.")
				}
				result.Set("warnings", warnings)
				hint := applyHint
				if len(blocked) > 0 {
					hint = fmt.Sprintf("Flag %s already exists in project(s) %s; review the differences, then pass --overwrite to replace it.", key, strings.Join(blocked, ", "))
				}
				if !opts.apply() {
					result.Set("hint", hint)
					return writeExact(result, globals.Format)
				}
				if len(blocked) > 0 {
					return agenterrors.New("Not copied: "+hint, agenterrors.FixableByAgent)
				}
				if err := requireWriteAllowed(); err != nil {
					return err
				}
				request["copy_schedule"] = copySchedule
				request["disable_copied_flag"] = disable
				request["copy_dependencies"] = copyDependencies
				raw, err := resolved.Client.Mutate(ctx, http.MethodPost, copyFlagsPath(resolved.OrgID), nil, request)
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
	cmd.Flags().BoolVar(&copySchedule, "copy-schedule", false, "Also copy the flag's scheduled changes (schedules already on a target stay)")
	addWriteFlags(cmd, &opts)
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func copyFlagsPath(orgID string) string {
	return "/api/organizations/" + orgID + "/feature_flags/copy_flags/"
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
	warnings = appendOnce([]string{
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
		warnings = appendOnce(warnings, targetWarnings...)
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
