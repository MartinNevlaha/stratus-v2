package api

import (
	"encoding/json"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCodexAgentRole_ExplicitBindingIsOwned(t *testing.T) {
	s := codexTestServer(t)
	for _, body := range []string{
		`{"event":"agent_start","session_id":"s1","agent_id":"a1","turn_id":"t1","agent_type":"default"}`,
		`{"event":"agent_role","workflow_id":"spec-codex","session_id":"s1","agent_id":"a1","delivery_role":"delivery-code-reviewer"}`,
		`{"event":"tool","session_id":"s1","agent_id":"a1","turn_id":"t2","tool_use_id":"u1","tool_name":"Bash","agent_type":"default"}`,
	} {
		if w := postCodex(t, s, body); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	agents, err := s.db.ListWorkflowAgents("spec-codex")
	if err != nil || len(agents) != 2 {
		t.Fatal(agents, err)
	}
	for _, a := range agents {
		if a.AgentType != "delivery-code-reviewer" {
			t.Fatal(a)
		}
	}
	for _, body := range []string{
		`{"event":"agent_role","workflow_id":"spec-codex","session_id":"s2","agent_id":"a1","delivery_role":"delivery-code-reviewer"}`,
		`{"event":"agent_role","workflow_id":"spec-codex","session_id":"s1","agent_id":"a1","delivery_role":"delivery-backend-engineer"}`,
	} {
		if w := postCodex(t, s, body); w.Code != 409 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "/api/workflows/spec-codex/agents", nil)
	r.SetPathValue("id", "spec-codex")
	w := httptest.NewRecorder()
	s.handleListWorkflowAgents(w, r)
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	if got["review"] == nil {
		t.Fatal(got)
	}
}

func TestCodexAgentRole_RejectsCrossWorkflowAdoption(t *testing.T) {
	s := codexTestServer(t)
	s.coordinator.StartOwned("spec-other", orchestration.WorkflowSpec, orchestration.ComplexitySimple, "Other", orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: "s1"})
	for i, wf := range []string{"spec-codex", "spec-other"} {
		w := postCodex(t, s, `{"event":"agent_role","workflow_id":"`+wf+`","session_id":"s1","agent_id":"a1","delivery_role":"delivery-code-reviewer"}`)
		if i == 0 && w.Code != 200 || i == 1 && w.Code != 409 {
			t.Fatal(i, w.Code, w.Body.String())
		}
	}
}

func TestCodexEvent_RejectsCrossWorkflowRun(t *testing.T) {
	s := codexTestServer(t)
	s.coordinator.StartOwned("spec-other", orchestration.WorkflowSpec, orchestration.ComplexitySimple, "Other", orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: "s1"})
	for i, wf := range []string{"spec-codex", "spec-other"} {
		w := postCodex(t, s, `{"event":"tool","workflow_id":"`+wf+`","session_id":"s1","agent_id":"a1","turn_id":"t1","tool_use_id":"u`+wf+`","tool_name":"Bash"}`)
		if i == 0 && w.Code != 200 || i == 1 && w.Code != 409 {
			t.Fatal(i, w.Code, w.Body.String())
		}
	}
}

func TestCodexAgentRole_BindingBeforeStartAndDuplicate(t *testing.T) {
	s := codexTestServer(t)
	body := `{"event":"agent_role","workflow_id":"spec-codex","session_id":"s1","agent_id":"a1","delivery_role":"delivery-backend-engineer"}`
	for range 2 {
		if w := postCodex(t, s, body); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	if w := postCodex(t, s, `{"event":"agent_start","session_id":"s1","agent_id":"a1","turn_id":"t1","agent_type":"default"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	a, err := s.db.ListWorkflowAgents("spec-codex")
	if err != nil || len(a) != 1 || a[0].AgentType != "delivery-backend-engineer" {
		t.Fatal(a, err)
	}
	r := httptest.NewRequest("GET", "/api/integrations/codex/agents/role?session_id=s1&agent_id=a1", nil)
	w := httptest.NewRecorder()
	s.handleCodexAgentRole(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "delivery-backend-engineer") {
		t.Fatal(w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "/api/integrations/codex/agents/role?session_id=s2&agent_id=a1", nil)
	w = httptest.NewRecorder()
	s.handleCodexAgentRole(w, r)
	if strings.Contains(w.Body.String(), "delivery-backend-engineer") {
		t.Fatal("foreign role exposed", w.Body.String())
	}
}
