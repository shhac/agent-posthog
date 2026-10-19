package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
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

func TestInvestigateEventPassesTheNameAsAValue(t *testing.T) {
	var body map[string]any
	s := newWrappedMockSession(t, "phx_mock", "456", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/query/") {
				data, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(data, &body)
				r.Body = io.NopCloser(bytes.NewReader(data))
			}
			next.ServeHTTP(w, r)
		})
	})
	name := `it's a \' trap` + "\n"
	if _, errOut, err := s.run("investigate", "event", "--event", name); err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	query := body["query"].(map[string]any)
	if query["values"].(map[string]any)["event"] != name || strings.Contains(query["query"].(string), "trap") {
		t.Fatalf("the event name must travel as a value, not inside the SQL: %v", query)
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
