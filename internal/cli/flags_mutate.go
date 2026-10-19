package cli

import (
	"context"
	"fmt"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/featureflag"
)

// writeOptions are the flags every flag write takes. Without --yes (or with
// --dry-run, which wins, because the MCP bridge injects --yes) a write prints
// its plan and changes nothing.
type writeOptions struct {
	yes       bool
	dryRun    bool
	ifVersion int
	savePlan  string
}

func addWriteFlags(cmd *cobra.Command, opts *writeOptions) {
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Apply the change; without it the plan is printed and nothing is written")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Print the plan without writing, even with --yes")
	cmd.Flags().IntVar(&opts.ifVersion, "if-version", featureflag.NoVersion, "Refuse unless the flag is still at this version (from your last read)")
	cmd.Flags().StringVar(&opts.savePlan, "save-plan", "", "Write the planned change to this file for 'flags apply-plan' instead of applying it")
}

// refuseSavePlan is for writes that saved plans do not cover (creates, copies,
// restores): they have no existing flag version to hold the plan to.
func (o writeOptions) refuseSavePlan(operation string) error {
	if o.savePlan == "" {
		return nil
	}
	return agenterrors.New("--save-plan covers edits to an existing flag, not "+operation, agenterrors.FixableByAgent)
}

func (o writeOptions) apply() bool { return o.yes && !o.dryRun }

// requireVersion is for edits that address a condition by position: the
// caller's version proves the index still names the condition they read.
func (o writeOptions) requireVersion(ref string) error {
	if o.ifVersion != featureflag.NoVersion {
		return nil
	}
	return agenterrors.New("this edit addresses a condition by position, so it needs --if-version", agenterrors.FixableByAgent).
		WithHint("Run 'agent-posthog flags conditions list " + ref + "' and pass the version it shows.")
}

const applyHint = "Nothing was written. Re-run with --yes to apply; that re-reads the flag, so add --if-version <base_version> to apply only if it is still unchanged."

// withFlags runs fn against the current project's feature flags.
func withFlags(cmd *cobra.Command, globals *GlobalFlags, fn func(ctx context.Context, p featureflag.Project) error) error {
	return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
		p, err := resolved.flagProject()
		if err != nil {
			return err
		}
		return fn(ctx, p)
	})
}

// editFlag plans the change build makes to the flag ref names and presents it.
func editFlag(ctx context.Context, p featureflag.Project, ref, operation string, opts writeOptions, build featureflag.Build, format string) error {
	pending, err := p.Plan(ctx, ref, operation, opts.ifVersion, build)
	if err != nil {
		return err
	}
	return writeFlag(ctx, pending, opts, format)
}

// writeFlag turns a planned flag write into its outcome: unchanged when
// nothing differs, saved with --save-plan, a plan without --yes (or with
// --dry-run), otherwise the write and PostHog's view of the result.
func writeFlag(ctx context.Context, pending *featureflag.Pending, opts writeOptions, format string) error {
	if opts.savePlan != "" && opts.apply() {
		return agenterrors.New("use --save-plan or --yes, not both; apply a saved plan with 'flags apply-plan'", agenterrors.FixableByAgent)
	}
	r := pending.Receipt
	switch {
	case !pending.Changed():
		r.Outcome = featureflag.OutcomeUnchanged
	case opts.savePlan != "":
		if err := pending.Save(opts.savePlan); err != nil {
			return err
		}
		r.Hint = fmt.Sprintf("Plan saved to %s. Apply exactly this change with 'agent-posthog flags apply-plan --file %s --yes'; it is refused if the flag has changed since.", opts.savePlan, opts.savePlan)
	case !opts.apply():
		r.Hint = applyHint
	default:
		applied, err := pending.Commit(ctx)
		if err != nil {
			return err
		}
		r = applied
	}
	return writeExact(r, format)
}
