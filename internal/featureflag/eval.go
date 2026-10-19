package featureflag

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/api"
	"github.com/shhac/agent-posthog/internal/flagspec"
	"github.com/shhac/agent-posthog/internal/output"
)

// Evaluation engines. public calls PostHog's /flags endpoint, as an SDK would,
// and accepts property overrides. diagnostic calls the private test_evaluation
// endpoint, which uses stored person properties only.
const (
	EnginePublic     = "public"
	EngineDiagnostic = "diagnostic"
)

// Subject is who a flag is evaluated for.
type Subject struct {
	DistinctID      string
	PersonProps     map[string]any
	Groups          map[string]any
	GroupProperties map[string]any
	// At replays the evaluation as of a past moment (diagnostic engine only).
	At                 string
	DeviceID           string
	EvaluationContexts []string
}

// Result is one evaluation. Status "error" means PostHog could not evaluate
// the flag: it is never reported as false.
type Result struct {
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

// Evaluator evaluates one flag with one engine.
type Evaluator struct {
	p         Project
	flag      map[string]any
	engine    string
	public    *api.Client
	projToken string
}

// NewEvaluator prepares to evaluate flag. The public engine reads the
// project token and sends /flags requests to flagsHost (see FlagsHost).
func (p Project) NewEvaluator(ctx context.Context, flag map[string]any, engine, flagsHost string) (*Evaluator, error) {
	e := &Evaluator{p: p, flag: flag, engine: engine}
	if engine == EngineDiagnostic {
		return e, nil
	}
	if engine != EnginePublic {
		return nil, agenterrors.New("--engine must be public or diagnostic", agenterrors.FixableByAgent)
	}
	raw, err := p.Client.Get(ctx, api.ProjectPath(p.ID), nil)
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
	e.public = p.Client.WithBase(flagsHost, "")
	return e, nil
}

// FlagsHost maps a PostHog app host to the host that serves /flags. Cloud
// splits them; self-hosted and proxied setups serve both from one host or
// name theirs explicitly.
func FlagsHost(appHost string) string {
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

// Evaluate evaluates the flag for in.
func (e *Evaluator) Evaluate(ctx context.Context, in Subject) Result {
	if e.engine == EngineDiagnostic {
		return e.diagnostic(ctx, in)
	}
	return e.publicEval(ctx, in)
}

func (e *Evaluator) diagnostic(ctx context.Context, in Subject) Result {
	body := map[string]any{"distinct_id": in.DistinctID}
	if len(in.Groups) > 0 {
		body["groups"] = in.Groups
	}
	if in.At != "" {
		body["timestamp"] = in.At
	}
	raw, err := e.p.Client.Post(ctx, e.p.flagPath(fmt.Sprint(e.flag["id"]), "test_evaluation"), nil, body)
	if err != nil {
		return Result{Status: "error", Error: err.Error()}
	}
	resp, err := flagspec.DecodeObject(raw)
	if err != nil {
		return Result{Status: "error", Error: "unexpected test_evaluation response"}
	}
	return interpretDiagnostic(resp)
}

// interpretDiagnostic reads a test_evaluation response: the result is false,
// true, or the variant served.
func interpretDiagnostic(resp map[string]any) Result {
	r := Result{Status: "ok", Reason: resp["reason"], Description: resp["reason_description"], ConditionIndex: resp["condition_index"]}
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

func (e *Evaluator) publicEval(ctx context.Context, in Subject) Result {
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
		return Result{Status: "error", Error: err.Error()}
	}
	resp, err := flagspec.DecodeObject(raw)
	if err != nil {
		return Result{Status: "error", Error: "unexpected /flags response"}
	}
	return interpretFlags(resp, e.flag)
}

// interpretFlags reads one flag's result out of a /flags response. A flag
// PostHog did not return or could not evaluate is an error, never false.
func interpretFlags(resp, flag map[string]any) Result {
	if slices.Contains(flagspec.List(resp["quotaLimited"]), any("feature_flags")) {
		return Result{Status: "error", Error: "the project is over its feature flag quota, so PostHog did not evaluate flags"}
	}
	detail, returned := flagspec.Object(resp["flags"])[fmt.Sprint(flag["key"])].(map[string]any)
	switch {
	case !returned && flag["active"] != true:
		return Result{Status: "ok", Reason: "flag_disabled"}
	case !returned && resp["errorsWhileComputingFlags"] == true:
		return Result{Status: "error", Error: "PostHog reported errors while computing flags and did not return this one"}
	case !returned:
		return Result{Status: "error", Error: "PostHog did not return this flag; a change made in the last few seconds may not have reached /flags yet, or its evaluation runtime or contexts exclude this request"}
	case detail["failed"] == true:
		return Result{Status: "error", Error: fmt.Sprint("evaluation failed: ", flagspec.Object(detail["reason"])["description"])}
	}
	reason := flagspec.Object(detail["reason"])
	r := Result{
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

// Record reports an evaluation, with the condition that matched as a spec.
func (e *Evaluator) Record(in Subject, r Result) output.Ordered {
	out := output.Ordered{}
	out = append(out, output.Field{Key: "engine", Value: e.engine})
	out = append(out, output.Field{Key: "key", Value: e.flag["key"]})
	out = append(out, output.Field{Key: "distinct_id", Value: in.DistinctID})
	if in.At != "" {
		out = append(out, output.Field{Key: "at", Value: in.At})
	}
	out = append(out, output.Field{Key: "status", Value: r.Status})
	if r.Status == "error" {
		out = append(out, output.Field{Key: "error", Value: r.Error})
		return out
	}
	out = append(out, output.Field{Key: "enabled", Value: r.Enabled})
	out = append(out, output.Field{Key: "variant", Value: r.Variant})
	if r.PayloadSet {
		out = append(out, output.Field{Key: "payload", Value: r.Payload})
	}
	out = append(out, output.Field{Key: "reason", Value: r.Reason})
	if r.Description != nil && r.Description != r.Reason {
		out = append(out, output.Field{Key: "reason_description", Value: r.Description})
	}
	out = append(out, output.Field{Key: "condition_index", Value: r.ConditionIndex})
	if index, ok := r.ConditionIndex.(json.Number); ok {
		groups := flagspec.List(flagspec.Object(e.flag["filters"])["groups"])
		if i := flagspec.Int(index); i >= 0 && i < len(groups) {
			out = append(out, output.Field{Key: "condition", Value: flagspec.ExportConditions(groups[i : i+1])[0]})
		}
	}
	return out
}

// Warnings are what the flag's state means for any evaluation of it.
func (e *Evaluator) Warnings() []string {
	var warnings []string
	if e.flag["active"] != true {
		warnings = append(warnings, "The flag is disabled, so it evaluates to false for everyone; this tests the saved definition only once it is enabled.")
	}
	if e.engine == EnginePublic && flagspec.Object(e.flag["filters"])["early_exit"] == true {
		warnings = append(warnings, "Early exit is on: /flags honors it, but SDKs evaluating locally honor it only in recent versions.")
	}
	return warnings
}

// Engine is the evaluator's engine.
func (e *Evaluator) Engine() string { return e.engine }

// CheckSubject refuses inputs the engine cannot honor.
func CheckSubject(engine string, in Subject) error {
	if engine == EngineDiagnostic && (in.PersonProps != nil || in.GroupProperties != nil) {
		return agenterrors.New("the diagnostic engine uses stored properties only; drop property overrides or use --engine public", agenterrors.FixableByAgent)
	}
	if engine == EngineDiagnostic && (in.DeviceID != "" || len(in.EvaluationContexts) > 0) {
		return agenterrors.New("--device-id and evaluation contexts apply to /flags; use --engine public", agenterrors.FixableByAgent)
	}
	if engine != EngineDiagnostic && in.At != "" {
		return agenterrors.New("evaluating as of a past moment needs --engine diagnostic, which rebuilds the flag and person as they were", agenterrors.FixableByAgent)
	}
	return nil
}

// NormalizeAt checks a point in time for historical evaluation, reading a
// zoneless timestamp as UTC the way PostHog does.
func NormalizeAt(at string) (string, error) {
	if at == "" {
		return "", nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, at); err == nil {
			return t.UTC().Format(time.RFC3339), nil
		}
	}
	return "", agenterrors.New("--at must be an ISO 8601 time such as 2026-04-29T15:30:00Z", agenterrors.FixableByAgent)
}
