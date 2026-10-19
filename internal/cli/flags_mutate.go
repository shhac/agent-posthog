package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/shhac/lib-agent-cli/creds"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/api"
	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

// writeOptions are the flags every flag write takes. Without --yes (or with
// --dry-run, which wins, because the MCP bridge injects --yes) a write prints
// its plan and changes nothing.
type writeOptions struct {
	yes       bool
	dryRun    bool
	ifVersion int
}

const noVersion = -1

func addWriteFlags(cmd *cobra.Command, opts *writeOptions) {
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Apply the change; without it the plan is printed and nothing is written")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Print the plan without writing, even with --yes")
	cmd.Flags().IntVar(&opts.ifVersion, "if-version", noVersion, "Refuse unless the flag is still at this version (from your last read)")
}

func (o writeOptions) apply() bool { return o.yes && !o.dryRun }

// receipt reports a planned or applied write. Before/after values are written
// exactly (no null pruning), since a null is often the change.
type receipt struct {
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
	outcomePlanned   = "planned"
	outcomeApplied   = "applied"
	outcomeUnchanged = "unchanged"
)

const applyHint = "Nothing was written. Re-run with --yes to apply; that re-reads the flag, so add --if-version <base_version> to apply only if it is still unchanged."

// flagUpdate builds the patch for one flag from its current definition.
type flagUpdate func(flag map[string]any) (*flagspec.Patch, []string, error)

// updateFlag is the one read → plan → write path behind every flag edit. The
// PATCH carries the version and the flag as read, so PostHog refuses it if
// someone else changed the same fields in between.
func updateFlag(ctx context.Context, resolved *resolvedContext, globals *GlobalFlags, ref, operation string, opts writeOptions, build flagUpdate) error {
	return updateFlagVia(ctx, resolved, globals, ref, operation, opts, build, patchFlag(resolved))
}

// flagSender delivers a planned patch for a flag.
type flagSender func(ctx context.Context, flag map[string]any, patch *flagspec.Patch) (json.RawMessage, error)

func patchFlag(resolved *resolvedContext) flagSender {
	return func(ctx context.Context, flag map[string]any, patch *flagspec.Patch) (json.RawMessage, error) {
		body := patch.Body()
		body["version"] = flag["version"]
		body["original_flag"] = flag
		return resolved.Client.Patch(ctx, flagPath(resolved.ProjectID, fmt.Sprint(flag["id"])), nil, body)
	}
}

// updateFlagVia is updateFlag with a different way to deliver the patch, for
// edits PostHog offers a stricter endpoint for.
func updateFlagVia(ctx context.Context, resolved *resolvedContext, globals *GlobalFlags, ref, operation string, opts writeOptions, build flagUpdate, send flagSender) error {
	if err := requireProject(resolved); err != nil {
		return err
	}
	flag, err := fetchFlag(ctx, resolved, resolved.ProjectID, ref)
	if err != nil {
		return err
	}
	if err := checkVersion(flag, ref, opts); err != nil {
		return err
	}
	patch, warnings, err := build(flag)
	if err != nil {
		return agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	patch.Prune(flag)
	r := newReceipt(operation, resolved, flag)
	r.Changes = flagspec.Changes(flag, patch)
	r.Warnings = warnings
	return commitWrite(r, opts, globals.Format, func() (json.RawMessage, error) {
		return checkStored(r, patch, func() (json.RawMessage, error) { return send(ctx, flag, patch) })
	})
}

// commitWrite is where every flag write turns into its outcome: unchanged when
// nothing differs, a plan without --yes (or with --dry-run), otherwise the
// write and the server's view of the result.
func commitWrite(r *receipt, opts writeOptions, format string, send func() (json.RawMessage, error)) error {
	if len(r.Changes) == 0 {
		r.Outcome = outcomeUnchanged
		return writeExact(r, format)
	}
	if !opts.apply() {
		r.Hint = applyHint
		return writeExact(r, format)
	}
	raw, err := send()
	if err != nil {
		return classifyWriteError(err)
	}
	return writeApplied(r, raw, format)
}

func newReceipt(operation string, resolved *resolvedContext, flag map[string]any) *receipt {
	return &receipt{
		Outcome:      outcomePlanned,
		Operation:    operation,
		Host:         resolved.Host,
		ProjectID:    resolved.ProjectID,
		ProjectAlias: resolved.ProjectAlias,
		ID:           flag["id"],
		Key:          flag["key"],
		BaseVersion:  flag["version"],
		Changes:      []flagspec.Change{},
	}
}

// checkStored sends a write and warns about any part of it the stored flag
// does not reflect, so a normalization by PostHog is visible in the receipt.
func checkStored(r *receipt, patch *flagspec.Patch, send func() (json.RawMessage, error)) (json.RawMessage, error) {
	raw, err := send()
	if err != nil {
		return nil, err
	}
	stored, decodeErr := flagspec.DecodeObject(raw)
	if decodeErr != nil {
		return raw, nil
	}
	for _, path := range flagspec.Unapplied(patch, stored) {
		r.Warnings = append(r.Warnings, "PostHog stored "+path+" differently than sent; check the flag with 'flags get'.")
	}
	return raw, nil
}

// writeApplied fills the receipt from the server's response, which may differ
// from what was sent when PostHog normalizes the flag.
func writeApplied(r *receipt, raw json.RawMessage, format string) error {
	r.Outcome = outcomeApplied
	r.Hint = ""
	if updated, err := flagspec.DecodeObject(raw); err == nil {
		r.ID = updated["id"]
		r.Key = updated["key"]
		r.Version = updated["version"]
	}
	return writeExact(r, format)
}

func checkVersion(flag map[string]any, ref string, opts writeOptions) error {
	if opts.ifVersion == noVersion {
		return nil
	}
	current := jsonInt(flag["version"])
	if current == opts.ifVersion {
		return nil
	}
	return agenterrors.New(fmt.Sprintf("flag %s is at version %d, not %d; nothing was written", ref, current, opts.ifVersion), agenterrors.FixableByAgent).
		WithHint("Re-read it with 'agent-posthog flags get " + ref + "' and decide the change against its current definition.")
}

// classifyWriteError separates approval-policy refusals (a human must approve)
// from stale-version conflicts (re-read and retry), which share HTTP 409.
func classifyWriteError(err error) error {
	var httpErr *api.HTTPError
	if !errors.As(err, &httpErr) {
		return err
	}
	var body struct {
		Code            string `json:"code"`
		Detail          string `json:"detail"`
		Error           string `json:"error"`
		ChangeRequestID string `json:"change_request_id"`
		Guidance        string `json:"guidance"`
	}
	_ = json.Unmarshal(httpErr.Body, &body)
	switch {
	case httpErr.Status == http.StatusConflict && (body.Code == "approval_required" || body.Code == "change_request_pending"):
		return agenterrors.New(fmt.Sprintf("Not applied: an approval policy gates this change (change request %s)", body.ChangeRequestID), agenterrors.FixableByHuman).
			WithHint("Ask an approver to review the change request in PostHog. Nothing was written.")
	case httpErr.Status == http.StatusConflict:
		return agenterrors.New("Not applied: "+creds.FirstNonEmpty(body.Detail, body.Error, "the flag changed since it was read"), agenterrors.FixableByAgent).
			WithHint("Re-read the flag with 'agent-posthog flags get <ref>' and decide the change against its current definition.")
	case body.Code == "policy_conflict":
		return agenterrors.New("Not applied: "+creds.FirstNonEmpty(body.Error, body.Detail, "the change matches more than one approval policy"), agenterrors.FixableByHuman).
			WithHint(creds.FirstNonEmpty(body.Guidance, "Split the change so each approval policy applies on its own."))
	default:
		return err
	}
}
