package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

// planFormat identifies saved plan files.
const planFormat = "agent-posthog-plan/1"

// savedPlan is a reviewed change that 'flags apply-plan' applies exactly, and
// only while the flag is still at the version the plan was made from. It
// holds the patch, never the flag as read, so decrypted payloads stay out of
// the file.
type savedPlan struct {
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

func savePlan(r *receipt, patch *flagspec.Patch, opts writeOptions, format string) error {
	if opts.apply() {
		return agenterrors.New("use --save-plan or --yes, not both; apply a saved plan with 'flags apply-plan'", agenterrors.FixableByAgent)
	}
	if len(r.Changes) == 0 {
		r.Outcome = outcomeUnchanged
		return writeExact(r, format)
	}
	plan := savedPlan{
		Format: planFormat, Host: r.Host, ProjectID: r.ProjectID, FlagID: r.ID, Key: r.Key,
		Operation: r.Operation, BaseVersion: r.BaseVersion, Fields: patch.Fields, Filters: patch.Filters,
		Changes: r.Changes, Warnings: r.Warnings,
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(opts.savePlan, append(data, '\n'), 0o600); err != nil {
		return agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	r.Hint = fmt.Sprintf("Plan saved to %s. Apply exactly this change with 'agent-posthog flags apply-plan --file %s --yes'; it is refused if the flag has changed since.", opts.savePlan, opts.savePlan)
	return writeExact(r, format)
}

func applyPlanCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	var file string
	cmd := &cobra.Command{
		Use:   "apply-plan --file <plan.json>",
		Short: "Apply a plan saved with --save-plan, only if the flag is unchanged since",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.refuseSavePlan("apply-plan"); err != nil {
				return err
			}
			plan, err := readPlan(file)
			if err != nil {
				return err
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				return applyPlan(ctx, resolved, globals, plan, opts)
			})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "Plan file written by --save-plan")
	_ = cmd.MarkFlagRequired("file")
	addWriteFlags(cmd, &opts)
	return cmd
}

func readPlan(file string) (*savedPlan, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	var plan savedPlan
	if err := json.Unmarshal(data, &plan); err != nil || plan.Format != planFormat {
		return nil, agenterrors.New(file+" is not an agent-posthog plan", agenterrors.FixableByAgent)
	}
	return &plan, nil
}

func applyPlan(ctx context.Context, resolved *resolvedContext, globals *GlobalFlags, plan *savedPlan, opts writeOptions) error {
	if plan.Host != resolved.Host || plan.ProjectID != resolved.ProjectID {
		return agenterrors.New(fmt.Sprintf("the plan is for project %d on %s, not project %d on %s", plan.ProjectID, plan.Host, resolved.ProjectID, resolved.Host), agenterrors.FixableByAgent).
			WithHint("Run apply-plan with the same profile and --project the plan was made with.")
	}
	ref := "id:" + fmt.Sprint(plan.FlagID)
	flag, err := fetchFlag(ctx, resolved, resolved.ProjectID, ref)
	if err != nil {
		return err
	}
	if jsonInt(flag["version"]) != jsonInt(plan.BaseVersion) {
		return agenterrors.New(fmt.Sprintf("flag %v changed since the plan (version %v, plan made at %v); nothing was written", plan.Key, flag["version"], plan.BaseVersion), agenterrors.FixableByAgent).
			WithHint("Plan the change again against the current flag.")
	}
	patch := &flagspec.Patch{Fields: plan.Fields, Filters: plan.Filters}
	if patch.Fields == nil {
		patch.Fields = map[string]any{}
	}
	if patch.Filters == nil {
		patch.Filters = map[string]any{}
	}
	r := newReceipt("apply-plan: "+plan.Operation, resolved, flag)
	r.Changes = plan.Changes
	r.Warnings = plan.Warnings
	return commitWrite(r, opts, globals.Format, func() (json.RawMessage, error) {
		return checkStored(r, patch, func() (json.RawMessage, error) { return patchFlag(resolved)(ctx, flag, patch) })
	})
}
