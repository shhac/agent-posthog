package featureflag

import (
	"context"
	"encoding/json"
	"fmt"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

const cohortWarning = "Cohorts are compared by ID, and cohort IDs differ between projects; compare their definitions separately."

// Comparison is a flag set against the same-key flag in another project.
type Comparison struct {
	Key     string
	Project int
	Against int
	// Exists is false when the other project has no flag with the key.
	Exists      bool
	Differences []flagspec.Change
	Warnings    []string
}

// Identical reports whether both projects have the flag, defined the same.
func (c *Comparison) Identical() bool { return c.Exists && len(c.Differences) == 0 }

// Compare reads the flag ref names and the same-key flag in against, and
// lists how they differ as specs, with flag dependencies named by key.
func (p Project) Compare(ctx context.Context, ref string, against Project) (*Comparison, error) {
	flag, err := p.Fetch(ctx, ref)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprint(flag["key"])
	other, err := against.WithKey(ctx, key)
	if err != nil {
		return nil, err
	}
	spec, warnings, err := p.comparableSpec(ctx, flag)
	if err != nil {
		return nil, err
	}
	againstSpec := map[string]any{}
	if other != nil {
		var againstWarnings []string
		againstSpec, againstWarnings, err = against.comparableSpec(ctx, other)
		if err != nil {
			return nil, err
		}
		warnings = flagspec.AppendOnce(warnings, againstWarnings...)
	}
	return &Comparison{
		Key: key, Project: p.ID, Against: against.ID, Exists: other != nil,
		Differences: flagspec.Diff("", spec, againstSpec), Warnings: warnings,
	}, nil
}

// comparableSpec exports a flag for comparison across projects. Flag
// dependencies are renamed from ID to key first, since IDs differ between
// projects.
func (p Project) comparableSpec(ctx context.Context, flag map[string]any) (map[string]any, []string, error) {
	named, _ := flagspec.Clone(flag).(map[string]any)
	var warnings []string
	for prop := range flagspec.ConditionProperties(named) {
		switch prop["type"] {
		case flagspec.PropertyCohort:
			warnings = flagspec.AppendOnce(warnings, cohortWarning)
		case flagspec.PropertyFlag:
			key, err := p.KeyByID(ctx, fmt.Sprint(prop["key"]))
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
