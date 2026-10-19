package featureflag

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

// Case is one scenario in a test suite.
type Case struct {
	Name   string
	In     Subject
	Expect map[string]any
}

var caseFields = map[string]bool{"name": true, "distinct_id": true, "props": true, "groups": true, "group_props": true, "at": true, "device_id": true, "evaluation_contexts": true, "expect": true}

var expectFields = map[string]bool{"enabled": true, "variant": true, "payload": true, "condition_index": true, "reason": true}

// ParseCases reads a suite: a list of cases, or {"cases": [...]}, in JSON or
// YAML. A case without a distinct_id gets agent-posthog-test:<name>, so
// percentage rollouts are stable.
func ParseCases(data []byte) ([]Case, error) {
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
	cases := make([]Case, 0, len(list))
	for i, item := range list {
		raw, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("case %d must be an object", i)
		}
		c, err := parseCase(i, raw, seen)
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// parseCase reads case i, refusing a name already in seen.
func parseCase(i int, raw map[string]any, seen map[string]bool) (Case, error) {
	if field, ok := unknownKey(raw, caseFields); ok {
		return Case{}, fmt.Errorf("case %d: unknown field %q", i, field)
	}
	name, _ := raw["name"].(string)
	if name == "" || seen[name] {
		return Case{}, fmt.Errorf("case %d needs a unique name", i)
	}
	seen[name] = true
	expect, _ := raw["expect"].(map[string]any)
	if len(expect) == 0 {
		return Case{}, fmt.Errorf("case %q has nothing to expect", name)
	}
	if field, ok := unknownKey(expect, expectFields); ok {
		return Case{}, fmt.Errorf("case %q: unknown expectation %q (use %s)", name, field, strings.Join(slices.Sorted(maps.Keys(expectFields)), ", "))
	}
	distinctID, _ := raw["distinct_id"].(string)
	if distinctID == "" {
		distinctID = "agent-posthog-test:" + name
	}
	rawAt, _ := raw["at"].(string)
	at, err := NormalizeAt(rawAt)
	if err != nil {
		return Case{}, fmt.Errorf("case %q: %w", name, err)
	}
	deviceID, _ := raw["device_id"].(string)
	var contexts []string
	for _, c := range flagspec.List(raw["evaluation_contexts"]) {
		contexts = append(contexts, fmt.Sprint(c))
	}
	in := Subject{
		DistinctID: distinctID, PersonProps: flagspec.Object(raw["props"]), Groups: flagspec.Object(raw["groups"]),
		GroupProperties: flagspec.Object(raw["group_props"]), At: at, DeviceID: deviceID, EvaluationContexts: contexts,
	}
	return Case{Name: name, In: in, Expect: expect}, nil
}

// unknownKey is the first key of m, in sorted order, that allowed lacks.
func unknownKey(m map[string]any, allowed map[string]bool) (string, bool) {
	for _, key := range slices.Sorted(maps.Keys(m)) {
		if !allowed[key] {
			return key, true
		}
	}
	return "", false
}

// mismatches compares only the fields a case names. A payload must match
// exactly; a null expectation means "none".
func mismatches(expect map[string]any, r Result) []any {
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

// MaxParallelCases bounds parallel runs so a suite cannot flood /flags.
const MaxParallelCases = 8

// evaluateCases runs up to parallel evaluations at once and returns results in
// case order, so output is the same whatever the parallelism.
func evaluateCases(ctx context.Context, e *Evaluator, cases []Case, parallel int) []Result {
	results := make([]Result, len(cases))
	slots := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, c := range cases {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			results[i] = e.Evaluate(ctx, c.In)
		}()
	}
	wg.Wait()
	return results
}

// CaseResult is how one case fared: pass, fail (with the mismatched
// fields), or error (PostHog could not evaluate it).
type CaseResult struct {
	Case       Case
	Outcome    string
	Mismatches []any
	Result     Result
}

// Summary counts a suite's outcomes.
type Summary struct {
	Pass, Fail, Error int
}

// Passed reports whether every case passed.
func (s Summary) Passed() bool { return s.Fail+s.Error == 0 }

// Run evaluates every case, up to parallel at once, returning results in case
// order.
func (e *Evaluator) Run(ctx context.Context, cases []Case, parallel int) ([]CaseResult, Summary) {
	var summary Summary
	results := evaluateCases(ctx, e, cases, parallel)
	out := make([]CaseResult, len(cases))
	for i, c := range cases {
		r := results[i]
		cr := CaseResult{Case: c, Result: r}
		diff := mismatches(c.Expect, r)
		switch {
		case r.Status == "error":
			cr.Outcome = "error"
			summary.Error++
		case len(diff) == 0:
			cr.Outcome = "pass"
			summary.Pass++
		default:
			cr.Outcome = "fail"
			cr.Mismatches = diff
			summary.Fail++
		}
		out[i] = cr
	}
	return out, summary
}
