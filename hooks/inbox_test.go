package hooks

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// commsAPI is a fake Stratus API for the comms endpoints; it records every request.
type commsAPI struct {
	mu       sync.Mutex
	requests []string
	inbox    string // JSON body served by inbox/claim
}

func (f *commsAPI) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func startCommsAPI(t *testing.T, inbox string) *commsAPI {
	t.Helper()
	f := &commsAPI{inbox: inbox}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/inbox/claim"):
			_, _ = w.Write([]byte(f.inbox))
		case r.Method == http.MethodPost && r.URL.Path == "/api/peers":
			_, _ = w.Write([]byte(`{"session_key":"s1","runtime":"claude-code","handle":"claude-1111"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/peers":
			_, _ = w.Write([]byte(`{"peers":[{"session_key":"s1","handle":"claude-1111","runtime":"claude-code"},{"session_key":"c1","handle":"codex-2222","runtime":"codex"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/end"):
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == "/api/dashboard/state":
			_, _ = w.Write([]byte(`{"workflows":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	_, port, _ := net.SplitHostPort(u.Host)
	t.Setenv("STRATUS_PORT", port)
	return f
}

const oneMessage = `{"messages":[{"id":1,"from_handle":"codex-2222","from_runtime":"codex","kind":"text","body":"ping from codex"}]}`

func TestInbox_DeliversMessagesAsContext(t *testing.T) {
	api := startCommsAPI(t, oneMessage)

	d := Inbox(HookEvent{HookEventName: "UserPromptSubmit", SessionID: "s1"})

	if !d.Continue || !strings.Contains(d.Context, "ping from codex") || !strings.Contains(d.Context, "codex-2222") {
		t.Fatalf("got %+v, want allow with the message as context", d)
	}
	if got := api.seen(); len(got) != 1 || got[0] != "POST /api/peers/s1/inbox/claim" {
		t.Fatalf("requests = %v, want exactly one claim", got)
	}
}

func TestInbox_EmptyInboxAddsNoContext(t *testing.T) {
	startCommsAPI(t, `{"messages":[]}`)

	d := Inbox(HookEvent{HookEventName: "PostToolUse", SessionID: "s1"})

	if !d.Continue || d.Context != "" {
		t.Fatalf("got %+v, want allow with no context", d)
	}
}

func TestInbox_APIDownAllowsWithoutContext(t *testing.T) {
	t.Setenv("STRATUS_PORT", "1")

	d := Inbox(HookEvent{HookEventName: "PostToolUse", SessionID: "s1"})

	if !d.Continue || d.Context != "" {
		t.Fatalf("got %+v, want allow with no context", d)
	}
}

func TestInbox_SubagentEventDoesNotClaim(t *testing.T) {
	api := startCommsAPI(t, oneMessage)

	d := Inbox(HookEvent{HookEventName: "PostToolUse", SessionID: "s1", AgentID: "agent-9"})

	if !d.Continue || d.Context != "" {
		t.Fatalf("got %+v, want allow with no context", d)
	}
	if got := api.seen(); len(got) != 0 {
		t.Fatalf("requests = %v, a subagent must not claim the main session's inbox", got)
	}
}

func TestInbox_NoSessionIDDoesNotClaim(t *testing.T) {
	api := startCommsAPI(t, oneMessage)

	d := Inbox(HookEvent{HookEventName: "UserPromptSubmit"})

	if !d.Continue || d.Context != "" || len(api.seen()) != 0 {
		t.Fatalf("got %+v, requests %v, want allow with no call", d, api.seen())
	}
}

func TestSessionEnd_EndsPeerAndAllows(t *testing.T) {
	api := startCommsAPI(t, `{}`)

	d := SessionEnd(HookEvent{HookEventName: "SessionEnd", SessionID: "s1"})

	if !d.Continue {
		t.Fatalf("SessionEnd must allow, got %+v", d)
	}
	if got := api.seen(); len(got) != 1 || got[0] != "POST /api/peers/s1/end" {
		t.Fatalf("requests = %v, want the end call", got)
	}
}

func TestSessionEnd_APIDownStillAllows(t *testing.T) {
	t.Setenv("STRATUS_PORT", "1")

	if d := SessionEnd(HookEvent{SessionID: "s1"}); !d.Continue {
		t.Fatalf("got %+v, want allow", d)
	}
}

func TestSessionStart_AddsCommsIntroAndWaitingMessages(t *testing.T) {
	api := startCommsAPI(t, oneMessage)

	d := SessionStart(HookEvent{HookEventName: "SessionStart", SessionID: "s1", Cwd: "/home/u/projects/shop"})

	if !d.Continue {
		t.Fatalf("SessionStart must never block, got %+v", d)
	}
	for _, want := range []string{`"claude-1111"`, `session_id "s1"`, "codex-2222 (codex)", "ping from codex"} {
		if !strings.Contains(d.Context, want) {
			t.Errorf("context lacks %q: %q", want, d.Context)
		}
	}
	registered := false
	for _, r := range api.seen() {
		registered = registered || r == "POST /api/peers"
	}
	if !registered {
		t.Errorf("requests = %v, want the peer registration", api.seen())
	}
}
