package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueryWaitFailsOnAFailedQuery(t *testing.T) {
	s := newMockSession(t, "phx_mock", "456")
	out, errOut, err := s.run("query", "wait", "query_failed", "--interval", "1ms", "--max-wait", "10ms")
	if err == nil || !strings.Contains(errOut, "query failed: Syntax error near frobulate") {
		t.Fatalf("a failed query must not exit 0: err=%v stderr=%s", err, errOut)
	}
	if !strings.Contains(out, "Syntax error near frobulate") {
		t.Fatalf("the status should still be on stdout: %s", out)
	}
}

func TestQueryAndAPIAcceptInlineJSON(t *testing.T) {
	s := newMockSession(t, "phx_mock", "456")
	body := `{"query":{"kind":"HogQLQuery","query":"select 1"}}`
	if out, errOut, err := s.run("query", "json", "--json", body); err != nil || !strings.Contains(out, "$pageview") {
		t.Fatalf("query json --json: %v %s %s", err, errOut, out)
	}
	if out, errOut, err := s.run("api", "post", "/api/environments/456/query/", "--json", body); err != nil || !strings.Contains(out, "$pageview") {
		t.Fatalf("api post --json: %v %s %s", err, errOut, out)
	}
	if errOut := s.mustFail("query", "json", "--json", "[1,2]"); !strings.Contains(errOut, "must hold a JSON object") || !strings.Contains(errOut, `"fixable_by":"agent"`) {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestQueryHogQLOutputsNDJSONRows(t *testing.T) {
	out := runOK(t, "--env", "456", "query", "hogql", "select event, count() from events group by event")
	if !strings.Contains(out, `"@query"`) || !strings.Contains(out, `"$pageview"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestAsyncQueryFixtures(t *testing.T) {
	bodyPath := filepath.Join(t.TempDir(), "query.json")
	if err := os.WriteFile(bodyPath, []byte(`{"async":true,"query":{"kind":"HogQLQuery","query":"select 1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runOK(t, "api", "post", "/api/environments/456/query/", "--body", bodyPath)
	if !strings.Contains(out, `"complete": false`) || !strings.Contains(out, `"query_pending"`) {
		t.Fatalf("stdout = %s", out)
	}

	out = runOK(t, "query", "get", "query_failed")
	if !strings.Contains(out, `"error": true`) || !strings.Contains(out, "Syntax error near frobulate") {
		t.Fatalf("stdout = %s", out)
	}

	out = runOK(t, "query", "log", "query_complete")
	if !strings.Contains(out, `"runtime_ms": 812`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestQueryHogQLAsyncReturnsStatus(t *testing.T) {
	out := runOK(t, "query", "hogql", "--async", "select 1")
	if !strings.Contains(out, `"query_pending"`) || !strings.Contains(out, `"complete":false`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestQueryWaitEmitsCompletedRows(t *testing.T) {
	out := runOK(t, "query", "wait", "query_complete", "--interval", "1ms", "--max-wait", "10ms")
	if !strings.Contains(out, `"event":"$pageview"`) || !strings.Contains(out, `"@query"`) {
		t.Fatalf("stdout = %s", out)
	}
}
