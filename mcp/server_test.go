package mcp

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func newTestServer() *Server {
	s := &Server{tools: make(map[string]Tool)}
	RegisterTools(s, "http://127.0.0.1:1", nil)
	return s
}

// call runs one JSON-RPC message through the server and returns the decoded
// response, or nil when the server wrote nothing.
func call(t *testing.T, s *Server, id any, method, params string) map[string]any {
	t.Helper()
	var out bytes.Buffer
	s.writer = &out
	req := jsonRPCRequest{JSONRPC: "2.0", Method: method}
	if id != nil {
		raw, _ := json.Marshal(id)
		req.ID = raw
	}
	if params != "" {
		req.Params = json.RawMessage(params)
	}
	s.handle(req)
	if out.Len() == 0 {
		return nil
	}
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (%q)", err, out.String())
	}
	return resp
}

func result(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if resp == nil || resp["error"] != nil {
		t.Fatalf("expected a result, got %v", resp)
	}
	r, _ := resp["result"].(map[string]any)
	return r
}

func TestInitialize_NegotiatesProtocolVersion(t *testing.T) {
	s := newTestServer()
	for asked, want := range map[string]string{
		"2025-06-18": "2025-06-18",
		"2024-11-05": "2024-11-05",
		"2025-03-26": "2025-06-18", // requires JSON-RPC batches, which this server does not handle
		"2099-01-01": "2025-06-18", // unknown: answer with the newest this server speaks
	} {
		r := result(t, call(t, s, 1, "initialize", `{"protocolVersion":"`+asked+`","capabilities":{},"clientInfo":{"name":"t","version":"1"}}`))
		if r["protocolVersion"] != want {
			t.Errorf("client asked %s: protocolVersion = %v, want %s", asked, r["protocolVersion"], want)
		}
	}
}

func TestInitialize_GivesInstructions(t *testing.T) {
	r := result(t, call(t, newTestServer(), 1, "initialize", `{"protocolVersion":"2025-06-18"}`))
	instructions, _ := r["instructions"].(string)
	for _, want := range []string{"register_workflow", "transition_phase", "retrieve"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("instructions do not mention %s: %q", want, instructions)
		}
	}
}

func TestPing_AnswersEmptyResult(t *testing.T) {
	r := result(t, call(t, newTestServer(), 7, "ping", ""))
	if len(r) != 0 {
		t.Fatalf("ping result = %v, want {}", r)
	}
}

func TestNotifications_GetNoResponse(t *testing.T) {
	s := newTestServer()
	for _, method := range []string{"notifications/initialized", "notifications/cancelled", "notifications/roots/list_changed"} {
		if resp := call(t, s, nil, method, `{}`); resp != nil {
			t.Errorf("%s: server answered a notification: %v", method, resp)
		}
	}
}

func TestToolsList_SortedAndReadOnlyAnnotated(t *testing.T) {
	r := result(t, call(t, newTestServer(), 2, "tools/list", ""))
	tools, _ := r["tools"].([]any)
	var names []string
	readOnly := map[string]bool{}
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		name, _ := tool["name"].(string)
		names = append(names, name)
		if ann, ok := tool["annotations"].(map[string]any); ok && ann["readOnlyHint"] == true {
			readOnly[name] = true
		}
	}
	if !slices.IsSorted(names) {
		t.Errorf("tools are not sorted by name: %v", names)
	}
	for _, name := range []string{"retrieve", "search", "get_workflow", "wiki_search"} {
		if !readOnly[name] {
			t.Errorf("%s should carry readOnlyHint", name)
		}
	}
	// swarm_signals marks the signals it returns as read, so a retried poll would lose them.
	for _, name := range []string{"transition_phase", "register_workflow", "save_memory", "wiki_query", "swarm_heartbeat", "swarm_signals"} {
		if readOnly[name] {
			t.Errorf("%s changes state and must not carry readOnlyHint", name)
		}
	}
}

func TestToolDescriptions_NameTheAgentTool(t *testing.T) {
	for name, tool := range newTestServer().tools {
		if strings.Contains(tool.Description, "Task tool") || strings.Contains(tool.Description, "Task delegation") {
			t.Errorf("%s description still names the Task tool: %q", name, tool.Description)
		}
	}
}

// MCP forbids a null id: answer it as an invalid request instead of leaving the client waiting.
func TestNullID_IsAnInvalidRequest(t *testing.T) {
	s := newTestServer()
	var out bytes.Buffer
	s.writer = &out
	s.handleLine([]byte(`{"jsonrpc":"2.0","id":null,"method":"ping"}`))
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("no JSON response to a null id: %v (%q)", err, out.String())
	}
	errObj, _ := resp["error"].(map[string]any)
	if errObj["code"] != float64(-32600) {
		t.Fatalf("response = %v, want error -32600", resp)
	}
}

func TestNotificationLine_GetsNoResponse(t *testing.T) {
	s := newTestServer()
	var out bytes.Buffer
	s.writer = &out
	s.handleLine([]byte(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{}}`))
	if out.Len() != 0 {
		t.Fatalf("a notification was answered: %q", out.String())
	}
}
