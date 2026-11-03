package codex

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type commsServer struct {
	mu       sync.Mutex
	requests []string
	messages string
}

func (c *commsServer) handler(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.requests = append(c.requests, r.Method+" "+r.URL.Path)
	msgs := c.messages
	c.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/peers":
		w.Write([]byte(`{"session_key":"codex:s1","runtime":"codex","handle":"codex-ab12","active":true}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/peers":
		w.Write([]byte(`{"peers":[{"session_key":"codex:s1","runtime":"codex","handle":"codex-ab12","active":true},{"session_key":"k2","runtime":"claude-code","handle":"claude-77aa","active":true}]}`))
	case r.URL.Path == "/api/peers/codex:s1/inbox/claim":
		w.Write([]byte(`{"messages":[` + msgs + `]}`))
	case r.URL.Path == "/api/peers/codex:s1/end":
		w.Write([]byte(`{"ok":true}`))
	case r.URL.Path == "/api/integrations/codex/workflows":
		w.Write([]byte(`{"workflows":[]}`))
	default:
		w.Write([]byte(`{}`))
	}
}

func (c *commsServer) saw(req string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.requests {
		if r == req {
			return true
		}
	}
	return false
}

const waitingMessage = `{"id":1,"from_handle":"claude-77aa","from_runtime":"claude-code","kind":"text","body":"please review api/server.go"}`

func runHook(t *testing.T, kind, event, root string, extra string) (string, string) {
	t.Helper()
	raw := fmt.Sprintf(`{"hook_event_name":%q,"cwd":%q,"session_id":"s1"%s}`, event, root, extra)
	var out, stderr bytes.Buffer
	if code := Run(kind, strings.NewReader(raw), &out, &stderr, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	return out.String(), stderr.String()
}

func TestCodexHook_InboxEmitsUserPromptSubmitContext(t *testing.T) {
	cs := &commsServer{messages: waitingMessage}
	root := hookRoot(t, cs.handler)
	out, _ := runHook(t, "inbox", "UserPromptSubmit", root, "")
	for _, want := range []string{`"hookEventName":"UserPromptSubmit"`, `"additionalContext"`, "claude-77aa", "please review api/server.go", "not from the user"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
	if !cs.saw("POST /api/peers/codex:s1/inbox/claim") {
		t.Fatalf("inbox not claimed under codex:<native id>: %v", cs.requests)
	}
}

func TestCodexHook_InboxEmptyEmitsEmptyObject(t *testing.T) {
	cs := &commsServer{}
	root := hookRoot(t, cs.handler)
	if out, _ := runHook(t, "inbox", "UserPromptSubmit", root, ""); strings.TrimSpace(out) != "{}" {
		t.Fatalf("got %s", out)
	}
}

func TestCodexHook_InboxAPIDownEmitsEmptyObject(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "offline", 503) })
	if out, _ := runHook(t, "inbox", "UserPromptSubmit", root, ""); strings.TrimSpace(out) != "{}" {
		t.Fatalf("got %s", out)
	}
}

func TestCodexHook_InboxSubagentDoesNotClaim(t *testing.T) {
	cs := &commsServer{messages: waitingMessage}
	root := hookRoot(t, cs.handler)
	out, _ := runHook(t, "inbox", "UserPromptSubmit", root, `,"agent_id":"worker-1"`)
	if strings.TrimSpace(out) != "{}" || cs.saw("POST /api/peers/codex:s1/inbox/claim") {
		t.Fatalf("subagent consumed parent inbox: %s %v", out, cs.requests)
	}
}

func TestCodexHook_SessionStartNoticeContainsCommsIdentity(t *testing.T) {
	cs := &commsServer{messages: waitingMessage}
	root := hookRoot(t, cs.handler)
	out, _ := runHook(t, "context", "SessionStart", root, "")
	for _, want := range []string{"codex-ab12", `Pass session_id \"s1\"`, "claude-77aa (claude-code)", "please review api/server.go"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
	if !cs.saw("POST /api/peers") {
		t.Fatalf("peer not registered: %v", cs.requests)
	}
}

func TestCodexHook_SessionStartAPIDownKeepsNotice(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "offline", 503) })
	out, _ := runHook(t, "context", "SessionStart", root, "")
	if !strings.Contains(out, "Native Codex session ID: s1") || strings.Contains(out, "Stratus comms") {
		t.Fatalf("got %s", out)
	}
}

func TestCodexHook_PostToolUseInjectsInbox(t *testing.T) {
	cs := &commsServer{messages: waitingMessage}
	root := hookRoot(t, cs.handler)
	out, _ := runHook(t, "activity", "PostToolUse", root, `,"turn_id":"t1","tool_name":"Bash","tool_use_id":"u1","tool_input":{"command":"ls"}`)
	if !strings.Contains(out, `"hookEventName":"PostToolUse"`) || !strings.Contains(out, "please review api/server.go") {
		t.Fatalf("got %s", out)
	}
}

func TestCodexHook_PostToolUseEmptyInboxEmitsEmptyObject(t *testing.T) {
	cs := &commsServer{}
	root := hookRoot(t, cs.handler)
	out, _ := runHook(t, "activity", "PostToolUse", root, `,"turn_id":"t1","tool_name":"Bash","tool_use_id":"u1","tool_input":{"command":"ls"}`)
	if strings.TrimSpace(out) != "{}" {
		t.Fatalf("got %s", out)
	}
}

func TestCodexHook_SessionEndEndsPeer(t *testing.T) {
	cs := &commsServer{}
	root := hookRoot(t, cs.handler)
	runHook(t, "activity", "SessionEnd", root, "")
	if !cs.saw("POST /api/peers/codex:s1/end") {
		t.Fatalf("peer not ended: %v", cs.requests)
	}
}
