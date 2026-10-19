package cli

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

// testCase is one scenario in a test suite.
type testCase struct {
	Name   string
	In     evalContext
	Expect map[string]any
}

var caseFields = map[string]bool{"name": true, "distinct_id": true, "props": true, "groups": true, "group_props": true, "expect": true}

var expectFields = map[string]bool{"enabled": true, "variant": true, "payload": true, "condition_index": true, "reason": true}

func parseCases(data []byte) ([]testCase, error) {
	doc, err := flagspec.ParseDocument(data)
	if err != nil {
		return nil, err
	}
	if obj, ok := doc.(map[string]any); ok {
		doc = obj["cases"]
	}
	list, ok := doc.([]any)
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("expected a non-empty list of cases (or {\"cases\": [...]})")
	}
	seen := map[string]bool{}
	cases := make([]testCase, 0, len(list))
	for i, item := range list {
		raw, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("case %d must be an object", i)
		}
		for field := range raw {
			if !caseFields[field] {
				return nil, fmt.Errorf("case %d: unknown field %q", i, field)
			}
		}
		name, _ := raw["name"].(string)
		if name == "" || seen[name] {
			return nil, fmt.Errorf("case %d needs a unique name", i)
		}
		seen[name] = true
		expect, _ := raw["expect"].(map[string]any)
		if len(expect) == 0 {
			return nil, fmt.Errorf("case %q has nothing to expect", name)
		}
		for field := range expect {
			if !expectFields[field] {
				return nil, fmt.Errorf("case %q: unknown expectation %q (use %s)", name, field, strings.Join(slices.Sorted(maps.Keys(expectFields)), ", "))
			}
		}
		distinctID, _ := raw["distinct_id"].(string)
		if distinctID == "" {
			distinctID = "agent-posthog-test:" + name
		}
		in := evalContext{DistinctID: distinctID, PersonProps: asObject(raw["props"]), Groups: asObject(raw["groups"]), GroupProperties: asObject(raw["group_props"])}
		cases = append(cases, testCase{Name: name, In: in, Expect: expect})
	}
	return cases, nil
}

// mismatches compares only the fields a case names. A payload must match
// exactly; a null expectation means "none".
func mismatches(expect map[string]any, r evalResult) []any {
	actual := map[string]any{
		"enabled": r.Enabled, "variant": r.Variant, "reason": r.Reason, "condition_index": r.ConditionIndex,
	}
	if r.PayloadSet {
		actual["payload"] = r.Payload
	} else {
		actual["payload"] = nil
	}
	var out []any
	for _, field := range slices.Sorted(maps.Keys(expectFields)) {
		want, ok := expect[field]
		if !ok || flagspec.Equal(want, actual[field]) {
			continue
		}
		out = append(out, map[string]any{"field": field, "expected": want, "actual": actual[field]})
	}
	return out
}

func testCommand(globals *GlobalFlags) *cobra.Command {
	var f evalFlags
	var casesFile, casesJSON string
	cmd := &cobra.Command{
		Use:   "test <id-or-key>",
		Short: "Run scenario cases against a saved flag; exits 1 if any case fails or cannot be evaluated",
		Long: "Cases (--cases file or --cases-json; JSON or YAML) are a list of " +
			`{name, distinct_id?, props?, groups?, group_props?, expect: {enabled?, variant?, payload?, condition_index?, reason?}}. ` +
			"Only the expected fields are checked; payload must match exactly. distinct_id defaults to " +
			"agent-posthog-test:<name> so percentage rollouts are stable. Prints one line per case, then @summary.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readInput(casesJSON, casesFile, "cases-json", "cases")
			if err != nil {
				return err
			}
			cases, err := parseCases(data)
			if err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			for _, c := range cases {
				if err := checkEngineContext(f.engine, c.In); err != nil {
					return err
				}
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				flag, err := fetchFlag(ctx, resolved, resolved.ProjectID, args[0])
				if err != nil {
					return err
				}
				e, err := newEvaluator(ctx, resolved, flag, f.engine, f.flagsHost)
				if err != nil {
					return err
				}
				return runCases(ctx, e, cases)
			})
		},
	}
	cmd.Flags().StringVar(&casesFile, "cases", "", "Cases file (JSON or YAML)")
	cmd.Flags().StringVar(&casesJSON, "cases-json", "", "Cases as inline JSON")
	f.add(cmd)
	return cmd
}

func runCases(ctx context.Context, e *evaluator, cases []testCase) error {
	counts := map[string]int{"pass": 0, "fail": 0, "error": 0}
	for _, c := range cases {
		r := e.evaluate(ctx, c.In)
		line := flagspec.NewOrdered()
		line.Set("case", c.Name)
		diff := mismatches(c.Expect, r)
		switch {
		case r.Status == "error":
			line.Set("result", "error")
			line.Set("error", r.Error)
			counts["error"]++
		case len(diff) == 0:
			line.Set("result", "pass")
			counts["pass"]++
		default:
			line.Set("result", "fail")
			line.Set("mismatches", diff)
			counts["fail"]++
		}
		line.Set("actual", e.record(c.In, r))
		if err := writeExactLine(line); err != nil {
			return err
		}
	}
	summary := flagspec.NewOrdered()
	summary.Set("engine", e.engine)
	summary.Set("pass", counts["pass"])
	summary.Set("fail", counts["fail"])
	summary.Set("error", counts["error"])
	if warnings := e.warnings(); len(warnings) > 0 {
		summary.Set("warnings", warnings)
	}
	wrapper := flagspec.NewOrdered()
	wrapper.Set("@summary", summary)
	if err := writeExactLine(wrapper); err != nil {
		return err
	}
	if bad := counts["fail"] + counts["error"]; bad > 0 {
		return agenterrors.New(fmt.Sprintf("%d of %d cases did not pass", bad, len(cases)), agenterrors.FixableByAgent).
			WithHint("Read the per-case mismatches on stdout.")
	}
	return nil
}
