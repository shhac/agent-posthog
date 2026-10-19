package featureflag

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

// lifecycleChanges are the writes that set top-level flag fields, with the
// warnings each calls for.
var lifecycleChanges = map[string]struct {
	fields   map[string]any
	warnings func(p Project, ctx context.Context, flag map[string]any) []string
}{
	"enable":    {fields: map[string]any{"active": true}, warnings: Project.disabledDependencies},
	"disable":   {fields: map[string]any{"active": false}, warnings: Project.activeDependents},
	"archive":   {fields: map[string]any{"archived": true, "active": false}, warnings: Project.activeDependents},
	"unarchive": {fields: map[string]any{"archived": false}},
	"delete":    {fields: map[string]any{"deleted": true}, warnings: Project.deleteWarnings},
}

// Lifecycle builds the change for a lifecycle operation: enable, disable,
// archive, unarchive, or delete.
func Lifecycle(operation string) Build {
	change, ok := lifecycleChanges[operation]
	return func(ctx context.Context, p Project, flag map[string]any) (*flagspec.Patch, []string, error) {
		if !ok {
			return nil, nil, fmt.Errorf("unknown lifecycle operation %q", operation)
		}
		patch := flagspec.NewPatch()
		maps.Copy(patch.Fields, change.fields)
		if change.warnings == nil {
			return patch, nil, nil
		}
		return patch, change.warnings(p, ctx, flag), nil
	}
}

// EarlyExit turns early exit on or off: with it on, a condition whose
// properties match but whose rollout excludes the user returns false instead
// of falling through.
func EarlyExit(on bool) Build {
	return func(_ context.Context, _ Project, flag map[string]any) (*flagspec.Patch, []string, error) {
		if err := flagspec.RequireV1(flagspec.Filters(flag)); err != nil {
			return nil, nil, err
		}
		patch := flagspec.NewPatch()
		patch.Filters["early_exit"] = on
		if !on {
			return patch, nil, nil
		}
		return patch, []string{"SDKs evaluating flags locally honor early exit only in recent versions (posthog-python, posthog-go >= 1.15.0); older ones evaluate every condition."}, nil
	}
}

func (p Project) deleteWarnings(ctx context.Context, flag map[string]any) []string {
	warnings := []string{fmt.Sprintf("Deleted flags drop out of lists; restore with 'agent-posthog flags restore %v'.", flag["id"])}
	if experiments, _ := flag["experiment_set"].([]any); len(experiments) > 0 {
		warnings = append(warnings, "This flag backs an experiment; PostHog may refuse the delete or rename the key.")
	}
	return append(warnings, p.activeDependents(ctx, flag)...)
}

// activeDependents warns that switching off a flag that active flags depend
// on may be refused, or stop them matching. The lookup is best effort.
func (p Project) activeDependents(ctx context.Context, flag map[string]any) []string {
	dependents, err := p.Related(ctx, fmt.Sprint(flag["id"]), "dependent_flags")
	if err != nil {
		return []string{"Could not check which flags depend on this one."}
	}
	var active []string
	for _, raw := range dependents {
		if dep, _ := flagspec.DecodeObject(raw); dep["active"] == true {
			active = append(active, fmt.Sprint(dep["key"]))
		}
	}
	if len(active) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("Active flags depend on this one (%s): PostHog may refuse this, or they will stop matching.", strings.Join(active, ", "))}
}

// disabledDependencies warns that switching on a flag that depends on a
// disabled flag may be refused. Lookups are best effort.
func (p Project) disabledDependencies(ctx context.Context, flag map[string]any) []string {
	var warnings []string
	for prop := range flagspec.ConditionProperties(flag) {
		if prop["type"] != flagspec.PropertyFlag {
			continue
		}
		raw, err := p.Get(ctx, fmt.Sprint(prop["key"]))
		if err != nil {
			warnings = flagspec.AppendOnce(warnings, "Could not check a flag this one depends on.")
			continue
		}
		dep, _ := flagspec.DecodeObject(raw)
		if dep["active"] != true {
			warnings = flagspec.AppendOnce(warnings, fmt.Sprintf("This flag depends on disabled flag %v; PostHog may refuse to enable it until that one is enabled.", dep["key"]))
		}
	}
	return warnings
}
