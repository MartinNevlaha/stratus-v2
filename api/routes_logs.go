package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type saveWorkflowLogInput struct {
	WorkflowID string `json:"workflow_id"`
	SessionID  string `json:"session_id"`
	ToolName   string `json:"tool_name"`
	Summary    string `json:"summary"`
}

// handleSaveWorkflowLog saves a streamed tool-call log entry and broadcasts it.
func (s *Server) handleSaveWorkflowLog(w http.ResponseWriter, r *http.Request) {
	var in saveWorkflowLogInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if in.SessionID == "" || in.ToolName == "" {
		http.Error(w, "session_id and tool_name are required", http.StatusBadRequest)
		return
	}

	// If workflow_id not provided, try to resolve from active workflows by session.
	workflowID := in.WorkflowID
	if workflowID == "" {
		if active, err := s.coordinator.ListActive(); err == nil {
			for _, wf := range active {
				if wf.SessionID == in.SessionID {
					workflowID = wf.ID
					break
				}
			}
		}
	}

	entry, err := s.db.SaveWorkflowLog(workflowID, in.SessionID, in.ToolName, in.Summary)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	s.hub.BroadcastJSON("workflow_log", entry)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// handleGetWorkflowLogs returns log entries for a workflow or session.
func (s *Server) handleGetWorkflowLogs(w http.ResponseWriter, r *http.Request) {
	workflowID := r.URL.Query().Get("workflow_id")
	sessionID := r.URL.Query().Get("session_id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	var logs []db.WorkflowLog
	var err error

	switch {
	case workflowID != "":
		logs, err = s.db.GetWorkflowLogs(workflowID, limit)
	case sessionID != "":
		logs, err = s.db.GetWorkflowLogsBySession(sessionID, limit)
	default:
		http.Error(w, "workflow_id or session_id required", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if logs == nil {
		logs = []db.WorkflowLog{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}
