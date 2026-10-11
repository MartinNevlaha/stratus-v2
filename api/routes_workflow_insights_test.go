package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

func getJSON(t *testing.T, h http.HandlerFunc, id string, out any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	h(w, req)
	if out != nil && w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(out); err != nil {
			t.Fatalf("decode: %v (body %s)", err, w.Body.String())
		}
	}
	return w
}

func TestHandleWorkflowDenials_FiltersBySession(t *testing.T) {
	s, id := agentTestServer(t)
	for _, sess := range []string{"sess-1", "other"} {
		if _, err := s.db.SaveEvent(db.SaveEventInput{
			Actor: "stratus-hook", Scope: "project", Type: "hook_denial", Title: "hook denied Write", Text: "phase_guard: no writes in verify",
			Refs: map[string]any{"session_id": sess, "hook": "phase_guard", "tool_name": "Write", "reason": "no writes in verify"},
		}); err != nil {
			t.Fatalf("SaveEvent: %v", err)
		}
	}
	var out struct {
		Denials []map[string]any `json:"denials"`
	}
	if w := getJSON(t, s.handleWorkflowDenials, id, &out); w.Code != http.StatusOK {
		t.Fatalf("code=%d %s", w.Code, w.Body.String())
	}
	if len(out.Denials) != 1 || out.Denials[0]["hook"] != "phase_guard" || out.Denials[0]["tool_name"] != "Write" {
		t.Errorf("denials = %+v", out.Denials)
	}
}

func TestHandleAttention_ListsStaleFailedAndConflicts(t *testing.T) {
	s, id := agentTestServer(t)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"r1","agent_type":"delivery-code-reviewer"}`)
	postAgentEvent(t, s, `{"event":"stop","session_id":"sess-1","agent_id":"r1","last_assistant_message":"Verdict: FAIL\n[must_fix] x — a.go:1"}`)
	for _, a := range []string{"a1", "a2"} {
		postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"`+a+`","agent_type":"x"}`)
		postAgentEvent(t, s, `{"event":"tool","session_id":"sess-1","agent_id":"`+a+`","tool_name":"Edit","files":["same.go"]}`)
	}
	if _, err := s.db.SQL().Exec(`UPDATE workflow_agents SET last_activity='2000-01-01T00:00:00.000Z' WHERE id='a1'`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workflow-agents/attention", nil)
	w := httptest.NewRecorder()
	s.handleAttention(w, req)
	var out struct {
		Items []struct {
			WorkflowID    string   `json:"workflow_id"`
			StaleAgents   int      `json:"stale_agents"`
			ReviewFailed  bool     `json:"review_failed"`
			Conflicts     []string `json:"conflicts"`
			RunningAgents int      `json:"running_agents"`
		} `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %+v", out.Items)
	}
	it := out.Items[0]
	if it.WorkflowID != id || it.StaleAgents != 1 || !it.ReviewFailed || len(it.Conflicts) != 1 || it.RunningAgents != 2 {
		t.Errorf("item = %+v", it)
	}
}

func TestHandleRunSummary_RendersMarkdown(t *testing.T) {
	s, id := agentTestServer(t)
	_, _ = s.coordinator.SetTasks(id, []string{"Build thing"})
	_, _ = s.coordinator.StartTask(id, 0)
	_, _ = s.coordinator.CompleteTask(id, 0)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"a1","agent_type":"delivery-backend-engineer"}`)
	postAgentEvent(t, s, `{"event":"tool","session_id":"sess-1","agent_id":"a1","tool_name":"Edit","files":["x.go"]}`)
	var out struct {
		Markdown string `json:"markdown"`
	}
	if w := getJSON(t, s.handleRunSummary, id, &out); w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	for _, want := range []string{"# Agents", "- [x] Build thing", "backend-engineer", "`x.go`"} {
		if !strings.Contains(out.Markdown, want) {
			t.Errorf("markdown missing %q:\n%s", want, out.Markdown)
		}
	}
}

func TestHandleRunComparison_FindsPreviousRunWithSameTitle(t *testing.T) {
	s, id := agentTestServer(t)
	coord := s.coordinator
	if _, err := coord.Start("spec-old", orchestration.WorkflowSpec, orchestration.ComplexitySimple, "Agents"); err != nil {
		t.Fatal(err)
	}
	_, _ = coord.SetTasks("spec-old", []string{"a", "b"})
	if _, err := s.db.SQL().Exec(`UPDATE workflows SET phase='complete', created_at='2000-01-01T00:00:00Z' WHERE id='spec-old'`); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Previous *struct {
			ID    string `json:"id"`
			Tasks int    `json:"tasks"`
		} `json:"previous"`
		Current struct {
			Tasks int `json:"tasks"`
		} `json:"current"`
	}
	if w := getJSON(t, s.handleRunComparison, id, &out); w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	if out.Previous == nil || out.Previous.ID != "spec-old" || out.Previous.Tasks != 2 {
		t.Errorf("previous = %+v", out.Previous)
	}
}

func TestHandleRunComparison_NoPreviousRun(t *testing.T) {
	s, id := agentTestServer(t)
	var out struct {
		Previous *struct{} `json:"previous"`
	}
	if w := getJSON(t, s.handleRunComparison, id, &out); w.Code != http.StatusOK || out.Previous != nil {
		t.Fatalf("code=%d previous=%v", w.Code, out.Previous)
	}
}

func TestHandleSkipTask_SkipsAndRejectsDone(t *testing.T) {
	s, id := agentTestServer(t)
	_, _ = s.coordinator.SetTasks(id, []string{"a", "b"})
	_, _ = s.coordinator.StartTask(id, 0)
	_, _ = s.coordinator.CompleteTask(id, 0)
	skip := func(idx string) int {
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.SetPathValue("id", id)
		req.SetPathValue("index", idx)
		w := httptest.NewRecorder()
		s.handleSkipTask(w, req)
		return w.Code
	}
	if c := skip("1"); c != http.StatusOK {
		t.Fatalf("skip pending: %d", c)
	}
	if c := skip("0"); c != http.StatusBadRequest {
		t.Fatalf("skip done: %d, want 400", c)
	}
}

func TestHandleRunSummary_ReturnsStoredSummaryAfterComplete(t *testing.T) {
	s, id := agentTestServer(t)
	_, _ = s.coordinator.SetRunSummary(id, "## stored report")
	var out struct {
		Markdown string `json:"markdown"`
		Stored   bool   `json:"stored"`
	}
	getJSON(t, s.handleRunSummary, id, &out)
	if out.Markdown != "## stored report" || !out.Stored {
		t.Errorf("out = %+v", out)
	}
}

func TestHandleListAllAgents_FiltersByStatusAndType(t *testing.T) {
	s, _ := agentTestServer(t)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"r1","agent_type":"delivery-backend-engineer"}`)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"r2","agent_type":"delivery-qa-engineer"}`)
	postAgentEvent(t, s, `{"event":"stop","session_id":"sess-1","agent_id":"r2"}`)
	list := func(q string) []map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/api/workflow-agents"+q, nil)
		w := httptest.NewRecorder()
		s.handleListAllAgents(w, req)
		var out struct {
			Agents []map[string]any `json:"agents"`
		}
		_ = json.NewDecoder(w.Body).Decode(&out)
		return out.Agents
	}
	if n := len(list("")); n != 2 {
		t.Fatalf("all = %d", n)
	}
	if got := list("?status=running"); len(got) != 1 || got[0]["id"] != "r1" || got[0]["workflow_title"] != "Agents" {
		t.Fatalf("running = %+v", got)
	}
	if got := list("?type=delivery-qa-engineer"); len(got) != 1 || got[0]["id"] != "r2" {
		t.Fatalf("type = %+v", got)
	}
}
