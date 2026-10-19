package featureflag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shhac/agent-posthog/internal/api"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestFlagsHostMapping(t *testing.T) {
	for in, want := range map[string]string{
		"https://us.posthog.com":     "https://us.i.posthog.com",
		"https://eu.posthog.com":     "https://eu.i.posthog.com",
		"https://posthog.example.io": "https://posthog.example.io",
	} {
		if got := FlagsHost(in); got != want {
			t.Fatalf("FlagsHost(%s) = %s", in, got)
		}
	}
}

func TestInterpretFlagsNeverTurnsFailureIntoFalse(t *testing.T) {
	active := map[string]any{"key": "gate", "active": true}
	cases := []struct {
		name, resp string
		flag       map[string]any
		status     string
		enabled    bool
	}{
		{"disabled flag omitted", `{"flags":{}}`, map[string]any{"key": "gate", "active": false}, "ok", false},
		{"active flag omitted", `{"flags":{}}`, active, "error", false},
		{"omitted with errors", `{"flags":{},"errorsWhileComputingFlags":true}`, active, "error", false},
		{"failed", `{"flags":{"gate":{"enabled":false,"failed":true,"reason":{"description":"timeout"}}}}`, active, "error", false},
		{"evaluated false", `{"flags":{"gate":{"enabled":false,"reason":{"code":"no_condition_match"}}}}`, active, "ok", false},
		{"evaluated true", `{"flags":{"gate":{"enabled":true,"reason":{"code":"condition_match","condition_index":0},"metadata":{"payload":"{\"a\":1}"}}}}`, active, "ok", true},
	}
	for _, tc := range cases {
		resp, _ := flagspec.DecodeObject([]byte(tc.resp))
		r := interpretFlags(resp, tc.flag)
		if r.Status != tc.status || r.Enabled != tc.enabled {
			t.Fatalf("%s: %+v", tc.name, r)
		}
		if tc.enabled && mustJSON(r.Payload) != `{"a":1}` {
			t.Fatalf("%s: payload %v", tc.name, r.Payload)
		}
	}
}

func TestPublicEvaluationRequestShape(t *testing.T) {
	var body map[string]any
	var auth, version string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, version = r.Header.Get("Authorization"), r.URL.Query().Get("v")
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"flags":{}}`))
	}))
	t.Cleanup(server.Close)
	e := &Evaluator{flag: map[string]any{"key": "gate", "active": false}, engine: EnginePublic, public: api.New(server.URL, ""), projToken: "phc_project"}
	e.publicEval(context.Background(), Subject{DistinctID: "u1", DeviceID: "d1", EvaluationContexts: []string{"web"}, PersonProps: map[string]any{"plan": "pro"}})
	if auth != "" || version != "2" {
		t.Fatalf("auth = %q, v = %q: /flags must not carry the personal key", auth, version)
	}
	want := `{"api_key":"phc_project","device_id":"d1","distinct_id":"u1","evaluation_contexts":["web"],"flag_keys":["gate"],"person_properties":{"plan":"pro"}}`
	if got := mustJSON(body); got != want {
		t.Fatalf("body = %s\nwant   %s", got, want)
	}
}

func TestScenarioDistinctIDsAreStable(t *testing.T) {
	cases, err := ParseCases([]byte(`[{"name":"same","expect":{"enabled":true}}]`))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := ParseCases([]byte(`[{"name":"same","expect":{"enabled":true}}]`))
	if cases[0].In.DistinctID != "agent-posthog-test:same" || cases[0].In.DistinctID != again[0].In.DistinctID {
		t.Fatalf("distinct ids = %s / %s", cases[0].In.DistinctID, again[0].In.DistinctID)
	}
}
