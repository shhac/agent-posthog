package cli

import (
	"strings"
	"testing"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

func TestKeyLookupFollowsPagesAndRefusesToGuess(t *testing.T) {
	saved := keyLookupPages
	t.Cleanup(func() { keyLookupPages = saved })
	s := flagSession(t)

	keyLookupPages = listOptions{Limit: 1, All: true, PageLimit: 5}
	out, _, _ := s.run("flags", "get", "ambiguous-flag")
	if !strings.Contains(out, "Multiple feature flags matched key ambiguous-flag") {
		t.Fatalf("a duplicate on page 2 must still be found: %s", out)
	}

	keyLookupPages = listOptions{Limit: 1, All: true, PageLimit: 1}
	if errOut := s.mustFail("flags", "apply", "--spec-json", `{"key":"ambiguous-flag","active":false}`); !strings.Contains(errOut, "too many feature flags match") {
		t.Fatalf("running out of pages must not read as not-found (it would create a duplicate): %s", errOut)
	}
}

func TestLegacyFlagWithNullVersion(t *testing.T) {
	s := flagSession(t)
	if v := s.flag("legacy-flag")["version"]; v != nil {
		t.Fatalf("fixture version = %v", v)
	}
	r := s.mustRun("flags", "disable", "legacy-flag", "--if-version", "0", "--yes")
	if r["outcome"] != "applied" || r["base_version"] != nil {
		t.Fatalf("a null version reads as 0: %v", r)
	}
}

func TestNumericFlagKeysMatchByKeyInSpecs(t *testing.T) {
	s := flagSession(t)
	if r := s.mustRun("flags", "apply", "--spec-json", `{"key":"2024","conditions":[{}]}`, "--yes"); r["operation"] != "create" {
		t.Fatalf("create = %v", r)
	}
	if r := s.mustRun("flags", "apply", "--spec-json", `{"key":"2024","conditions":[{}]}`, "--yes"); r["outcome"] != "unchanged" {
		t.Fatalf("apply must find a numeric key by key, not as an ID: %v", r)
	}
	if out, _, _ := s.run("flags", "get", "2024"); !strings.Contains(out, "@unresolved") {
		t.Fatalf("a numeric argument is an ID, so this should miss: %s", out)
	}
}

func TestEveryJSONPayloadTypeRoundTrips(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "create", "typed", "--yes")
	for _, payload := range []string{`[1,"two",{"three":3}]`, `true`, `false`, `null`, `3.5`, `12345678901234567890`, `"text"`, `{}`, `""`} {
		s.mustRun("flags", "payload", "set", "typed", "--json", payload, "--yes")
		raw, _, err := s.run("flags", "payload", "get", "typed", "--format", "json")
		if err != nil {
			t.Fatal(err)
		}
		got, _ := flagspec.DecodeObject([]byte(raw))
		want, _ := flagspec.Decode([]byte(payload))
		if got["set"] != true || !flagspec.Equal(got["payload"], want) {
			t.Fatalf("payload %s came back as %s", payload, raw)
		}
	}
}

func TestPendingChangeRequestIsForAHuman(t *testing.T) {
	err := classifyWriteError(conflictError(t, `{"code":"change_request_pending","status":"approval_required","detail":"Already pending","change_request_id":"cr_9"}`))
	if !strings.Contains(err.Error(), "change request cr_9") {
		t.Fatalf("err = %v", err)
	}
}

func TestQuotaLimitedEvaluationIsAnError(t *testing.T) {
	s := flagSession(t)
	if errOut := s.mustFail("flags", "evaluate", "checkout-v2", "--distinct-id", "mock-quota"); !strings.Contains(errOut, "quota") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestRequiredTagsSurfaceFromPostHog(t *testing.T) {
	s := flagSession(t)
	if errOut := s.mustFail("--project", "126", "flags", "create", "untagged", "--yes"); !strings.Contains(errOut, "requires at least one tag") {
		t.Fatalf("stderr = %s", errOut)
	}
	if r := s.mustRun("--project", "126", "flags", "create", "tagged", "--tag", "team-x", "--yes"); r["outcome"] != "applied" {
		t.Fatalf("create = %v", r)
	}
}

func TestCopyPlanNeverShowsEncryptedPayloads(t *testing.T) {
	s := stagingSession(t)
	s.mustRun("--project", "staging", "flags", "create", "secret-config", "--type", "remote_config", "--payload-json", `{"api_secret":"other"}`, "--yes")
	out, errOut, err := s.run("flags", "copy", "secret-config", "--to", "staging")
	if err != nil || strings.Contains(out, "s3cret") || !strings.Contains(out, `"action": "overwrite"`) {
		t.Fatalf("err=%v\n%s\n%s", err, out, errOut)
	}
}

func TestCopyReportsDroppedDependencies(t *testing.T) {
	s := stagingSession(t)
	s.mustRun("flags", "create", "base", "--yes")
	s.mustRun("flags", "apply", "--spec-json", `{"key":"needs-base","active":true,"conditions":[{"where":["flag:base = true"]}]}`, "--yes")
	r := s.mustRun("flags", "copy", "needs-base", "--to", "staging", "--yes")
	if !strings.Contains(mustJSON(r["succeeded"]), "flag_dependency_warnings") || strings.Contains(mustJSON(r["succeeded"]), `"active":true`) {
		t.Fatalf("copy = %v", r)
	}
}

func TestAggregationNullClearsAndAbsentLeavesAlone(t *testing.T) {
	flag, _ := flagspec.DecodeObject([]byte(`{"key":"k","filters":{"groups":[],"aggregation_group_type_index":0}}`))
	for spec, wantChange := range map[string]bool{
		`{"key":"k","aggregation_group_type_index":null}`: true,
		`{"key":"k"}`: false,
		`{"key":"k","aggregation_group_type_index":0}`: false,
	} {
		parsed, err := flagspec.ParseSpec([]byte(spec))
		if err != nil {
			t.Fatal(err)
		}
		patch, err := flagspec.Build(flag, parsed, nil)
		if err != nil {
			t.Fatal(err)
		}
		patch.Prune(flag)
		_, changed := patch.Filters["aggregation_group_type_index"]
		if changed != wantChange {
			t.Fatalf("%s: change = %v (%v)", spec, changed, patch.Filters)
		}
	}
}

func TestScenarioDistinctIDsAreStable(t *testing.T) {
	cases, err := parseCases([]byte(`[{"name":"same","expect":{"enabled":true}}]`))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := parseCases([]byte(`[{"name":"same","expect":{"enabled":true}}]`))
	if cases[0].In.DistinctID != "agent-posthog-test:same" || cases[0].In.DistinctID != again[0].In.DistinctID {
		t.Fatalf("distinct ids = %s / %s", cases[0].In.DistinctID, again[0].In.DistinctID)
	}
}
