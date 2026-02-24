package api

import (
	"net/http"
	"strconv"

	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

func (s *Server) handleStartWorkflow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID         string `json:"id"`
		Type       string `json:"type"`       // "spec" | "bug"
		Complexity string `json:"complexity"` // "simple" | "complex"
		Title      string `json:"title"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.ID == "" {
		jsonErr(w, http.StatusBadRequest, "id is required")
		return
	}
	wtype := orchestration.WorkflowSpec
	if body.Type == "bug" {
		wtype = orchestration.WorkflowBug
	}
	complexity := orchestration.ComplexitySimple
	if body.Complexity == "complex" {
		complexity = orchestration.ComplexityComplex
	}
	state, err := s.coordinator.Start(body.ID, wtype, complexity, body.Title)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, err := s.coordinator.Get(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	json200(w, state)
}

func (s *Server) handleTransitionPhase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Phase string `json:"phase"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	state, err := s.coordinator.Transition(id, orchestration.Phase(body.Phase))
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleRecordDelegation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		AgentID string `json:"agent_id"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	state, err := s.coordinator.RecordDelegation(id, body.AgentID)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleSetTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Tasks []string `json:"tasks"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	state, err := s.coordinator.SetTasks(id, body.Tasks)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleStartTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid task index")
		return
	}
	state, err := s.coordinator.StartTask(id, index)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleCompleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid task index")
		return
	}
	state, err := s.coordinator.CompleteTask(id, index)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleAbortWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, err := s.coordinator.Abort(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_aborted", map[string]string{"id": id})
	json200(w, state)
}

func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, err := s.coordinator.Get(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	phase := string(state.Phase)
	delegated := state.Delegated[phase]
	if delegated == nil {
		delegated = []string{}
	}
	json200(w, map[string]any{
		"workflow_id":       id,
		"type":              state.Type,
		"phase":             phase,
		"delegated_agents":  delegated,
		"total_tasks":       state.TotalTasks,
		"current_task":      state.CurrentTask,
		"tasks":             state.Tasks,
	})
}
