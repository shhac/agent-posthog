package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

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

func TestInvestigateFlagEmitsEvidence(t *testing.T) {
	out := runOK(t, "investigate", "flag", "checkout-v2")
	if !strings.Contains(out, `"type":"entity"`) || !strings.Contains(out, `"name":"activity"`) || !strings.Contains(out, `"type":"next_step"`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestInvestigateHonorsFormat(t *testing.T) {
	out, errOut := runCLI(t, "investigate", "flag", "checkout-v2", "--format", "json")
	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || errOut != "" || len(envelope.Data) < 3 || envelope.Data[0]["type"] != "entity" {
		t.Fatalf("err=%v stderr=%s stdout=%s", err, errOut, out)
	}
}
