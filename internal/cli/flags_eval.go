package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/api"
	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

// Evaluation engines. public calls PostHog's /flags endpoint, as an SDK would,
// and accepts property overrides. diagnostic calls the private test_evaluation
// endpoint, which uses stored person properties only.
const (
	enginePublic     = "public"
	engineDiagnostic = "diagnostic"
)

func registerFlagEvalCommands(flags *cobra.Command, globals *GlobalFlags) {
	flags.AddCommand(evaluateCommand(globals))
	flags.AddCommand(testCommand(globals))
	flags.AddCommand(reachCommand(globals))
}

// evalContext is who a flag is evaluated for.
type evalContext struct {
	DistinctID      string
	PersonProps     map[string]any
	Groups          map[string]any
	GroupProperties map[string]any
}

// evalResult is one evaluation. Status "error" means PostHog could not
// evaluate the flag: it is never reported as false.
type evalResult struct {
	Status         string
	Enabled        bool
	Variant        any
	Payload        any
	PayloadSet     bool
	Reason         any
	ConditionIndex any
	Error          string
}

type evaluator struct {
	resolved  *resolvedContext
	flag      map[string]any
	engine    string
	public    *api.Client
	projToken string
}

func newEvaluator(ctx context.Context, resolved *resolvedContext, flag map[string]any, engine, flagsHostOverride string) (*evaluator, error) {
	e := &evaluator{resolved: resolved, flag: flag, engine: engine}
	switch engine {
	case engineDiagnostic:
		return e, nil
	case enginePublic:
	default:
		return nil, agenterrors.New("--engine must be public or diagnostic", agenterrors.FixableByAgent)
	}
	raw, err := resolved.Client.Get(ctx, fmt.Sprintf("/api/projects/%d/", resolved.ProjectID), nil)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman).
			WithHint("The public engine reads the project token from the project; the personal API key needs project read access. Or use --engine diagnostic.")
	}
	var project struct {
		APIToken string `json:"api_token"`
	}
	if err := json.Unmarshal(raw, &project); err != nil || project.APIToken == "" {
		return nil, agenterrors.New("the project did not include its token", agenterrors.FixableByHuman).
			WithHint("Use --engine diagnostic, which needs no project token.")
	}
	e.projToken = project.APIToken
	e.public = resolved.Client.WithBase(flagsHost(resolved.Host, flagsHostOverride), "")
	return e, nil
}

// flagsHost maps a PostHog app host to the host that serves /flags. Cloud
// splits them; self-hosted and proxied setups serve both from one host or
// need AGENT_POSTHOG_FLAGS_HOST / --flags-host.
func flagsHost(appHost, override string) string {
	if override != "" {
		return override
	}
	if env := os.Getenv("AGENT_POSTHOG_FLAGS_HOST"); env != "" {
		return env
	}
	u, err := url.Parse(appHost)
	if err != nil {
		return appHost
	}
	switch u.Host {
	case "us.posthog.com", "app.posthog.com":
		u.Host = "us.i.posthog.com"
	case "eu.posthog.com":
		u.Host = "eu.i.posthog.com"
	}
	return u.String()
}

func (e *evaluator) evaluate(ctx context.Context, in evalContext) evalResult {
	if e.engine == engineDiagnostic {
		return e.diagnostic(ctx, in)
	}
	return e.publicEval(ctx, in)
}

func (e *evaluator) diagnostic(ctx context.Context, in evalContext) evalResult {
	body := map[string]any{"distinct_id": in.DistinctID}
	if len(in.Groups) > 0 {
		body["groups"] = in.Groups
	}
	raw, err := e.resolved.Client.Post(ctx, flagPath(e.resolved.ProjectID, fmt.Sprint(e.flag["id"]))+"test_evaluation/", nil, body)
	if err != nil {
		return evalResult{Status: "error", Error: err.Error()}
	}
	resp, err := flagspec.DecodeObject(raw)
	if err != nil {
		return evalResult{Status: "error", Error: "unexpected test_evaluation response"}
	}
	r := evalResult{Status: "ok", Reason: resp["reason"], ConditionIndex: resp["condition_index"]}
	switch value := resp["result"].(type) {
	case bool:
		r.Enabled = value
	case string:
		r.Enabled, r.Variant = true, value
	}
	if r.Enabled {
		r.Payload, r.PayloadSet = decodeEvalPayload(resp["payload"])
	}
	return r
}

func (e *evaluator) publicEval(ctx context.Context, in evalContext) evalResult {
	body := map[string]any{"api_key": e.projToken, "distinct_id": in.DistinctID}
	if len(in.PersonProps) > 0 {
		body["person_properties"] = in.PersonProps
	}
	if len(in.Groups) > 0 {
		body["groups"] = in.Groups
	}
	if len(in.GroupProperties) > 0 {
		body["group_properties"] = in.GroupProperties
	}
	raw, err := e.public.Post(ctx, "/flags", url.Values{"v": {"2"}}, body)
	if err != nil {
		return evalResult{Status: "error", Error: err.Error()}
	}
	resp, err := flagspec.DecodeObject(raw)
	if err != nil {
		return evalResult{Status: "error", Error: "unexpected /flags response"}
	}
	key := fmt.Sprint(e.flag["key"])
	detail, returned := asObject(resp["flags"])[key].(map[string]any)
	switch {
	case !returned && e.flag["active"] != true:
		return evalResult{Status: "ok", Reason: "flag_disabled"}
	case !returned && resp["errorsWhileComputingFlags"] == true:
		return evalResult{Status: "error", Error: "PostHog reported errors while computing flags and did not return this one"}
	case !returned:
		return evalResult{Status: "error", Error: "PostHog did not return this flag; its evaluation runtime or contexts may exclude this request"}
	case detail["failed"] == true:
		return evalResult{Status: "error", Error: fmt.Sprint("evaluation failed: ", asObject(detail["reason"])["description"])}
	}
	reason := asObject(detail["reason"])
	r := evalResult{
		Status:         "ok",
		Enabled:        detail["enabled"] == true,
		Variant:        detail["variant"],
		Reason:         reason["code"],
		ConditionIndex: reason["condition_index"],
	}
	if r.Enabled {
		r.Payload, r.PayloadSet = decodeEvalPayload(asObject(detail["metadata"])["payload"])
	}
	return r
}

func decodeEvalPayload(stored any) (any, bool) {
	if stored == nil {
		return nil, false
	}
	value, err := flagspec.DecodePayload(stored)
	if err != nil {
		return stored, true
	}
	return value, true
}

func (e *evaluator) record(in evalContext, r evalResult) *flagspec.Ordered {
	out := flagspec.NewOrdered()
	out.Set("engine", e.engine)
	out.Set("key", e.flag["key"])
	out.Set("distinct_id", in.DistinctID)
	out.Set("status", r.Status)
	if r.Status == "error" {
		out.Set("error", r.Error)
		return out
	}
	out.Set("enabled", r.Enabled)
	out.Set("variant", r.Variant)
	if r.PayloadSet {
		out.Set("payload", r.Payload)
	}
	out.Set("reason", r.Reason)
	out.Set("condition_index", r.ConditionIndex)
	if index, ok := r.ConditionIndex.(json.Number); ok {
		groups := asList(asObject(e.flag["filters"])["groups"])
		if i := jsonInt(index); i >= 0 && i < len(groups) {
			out.Set("condition", flagspec.ExportConditions(groups[i : i+1])[0])
		}
	}
	return out
}

func (e *evaluator) warnings() []string {
	var warnings []string
	if e.flag["active"] != true {
		warnings = append(warnings, "The flag is disabled, so it evaluates to false for everyone; this tests the saved definition only once it is enabled.")
	}
	if e.engine == enginePublic && asObject(e.flag["filters"])["early_exit"] == true {
		warnings = append(warnings, "Early exit is on: /flags honors it, but SDKs evaluating locally honor it only in recent versions.")
	}
	return warnings
}

type evalFlags struct {
	engine, flagsHost, propsJSON, groupsJSON, groupPropsJSON string
}

func (f *evalFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.engine, "engine", enginePublic, "public (/flags, as an SDK sees it; accepts property overrides) or diagnostic (stored properties only)")
	cmd.Flags().StringVar(&f.flagsHost, "flags-host", "", "Host serving /flags (default: derived from the PostHog host, or AGENT_POSTHOG_FLAGS_HOST)")
}

func (f *evalFlags) addContext(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.propsJSON, "props-json", "", `Person properties for this evaluation, e.g. '{"app_env":"staging"}' (public engine)`)
	cmd.Flags().StringVar(&f.groupsJSON, "groups-json", "", `Groups, e.g. '{"company":"acme"}'`)
	cmd.Flags().StringVar(&f.groupPropsJSON, "group-props-json", "", `Group properties, e.g. '{"company":{"plan":"pro"}}' (public engine)`)
}

func evaluateCommand(globals *GlobalFlags) *cobra.Command {
	var f evalFlags
	var distinctID string
	cmd := &cobra.Command{
		Use:   "evaluate <id-or-key>",
		Short: "Evaluate a saved flag for one user: enabled, variant, payload, and which condition matched",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in, err := f.context(distinctID)
			if err != nil {
				return err
			}
			if err := checkEngineContext(f.engine, in); err != nil {
				return err
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
				r := e.evaluate(ctx, in)
				if r.Status == "error" {
					return agenterrors.New("Could not evaluate the flag: "+r.Error, agenterrors.FixableByRetry).
						WithHint("This is not a false result. Retry, or try the other --engine.")
				}
				record := e.record(in, r)
				if warnings := e.warnings(); len(warnings) > 0 {
					record.Set("warnings", warnings)
				}
				return writeExact(record, globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&distinctID, "distinct-id", "", "Distinct ID to evaluate for (required; it also decides percentage rollouts)")
	f.add(cmd)
	f.addContext(cmd)
	return cmd
}

func (f *evalFlags) context(distinctID string) (evalContext, error) {
	if distinctID == "" {
		return evalContext{}, agenterrors.New("--distinct-id is required; it decides percentage rollouts", agenterrors.FixableByAgent)
	}
	in := evalContext{DistinctID: distinctID}
	for _, field := range []struct {
		name, value string
		target      *map[string]any
	}{
		{"props-json", f.propsJSON, &in.PersonProps},
		{"groups-json", f.groupsJSON, &in.Groups},
		{"group-props-json", f.groupPropsJSON, &in.GroupProperties},
	} {
		if field.value == "" {
			continue
		}
		obj, err := flagspec.DecodeObject([]byte(field.value))
		if err != nil {
			return evalContext{}, agenterrors.New("--"+field.name+" must be a JSON object: "+err.Error(), agenterrors.FixableByAgent)
		}
		*field.target = obj
	}
	return in, nil
}

func checkEngineContext(engine string, in evalContext) error {
	if engine == engineDiagnostic && (in.PersonProps != nil || in.GroupProperties != nil) {
		return agenterrors.New("the diagnostic engine uses stored properties only; drop property overrides or use --engine public", agenterrors.FixableByAgent)
	}
	return nil
}

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

func reachCommand(globals *GlobalFlags) *cobra.Command {
	var condition int
	cmd := &cobra.Command{
		Use:   "reach <id-or-key>",
		Short: "Count users each condition's filters match (ignores rollout; conditions overlap)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				flag, err := fetchFlag(ctx, resolved, resolved.ProjectID, args[0])
				if err != nil {
					return err
				}
				filters := asObject(flag["filters"])
				groups := asList(filters["groups"])
				indexes := make([]int, 0, len(groups))
				for i := range groups {
					if !cmd.Flags().Changed("condition") || i == condition {
						indexes = append(indexes, i)
					}
				}
				if len(indexes) == 0 {
					return agenterrors.New(fmt.Sprintf("the flag has no condition %d", condition), agenterrors.FixableByAgent)
				}
				for _, i := range indexes {
					group := asObject(groups[i])
					aggregation, ok := group["aggregation_group_type_index"]
					if !ok {
						aggregation = filters["aggregation_group_type_index"]
					}
					body := map[string]any{"condition": group, "group_type_index": aggregation}
					raw, err := resolved.Client.Post(ctx, flagsPath(resolved.ProjectID)+"user_blast_radius/", nil, body)
					if err != nil {
						return err
					}
					counts, err := flagspec.DecodeObject(raw)
					if err != nil {
						return agenterrors.Wrap(err, agenterrors.FixableByRetry)
					}
					line := flagspec.NewOrdered()
					line.Set("index", i)
					line.Set("matching", counts["affected"])
					line.Set("total", counts["total"])
					line.Set("rollout", group["rollout_percentage"])
					if err := writeExactLine(line); err != nil {
						return err
					}
				}
				return writeExactLine(map[string]string{"@note": "matching counts users whose properties pass a condition's filters, before its rollout; conditions overlap, so do not sum them"})
			})
		},
	}
	cmd.Flags().IntVar(&condition, "condition", 0, "Only this condition index")
	return cmd
}
