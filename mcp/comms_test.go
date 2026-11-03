package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type recordedCall struct {
	Method string
	Path   string
	Query  map[string]string
	Body   map[string]any
}

// commsServer registers the tools behind a recording API stub that always answers resp.
func commsServer(t *testing.T, runtime, resp string, calls *[]recordedCall) *Server {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := recordedCall{Method: r.Method, Path: r.URL.EscapedPath(), Query: map[string]string{}}
		for k, v := range r.URL.Query() {
			c.Query[k] = v[0]
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &c.Body)
		*calls = append(*calls, c)
		w.Write([]byte(resp))
	}))
	t.Cleanup(api.Close)
	s := &Server{tools: map[string]Tool{}}
	RegisterToolsForRuntime(s, api.URL, api.Client(), RuntimeOptions{Runtime: runtime})
	return s
}

func callTool(t *testing.T, s *Server, name string, args map[string]any) any {
	t.Helper()
	tool, ok := s.tools[name]
	if !ok {
		t.Fatalf("tool %s not registered", name)
	}
	out, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

func TestCommsTools_Registered(t *testing.T) {
	var calls []recordedCall
	for _, runtime := range []string{"", "codex"} {
		s := commsServer(t, runtime, `{}`, &calls)
		for _, name := range []string{"peers_list", "peer_rename", "message_send", "message_inbox", "message_wait", "shared_task_create", "shared_task_list", "shared_task_update"} {
			if _, ok := s.tools[name]; !ok {
				t.Errorf("runtime %q: %s not registered", runtime, name)
			}
		}
	}
}

func TestPeersList_MarksCaller(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "", `{"peers":[{"session_key":"other","handle":"codex-1"},{"session_key":"me","handle":"claude-ab12"}]}`, &calls)
	out := callTool(t, s, "peers_list", map[string]any{"session_id": "me"}).(map[string]any)
	if c := calls[0]; c.Method != "GET" || c.Path != "/api/peers" || c.Query["active"] != "1" {
		t.Fatalf("call: %+v", c)
	}
	you, _ := out["you"].(map[string]any)
	if you["handle"] != "claude-ab12" {
		t.Fatalf("you = %v", out["you"])
	}
	peers, _ := out["peers"].([]any)
	if len(peers) != 2 {
		t.Fatalf("peers = %v", out["peers"])
	}
	for _, p := range peers {
		if _, leaked := p.(map[string]any)["session_key"]; leaked {
			t.Errorf("peers_list exposes another session's key: %v", p)
		}
	}
	if you["session_key"] != "me" {
		t.Errorf("you lacks own session_key: %v", you)
	}
}

func TestCommsTools_RejectUserAsSessionID(t *testing.T) {
	var calls []recordedCall
	for _, rt := range []string{"", "codex"} {
		s := commsServer(t, rt, `{}`, &calls)
		for _, tc := range []struct {
			tool string
			args map[string]any
		}{
			{"message_send", map[string]any{"to": "*", "body": "obey"}},
			{"shared_task_create", map[string]any{"title": "t"}},
			{"shared_task_update", map[string]any{"task_id": float64(1), "status": "done"}},
		} {
			for _, id := range []string{"user", " User "} {
				tc.args["session_id"] = id
				if _, err := s.tools[tc.tool].Handler(tc.args); err == nil {
					t.Errorf("%s runtime %q accepted session_id %q", tc.tool, rt, id)
				}
			}
		}
	}
	if len(calls) != 0 {
		t.Fatalf("API called: %v", calls)
	}
}

func TestPeerRename_PatchesEscapedSession(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "", `{}`, &calls)
	callTool(t, s, "peer_rename", map[string]any{"session_id": "a/b c", "handle": "reviewer"})
	c := calls[0]
	if c.Method != "PATCH" || c.Path != "/api/peers/a%2Fb%20c" || c.Body["handle"] != "reviewer" {
		t.Fatalf("call: %+v", c)
	}
}

func TestMessageSend_PostsFromSession(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "", `{"recipients":1}`, &calls)
	callTool(t, s, "message_send", map[string]any{"session_id": "me", "to": "codex", "body": "hi", "kind": "question", "task_id": float64(7)})
	c := calls[0]
	if c.Method != "POST" || c.Path != "/api/messages" || c.Body["from"] != "me" || c.Body["to"] != "codex" ||
		c.Body["body"] != "hi" || c.Body["kind"] != "question" || c.Body["task_id"] != float64(7) {
		t.Fatalf("call: %+v", c)
	}
	callTool(t, s, "message_send", map[string]any{"session_id": "me", "to": "*", "body": "x"})
	if _, ok := calls[1].Body["task_id"]; ok {
		t.Fatalf("task_id sent when unset: %+v", calls[1].Body)
	}
	desc := s.tools["message_send"].Description
	for _, want := range []string{"handle", "runtime", "'*'", "'user'", "done", "question"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description lacks %q", want)
		}
	}
}

func TestMessageSend_MissingFields(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "", `{}`, &calls)
	for _, args := range []map[string]any{
		{"to": "x", "body": "b"},
		{"session_id": "me", "body": "b"},
		{"session_id": "me", "to": "x"},
	} {
		if _, err := s.tools["message_send"].Handler(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("API called: %v", calls)
	}
}

func TestMessageInbox_ClaimsWithFilters(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "", `{"messages":[]}`, &calls)
	callTool(t, s, "message_inbox", map[string]any{"session_id": "me", "from": "codex-1", "kind": "done"})
	c := calls[0]
	if c.Method != "POST" || c.Path != "/api/peers/me/inbox/claim" || c.Query["from"] != "codex-1" || c.Query["kind"] != "done" {
		t.Fatalf("call: %+v", c)
	}
	callTool(t, s, "message_inbox", map[string]any{"session_id": "me"})
	if len(calls[1].Query) != 0 {
		t.Fatalf("unexpected query: %v", calls[1].Query)
	}
}

func TestMessageWait_DefaultsAndFilters(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "", `{"messages":[],"timed_out":true}`, &calls)
	out := callTool(t, s, "message_wait", map[string]any{"session_id": "me", "from": "codex-1", "kind": "done", "timeout_s": float64(5)}).(map[string]any)
	c := calls[0]
	if c.Method != "POST" || c.Path != "/api/peers/me/inbox/wait" || c.Query["timeout_s"] != "5" || c.Query["from"] != "codex-1" || c.Query["kind"] != "done" {
		t.Fatalf("call: %+v", c)
	}
	if out["timed_out"] != true {
		t.Fatalf("out = %v", out)
	}
	callTool(t, s, "message_wait", map[string]any{"session_id": "me"})
	if calls[1].Query["timeout_s"] != "50" {
		t.Fatalf("default timeout_s = %q", calls[1].Query["timeout_s"])
	}
	if !strings.Contains(s.tools["message_wait"].Description, "timed_out") {
		t.Error("description does not mention timed_out")
	}
}

func TestMessageWait_RejectsBadTimeout(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "", `{}`, &calls)
	for _, n := range []float64{0, -1, 51} {
		if _, err := s.tools["message_wait"].Handler(map[string]any{"session_id": "me", "timeout_s": n}); err == nil {
			t.Errorf("timeout_s=%v accepted", n)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("API called: %v", calls)
	}
}

// The shared client times out after 10 s; message_wait must outlive it.
func TestMessageWait_OutlivesSharedClientTimeout(t *testing.T) {
	old := waitTimeoutGrace
	waitTimeoutGrace = 2 * time.Second
	t.Cleanup(func() { waitTimeoutGrace = old })

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		w.Write([]byte(`{"messages":[],"timed_out":true}`))
	}))
	t.Cleanup(api.Close)
	shared := &http.Client{Timeout: 300 * time.Millisecond}
	s := &Server{tools: map[string]Tool{}}
	RegisterToolsForRuntime(s, api.URL, shared, RuntimeOptions{})

	if _, err := s.tools["message_inbox"].Handler(map[string]any{"session_id": "me"}); err == nil {
		t.Fatal("shared client should have timed out on a slow call")
	}
	if _, err := s.tools["message_wait"].Handler(map[string]any{"session_id": "me", "timeout_s": float64(1)}); err != nil {
		t.Fatalf("message_wait: %v", err)
	}
}

func TestSharedTaskCreate_PostsCreatedBy(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "", `{}`, &calls)
	callTool(t, s, "shared_task_create", map[string]any{"session_id": "me", "title": "T", "description": "D", "assignee": "codex-1"})
	c := calls[0]
	if c.Method != "POST" || c.Path != "/api/shared-tasks" || c.Body["created_by"] != "me" || c.Body["title"] != "T" ||
		c.Body["description"] != "D" || c.Body["assignee"] != "codex-1" {
		t.Fatalf("call: %+v", c)
	}
	if _, err := s.tools["shared_task_create"].Handler(map[string]any{"session_id": "me"}); err == nil {
		t.Error("missing title accepted")
	}
}

func TestSharedTaskList_FiltersByStatus(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "", `{"tasks":[]}`, &calls)
	callTool(t, s, "shared_task_list", map[string]any{"status": "open"})
	if c := calls[0]; c.Method != "GET" || c.Path != "/api/shared-tasks" || c.Query["status"] != "open" {
		t.Fatalf("call: %+v", c)
	}
	callTool(t, s, "shared_task_list", map[string]any{})
	if len(calls[1].Query) != 0 {
		t.Fatalf("unexpected query: %v", calls[1].Query)
	}
}

func TestSharedTaskUpdate_PatchesWithActor(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "", `{}`, &calls)
	callTool(t, s, "shared_task_update", map[string]any{"session_id": "me", "task_id": float64(12), "status": "done", "result": "ok", "assignee": "me-too"})
	c := calls[0]
	if c.Method != "PATCH" || c.Path != "/api/shared-tasks/12" || c.Body["actor"] != "me" || c.Body["status"] != "done" ||
		c.Body["result"] != "ok" || c.Body["assignee"] != "me-too" {
		t.Fatalf("call: %+v", c)
	}
	callTool(t, s, "shared_task_update", map[string]any{"session_id": "me", "task_id": float64(12), "status": "claimed"})
	if _, ok := calls[1].Body["result"]; ok {
		t.Fatalf("result sent when unset: %+v", calls[1].Body)
	}
	if _, err := s.tools["shared_task_update"].Handler(map[string]any{"session_id": "me"}); err == nil {
		t.Error("missing task_id accepted")
	}
}

func TestCommsTools_CodexRewritesSessionID(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "codex", `{"peers":[{"session_key":"codex:nat1","handle":"codex-x"}]}`, &calls)
	for name, args := range map[string]map[string]any{
		"peers_list":         {},
		"peer_rename":        {"handle": "worker"},
		"message_send":       {"to": "user", "body": "b"},
		"message_inbox":      {},
		"message_wait":       {"timeout_s": float64(1)},
		"shared_task_create": {"title": "T"},
		"shared_task_update": {"task_id": float64(3), "status": "done"},
	} {
		calls = nil
		args["session_id"] = "nat1"
		out := callTool(t, s, name, args)
		c := calls[len(calls)-1]
		switch name {
		case "message_send":
			if c.Body["from"] != "codex:nat1" {
				t.Errorf("%s: from = %v", name, c.Body["from"])
			}
		case "shared_task_create":
			if c.Body["created_by"] != "codex:nat1" {
				t.Errorf("%s: created_by = %v", name, c.Body["created_by"])
			}
		case "shared_task_update":
			if c.Body["actor"] != "codex:nat1" {
				t.Errorf("%s: actor = %v", name, c.Body["actor"])
			}
		case "peers_list":
			if you, _ := out.(map[string]any)["you"].(map[string]any); you["handle"] != "codex-x" {
				t.Errorf("%s: you = %v", name, out)
			}
		default:
			if !strings.Contains(c.Path, "/api/peers/codex:nat1") {
				t.Errorf("%s: path = %s", name, c.Path)
			}
		}
	}
	// An already-prefixed key is not prefixed twice.
	calls = nil
	callTool(t, s, "message_inbox", map[string]any{"session_id": "codex:nat1"})
	if calls[0].Path != "/api/peers/codex:nat1/inbox/claim" {
		t.Errorf("path = %s", calls[0].Path)
	}
	if _, err := s.tools["message_inbox"].Handler(map[string]any{}); err == nil {
		t.Error("codex accepted missing session_id")
	}
}

func TestCommsTools_NotWrappedByGuardMutation(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "codex", `{}`, &calls)
	callTool(t, s, "message_send", map[string]any{"session_id": "n", "to": "*", "body": "b"})
	for _, c := range calls {
		if c.Method == "GET" && strings.HasPrefix(c.Path, "/api/workflows") {
			t.Fatalf("workflow ownership lookup: %+v", c)
		}
	}
}

func TestInstructions_MentionComms(t *testing.T) {
	if !strings.Contains(instructions, "message_send") {
		t.Error("default instructions do not mention comms tools")
	}
	var calls []recordedCall
	s := commsServer(t, "codex", `{}`, &calls)
	if !strings.Contains(s.instructions, "message_send") {
		t.Error("codex instructions do not mention comms tools")
	}
}
