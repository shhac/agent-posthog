package cli

import (
	"fmt"
	"strings"
	"testing"
)

// TestFlagLifecycleSmoke drives one flag through every write and check an
// agent would use, in order, against the stateful mock: create, payload,
// positional edits with version checks, the strict rollout endpoint, enable,
// scenario tests through /flags, export, archive, and delete.
func TestFlagLifecycleSmoke(t *testing.T) {
	s := flagSession(t)
	key := "lifecycle-smoke"
	version := func(r map[string]any) string { return fmt.Sprint(r["version"]) }

	if r := s.mustRun("flags", "create", key, "--where", "email icontains @example.com"); r["outcome"] != "planned" {
		t.Fatalf("create plan = %v", r)
	}
	if r := s.mustRun("flags", "create", key, "--where", "email icontains @example.com", "--yes"); r["outcome"] != "applied" {
		t.Fatalf("create = %v", r)
	}
	r := s.mustRun("flags", "apply", "--spec-json", fmt.Sprintf(`{"key":%q,"payload":{"smoke":true}}`, key), "--yes")
	if r["outcome"] != "applied" {
		t.Fatalf("apply payload = %v", r)
	}
	r = s.mustRun("flags", "conditions", "add", key, "--where", "app_env = smoke", "--at", "0", "--if-version", version(r), "--yes")
	if r["outcome"] != "applied" {
		t.Fatalf("conditions add = %v", r)
	}
	r = s.mustRun("flags", "conditions", "set", key, "1", "--rollout", "0", "--if-version", version(r), "--yes")
	if r["outcome"] != "applied" {
		t.Fatalf("strict rollout = %v", r)
	}
	if again := s.mustRun("flags", "conditions", "set", key, "1", "--rollout", "0", "--if-version", version(r), "--yes"); again["outcome"] != "unchanged" {
		t.Fatalf("repeat rollout = %v", again)
	}
	if stale := s.mustFail("flags", "conditions", "remove", key, "0", "--if-version", "0", "--yes"); !strings.Contains(stale, "nothing was written") {
		t.Fatalf("stale version = %s", stale)
	}
	s.mustRun("flags", "enable", key, "--yes")

	cases := `[
		{"name":"smoke env","props":{"app_env":"smoke"},"expect":{"enabled":true,"condition_index":0,"payload":{"smoke":true}}},
		{"name":"example.com at 0%","props":{"email":"a@example.com"},"expect":{"enabled":false}},
		{"name":"nobody","props":{"email":"a@elsewhere.test"},"expect":{"enabled":false}}
	]`
	if out, errOut, err := s.run("flags", "test", key, "--cases-json", cases); err != nil {
		t.Fatalf("scenarios: %v\n%s\n%s", err, out, errOut)
	}
	if spec, _, err := s.run("flags", "export", key); err != nil || !strings.Contains(spec, "app_env = smoke") {
		t.Fatalf("export = %s (%v)", spec, err)
	}
	s.mustRun("flags", "disable", key, "--yes")
	s.mustRun("flags", "archive", key, "--yes")
	if r := s.mustRun("flags", "unarchive", key, "--yes"); r["outcome"] != "applied" {
		t.Fatalf("unarchive (found via the archived fallback) = %v", r)
	}
	if r := s.mustRun("flags", "delete", key, "--yes"); r["outcome"] != "applied" {
		t.Fatalf("delete = %v", r)
	}
}
