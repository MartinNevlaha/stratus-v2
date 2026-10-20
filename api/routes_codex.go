package api

import (
	"encoding/json"
	"errors"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/integrations"
	codexadapter "github.com/MartinNevlaha/stratus-v2/integrations/codex"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"net/http"
	"regexp"
	"strings"
)

func (s *Server) handleCodexEvent(w http.ResponseWriter, r *http.Request) {
	var e integrations.Event
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := decodeBody(r, &e); err != nil {
		jsonErr(w, 400, "invalid Codex event")
		return
	}
	if strings.TrimSpace(e.NativeSessionID) == "" || len(e.NativeSessionID) > 512 || len(e.Summary) > 4096 || len(e.Report) > 16<<10 || len(e.Files) > 100 {
		jsonErr(w, 400, "invalid event identity or size")
		return
	}
	switch e.Kind {
	case "agent_role", "session_start", "agent_start", "agent_stop", "tool", "waiting", "turn_stop", "interrupt", "session_end":
	default:
		jsonErr(w, 400, "unsupported event")
		return
	}
	if (e.Kind == "agent_start" || e.Kind == "agent_stop") && e.NativeAgentID == "" || e.Kind == "tool" && e.ToolName == "" {
		jsonErr(w, 400, "agent identity or tool name missing")
		return
	}
	if e.Kind == "agent_role" && (e.NativeAgentID == "" || e.WorkflowID == "" || !regexp.MustCompile(`^delivery-[a-z][a-z0-9-]{0,100}$`).MatchString(e.DeliveryRole)) {
		jsonErr(w, 400, "explicit worker identity, workflow and delivery role required")
		return
	}
	owner := orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: e.NativeSessionID}
	key, err := orchestration.SessionKey(owner)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	if e.Kind == "session_start" {
		if _, err = s.db.SaveSession(key, s.projectRoot, nil); err != nil {
			jsonErr(w, 500, "record session failed")
			return
		}
		json200(w, map[string]any{"recorded": true})
		return
	}
	var active []*orchestration.WorkflowState
	if e.WorkflowID != "" {
		var explicit *orchestration.WorkflowState
		explicit, err = s.coordinator.Get(e.WorkflowID)
		if err == nil {
			active = []*orchestration.WorkflowState{explicit}
		}
	} else {
		active, err = s.coordinator.ListActiveForRuntime("codex")
	}
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, orchestration.ErrWorkflowNotFound) {
			code = http.StatusNotFound
		}
		jsonErr(w, code, err.Error())
		return
	}
	wf, err := orchestration.ResolveOwned(active, e.WorkflowID, owner)
	if err != nil {
		code := 409
		if errors.Is(err, orchestration.ErrWorkflowNotFound) {
			code = 404
		}
		jsonErr(w, code, err.Error())
		return
	}
	if wf == nil {
		json200(w, map[string]any{"recorded": false})
		return
	}
	agent := codexadapter.AgentRunKey(e.NativeSessionID, e.NativeAgentID, e.TurnID)
	if e.MainAgent && e.NativeAgentID == "" {
		agent = codexadapter.CoordinatorKey(e.NativeSessionID, e.TurnID)
		e.AgentType = "coordinator"
	}
	paths := []string{}
	if e.Succeeded != nil && *e.Succeeded {
		for _, p := range e.Files {
			if p, ok := codexadapter.RepositoryPath(s.projectRoot, p); ok {
				paths = append(paths, p)
			}
		}
	}
	e.Files = paths
	result, err := s.db.ApplyIntegrationActivity(r.Context(), db.IntegrationActivity{Event: e, EventKey: codexadapter.EventKey(e), SessionKey: key, AgentKey: agent, WorkflowID: wf.ID, Phase: string(wf.Phase), TaskIndex: wf.CurrentTask, Verdict: parseReviewVerdict(e.Report)})
	if err != nil {
		jsonErr(w, 409, "record activity failed: "+err.Error())
		return
	}
	if result.Recorded {
		payload := map[string]any{"workflow_id": wf.ID, "agent_id": agent, "agent_type": e.AgentType, "tool_name": e.ToolName, "runtime": "codex"}
		switch e.Kind {
		case "agent_start":
			if a, err := s.db.GetWorkflowAgent(wf.ID, agent); err == nil && a.Status == "running" {
				s.hub.BroadcastJSON("agent_started", payload)
			}
		case "tool":
			s.hub.BroadcastJSON("agent_activity", payload)
			s.hub.BroadcastJSON("workflow_log", payload)
			if len(e.Files) > 0 {
				s.markDirty(e.Files)
			}
		case "agent_stop", "turn_stop":
			payload["status"] = "done"
			s.hub.BroadcastJSON("agent_status", payload)
		case "waiting":
			payload["message"] = "Codex is waiting for your decision"
			s.hub.BroadcastJSON("workflow_waiting", payload)
		default:
			s.hub.BroadcastJSON("agent_activity", payload)
		}
	}
	json200(w, map[string]any{"recorded": result.Recorded, "duplicate": result.Duplicate, "workflow_id": wf.ID})
}
func (s *Server) codexTranscript(w http.ResponseWriter, r *http.Request, agent db.WorkflowAgent) {
	events, err := s.db.ListIntegrationSteps(r.Context(), agent.WorkflowID, agent.ID, 40)
	if err != nil {
		jsonErr(w, 500, "activity unavailable")
		return
	}
	steps := []TranscriptStep{}
	for _, v := range events {
		var e integrations.Event
		if json.Unmarshal([]byte(v.Text), &e) != nil {
			continue
		}
		if e.Kind == "tool" {
			steps = append(steps, TranscriptStep{Kind: "tool", Name: e.ToolName, Summary: e.Summary})
		} else if e.Report != "" {
			steps = append(steps, TranscriptStep{Kind: "text", Text: e.Report})
		}
	}
	json200(w, map[string]any{"steps": steps, "usage": nil, "usage_available": false, "runtime": "codex"})
}
func runtimeAgents(agents []db.WorkflowAgent, runtime string) []db.WorkflowAgent {
	if runtime == "codex" {
		no := false
		for i := range agents {
			agents[i].Runtime = "codex"
			agents[i].UsageAvailable = &no
		}
	}
	return agents
}

// Ownership discovery is separate from the deliberately bounded dashboard snapshot.
func (s *Server) handleCodexWorkflows(w http.ResponseWriter, r *http.Request) {
	states, err := s.coordinator.ListActiveForRuntime("codex")
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list Codex workflows failed")
		return
	}
	if states == nil {
		states = []*orchestration.WorkflowState{}
	}
	json200(w, map[string]any{"workflows": states})
}

func (s *Server) handleCodexAgentRole(w http.ResponseWriter, r *http.Request) {
	session, agent := r.URL.Query().Get("session_id"), r.URL.Query().Get("agent_id")
	if strings.TrimSpace(session) == "" || strings.TrimSpace(agent) == "" {
		jsonErr(w, 400, "session_id and agent_id required")
		return
	}
	role, id, err := s.db.CodexAgentRole("codex:"+session, agent, r.URL.Query().Get("workflow_id"))
	if err != nil {
		jsonErr(w, 409, "agent role could not be resolved")
		return
	}
	json200(w, map[string]string{"delivery_role": role, "workflow_id": id})
}
