package featureflag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

// planFormat identifies saved plan files.
const planFormat = "agent-posthog-plan/1"

// SavedPlan is a reviewed change to apply exactly, and only while the flag is
// still at the version the plan was made from. It holds the patch, never the
// flag as read, so decrypted payloads stay out of the file.
type SavedPlan struct {
	Format      string            `json:"format"`
	Host        string            `json:"host"`
	ProjectID   int               `json:"project_id"`
	FlagID      any               `json:"flag_id"`
	Key         any               `json:"key"`
	Operation   string            `json:"operation"`
	BaseVersion any               `json:"base_version"`
	Fields      map[string]any    `json:"fields"`
	Filters     map[string]any    `json:"filters"`
	Changes     []flagspec.Change `json:"changes"`
	Warnings    []string          `json:"warnings,omitempty"`
}

// Save writes the planned edit to path for PlanSaved to apply later.
func (pd *Pending) Save(path string) error {
	if pd.patch == nil || pd.Receipt.ID == nil {
		return agenterrors.New("only edits to an existing flag can be saved as a plan, not "+pd.Receipt.Operation, agenterrors.FixableByAgent)
	}
	r := pd.Receipt
	plan := SavedPlan{
		Format: planFormat, Host: r.Host, ProjectID: r.ProjectID, FlagID: r.ID, Key: r.Key,
		Operation: r.Operation, BaseVersion: r.BaseVersion, Fields: pd.patch.Fields, Filters: pd.patch.Filters,
		Changes: r.Changes, Warnings: r.Warnings,
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	return nil
}

// ReadPlan reads a plan file written by Save.
func ReadPlan(path string) (*SavedPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	// Numbers stay json.Number, as in a flag read from PostHog, so the stored
	// flag compares equal to what the plan sends.
	var plan SavedPlan
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&plan); err != nil || plan.Format != planFormat {
		return nil, agenterrors.New(path+" is not an agent-posthog plan", agenterrors.FixableByAgent)
	}
	return &plan, nil
}

// PlanSaved plans applying a saved plan exactly as reviewed. It refuses a
// plan made for another project or host, or for an older flag version.
func (p Project) PlanSaved(ctx context.Context, plan *SavedPlan) (*Pending, error) {
	if plan.Host != p.Host || plan.ProjectID != p.ID {
		return nil, agenterrors.New(fmt.Sprintf("the plan is for project %d on %s, not project %d on %s", plan.ProjectID, plan.Host, p.ID, p.Host), agenterrors.FixableByAgent).
			WithHint("Run apply-plan with the same profile and --project the plan was made with.")
	}
	flag, err := p.Fetch(ctx, "id:"+fmt.Sprint(plan.FlagID))
	if err != nil {
		return nil, err
	}
	if flagspec.Int(flag["version"]) != flagspec.Int(plan.BaseVersion) {
		return nil, agenterrors.New(fmt.Sprintf("flag %v changed since the plan (version %v, plan made at %v); nothing was written", plan.Key, flag["version"], plan.BaseVersion), agenterrors.FixableByAgent).
			WithHint("Plan the change again against the current flag.")
	}
	patch := &flagspec.Patch{Fields: plan.Fields, Filters: plan.Filters}
	if patch.Fields == nil {
		patch.Fields = map[string]any{}
	}
	if patch.Filters == nil {
		patch.Filters = map[string]any{}
	}
	r := p.receipt("apply-plan: "+plan.Operation, flag)
	r.Changes = plan.Changes
	r.Warnings = plan.Warnings
	return &Pending{Receipt: r, patch: patch, send: func(ctx context.Context) (json.RawMessage, error) {
		return p.patch(ctx, flag, patch)
	}}, nil
}
