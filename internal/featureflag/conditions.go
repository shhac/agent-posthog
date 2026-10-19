package featureflag

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

// Condition is one release condition to add: filters (ANDed), the percent of
// matching users served, and an optional variant override.
type Condition struct {
	Where   []string
	Rollout float64
	Variant string
}

// ConditionEdit changes parts of one condition; a nil field stays as it is.
// An empty Where matches everyone, and an empty Variant removes the override.
type ConditionEdit struct {
	Where   *[]string
	Rollout *float64
	Variant *string
}

// CheckRollout refuses a percent outside 0..100.
func CheckRollout(pct float64) error {
	if pct < 0 || pct > 100 {
		return agenterrors.New("--rollout must be from 0 to 100", agenterrors.FixableByAgent)
	}
	return nil
}

// AddCondition inserts c at index at, or appends it when at is negative.
func (p Project) AddCondition(ctx context.Context, c Condition, at int) Build {
	resolve := p.Resolver(ctx)
	return func(flag map[string]any) (*flagspec.Patch, []string, error) {
		condition := map[string]any{"where": flagspec.Strings(c.Where), "rollout": rolloutNumber(c.Rollout)}
		if c.Variant != "" {
			condition["variant"] = c.Variant
		}
		group, err := flagspec.BuildGroup(condition, resolve)
		if err != nil {
			return nil, nil, err
		}
		groups, err := editableGroups(flag)
		if err != nil {
			return nil, nil, err
		}
		index := at
		if index < 0 {
			index = len(groups)
		}
		if index > len(groups) {
			return nil, nil, fmt.Errorf("--at %d is outside 0..%d", index, len(groups))
		}
		return groupsPatch(flag, slices.Insert(groups, index, any(group)))
	}
}

// EditCondition changes the condition at index.
func (p Project) EditCondition(ctx context.Context, index int, e ConditionEdit) Build {
	resolve := p.Resolver(ctx)
	return func(flag map[string]any) (*flagspec.Patch, []string, error) {
		groups, err := editableGroups(flag)
		if err != nil {
			return nil, nil, err
		}
		if err := checkConditionIndex(groups, index); err != nil {
			return nil, nil, err
		}
		group := flagspec.Object(groups[index])
		if e.Where != nil {
			props, err := buildProperties(*e.Where, resolve)
			if err != nil {
				return nil, nil, err
			}
			group["properties"] = props
		}
		if e.Rollout != nil {
			group["rollout_percentage"] = rolloutNumber(*e.Rollout)
		}
		if e.Variant != nil {
			group["variant"] = nil
			if *e.Variant != "" {
				group["variant"] = *e.Variant
			}
		}
		return groupsPatch(flag, groups)
	}
}

// PlanConditionRollout plans a rollout-only change to the condition at index
// through PostHog's strict endpoint, which refuses the write if the flag
// changed at all since the version read.
func (p Project) PlanConditionRollout(ctx context.Context, ref string, ifVersion, index int, rollout float64) (*Pending, error) {
	build := func(flag map[string]any) (*flagspec.Patch, []string, error) {
		groups, err := editableGroups(flag)
		if err != nil {
			return nil, nil, err
		}
		if err := checkConditionIndex(groups, index); err != nil {
			return nil, nil, err
		}
		flagspec.Object(groups[index])["rollout_percentage"] = rolloutNumber(rollout)
		patch := flagspec.NewPatch()
		patch.Filters["groups"] = groups
		return patch, nil, nil
	}
	send := func(ctx context.Context, flag map[string]any, _ *flagspec.Patch) (json.RawMessage, error) {
		body := map[string]any{"condition_index": index, "rollout_percentage": rolloutNumber(rollout), "version": flag["version"]}
		return p.Client.Mutate(ctx, http.MethodPost, p.flagPath(fmt.Sprint(flag["id"]), "set_release_condition_rollout"), nil, body)
	}
	return p.PlanVia(ctx, ref, "conditions set", ifVersion, build, send)
}

// RemoveCondition removes the condition at index.
func RemoveCondition(index int) Build {
	return func(flag map[string]any) (*flagspec.Patch, []string, error) {
		groups, err := editableGroups(flag)
		if err != nil {
			return nil, nil, err
		}
		if err := checkConditionIndex(groups, index); err != nil {
			return nil, nil, err
		}
		return groupsPatch(flag, slices.Delete(groups, index, index+1))
	}
}

// MoveCondition moves the condition at from so it ends up at index to.
func MoveCondition(from, to int) Build {
	return func(flag map[string]any) (*flagspec.Patch, []string, error) {
		groups, err := editableGroups(flag)
		if err != nil {
			return nil, nil, err
		}
		moved, err := moveGroup(groups, from, to)
		if err != nil {
			return nil, nil, err
		}
		return groupsPatch(flag, moved)
	}
}

func buildProperties(where []string, resolve flagspec.FlagResolver) ([]any, error) {
	if len(where) == 0 {
		return []any{}, nil
	}
	return flagspec.BuildProperties(flagspec.Strings(where), resolve)
}

func moveGroup(groups []any, from, to int) ([]any, error) {
	if from >= len(groups) || to >= len(groups) {
		return nil, fmt.Errorf("indexes must be below %d", len(groups))
	}
	moved := groups[from]
	return slices.Insert(slices.Delete(groups, from, from+1), to, moved), nil
}

func checkConditionIndex(groups []any, index int) error {
	if index >= len(groups) {
		return fmt.Errorf("the flag has no condition %d (it has %d)", index, len(groups))
	}
	return nil
}

// editableGroups returns a copy of the flag's conditions after checking the
// flag is one whose conditions can be edited.
func editableGroups(flag map[string]any) ([]any, error) {
	filters := flagspec.Object(flag["filters"])
	if err := flagspec.RequireV1(filters); err != nil {
		return nil, err
	}
	if flagspec.TypeOf(flag) == flagspec.TypeRemoteConfig {
		return nil, fmt.Errorf("remote_config flags serve one payload to everyone and have no conditions to edit")
	}
	groups, _ := flagspec.Clone(filters["groups"]).([]any)
	return append([]any{}, groups...), nil
}

func groupsPatch(flag map[string]any, groups []any) (*flagspec.Patch, []string, error) {
	flagType := flagspec.TypeOf(flag)
	if err := flagspec.CheckOverrides(groups, flagType, flagspec.VariantKeys(flag)); err != nil {
		return nil, nil, err
	}
	patch := flagspec.NewPatch()
	patch.Filters["groups"] = groups
	return patch, flagspec.ConditionWarnings(groups, flagType), nil
}

func rolloutNumber(pct float64) json.Number {
	return json.Number(strconv.FormatFloat(pct, 'f', -1, 64))
}
