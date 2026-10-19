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

	cmd := exec.Command(bin, "mcp")
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+t.TempDir(),
		"AGENT_POSTHOG_PROFILE=",
		"AGENT_POSTHOG_BASE_URL="+server.URL,
		"POSTHOG_PERSONAL_API_KEY=phx_mock",
		"AGENT_POSTHOG_ORGANIZATION_ID=org_1",
		"AGENT_POSTHOG_PROJECT_ID=123",
	)
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
	s := &mcpSession{t: t, enc: json.NewEncoder(stdin), rd: bufio.NewReader(stdout)}
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

// flags calls the coarse flags tool and returns its first record.
func (s *mcpSession) flags(args []any, options map[string]any) map[string]any {
	s.t.Helper()
	if options == nil {
		options = map[string]any{}
	}
	result := s.request("tools/call", map[string]any{"name": "flags", "arguments": map[string]any{"args": args, "options": options}})
	if result["isError"] == true {
		s.t.Fatalf("flags %v: %v", args, result)
	}
	records, _ := result["structuredContent"].(map[string]any)["records"].([]any)
	if len(records) == 0 {
		s.t.Fatalf("flags %v returned no records: %v", args, result)
	}
	return records[0].(map[string]any)
}

// TestMCPFlagWritesPreviewOnlyWithDryRun pins how flag writes behave through
// the MCP bridge: the host's confirmation becomes an injected --yes, so a
// write applies unless the agent asks for --dry-run.
func TestMCPFlagWritesPreviewOnlyWithDryRun(t *testing.T) {
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
	if got := s.flags([]any{"get", "checkout-v2"}, nil); got["active"] != true {
		t.Fatalf("dry-run wrote the flag: %v", got)
	}

	applied := s.flags([]any{"disable", "checkout-v2"}, nil)
	if applied["outcome"] != "applied" {
		t.Fatalf("confirmed call should apply via the injected --yes: %v", applied)
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
