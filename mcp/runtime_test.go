package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runtimeServer(t *testing.T, h http.HandlerFunc) *Server {
	t.Helper()
	api := httptest.NewServer(h)
	t.Cleanup(api.Close)
	s := &Server{tools: map[string]Tool{}}
	RegisterToolsForRuntime(s, api.URL, api.Client(), RuntimeOptions{Runtime: "codex"})
	return s
}
func TestRuntimeMCP_RegisterOwnership(t *testing.T) {
	var body map[string]any
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{}`))
	})
	_, e := s.tools["register_workflow"].Handler(map[string]any{"id": "spec-a", "type": "spec", "title": "x", "session_id": "s1"})
	if e != nil || body["session_id"] != "codex:s1" || body["runtime_session_id"] != "s1" || body["runtime"] != "codex" {
		t.Fatal(body, e)
	}
	if _, e = s.tools["register_workflow"].Handler(map[string]any{"id": "missing"}); e == nil {
		t.Fatal("missing session accepted")
	}
}
func TestRuntimeMCP_MutationsCheckOwnership(t *testing.T) {
	writes := 0
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		w.Write([]byte(`{"id":"spec-legacy","runtime":"","session_id":"s1"}`))
	})
	_, e := s.tools["transition_phase"].Handler(map[string]any{"workflow_id": "spec-legacy", "phase": "implement", "tasks": []any{"should not save"}, "plan_content": "should not save"})
	if e == nil || writes != 0 {
		t.Fatal(e, writes)
	}
}
func TestRuntimeMCP_BindAndDispatch(t *testing.T) {
	var body map[string]any
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			json.NewDecoder(r.Body).Decode(&body)
			w.Write([]byte(`{}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "workflows") {
			w.Write([]byte(`{"workflows":[{"id":"spec-codex","runtime":"codex","session_id":"codex:s1"},{"id":"spec-legacy","session_id":"s1"}]}`))
			return
		}
		w.Write([]byte(`{"id":"spec-codex","runtime":"codex"}`))
	})
	if _, e := s.tools["bind_workflow_session"].Handler(map[string]any{"workflow_id": "spec-codex", "session_id": "s2"}); e != nil || body["session_id"] != "codex:s2" {
		t.Fatal(body, e)
	}
	if _, e := s.tools["delivery_dispatch"].Handler(map[string]any{"session_id": "s1"}); e != nil {
		t.Fatal(e)
	}
}
func TestLegacyResolution_ExcludesCodex(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"workflows":[{"id":"spec-codex","runtime":"codex","phase":"plan"}]}`))
	}))
	defer api.Close()
	c := &apiClient{base: api.URL, http: api.Client()}
	id, e := resolveDispatchWorkflowID(c, map[string]any{})
	if e != nil || id != "" {
		t.Fatal(id, e)
	}
}

func TestReview_CodexWikiQueryRemainsUsable(t *testing.T) {
	calls := 0
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/wiki/query" {
			calls++
		}
		w.Write([]byte(`{"answer":"ok"}`))
	})
	_, err := s.tools["wiki_query"].Handler(map[string]any{"query": "What does this project do?", "persist": false})
	if err != nil || calls != 1 {
		t.Fatalf("wiki query rejected: calls=%d err=%v", calls, err)
	}
}

func TestReview_SwarmTicketMustMatchApprovedMission(t *testing.T) {
	writes := 0
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
			w.Write([]byte(`{}`))
			return
		}
		switch r.URL.Path {
		case "/api/swarm/missions/codex-mission":
			w.Write([]byte(`{"mission":{"id":"codex-mission","workflow_id":"spec-codex"},"tickets":[]}`))
		case "/api/workflows/spec-codex":
			w.Write([]byte(`{"id":"spec-codex","runtime":"codex"}`))
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
		}
	})
	_, err := s.tools["swarm_ticket_update"].Handler(map[string]any{"ticket_id": "legacy-ticket", "mission_id": "codex-mission", "status": "done"})
	if err == nil || writes != 0 {
		t.Fatalf("foreign ticket mutation accepted: writes=%d err=%v", writes, err)
	}
}
func TestReview_SwarmSignalRecipientMustBeOwned(t *testing.T) {
	writes := 0
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
			w.Write([]byte(`{}`))
			return
		}
		switch r.URL.Path {
		case "/api/swarm/workers/codex-worker":
			w.Write([]byte(`{"id":"codex-worker","mission_id":"codex-mission"}`))
		case "/api/swarm/workers/legacy-worker":
			w.Write([]byte(`{"id":"legacy-worker","mission_id":"legacy-mission"}`))
		case "/api/swarm/missions/codex-mission":
			w.Write([]byte(`{"mission":{"id":"codex-mission","workflow_id":"spec-codex"}}`))
		case "/api/swarm/missions/legacy-mission":
			w.Write([]byte(`{"mission":{"id":"legacy-mission","workflow_id":"spec-legacy"}}`))
		case "/api/workflows/spec-codex":
			w.Write([]byte(`{"id":"spec-codex","runtime":"codex"}`))
		case "/api/workflows/spec-legacy":
			w.Write([]byte(`{"id":"spec-legacy"}`))
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
		}
	})
	_, err := s.tools["swarm_send_signal"].Handler(map[string]any{"from_worker": "codex-worker", "to_worker": "legacy-worker", "mission_id": "codex-mission", "type": "STATUS", "payload": "{}"})
	if err == nil || writes != 0 {
		t.Fatalf("foreign recipient mutation accepted: writes=%d err=%v", writes, err)
	}
}

func TestReview_DispatchDoesNotUseDashboardLimit(t *testing.T) {
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/integrations/codex/workflows" {
			w.Write([]byte(`{"workflows":[{"id":"spec-old","runtime":"codex","session_id":"codex:s1"},{"id":"spec-new","runtime":"codex","session_id":"codex:s1"}]}`))
			return
		}
		w.Write([]byte(`{"workflows":[{"id":"spec-new","runtime":"codex","session_id":"codex:s1"}]}`))
	})
	if _, err := s.tools["delivery_dispatch"].Handler(map[string]any{"session_id": "s1"}); err == nil {
		t.Fatal("dispatch ignored an older owned workflow")
	}
}

func TestReview_RegistrationSchemaRequiresNativeIdentity(t *testing.T) {
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected HTTP request") })
	b, err := json.Marshal(s.tools["register_workflow"].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(b, &schema)
	required := false
	for _, name := range schema.Required {
		required = required || name == "session_id"
	}
	if !required || strings.Contains(string(b), "CLAUDE_SESSION_ID") || strings.Contains(string(b), "Claude") {
		t.Fatalf("Codex registration advertises the wrong contract: %s", b)
	}
}

func TestRuntimeMCP_SwarmAllowsConsistentOwnedTargets(t *testing.T) {
	writes := 0
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
			w.Write([]byte(`{}`))
			return
		}
		switch r.URL.Path {
		case "/api/swarm/workers/sender", "/api/swarm/workers/recipient":
			w.Write([]byte(`{"mission_id":"codex-mission"}`))
		case "/api/swarm/missions/codex-mission":
			w.Write([]byte(`{"mission":{"workflow_id":"spec-codex"},"tickets":[{"id":"owned-ticket"}]}`))
		case "/api/workflows/spec-codex":
			w.Write([]byte(`{"runtime":"codex"}`))
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
		}
	})
	for name, args := range map[string]map[string]any{
		"swarm_ticket_update": {"ticket_id": "owned-ticket", "mission_id": "codex-mission", "status": "done"},
		"swarm_send_signal":   {"from_worker": "sender", "to_worker": "recipient", "mission_id": "codex-mission", "type": "STATUS"},
	} {
		if _, err := s.tools[name].Handler(args); err != nil {
			t.Fatal(name, err)
		}
	}
	if writes != 2 {
		t.Fatal(writes)
	}
}
