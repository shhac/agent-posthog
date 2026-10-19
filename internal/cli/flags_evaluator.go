package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"slices"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/api"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

// Evaluation engines. public calls PostHog's /flags endpoint, as an SDK would,
// and accepts property overrides. diagnostic calls the private test_evaluation
// endpoint, which uses stored person properties only.
const (
	enginePublic     = "public"
	engineDiagnostic = "diagnostic"
)

// evalContext is who a flag is evaluated for.
type evalContext struct {
	DistinctID      string
	PersonProps     map[string]any
	Groups          map[string]any
	GroupProperties map[string]any
	// At replays the evaluation as of a past moment (diagnostic engine only).
	At                 string
	DeviceID           string
	EvaluationContexts []string
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
	Description    any
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
	raw, err := resolved.Client.Get(ctx, projectAPIPath(resolved.ProjectID), nil)
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
	if in.At != "" {
		body["timestamp"] = in.At
	}
	raw, err := e.resolved.Client.Post(ctx, flagPath(e.resolved.ProjectID, fmt.Sprint(e.flag["id"]), "test_evaluation"), nil, body)
	if err != nil {
		return evalResult{Status: "error", Error: err.Error()}
	}
	resp, err := flagspec.DecodeObject(raw)
	if err != nil {
		return evalResult{Status: "error", Error: "unexpected test_evaluation response"}
	}
	r := evalResult{Status: "ok", Reason: resp["reason"], Description: resp["reason_description"], ConditionIndex: resp["condition_index"]}
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
	body := map[string]any{"api_key": e.projToken, "distinct_id": in.DistinctID, "flag_keys": []string{fmt.Sprint(e.flag["key"])}}
	if in.DeviceID != "" {
		body["device_id"] = in.DeviceID
	}
	if len(in.EvaluationContexts) > 0 {
		body["evaluation_contexts"] = in.EvaluationContexts
	}
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
	return interpretFlags(resp, e.flag)
}

// interpretFlags reads one flag's result out of a /flags response. A flag
// PostHog did not return or could not evaluate is an error, never false.
func interpretFlags(resp, flag map[string]any) evalResult {
	if slices.Contains(flagspec.List(resp["quotaLimited"]), any("feature_flags")) {
		return evalResult{Status: "error", Error: "the project is over its feature flag quota, so PostHog did not evaluate flags"}
	}
	detail, returned := flagspec.Object(resp["flags"])[fmt.Sprint(flag["key"])].(map[string]any)
	switch {
	case !returned && flag["active"] != true:
		return evalResult{Status: "ok", Reason: "flag_disabled"}
	case !returned && resp["errorsWhileComputingFlags"] == true:
		return evalResult{Status: "error", Error: "PostHog reported errors while computing flags and did not return this one"}
	case !returned:
		return evalResult{Status: "error", Error: "PostHog did not return this flag; a change made in the last few seconds may not have reached /flags yet, or its evaluation runtime or contexts exclude this request"}
	case detail["failed"] == true:
		return evalResult{Status: "error", Error: fmt.Sprint("evaluation failed: ", flagspec.Object(detail["reason"])["description"])}
	}
	reason := flagspec.Object(detail["reason"])
	r := evalResult{
		Status:         "ok",
		Enabled:        detail["enabled"] == true,
		Variant:        detail["variant"],
		Reason:         reason["code"],
		Description:    reason["description"],
		ConditionIndex: reason["condition_index"],
	}
	if r.Enabled {
		r.Payload, r.PayloadSet = decodeEvalPayload(flagspec.Object(detail["metadata"])["payload"])
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
	if in.At != "" {
		out.Set("at", in.At)
	}
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
	if r.Description != nil && r.Description != r.Reason {
		out.Set("reason_description", r.Description)
	}
	out.Set("condition_index", r.ConditionIndex)
	if index, ok := r.ConditionIndex.(json.Number); ok {
		groups := flagspec.List(flagspec.Object(e.flag["filters"])["groups"])
		if i := flagspec.Int(index); i >= 0 && i < len(groups) {
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
	if e.engine == enginePublic && flagspec.Object(e.flag["filters"])["early_exit"] == true {
		warnings = append(warnings, "Early exit is on: /flags honors it, but SDKs evaluating locally honor it only in recent versions.")
	}
	return warnings
}
