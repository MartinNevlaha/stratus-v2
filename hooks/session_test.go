package hooks

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeAPI serves the dashboard state and single workflows, and records every POST.
type fakeAPI struct {
	mu    sync.Mutex
	posts []string // "<path> <body>"
}

func (f *fakeAPI) posted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.posts...)
}

func startFakeAPI(t *testing.T, state dashboardState) *fakeAPI {
	t.Helper()
	f := &fakeAPI{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.posts = append(f.posts, r.URL.Path+" "+string(bytes.TrimSpace(body)))
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.URL.Path == "/api/dashboard/state" {
			_ = json.NewEncoder(w).Encode(state)
			return
		}
		id, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/workflows/"))
		for _, wf := range state.Workflows {
			if wf["id"] == id {
				_ = json.NewEncoder(w).Encode(wf)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	_, port, _ := net.SplitHostPort(u.Host)
	t.Setenv("STRATUS_PORT", port)
	return f
}

func TestDelegationGuard_RecordsAllowedDelegation(t *testing.T) {
	api := startFakeAPI(t, dashboardState{Workflows: []map[string]any{
		{"id": "spec-x", "session_id": "s1", "type": "spec", "phase": "implement"},
	}})

	decision := DelegationGuard(HookEvent{
		ToolName:  "Agent",
		SessionID: "s1",
		ToolInput: map[string]any{"subagent_type": "delivery-backend-engineer", "workflow_id": "spec-x"},
	})

	if !decision.Continue {
		t.Fatalf("expected the delegation to be allowed, got %+v", decision)
	}
	want := `/api/workflows/spec-x/delegate {"agent_id":"delivery-backend-engineer","workflow_id":"spec-x"}`
	if got := api.posted(); len(got) != 1 || got[0] != want {
		t.Fatalf("posts = %v, want [%s]", got, want)
	}
}

func TestDelegationGuard_DoesNotRecordBlockedDelegation(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir()) // not the Stratus repo: no self-repo escape hatch
	t.Chdir(t.TempDir())
	api := startFakeAPI(t, dashboardState{})

	decision := DelegationGuard(HookEvent{
		ToolName:  "Agent",
		SessionID: "s1",
		Cwd:       t.TempDir(),
		ToolInput: map[string]any{"subagent_type": "delivery-backend-engineer"},
	})

	if decision.Continue {
		t.Fatalf("expected a block without a workflow, got %+v", decision)
	}
	for _, p := range api.posted() {
		if strings.Contains(p, "/delegate") {
			t.Fatalf("a blocked delegation was recorded: %v", api.posted())
		}
	}
}

func TestSessionStart_TellsActiveWorkflowsAndRegistersSession(t *testing.T) {
	api := startFakeAPI(t, dashboardState{Workflows: []map[string]any{
		{"id": "spec-x", "type": "spec", "phase": "implement", "title": "Add mods", "total_tasks": 2,
			"tasks": []any{map[string]any{"status": "done"}, map[string]any{"status": "pending"}}},
		{"id": "bug-gone", "type": "bug", "phase": "fix", "aborted": true},
		{"id": "spec-done", "type": "spec", "phase": "complete"},
	}})

	decision := SessionStart(HookEvent{HookEventName: "SessionStart", SessionID: "s9", Cwd: "/home/u/projects/shop"})

	if !decision.Continue {
		t.Fatalf("SessionStart must never block, got %+v", decision)
	}
	for _, want := range []string{"spec-x", "Add mods", "implement", "1/2", "/resume"} {
		if !strings.Contains(decision.Context, want) {
			t.Errorf("context lacks %q: %q", want, decision.Context)
		}
	}
	for _, gone := range []string{"bug-gone", "spec-done"} {
		if strings.Contains(decision.Context, gone) {
			t.Errorf("context lists the inactive workflow %s: %q", gone, decision.Context)
		}
	}
	want := `/api/sessions {"content_session_id":"s9","project":"shop"}`
	if got := api.posted(); len(got) != 1 || got[0] != want {
		t.Fatalf("posts = %v, want [%s]", got, want)
	}
}

func TestSessionStart_AddsNothingWithoutActiveWorkflows(t *testing.T) {
	startFakeAPI(t, dashboardState{Workflows: []map[string]any{{"id": "spec-done", "phase": "complete"}}})

	decision := SessionStart(HookEvent{HookEventName: "SessionStart", SessionID: "s9"})

	if !decision.Continue || decision.Context != "" {
		t.Fatalf("got %+v, want allow with no context", decision)
	}
}

func TestSessionStart_AllowsWhenAPIIsDown(t *testing.T) {
	t.Setenv("STRATUS_PORT", "1")

	decision := SessionStart(HookEvent{HookEventName: "SessionStart", SessionID: "s9"})

	if !decision.Continue || decision.Context != "" {
		t.Fatalf("got %+v, want allow with no context", decision)
	}
}

func TestWriteContext_UsesHookSpecificOutput(t *testing.T) {
	var out bytes.Buffer
	writeContext(&out, "SessionStart", "Active Stratus workflows: spec-x")

	var got struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, out.String())
	}
	if got.HookSpecificOutput.HookEventName != "SessionStart" || got.HookSpecificOutput.AdditionalContext != "Active Stratus workflows: spec-x" {
		t.Fatalf("got %+v", got.HookSpecificOutput)
	}
	if strings.Contains(out.String(), `"continue"`) {
		t.Fatalf("context output must not carry continue: %q", out.String())
	}
}
