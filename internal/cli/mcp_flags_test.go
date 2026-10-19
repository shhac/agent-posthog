package cli

import (
	"bufio"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shhac/agent-posthog/internal/mockposthog"
)

// mcpSession is a running `agent-posthog mcp` subprocess against the mock.
type mcpSession struct {
	t      *testing.T
	enc    *json.Encoder
	rd     *bufio.Reader
	nextID int
	bin    string
	env    []string
}

func startMCP(t *testing.T) *mcpSession {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping MCP e2e build in -short")
	}
	bin := filepath.Join(t.TempDir(), "agent-posthog")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/agent-posthog").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	server := httptest.NewServer(mockposthog.NewServer())
	t.Cleanup(server.Close)

	env := append(os.Environ(),
		"XDG_CONFIG_HOME="+t.TempDir(),
		"AGENT_POSTHOG_PROFILE=",
		"AGENT_POSTHOG_BASE_URL="+server.URL,
		"POSTHOG_PERSONAL_API_KEY=phx_mock",
		"AGENT_POSTHOG_ORGANIZATION_ID=org_1",
		"AGENT_POSTHOG_PROJECT_ID=123",
	)
	cmd := exec.Command(bin, "mcp")
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	s := &mcpSession{t: t, enc: json.NewEncoder(stdin), rd: bufio.NewReader(stdout), bin: bin, env: env}
	s.request("initialize", map[string]any{"protocolVersion": "2025-06-18"})
	_ = s.enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return s
}

func (s *mcpSession) request(method string, params any) map[string]any {
	s.t.Helper()
	s.nextID++
	msg := map[string]any{"jsonrpc": "2.0", "id": s.nextID, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if err := s.enc.Encode(msg); err != nil {
		s.t.Fatal(err)
	}
	line, err := s.rd.ReadBytes('\n')
	if err != nil {
		s.t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		s.t.Fatalf("parse %q: %v", line, err)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		s.t.Fatalf("no result: %v", resp)
	}
	return result
}

// call invokes a coarse group tool and returns the raw tool result.
func (s *mcpSession) call(tool string, args []any, options map[string]any) map[string]any {
	s.t.Helper()
	if options == nil {
		options = map[string]any{}
	}
	return s.request("tools/call", map[string]any{"name": tool, "arguments": map[string]any{"args": args, "options": options}})
}

// localCLI runs the binary outside MCP, as the person at the machine would.
func (s *mcpSession) localCLI(args ...string) {
	s.t.Helper()
	cmd := exec.Command(s.bin, args...)
	cmd.Env = s.env
	if out, err := cmd.CombinedOutput(); err != nil {
		s.t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

// flags calls the coarse flags tool and returns its first record.
func (s *mcpSession) flags(args []any, options map[string]any) map[string]any {
	s.t.Helper()
	result := s.call("flags", args, options)
	if result["isError"] == true {
		s.t.Fatalf("flags %v: %v", args, result)
	}
	records, _ := result["structuredContent"].(map[string]any)["records"].([]any)
	if len(records) == 0 {
		s.t.Fatalf("flags %v returned no records: %v", args, result)
	}
	return records[0].(map[string]any)
}

// TestMCPFlagWritesNeedLocalOptIn pins how flag writes behave through the
// MCP bridge. The host's confirmation becomes an injected --yes, so writes
// are off until a person runs 'config set mcp_writes on' locally; --dry-run plans
// always work.
func TestMCPFlagWritesNeedLocalOptIn(t *testing.T) {
	s := startMCP(t)

	tools, _ := s.request("tools/list", nil)["tools"].([]any)
	var flagsTool map[string]any
	for _, tool := range tools {
		if tool.(map[string]any)["name"] == "flags" {
			flagsTool = tool.(map[string]any)
		}
	}
	if flagsTool == nil || !strings.Contains(mustJSON(flagsTool["annotations"]), `"destructiveHint":true`) {
		t.Fatalf("flags tool should be marked destructive so hosts confirm: %v", flagsTool)
	}

	plan := s.flags([]any{"disable", "checkout-v2"}, map[string]any{"dry-run": true})
	if plan["outcome"] != "planned" {
		t.Fatalf("dry-run through MCP = %v", plan)
	}
	refused := s.call("flags", []any{"disable", "checkout-v2"}, nil)
	if refused["isError"] != true || !strings.Contains(mustJSON(refused), "config set mcp_writes on") {
		t.Fatalf("writes must be off through MCP by default: %v", refused)
	}
	raw := s.call("api", []any{"post", "/api/projects/123/feature_flags/"}, nil)
	if raw["isError"] != true || !strings.Contains(mustJSON(raw), "config set mcp_writes on") {
		t.Fatalf("raw API writes must be off through MCP too: %v", raw)
	}
	if got := s.flags([]any{"get", "checkout-v2"}, nil); got["active"] != true {
		t.Fatalf("a refused write changed the flag: %v", got)
	}

	s.localCLI("config", "set", "mcp_writes", "on")
	applied := s.flags([]any{"disable", "checkout-v2"}, nil)
	if applied["outcome"] != "applied" {
		t.Fatalf("once enabled, a confirmed call applies via the injected --yes: %v", applied)
	}
	if got := s.flags([]any{"get", "checkout-v2"}, nil); got["active"] != false {
		t.Fatalf("flag not disabled: %v", got)
	}

	inline := s.flags([]any{"apply"}, map[string]any{"spec-json": `{"key":"via-mcp","conditions":[{"where":["plan = pro"]}]}`})
	if inline["outcome"] != "applied" || inline["operation"] != "create" {
		t.Fatalf("inline spec through MCP (no stdin, no shared filesystem) = %v", inline)
	}

	nested := s.flags([]any{"conditions", "list", "checkout-v2"}, nil)
	if nested["index"] != float64(0) {
		t.Fatalf("nested subcommand through the group tool = %v", nested)
	}
}
