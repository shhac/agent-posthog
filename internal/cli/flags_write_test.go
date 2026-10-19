package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shhac/agent-posthog/internal/api"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func flagSession(t *testing.T) *cliSession {
	t.Helper()
	return newMockSession(t, "phx_mock", "456")
}

// mustRun runs a command that must succeed and decodes its single JSON output.
func (s *cliSession) mustRun(args ...string) map[string]any {
	s.t.Helper()
	out, errOut, err := s.run(append(args, "--format", "json")...)
	if err != nil {
		s.t.Fatalf("%v: err=%v stderr=%s", args, err, errOut)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		s.t.Fatalf("%v: stdout not JSON: %v\n%s", args, err, out)
	}
	return v
}

// mustFail runs a command that must fail and returns its stderr.
func (s *cliSession) mustFail(args ...string) string {
	s.t.Helper()
	_, errOut, err := s.run(args...)
	if err == nil {
		s.t.Fatalf("%v: expected failure", args)
	}
	return errOut
}

func (s *cliSession) flag(ref string) map[string]any {
	s.t.Helper()
	return s.mustRun("flags", "get", ref)["data"].([]any)[0].(map[string]any)
}

func TestFlagWritePlansWithoutYes(t *testing.T) {
	s := flagSession(t)
	r := s.mustRun("flags", "disable", "checkout-v2")
	if r["outcome"] != "planned" || !strings.Contains(r["hint"].(string), "--yes") {
		t.Fatalf("receipt = %v", r)
	}
	changes := r["changes"].([]any)
	if len(changes) != 1 || changes[0].(map[string]any)["path"] != "active" {
		t.Fatalf("changes = %v", changes)
	}
	if s.flag("checkout-v2")["active"] != true {
		t.Fatal("plan wrote the flag")
	}
}

func TestFlagWriteDryRunWinsOverYes(t *testing.T) {
	s := flagSession(t)
	r := s.mustRun("flags", "disable", "checkout-v2", "--yes", "--dry-run")
	if r["outcome"] != "planned" || s.flag("checkout-v2")["active"] != true {
		t.Fatalf("dry-run applied: %v", r)
	}
}

func TestFlagWriteAppliesAndReportsVersion(t *testing.T) {
	s := flagSession(t)
	r := s.mustRun("flags", "disable", "checkout-v2", "--yes")
	if r["outcome"] != "applied" || r["base_version"] != float64(1) || r["version"] != float64(2) {
		t.Fatalf("receipt = %v", r)
	}
	if s.flag("checkout-v2")["active"] != false {
		t.Fatal("flag still active")
	}
	again := s.mustRun("flags", "disable", "checkout-v2", "--yes")
	if again["outcome"] != "unchanged" || s.flag("checkout-v2")["version"] != float64(2) {
		t.Fatalf("no-op wrote: %v", again)
	}
}

func TestFlagWriteIfVersionRefusesStaleRead(t *testing.T) {
	s := flagSession(t)
	errOut := s.mustFail("flags", "disable", "checkout-v2", "--if-version", "7", "--yes")
	if !strings.Contains(errOut, "is at version 1, not 7") || !strings.Contains(errOut, `"fixable_by":"agent"`) {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestFlagWriteClassifiesApprovalAndPolicyErrors(t *testing.T) {
	s := flagSession(t)
	errOut := s.mustFail("flags", "enable", "approval-flag", "--yes")
	if !strings.Contains(errOut, "change request cr_1") || !strings.Contains(errOut, `"fixable_by":"human"`) {
		t.Fatalf("approval stderr = %s", errOut)
	}
	errOut = s.mustFail("flags", "enable", "policy-flag", "--yes")
	if !strings.Contains(errOut, "more than one approval policy") || !strings.Contains(errOut, "separately") {
		t.Fatalf("policy stderr = %s", errOut)
	}
}

func TestFlagWriteClassifiesStaleConflict(t *testing.T) {
	err := classifyWriteError(conflictError(t, `{"type":"server_error","code":"conflict","detail":"The feature flag was updated by someone"}`))
	if !strings.Contains(err.Error(), "updated by someone") {
		t.Fatalf("err = %v", err)
	}
}

func TestFlagArchiveDisablesAndUnarchiveStaysDisabled(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "archive", "checkout-v2", "--yes")
	flag := s.flag("checkout-v2")
	if flag["archived"] != true || flag["active"] != false {
		t.Fatalf("flag = %v", flag)
	}
	s.mustRun("flags", "unarchive", "checkout-v2", "--yes")
	flag = s.flag("checkout-v2")
	if flag["archived"] != false || flag["active"] != false {
		t.Fatalf("flag = %v", flag)
	}
}

func TestFlagDeleteAndRestore(t *testing.T) {
	s := flagSession(t)
	r := s.mustRun("flags", "delete", "checkout-v2", "--yes")
	if r["outcome"] != "applied" || len(r["warnings"].([]any)) == 0 {
		t.Fatalf("receipt = %v", r)
	}
	out, _, _ := s.run("flags", "get", "checkout-v2")
	if !strings.Contains(out, "No feature flag matched") {
		t.Fatalf("deleted flag still resolves: %s", out)
	}
	if r := s.mustRun("flags", "restore", "22", "--yes"); r["outcome"] != "applied" {
		t.Fatalf("restore = %v", r)
	}
	if s.flag("checkout-v2")["deleted"] != false {
		t.Fatal("flag not restored")
	}
	if errOut := s.mustFail("flags", "restore", "checkout-v2"); !strings.Contains(errOut, "numeric flag ID") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestFlagEarlyExit(t *testing.T) {
	s := flagSession(t)
	r := s.mustRun("flags", "early-exit", "checkout-v2", "on", "--yes")
	if r["outcome"] != "applied" || len(r["warnings"].([]any)) == 0 {
		t.Fatalf("receipt = %v", r)
	}
	filters := s.flag("checkout-v2")["filters"].(map[string]any)
	if filters["early_exit"] != true || filters["groups"] == nil {
		t.Fatalf("filters = %v", filters)
	}
}

func TestFlagWriteCommandsExposeYesAndDryRun(t *testing.T) {
	root := newRootCmd("test")
	flags, _, err := root.Find([]string{"flags"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"enable", "disable", "archive", "unarchive", "delete", "restore", "early-exit"} {
		cmd, _, err := flags.Find([]string{name})
		if err != nil || cmd.Name() != name {
			t.Fatalf("missing flags %s", name)
		}
		if cmd.Flags().Lookup("yes") == nil || cmd.Flags().Lookup("dry-run") == nil {
			t.Fatalf("flags %s lacks --yes/--dry-run", name)
		}
	}
}

func conflictError(t *testing.T, body string) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	_, err := api.New(server.URL, "phx_mock").Patch(context.Background(), "/x/", nil, map[string]any{})
	return err
}

func TestReceiptNamesHostProjectAndAlias(t *testing.T) {
	s := stagingSession(t)
	r := s.mustRun("--project", "staging", "flags", "create", "aliased", "--yes")
	if r["project_id"] != float64(124) || r["project_alias"] != "staging" || !strings.HasPrefix(r["host"].(string), "http://127.0.0.1") {
		t.Fatalf("receipt = %v", r)
	}
	plan := s.mustRun("--project", "staging", "flags", "enable", "aliased")
	if !strings.Contains(plan["hint"].(string), "--if-version") {
		t.Fatalf("plan hint should explain a re-run re-reads the flag: %v", plan)
	}
}

func TestPatchCarriesVersionAndFlagAsRead(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":7,"key":"k","version":4,"active":false}`))
	}))
	t.Cleanup(server.Close)
	resolved := &resolvedContext{Client: api.New(server.URL, "phx_mock"), ProjectID: 1}
	flag := map[string]any{"id": json.Number("7"), "key": "k", "version": json.Number("3"), "active": true}
	patch := flagspec.NewPatch()
	patch.Fields["active"] = false
	if _, err := patchFlag(resolved)(context.Background(), flag, patch); err != nil {
		t.Fatal(err)
	}
	original, _ := body["original_flag"].(map[string]any)
	if body["version"] != float64(3) || original["active"] != true || body["active"] != false {
		t.Fatalf("PATCH body = %v", body)
	}
}

func TestCheckStoredWarnsWhenPostHogNormalizes(t *testing.T) {
	patch := flagspec.NewPatch()
	patch.Fields["active"] = true
	patch.Filters["groups"] = []any{map[string]any{"properties": []any{}, "rollout_percentage": json.Number("0")}}
	r := &receipt{}
	stored := `{"active":true,"filters":{"groups":[{"properties":[],"rollout_percentage":100}]}}`
	if _, err := checkStored(r, patch, func() (json.RawMessage, error) { return json.RawMessage(stored), nil }); err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "filters.groups") {
		t.Fatalf("warnings = %v", r.Warnings)
	}
}

func TestLifecyclePlansWarnAboutDependencies(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "create", "base", "--active", "--yes")
	s.mustRun("flags", "apply", "--spec-json", `{"key":"needs-base","active":true,"conditions":[{"where":["flag:base = true"]}]}`, "--yes")
	for _, op := range []string{"disable", "archive", "delete"} {
		r := s.mustRun("flags", op, "base")
		if !strings.Contains(mustJSON(r["warnings"]), "Active flags depend on this one (needs-base)") {
			t.Fatalf("%s plan = %v", op, r)
		}
	}
	s.mustRun("flags", "disable", "needs-base", "--yes")
	s.mustRun("flags", "disable", "base", "--yes")
	r := s.mustRun("flags", "enable", "needs-base")
	if !strings.Contains(mustJSON(r["warnings"]), "depends on disabled flag base") {
		t.Fatalf("enable plan = %v", r)
	}
	if r := s.mustRun("flags", "disable", "needs-base"); r["warnings"] != nil && strings.Contains(mustJSON(r["warnings"]), "depend") {
		t.Fatalf("a flag nothing depends on should not warn: %v", r)
	}
}
