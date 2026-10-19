package cli

import (
	"context"
	"fmt"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/featureflag"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func testCommand(globals *GlobalFlags) *cobra.Command {
	var f evalFlags
	var casesFile, casesJSON string
	var parallel int
	cmd := &cobra.Command{
		Use:   "test <id-or-key>",
		Short: "Run scenario cases against a saved flag; exits 1 if any case fails or cannot be evaluated",
		Long: "Cases (--cases file or --cases-json; JSON or YAML) are a list of " +
			`{name, distinct_id?, props?, groups?, group_props?, device_id?, evaluation_contexts?, at?, expect: {enabled?, variant?, payload?, condition_index?, reason?}}. ` +
			"at (an ISO 8601 time) replays the case as of then and needs --engine diagnostic. " +
			"Only the expected fields are checked; payload must match exactly. distinct_id defaults to " +
			"agent-posthog-test:<name> so percentage rollouts are stable. Prints one line per case, then @summary.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if parallel < 1 || parallel > featureflag.MaxParallelCases {
				return agenterrors.New(fmt.Sprintf("--parallel must be from 1 to %d", featureflag.MaxParallelCases), agenterrors.FixableByAgent)
			}
			data, err := ReadInput(casesJSON, casesFile, "cases-json", "cases")
			if err != nil {
				return err
			}
			cases, err := featureflag.ParseCases(data)
			if err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			for _, c := range cases {
				if err := featureflag.CheckSubject(f.engine, c.In); err != nil {
					return err
				}
			}
			return WithProject(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				e, err := f.evaluator(ctx, p, args[0])
				if err != nil {
					return err
				}
				results, summary := e.Run(ctx, cases, parallel)
				return writeCaseResults(e, results, summary, globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&casesFile, "cases", "", "Cases file (JSON or YAML)")
	cmd.Flags().IntVar(&parallel, "parallel", 1, fmt.Sprintf("Cases evaluated at once (1-%d); results print in case order", featureflag.MaxParallelCases))
	cmd.Flags().StringVar(&casesJSON, "cases-json", "", "Cases as inline JSON")
	f.add(cmd)
	return cmd
}

// writeCaseResults prints one line per case and an @summary, then fails the
// command if any case did not pass.
func writeCaseResults(e *featureflag.Evaluator, results []featureflag.CaseResult, summary featureflag.Summary, format string) error {
	lines := make([]any, 0, len(results))
	for _, cr := range results {
		line := flagspec.NewOrdered()
		line.Set("case", cr.Case.Name)
		line.Set("result", cr.Outcome)
		switch cr.Outcome {
		case "error":
			line.Set("error", cr.Result.Error)
		case "fail":
			line.Set("mismatches", cr.Mismatches)
		}
		line.Set("actual", e.Record(cr.Case.In, cr.Result))
		lines = append(lines, line)
	}
	totals := flagspec.NewOrdered()
	totals.Set("engine", e.Engine())
	totals.Set("pass", summary.Pass)
	totals.Set("fail", summary.Fail)
	totals.Set("error", summary.Error)
	if warnings := e.Warnings(); len(warnings) > 0 {
		totals.Set("warnings", warnings)
	}
	if err := WriteExactList(lines, map[string]any{"@summary": totals}, format); err != nil {
		return err
	}
	if summary.Passed() {
		return nil
	}
	return agenterrors.New(fmt.Sprintf("%d of %d cases did not pass", summary.Fail+summary.Error, len(results)), agenterrors.FixableByAgent).
		WithHint("Read the per-case mismatches on stdout.")
}
