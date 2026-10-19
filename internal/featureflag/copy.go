package featureflag

import (
	"context"
	"fmt"
	"net/http"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/api"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

// MaxCopyTargets is the most projects PostHog copies to at once.
const MaxCopyTargets = 50

// Target is a destination project and the alias that named it, if any.
type Target struct {
	ID    int
	Alias string
}

// CopyOptions are the choices PostHog's copy offers.
type CopyOptions struct {
	// Overwrite allows replacing the flag where a target already has it.
	Overwrite bool
	// Disable creates the copies disabled regardless of the source.
	Disable bool
	// Dependencies also copies flags this one depends on that targets lack.
	Dependencies bool
	// Schedules also copies the flag's scheduled changes.
	Schedules bool
}

// TargetPlan is what copying does to one target: create the flag, or
// overwrite the one there, which differs as listed.
type TargetPlan struct {
	Target      Target
	Action      string
	Differences []flagspec.Change
}

// CopyPlan is a planned copy. It is advisory: PostHog's copy cannot check that
// a target is unchanged since it was read.
type CopyPlan struct {
	Key     string
	From    int
	Targets []TargetPlan
	// Blocked lists targets that already have the flag, when overwriting
	// was not allowed.
	Blocked []int
	// Dependencies is PostHog's report of the dependency flags each target
	// lacks, when the copy includes dependencies.
	Dependencies map[string]any
	Warnings     []string

	p       Project
	request map[string]any
}

// CopyResult is PostHog's per-target report of a copy.
type CopyResult struct {
	// Outcome is applied, partial, or failed.
	Outcome   string
	Succeeded []any
	Failed    []any
}

// PlanCopy plans copying the flag ref names to targets in the same
// organization.
func (p Project) PlanCopy(ctx context.Context, ref string, targets []Target, o CopyOptions) (*CopyPlan, error) {
	targets, err := p.checkTargets(targets)
	if err != nil {
		return nil, err
	}
	flag, err := p.Fetch(ctx, ref)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprint(flag["key"])
	source, warnings, err := p.comparableSpec(ctx, flag)
	if err != nil {
		return nil, err
	}
	plan := &CopyPlan{Key: key, From: p.ID, p: p}
	plan.Warnings = flagspec.AppendOnce([]string{
		"The plan is advisory: PostHog's copy cannot check that a target is unchanged since it was read.",
		"Tags, evaluation contexts, and archived state are not copied; cohorts are matched by name in each target.",
	}, warnings...)
	ids := make([]int, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
		dest := p.In(target.ID, target.Alias)
		existing, err := dest.WithKey(ctx, key)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			plan.Targets = append(plan.Targets, TargetPlan{Target: target, Action: "create"})
			continue
		}
		targetSpec, targetWarnings, err := dest.comparableSpec(ctx, existing)
		if err != nil {
			return nil, err
		}
		plan.Warnings = flagspec.AppendOnce(plan.Warnings, targetWarnings...)
		plan.Targets = append(plan.Targets, TargetPlan{Target: target, Action: "overwrite", Differences: flagspec.Diff("", source, targetSpec)})
		if !o.Overwrite {
			plan.Blocked = append(plan.Blocked, target.ID)
		}
	}
	plan.request = map[string]any{"feature_flag_key": key, "from_project": p.ID, "target_project_ids": ids}
	if o.Dependencies {
		raw, err := p.Client.Post(ctx, p.copyFlagsPath("dependency_requirements"), nil, plan.request)
		if err != nil {
			return nil, err
		}
		plan.Dependencies, _ = flagspec.DecodeObject(raw)
	}
	plan.Warnings = append(plan.Warnings, o.warnings()...)
	plan.request["copy_schedule"] = o.Schedules
	plan.request["disable_copied_flag"] = o.Disable
	plan.request["copy_dependencies"] = o.Dependencies
	return plan, nil
}

// Apply carries out the copy. PostHog answers 200 even when some targets fail
// or wait for approval, so a result with failures comes back together with an
// error describing them.
func (cp *CopyPlan) Apply(ctx context.Context) (*CopyResult, error) {
	if len(cp.Blocked) > 0 {
		return nil, agenterrors.New(fmt.Sprintf("flag %s already exists in project(s) %v and overwriting was not allowed", cp.Key, cp.Blocked), agenterrors.FixableByAgent)
	}
	raw, err := cp.p.Client.Mutate(ctx, http.MethodPost, cp.p.copyFlagsPath(), nil, cp.request)
	if err != nil {
		return nil, ClassifyWriteError(err)
	}
	resp, err := flagspec.DecodeObject(raw)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByRetry)
	}
	result := &CopyResult{Outcome: OutcomeApplied, Succeeded: flagspec.List(resp["success"]), Failed: flagspec.List(resp["failed"])}
	switch {
	case len(result.Failed) > 0 && len(result.Succeeded) > 0:
		result.Outcome = "partial"
	case len(result.Failed) > 0:
		result.Outcome = "failed"
	default:
		return result, nil
	}
	for _, f := range result.Failed {
		if flagspec.Object(f)["approval_pending"] == true {
			return result, agenterrors.New(fmt.Sprintf("%d target(s) were not copied; at least one waits on an approval", len(result.Failed)), agenterrors.FixableByHuman).
				WithHint("See failed[] for change request IDs; an approver must review them in PostHog.")
		}
	}
	return result, agenterrors.New(fmt.Sprintf("%d target(s) were not copied", len(result.Failed)), agenterrors.FixableByAgent).
		WithHint("See failed[] on stdout for each project's reason.")
}

// warnings are the side effects the chosen options bring.
func (o CopyOptions) warnings() []string {
	var out []string
	if o.Disable && o.Dependencies {
		out = append(out, "--disable applies to this flag only; dependency flags copied alongside it keep their source state.")
	}
	if o.Schedules {
		out = append(out, "Scheduled changes are copied too; schedules already on a target stay and may change the copy later.")
	}
	return out
}

// checkTargets drops repeats and refuses the source project or too many
// targets.
func (p Project) checkTargets(targets []Target) ([]Target, error) {
	seen := map[int]bool{}
	var out []Target
	for _, target := range targets {
		if target.ID == p.ID {
			return nil, agenterrors.New(fmt.Sprintf("project %d is the source project", target.ID), agenterrors.FixableByAgent)
		}
		if !seen[target.ID] {
			seen[target.ID] = true
			out = append(out, target)
		}
	}
	if len(out) > MaxCopyTargets {
		return nil, agenterrors.New(fmt.Sprintf("PostHog copies to at most %d projects at once", MaxCopyTargets), agenterrors.FixableByAgent)
	}
	return out, nil
}

func (p Project) copyFlagsPath(segments ...string) string {
	return api.Path(append([]string{"organizations", p.OrgID, "feature_flags", "copy_flags"}, segments...)...)
}
