package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLostResponseAfterCommittedWrite: PostHog applies the PATCH, then the
// connection drops before the response arrives. The CLI must not replay the
// write and must say the outcome is unknown rather than failed.
func TestLostResponseAfterCommittedWrite(t *testing.T) {
	var patches atomic.Int32
	s := newWrappedMockSession(t, "phx_mock", "456", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPatch {
				next.ServeHTTP(w, r)
				return
			}
			patches.Add(1)
			next.ServeHTTP(httptest.NewRecorder(), r)
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
		})
	})
	errOut := s.mustFail("flags", "disable", "checkout-v2", "--yes")
	if !strings.Contains(errOut, "may or may not have been applied") || !strings.Contains(errOut, `"fixable_by":"agent"`) {
		t.Fatalf("stderr = %s", errOut)
	}
	if n := patches.Load(); n != 1 {
		t.Fatalf("PATCH sent %d times; a write must never be replayed", n)
	}
	if s.flag("checkout-v2")["active"] != false {
		t.Fatal("fixture should have committed the write before dropping the response")
	}
}

// TestReorderedConditionsRefuseAStalePositionalEdit: the caller lists
// conditions at version 1; another writer reorders them; the caller's edit of
// "condition 0" would now hit a different condition, so it must be refused.
func TestReorderedConditionsRefuseAStalePositionalEdit(t *testing.T) {
	s := gateSession(t)
	s.mustRun("flags", "conditions", "move", "beta-gate", "0", "1", "--if-version", "1", "--yes")
	before := mustJSON(groupsOf(t, s, "beta-gate"))
	for _, args := range [][]string{
		{"flags", "conditions", "set", "beta-gate", "0", "--rollout", "50", "--if-version", "1", "--yes"},
		{"flags", "conditions", "set", "beta-gate", "0", "--where", "plan = pro", "--if-version", "1", "--yes"},
		{"flags", "conditions", "remove", "beta-gate", "0", "--if-version", "1", "--yes"},
		{"flags", "conditions", "move", "beta-gate", "1", "0", "--if-version", "1", "--yes"},
		{"flags", "conditions", "add", "beta-gate", "--at", "0", "--if-version", "1", "--yes"},
	} {
		if errOut := s.mustFail(args...); !strings.Contains(errOut, "is at version 2, not 1") {
			t.Fatalf("%v: stderr = %s", args[2:4], errOut)
		}
	}
	if after := mustJSON(groupsOf(t, s, "beta-gate")); after != before {
		t.Fatalf("a refused edit changed the flag:\n%s\n%s", before, after)
	}
}

// TestCopyWarnsThatCohortsMatchByName: PostHog's copy maps cohorts by name in
// the target, and cohort IDs differ between projects, so plans must say both.
func TestCopyWarnsThatCohortsMatchByName(t *testing.T) {
	s := stagingSession(t)
	spec := `{"key":"cohort-gate","conditions":[{"where":["cohort in 5"]}]}`
	s.mustRun("flags", "apply", "--spec-json", spec, "--yes")
	s.mustRun("--project", "staging", "flags", "apply", "--spec-json", spec, "--yes")
	warnings := mustJSON(s.mustRun("flags", "copy", "cohort-gate", "--to", "staging", "--overwrite")["warnings"])
	for _, want := range []string{"cohorts are matched by name in each target", "cohort IDs differ between projects"} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("warnings missing %q: %s", want, warnings)
		}
	}
}

// TestRuntimeFilteringIsReportedNotFalse: a client-only flag is not served to
// the CLI's server-side /flags call; the diagnostic engine evaluates all
// runtimes. The public result must be an error naming the runtime, never false.
func TestRuntimeFilteringIsReportedNotFalse(t *testing.T) {
	s := flagSession(t)
	s.mustRun("flags", "apply", "--spec-json", `{"key":"client-only","active":true,"evaluation_runtime":"client","conditions":[{}]}`, "--yes")
	if errOut := s.mustFail("flags", "evaluate", "client-only", "--distinct-id", "user_123"); !strings.Contains(errOut, "evaluation runtime") {
		t.Fatalf("stderr = %s", errOut)
	}
	if r := s.mustRun("flags", "evaluate", "client-only", "--distinct-id", "user_123", "--engine", "diagnostic"); r["enabled"] != true {
		t.Fatalf("diagnostic (all runtimes) = %v", r)
	}
}

// TestScenarioSuitesRunOneAtATimeWithinTheTimeout: cases never overlap, and
// --timeout bounds the whole suite rather than each case.
func TestScenarioSuitesRunOneAtATimeWithinTheTimeout(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	s := newWrappedMockSession(t, "phx_mock", "456", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/flags" {
				next.ServeHTTP(w, r)
				return
			}
			mu.Lock()
			inFlight++
			peak = max(peak, inFlight)
			mu.Unlock()
			next.ServeHTTP(w, r)
			mu.Lock()
			inFlight--
			mu.Unlock()
		})
	})
	s.mustRun("flags", "apply", "--spec-json", `{"key":"plain","active":true,"conditions":[{}]}`, "--yes")
	cases := `[{"name":"a","expect":{"enabled":true}},{"name":"b","expect":{"enabled":true}},{"name":"c","expect":{"enabled":true}},{"name":"d","expect":{"enabled":true}}]`
	if out, errOut, err := s.run("flags", "test", "plain", "--cases-json", cases); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, errOut)
	}
	if peak != 1 {
		t.Fatalf("cases overlapped: %d requests in flight at once", peak)
	}

	mu.Lock()
	peak = 0
	mu.Unlock()
	slowSix := `[{"name":"1","distinct_id":"mock-slow","expect":{"enabled":true}},{"name":"2","distinct_id":"mock-slow","expect":{"enabled":true}},{"name":"3","distinct_id":"mock-slow","expect":{"enabled":true}},{"name":"4","distinct_id":"mock-slow","expect":{"enabled":true}},{"name":"5","distinct_id":"mock-slow","expect":{"enabled":true}},{"name":"6","distinct_id":"mock-slow","expect":{"enabled":true}}]`
	out, _, err := s.run("flags", "test", "plain", "--parallel", "3", "--cases-json", slowSix)
	if err != nil || peak != 3 {
		t.Fatalf("--parallel 3 should run exactly 3 at once (peak %d): %v\n%s", peak, err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i, line := range lines[:6] {
		if !strings.HasPrefix(line, fmt.Sprintf(`{"case":"%d"`, i+1)) {
			t.Fatalf("results must print in case order: %s", out)
		}
	}
	if errOut := s.mustFail("flags", "test", "plain", "--parallel", "9", "--cases-json", slowSix); !strings.Contains(errOut, "--parallel must be from 1 to 8") {
		t.Fatalf("stderr = %s", errOut)
	}

	slow := `[{"name":"s1","distinct_id":"mock-slow","expect":{"enabled":true}},{"name":"s2","distinct_id":"mock-slow","expect":{"enabled":true}},{"name":"s3","distinct_id":"mock-slow","expect":{"enabled":true}}]`
	start := time.Now()
	out, _, err = s.run("--timeout", "200", "flags", "test", "plain", "--cases-json", slow)
	if elapsed := time.Since(start); elapsed > 650*time.Millisecond {
		t.Fatalf("suite took %v; --timeout should bound the whole suite, not each 300ms case", elapsed)
	}
	if err == nil || !strings.Contains(out, `"result":"error"`) || strings.Contains(out, `"result":"pass"`) {
		t.Fatalf("timed-out cases must be errors: err=%v\n%s", err, out)
	}
}
