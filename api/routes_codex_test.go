package api

import (
	"encoding/json"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"net/http/httptest"
	"strings"
	"testing"
)

func codexTestServer(t *testing.T) *Server {
	t.Helper()
	d := setupTestDB(t)
	t.Cleanup(func() { d.Close() })
	c := orchestration.NewCoordinator(d)
	c.StartOwned("spec-codex", orchestration.WorkflowSpec, orchestration.ComplexitySimple, "Codex", orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: "s1"})
	return &Server{db: d, coordinator: c, hub: NewHub(), projectRoot: t.TempDir()}
}
func postCodex(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/integrations/codex/events", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleCodexEvent(w, r)
	return w
}
func TestCodexEvent_OwnedLifecycle(t *testing.T) {
	s := codexTestServer(t)
	for _, kind := range []string{"agent_start", "tool", "agent_stop", "turn_stop"} {
		w := postCodex(t, s, `{"event":"`+kind+`","session_id":"s1","agent_id":"a1","agent_type":"worker","tool_name":"Bash","turn_id":"t1","tool_use_id":"u1"}`)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	a, e := s.db.ListWorkflowAgents("spec-codex")
	if e != nil || len(a) != 1 || a[0].Status != "done" || a[0].ToolCalls != 1 {
		t.Fatal(a, e)
	}
	wf, _ := s.coordinator.Get("spec-codex")
	if wf.Phase != "plan" {
		t.Fatal(wf)
	}
}
func TestCodexEvent_AmbiguousSession(t *testing.T) {
	s := codexTestServer(t)
	s.coordinator.StartOwned("spec-other", orchestration.WorkflowSpec, orchestration.ComplexitySimple, "x", orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: "s1"})
	w := postCodex(t, s, `{"event":"agent_start","session_id":"s1","agent_id":"a1"}`)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestCodexEvent_NoCrossRuntime(t *testing.T) {
	s := codexTestServer(t)
	s.coordinator.Start("spec-legacy", orchestration.WorkflowSpec, orchestration.ComplexitySimple, "x")
	w := postCodex(t, s, `{"event":"agent_start","session_id":"s1","agent_id":"a1","workflow_id":"spec-legacy"}`)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	a, _ := s.db.ListWorkflowAgents("spec-legacy")
	if len(a) != 0 {
		t.Fatal(a)
	}
}
func TestCodexTranscript_UsesNormalizedEvents(t *testing.T) {
	s := codexTestServer(t)
	postCodex(t, s, `{"event":"agent_start","session_id":"s1","agent_id":"a1","agent_type":"worker"}`)
	postCodex(t, s, `{"event":"tool","session_id":"s1","agent_id":"a1","tool_name":"Bash","summary":"pwd","turn_id":"t1","tool_use_id":"u1"}`)
	agents, _ := s.db.ListWorkflowAgents("spec-codex")
	r := httptest.NewRequest("GET", "/", nil)
	r.SetPathValue("id", "spec-codex")
	r.SetPathValue("agent", agents[0].ID)
	w := httptest.NewRecorder()
	s.handleAgentTranscript(w, r)
	var v map[string]any
	json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != 200 || v["usage"] != nil || v["usage_available"] != false || len(v["steps"].([]any)) == 0 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestReview_ExplicitOwnedEventOutsideDashboardLimit(t *testing.T) {
	s := codexTestServer(t)
	for _, suffix := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		s.coordinator.Start("spec-"+suffix, orchestration.WorkflowSpec, orchestration.ComplexitySimple, "unrelated")
	}
	w := postCodex(t, s, `{"event":"agent_start","session_id":"s1","workflow_id":"spec-codex","agent_id":"a1"}`)
	if w.Code != 200 {
		t.Fatalf("explicit active owned workflow dropped: %d %s", w.Code, w.Body.String())
	}
}
func TestReview_AmbiguityMustIncludeHiddenOwnedWorkflow(t *testing.T) {
	s := codexTestServer(t)
	for _, suffix := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} {
		s.coordinator.Start("spec-"+suffix, orchestration.WorkflowSpec, orchestration.ComplexitySimple, "unrelated")
	}
	s.coordinator.StartOwned("spec-new", orchestration.WorkflowSpec, orchestration.ComplexitySimple, "new", orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: "s1"})
	w := postCodex(t, s, `{"event":"agent_start","session_id":"s1","agent_id":"a1"}`)
	if w.Code != 409 {
		t.Fatalf("ambiguous event incorrectly assigned: %d %s", w.Code, w.Body.String())
	}
}
