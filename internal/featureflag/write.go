package featureflag

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

// Receipt reports a planned or applied write. Before/after values are kept
// exactly (no null pruning), since a null is often the change.
type Receipt struct {
	Outcome      string            `json:"outcome"`
	Operation    string            `json:"operation"`
	Host         string            `json:"host"`
	ProjectID    int               `json:"project_id"`
	ProjectAlias string            `json:"project_alias,omitempty"`
	ID           any               `json:"id,omitempty"`
	Key          any               `json:"key"`
	BaseVersion  any               `json:"base_version"`
	Version      any               `json:"version,omitempty"`
	Changes      []flagspec.Change `json:"changes"`
	Warnings     []string          `json:"warnings,omitempty"`
	Hint         string            `json:"hint,omitempty"`
}

const (
	OutcomePlanned   = "planned"
	OutcomeApplied   = "applied"
	OutcomeUnchanged = "unchanged"
)

// NoVersion skips the version check in Plan.
const NoVersion = -1

// Build computes the patch for one flag from its current definition, with
// any warnings the change calls for.
type Build func(flag map[string]any) (*flagspec.Patch, []string, error)

// Sender delivers a planned patch for a flag.
type Sender func(ctx context.Context, flag map[string]any, patch *flagspec.Patch) (json.RawMessage, error)

// Pending is a planned write: its receipt, and how to carry it out.
type Pending struct {
	Receipt *Receipt
	// patch is what Commit checks the stored flag against, and what Save
	// keeps; nil for writes that set no fields (restore).
	patch *flagspec.Patch
	send  func(ctx context.Context) (json.RawMessage, error)
}

// Changed reports whether the write would change anything.
func (pd *Pending) Changed() bool { return len(pd.Receipt.Changes) > 0 }

// Plan reads the flag ref names and plans the patch build makes, without
// writing. With ifVersion other than NoVersion it refuses unless the flag is
// still at that version. The write PATCHes the flag with its version and the
// flag as read, so PostHog refuses it if someone changed the same fields since.
func (p Project) Plan(ctx context.Context, ref, operation string, ifVersion int, build Build) (*Pending, error) {
	return p.PlanVia(ctx, ref, operation, ifVersion, build, p.patch)
}

// PlanVia is Plan with a different way to deliver the patch, for edits
// PostHog offers a stricter endpoint for.
func (p Project) PlanVia(ctx context.Context, ref, operation string, ifVersion int, build Build, send Sender) (*Pending, error) {
	flag, err := p.Fetch(ctx, ref)
	if err != nil {
		return nil, err
	}
	if err := checkVersion(flag, ref, ifVersion); err != nil {
		return nil, err
	}
	patch, warnings, err := build(flag)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	patch.Prune(flag)
	r := p.receipt(operation, flag)
	r.Changes = flagspec.Changes(flag, patch)
	r.Warnings = warnings
	return &Pending{Receipt: r, patch: patch, send: func(ctx context.Context) (json.RawMessage, error) {
		return send(ctx, flag, patch)
	}}, nil
}

// PlanCreate plans creating a flag from spec.
func (p Project) PlanCreate(ctx context.Context, spec *flagspec.Spec) (*Pending, error) {
	patch, err := flagspec.Build(nil, spec, p.Resolver(ctx))
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	r := p.receipt("create", map[string]any{"key": spec.Key()})
	r.Changes = flagspec.Changes(nil, patch)
	r.Warnings = SpecWarnings(nil, patch)
	if patch.Fields["active"] != true {
		r.Warnings = append(r.Warnings, fmt.Sprintf("The flag is created disabled; enable it with 'agent-posthog flags enable %s --yes'.", spec.Key()))
	}
	return &Pending{Receipt: r, patch: patch, send: func(ctx context.Context) (json.RawMessage, error) {
		return p.Client.Mutate(ctx, http.MethodPost, p.flagsPath(), nil, patch.Body())
	}}, nil
}

// PlanRestore plans undeleting a flag by ID. Deleted flags cannot be read, so
// the plan is the single deleted → false change.
func (p Project) PlanRestore(id int) *Pending {
	r := p.receipt("restore", map[string]any{"id": id})
	r.Changes = []flagspec.Change{{Path: "deleted", Before: true, After: false}}
	r.Warnings = []string{"PostHog may give the restored flag a suffixed key if its key was reused."}
	return &Pending{Receipt: r, send: func(ctx context.Context) (json.RawMessage, error) {
		return p.Client.Patch(ctx, p.flagPath(fmt.Sprint(id)), nil, map[string]any{"deleted": false})
	}}
}

// SpecWarnings reviews the conditions a write leaves the flag with.
func SpecWarnings(current map[string]any, patch *flagspec.Patch) []string {
	groups, changed := patch.Filters["groups"]
	if !changed {
		return nil
	}
	return flagspec.ConditionWarnings(flagspec.List(groups), flagspec.TypeOf(flagspec.Result(current, patch)))
}

// Commit carries out a planned write and fills the receipt from PostHog's
// response, which may differ from what was sent when PostHog normalizes the
// flag; a part of the patch the stored flag does not reflect becomes a warning.
func (pd *Pending) Commit(ctx context.Context) (*Receipt, error) {
	raw, err := pd.send(ctx)
	if err != nil {
		return nil, ClassifyWriteError(err)
	}
	r := pd.Receipt
	r.Outcome = OutcomeApplied
	r.Hint = ""
	stored, err := flagspec.DecodeObject(raw)
	if err != nil {
		return r, nil
	}
	if pd.patch != nil {
		for _, path := range flagspec.Unapplied(pd.patch, stored) {
			r.Warnings = append(r.Warnings, "PostHog stored "+path+" differently than sent; check the flag with 'flags get'.")
		}
	}
	r.ID = stored["id"]
	r.Key = stored["key"]
	r.Version = stored["version"]
	return r, nil
}

func (p Project) patch(ctx context.Context, flag map[string]any, patch *flagspec.Patch) (json.RawMessage, error) {
	body := patch.Body()
	body["version"] = flag["version"]
	body["original_flag"] = flag
	return p.Client.Patch(ctx, p.flagPath(fmt.Sprint(flag["id"])), nil, body)
}

func (p Project) receipt(operation string, flag map[string]any) *Receipt {
	return &Receipt{
		Outcome:      OutcomePlanned,
		Operation:    operation,
		Host:         p.Host,
		ProjectID:    p.ID,
		ProjectAlias: p.Alias,
		ID:           flag["id"],
		Key:          flag["key"],
		BaseVersion:  flag["version"],
		Changes:      []flagspec.Change{},
	}
}

func checkVersion(flag map[string]any, ref string, ifVersion int) error {
	if ifVersion == NoVersion {
		return nil
	}
	current := flagspec.Int(flag["version"])
	if current == ifVersion {
		return nil
	}
	return agenterrors.New(fmt.Sprintf("flag %s is at version %d, not %d; nothing was written", ref, current, ifVersion), agenterrors.FixableByAgent).
		WithHint("Re-read it with 'agent-posthog flags get " + ref + "' and decide the change against its current definition.")
}
