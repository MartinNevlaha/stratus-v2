package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

func agentTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	database := setupTestDB(t)
	t.Cleanup(func() { database.Close() })
	coord := orchestration.NewCoordinator(database)
	s := &Server{db: database, coordinator: coord, hub: NewHub()}
	const id = "spec-agents"
	if _, err := coord.Start(id, orchestration.WorkflowSpec, orchestration.ComplexitySimple, "Agents"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := coord.SetSessionID(id, "sess-1"); err != nil {
		t.Fatalf("SetSession: %v", err)
	}
	return s, id
}

func postAgentEvent(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/workflow-agents/events", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorkflowAgentEvent(w, req)
	return w
}

func TestHandleWorkflowAgentEvent_StartToolStopLifecycle(t *testing.T) {
	s, id := agentTestServer(t)

	for _, body := range []string{
		`{"event":"start","session_id":"sess-1","agent_id":"a1","agent_type":"delivery-backend-engineer","transcript_path":"/tmp/a1.jsonl"}`,
		`{"event":"tool","session_id":"sess-1","agent_id":"a1","tool_name":"Edit","files":["x.go"],"summary":"x.go"}`,
		`{"event":"stop","session_id":"sess-1","agent_id":"a1","last_assistant_message":"done"}`,
	} {
		if w := postAgentEvent(t, s, body); w.Code != http.StatusOK {
			t.Fatalf("%s -> %d %s", body, w.Code, w.Body.String())
		}
	}

	agents, err := s.db.ListWorkflowAgents(id)
	if err != nil || len(agents) != 1 {
		t.Fatalf("agents = %+v err=%v", agents, err)
	}
	a := agents[0]
	if a.Status != "done" || a.ToolCalls != 1 || a.Phase != "plan" || len(a.Files) != 1 {
		t.Errorf("unexpected agent: %+v", a)
	}
	logs, _ := s.db.GetWorkflowLogs(id, 10)
	if len(logs) != 1 || logs[0].ToolName != "Edit" {
		t.Errorf("logs = %+v", logs)
	}
}

func TestHandleWorkflowAgentEvent_NoWorkflowForSessionIsNoop(t *testing.T) {
	s, id := agentTestServer(t)
	w := postAgentEvent(t, s, `{"event":"start","session_id":"other","agent_id":"a1","agent_type":"x"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if agents, _ := s.db.ListWorkflowAgents(id); len(agents) != 0 {
		t.Errorf("agents recorded for unrelated session: %+v", agents)
	}
}

func TestHandleWorkflowAgentEvent_StopParsesReviewVerdict(t *testing.T) {
	s, id := agentTestServer(t)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"r1","agent_type":"delivery-code-reviewer"}`)
	msg := "## Review Verdict: FAIL\n\n### Issues\n\n[must_fix] Bug — a.go:1\n[must_fix] Leak — b.go:2\n[should_fix] Style — c.go:3\n"
	b, _ := json.Marshal(map[string]string{"event": "stop", "session_id": "sess-1", "agent_id": "r1", "last_assistant_message": msg})
	if w := postAgentEvent(t, s, string(b)); w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	a, _ := s.db.ListWorkflowAgents(id)
	if a[0].Verdict != "FAIL" || a[0].MustFix != 2 || a[0].ShouldFix != 1 {
		t.Errorf("verdict = %+v", a[0])
	}
}

func TestHandleWorkflowAgentEvent_InvalidEventIs400(t *testing.T) {
	s, _ := agentTestServer(t)
	if w := postAgentEvent(t, s, `{"event":"bogus","session_id":"sess-1","agent_id":"a1"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", w.Code)
	}
	if w := postAgentEvent(t, s, `{"event":"start","session_id":"sess-1"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing agent_id: code = %d, want 400", w.Code)
	}
}

func TestHandleListWorkflowAgents_ReportsFileConflicts(t *testing.T) {
	s, id := agentTestServer(t)
	for _, a := range []string{"a1", "a2"} {
		postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"`+a+`","agent_type":"x"}`)
		postAgentEvent(t, s, `{"event":"tool","session_id":"sess-1","agent_id":"`+a+`","tool_name":"Edit","files":["shared.go"]}`)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/"+id+"/agents", nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.handleListWorkflowAgents(w, req)
	var resp struct {
		Agents    []map[string]any `json:"agents"`
		Conflicts []string         `json:"conflicts"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Agents) != 2 || len(resp.Conflicts) != 1 || resp.Conflicts[0] != "shared.go" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestHandleSetWorkflowAgentStatus_RejectsUnknownStatus(t *testing.T) {
	s, _ := agentTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/workflow-agents/a1/status", strings.NewReader(`{"status":"exploded"}`))
	req.SetPathValue("id", "a1")
	w := httptest.NewRecorder()
	s.handleSetWorkflowAgentStatus(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", w.Code)
	}
}

func TestParseReviewVerdict_NoVerdictLeavesEmpty(t *testing.T) {
	v := parseReviewVerdict("all good, nothing structured")
	if v.Verdict != "" || v.MustFix != 0 {
		t.Errorf("v = %+v", v)
	}
}

func TestHandleSetTasks_AcceptsDependsOnAndRejectsForwardRefs(t *testing.T) {
	s, id := agentTestServer(t)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		s.handleSetTasks(w, req)
		return w
	}
	if w := post(`{"tasks":["a","b"],"depends_on":[[],[0]]}`); w.Code != http.StatusOK {
		t.Fatalf("valid deps: code = %d body=%s", w.Code, w.Body.String())
	}
	st, _ := s.coordinator.Get(id)
	if len(st.Tasks[1].DependsOn) != 1 || st.Tasks[1].DependsOn[0] != 0 {
		t.Errorf("tasks = %+v", st.Tasks)
	}
	if w := post(`{"tasks":["a","b"],"depends_on":[[1],[]]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("forward ref: code = %d, want 400", w.Code)
	}
}

func TestHandleWorkflowAgentEvent_WaitingBroadcastsWithoutAgentID(t *testing.T) {
	s, _ := agentTestServer(t)
	ch := listenHub(s.hub)
	w := postAgentEvent(t, s, `{"event":"waiting","session_id":"sess-1","message":"Claude needs your permission to use Bash","notification_type":"permission_prompt"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	select {
	case msg := <-ch:
		b, _ := json.Marshal(msg)
		if !strings.Contains(string(b), "workflow_waiting") || !strings.Contains(string(b), "needs your permission") {
			t.Errorf("broadcast = %s", b)
		}
	default:
		t.Fatal("no workflow_waiting broadcast")
	}
}

func TestHandleSetWorkflowAgentStatus_BroadcastsWorkflowAndType(t *testing.T) {
	s, id := agentTestServer(t)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"a1","agent_type":"delivery-backend-engineer"}`)
	ch := listenHub(s.hub)
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"status":"failed"}`))
	req.SetPathValue("id", "a1")
	w := httptest.NewRecorder()
	s.handleSetWorkflowAgentStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	select {
	case msg := <-ch:
		b, _ := json.Marshal(msg)
		if !strings.Contains(string(b), id) || !strings.Contains(string(b), "delivery-backend-engineer") {
			t.Errorf("broadcast lacks workflow/type: %s", b)
		}
	default:
		t.Fatal("no agent_status broadcast")
	}
}

// listenHub registers an in-memory client so tests can read what the hub broadcasts.
func listenHub(h *Hub) chan Message {
	c := &wsClient{send: make(chan Message, 16)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c.send
}

func TestHandleWorkflowAgentEvent_IdlePromptIsThrottledPerWorkflow(t *testing.T) {
	s, _ := agentTestServer(t)
	ch := listenHub(s.hub)
	count := func() int {
		n := 0
		for {
			select {
			case m := <-ch:
				if m.Type == "workflow_waiting" {
					n++
				}
			default:
				return n
			}
		}
	}
	idle := `{"event":"waiting","session_id":"sess-1","message":"Claude is waiting for your input","notification_type":"idle_prompt"}`
	perm := `{"event":"waiting","session_id":"sess-1","message":"needs permission","notification_type":"permission_prompt"}`

	postAgentEvent(t, s, idle)
	postAgentEvent(t, s, idle)
	postAgentEvent(t, s, idle)
	if n := count(); n != 1 {
		t.Fatalf("3 idle prompts in a row produced %d broadcasts, want 1", n)
	}
	postAgentEvent(t, s, perm)
	postAgentEvent(t, s, perm)
	if n := count(); n != 2 {
		t.Fatalf("permission prompts must never be throttled, got %d broadcasts", n)
	}
}

func TestIdleThrottle_AllowsAgainAfterTheWindow(t *testing.T) {
	th := newWaitingThrottle(50 * time.Millisecond)
	if !th.allow("wf") {
		t.Fatal("first must pass")
	}
	if th.allow("wf") {
		t.Fatal("second within window must be blocked")
	}
	if !th.allow("other") {
		t.Fatal("a different workflow has its own window")
	}
	time.Sleep(70 * time.Millisecond)
	if !th.allow("wf") {
		t.Fatal("must pass again after the window")
	}
}
