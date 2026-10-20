package api

import (
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

// defaultIdleWindow is the minimum gap between idle_prompt notifications of one workflow.
const defaultIdleWindow = 3 * time.Minute

// waitingThrottle allows one event per key per window. The zero value uses defaultIdleWindow.
type waitingThrottle struct {
	mu     sync.Mutex
	window time.Duration
	last   map[string]time.Time
}

func newWaitingThrottle(window time.Duration) *waitingThrottle {
	return &waitingThrottle{window: window}
}

func (t *waitingThrottle) allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	window := t.window
	if window <= 0 {
		window = defaultIdleWindow
	}
	if t.last == nil {
		t.last = map[string]time.Time{}
	}
	now := time.Now()
	if prev, ok := t.last[key]; ok && now.Sub(prev) < window {
		return false
	}
	t.last[key] = now
	return true
}

// agentEventInput is posted by the SubagentStart / SubagentStop / PostToolUse hooks.
type agentEventInput struct {
	Event                string   `json:"event"` // start | tool | stop
	SessionID            string   `json:"session_id"`
	AgentID              string   `json:"agent_id"`
	AgentType            string   `json:"agent_type"`
	ToolName             string   `json:"tool_name"`
	Files                []string `json:"files"`
	Summary              string   `json:"summary"`
	TranscriptPath       string   `json:"transcript_path"`
	AgentTranscriptPath  string   `json:"agent_transcript_path"`
	Message              string   `json:"message"`
	NotificationType     string   `json:"notification_type"`
	LastAssistantMessage string   `json:"last_assistant_message"`
}

// activeWorkflowForSession finds the active workflow owned by a Claude session.
func (s *Server) activeWorkflowForSession(sessionID string) *orchestration.WorkflowState {
	if sessionID == "" {
		return nil
	}
	active, err := s.coordinator.ListActive()
	if err != nil {
		return nil
	}
	for _, wf := range active {
		if wf.SessionID == sessionID {
			return wf
		}
	}
	return nil
}

// handleWorkflowAgentEvent records subagent lifecycle and activity for the workflow the
// session owns. Sessions without an active workflow are ignored (200), so hooks stay cheap.
func (s *Server) handleWorkflowAgentEvent(w http.ResponseWriter, r *http.Request) {
	var in agentEventInput
	if err := decodeBody(r, &in); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if in.Event != "start" && in.Event != "tool" && in.Event != "stop" && in.Event != "waiting" {
		jsonErr(w, http.StatusBadRequest, "event must be one of: start, tool, stop, waiting")
		return
	}
	if in.Event != "waiting" && in.AgentID == "" {
		jsonErr(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	if in.Event == "stop" {
		s.closeSwarmTickets(in)
	}
	wf := s.activeWorkflowForSession(in.SessionID)
	if wf == nil {
		json200(w, map[string]any{"recorded": false})
		return
	}

	switch in.Event {
	case "waiting":
		// An idle prompt fires after every finished turn; one toast per window is enough.
		// Permission prompts and questions block the work, so they always go out.
		if in.NotificationType == "idle_prompt" && !s.idleThrottle.allow(wf.ID) {
			json200(w, map[string]any{"recorded": false, "throttled": true})
			return
		}
		s.hub.BroadcastJSON("workflow_waiting", map[string]any{
			"workflow_id": wf.ID, "title": wf.Title, "message": in.Message, "notification_type": in.NotificationType,
		})
	case "start":
		err := s.db.StartWorkflowAgent(db.WorkflowAgent{
			ID: in.AgentID, WorkflowID: wf.ID, SessionID: in.SessionID, AgentType: in.AgentType,
			Phase: string(wf.Phase), TaskIndex: wf.CurrentTask, TranscriptPath: in.TranscriptPath,
		})
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "record agent start failed")
			return
		}
		s.hub.BroadcastJSON("agent_started", map[string]any{"workflow_id": wf.ID, "agent_id": in.AgentID, "agent_type": in.AgentType})
	case "tool":
		if err := s.db.TouchWorkflowAgent(in.AgentID, in.ToolName, in.Files); err != nil {
			jsonErr(w, http.StatusInternalServerError, "record agent activity failed")
			return
		}
		entry, err := s.db.SaveWorkflowLog(wf.ID, in.SessionID, in.ToolName, in.Summary)
		if err == nil {
			s.hub.BroadcastJSON("workflow_log", entry)
		}
		s.hub.BroadcastJSON("agent_activity", map[string]any{"workflow_id": wf.ID, "agent_id": in.AgentID, "tool_name": in.ToolName})
	case "stop":
		verdict := parseReviewVerdict(in.LastAssistantMessage)
		if err := s.db.StopWorkflowAgent(in.AgentID, "done", verdict); err != nil {
			jsonErr(w, http.StatusInternalServerError, "record agent stop failed")
			return
		}
		s.recordAgentUsage(wf.ID, in)
		s.hub.BroadcastJSON("agent_status", map[string]any{
			"workflow_id": wf.ID, "agent_id": in.AgentID, "agent_type": in.AgentType,
			"status": "done", "verdict": verdict.Verdict,
		})
	}
	json200(w, map[string]any{"recorded": true, "workflow_id": wf.ID})
}

var swarmWorkerMarkerRe = regexp.MustCompile(`SWARM_WORKER_ID:\s*([A-Za-z0-9_-]+)`)

// autoCloseNote prefixes the result of a ticket Stratus closed because its agent stopped.
const autoCloseNote = "[auto-closed: agent stopped without reporting] "

// workerIDFromTranscript finds the SWARM_WORKER_ID marker the worker protocol puts in the
// agent's prompt. It only reads the head of the transcript, where the prompt is.
func workerIDFromTranscript(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 256*1024)
	n, _ := io.ReadFull(f, head)
	if m := swarmWorkerMarkerRe.FindSubmatch(head[:n]); m != nil {
		return string(m[1])
	}
	return ""
}

// closeSwarmTickets makes sure a stopped swarm worker never leaves its tickets looking
// unfinished. Delivery agents may be unable to call the swarm MCP tools, so when the agent
// stops, its tickets that are still assigned or in progress are marked done with the
// agent's final report as the result. A ticket the agent reported on itself (done, failed,
// blocked) is never touched. Best-effort and silent when the agent is not a swarm worker.
func (s *Server) closeSwarmTickets(in agentEventInput) {
	if s.swarm == nil {
		return
	}
	workerID := workerIDFromTranscript(in.AgentTranscriptPath)
	if workerID == "" {
		return
	}
	worker, err := s.db.GetWorker(workerID)
	if err != nil {
		return
	}
	tickets, err := s.db.ListTickets(worker.MissionID)
	if err != nil {
		return
	}
	report := strings.TrimSpace(in.LastAssistantMessage)
	if len(report) > 500 {
		report = report[:500]
	}
	closed := 0
	for _, t := range tickets {
		if t.WorkerID == nil || *t.WorkerID != worker.ID {
			continue
		}
		if t.Status != "assigned" && t.Status != "in_progress" {
			continue
		}
		result := autoCloseNote + report
		if err := s.swarm.UpdateTicketStatus(t.ID, "done", result); err != nil {
			log.Printf("warning: auto-close ticket %s: %v", t.ID, err)
			continue
		}
		closed++
		if updated, err := s.swarm.GetTicket(t.ID); err == nil {
			s.hub.BroadcastJSON("ticket_status", updated)
		}
	}
	if worker.Status == "active" || worker.Status == "pending" {
		if err := s.db.UpdateWorkerStatus(worker.ID, "done"); err == nil {
			s.hub.BroadcastJSON("worker_status", map[string]any{"id": worker.ID, "status": "done", "auto_closed_tickets": closed})
		}
	}
}

// recordAgentUsage stores token totals from the agent's own transcript. Best-effort: a
// missing or unreadable transcript just leaves the totals at zero.
func (s *Server) recordAgentUsage(workflowID string, in agentEventInput) {
	path := in.AgentTranscriptPath
	if path == "" {
		if a, err := s.db.GetWorkflowAgent(workflowID, in.AgentID); err == nil {
			path = subagentTranscriptPath(a.TranscriptPath, in.AgentID)
		}
	}
	if path == "" {
		return
	}
	if _, usage, err := readAgentTranscript(path, 0); err == nil {
		_ = s.db.SetWorkflowAgentUsage(in.AgentID, usage.InputTokens+usage.CacheReadTokens+usage.CacheCreationTokens, usage.OutputTokens)
	}
}

// handleAgentTranscript returns the recent steps and token usage of one agent.
func (s *Server) handleAgentTranscript(w http.ResponseWriter, r *http.Request) {
	a, err := s.db.GetWorkflowAgent(r.PathValue("id"), r.PathValue("agent"))
	if err != nil {
		jsonErr(w, http.StatusNotFound, "agent not found")
		return
	}
	if wf, err := s.coordinator.Get(a.WorkflowID); err == nil && wf.Runtime == "codex" {
		s.codexTranscript(w, r, a)
		return
	}
	path := subagentTranscriptPath(a.TranscriptPath, a.ID)
	if path == "" {
		jsonErr(w, http.StatusNotFound, "no transcript recorded for this agent")
		return
	}
	steps, usage, err := readAgentTranscript(path, 40)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "transcript not readable")
		return
	}
	json200(w, map[string]any{"steps": steps, "usage": usage})
}

// handleSkipTask marks a task skipped so it stops blocking the next phase.
func (s *Server) handleSkipTask(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid task index")
		return
	}
	state, err := s.coordinator.SkipTask(r.PathValue("id"), index)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

// handleListAllAgents lists agents of active workflows (mission control), optionally
// filtered by ?status=running|idle|done|failed and ?type=<agent type>.
func (s *Server) handleListAllAgents(w http.ResponseWriter, r *http.Request) {
	active, err := s.coordinator.ListActive()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list workflows failed")
		return
	}
	status, agentType := queryStr(r, "status"), queryStr(r, "type")
	type row struct {
		db.WorkflowAgent
		WorkflowTitle string `json:"workflow_title"`
		Idle          bool   `json:"idle"`
	}
	rows := []row{}
	for _, wf := range active {
		agents, err := s.db.ListWorkflowAgents(wf.ID)
		if err != nil {
			continue
		}
		for _, a := range runtimeAgents(agents, wf.Runtime) {
			idle := false
			if a.Status == "running" {
				if t, err := time.Parse("2006-01-02T15:04:05.000Z", a.LastActivity); err == nil && time.Since(t) > staleAgentAfter {
					idle = true
				}
			}
			switch {
			case agentType != "" && a.AgentType != agentType:
				continue
			case status == "idle" && !idle:
				continue
			case status == "running" && (a.Status != "running" || idle):
				continue
			case (status == "done" || status == "failed") && a.Status != status:
				continue
			}
			rows = append(rows, row{WorkflowAgent: a, WorkflowTitle: wf.Title, Idle: idle})
		}
	}
	json200(w, map[string]any{"agents": rows})
}

// handleResetTask reopens a task so the coordinator can retry it.
func (s *Server) handleResetTask(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid task index")
		return
	}
	state, err := s.coordinator.ResetTask(r.PathValue("id"), index)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleListWorkflowAgents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	agents, err := s.db.ListWorkflowAgents(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list agents failed")
		return
	}
	body := map[string]any{"agents": agents, "conflicts": fileConflicts(agents)}
	if wf, e := s.coordinator.Get(id); e == nil {
		body["agents"] = runtimeAgents(agents, wf.Runtime)
		if wf.Runtime == "codex" {
			body["review"] = db.CodexReview(agents, string(wf.Phase), wf.CurrentTask)
		}
	}
	json200(w, body)
}

func (s *Server) handleListWorkflowLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := s.db.GetWorkflowLogs(r.PathValue("id"), 200)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list logs failed")
		return
	}
	if logs == nil {
		logs = []db.WorkflowLog{}
	}
	json200(w, logs)
}

// handleSetWorkflowAgentStatus lets the dashboard close an agent that is stuck.
func (s *Server) handleSetWorkflowAgentStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string `json:"status"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Status != "done" && body.Status != "failed" {
		jsonErr(w, http.StatusBadRequest, "status must be one of: done, failed")
		return
	}
	id := r.PathValue("id")
	agent, found, err := s.db.FindWorkflowAgent(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "update agent failed")
		return
	}
	if err := s.db.StopWorkflowAgent(id, body.Status, db.ReviewVerdict{}); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update agent failed")
		return
	}
	payload := map[string]any{"agent_id": id, "status": body.Status}
	if found {
		payload["workflow_id"] = agent.WorkflowID
		payload["agent_type"] = agent.AgentType
	}
	s.hub.BroadcastJSON("agent_status", payload)
	json200(w, map[string]any{"id": id, "status": body.Status})
}

// fileConflicts returns files touched by more than one agent of the same workflow.
func fileConflicts(agents []db.WorkflowAgent) []string {
	owners := map[string]map[string]bool{}
	for _, a := range agents {
		for _, f := range a.Files {
			if owners[f] == nil {
				owners[f] = map[string]bool{}
			}
			owners[f][a.ID] = true
		}
	}
	out := []string{}
	for f, o := range owners {
		if len(o) > 1 {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

var (
	verdictRe    = regexp.MustCompile(`(?i)verdict:\s*(PASS|FAIL)`)
	mustFixRe    = regexp.MustCompile(`(?m)^\s*(?:[-*]\s*)?\[must_fix\]`)
	shouldFixRe  = regexp.MustCompile(`(?m)^\s*(?:[-*]\s*)?\[should_fix\]`)
	suggestionRe = regexp.MustCompile(`(?m)^\s*(?:[-*]\s*)?\[suggestion\]`)
)

// parseReviewVerdict extracts the PASS/FAIL verdict and issue counts that
// delivery-code-reviewer reports (see .claude/rules/review-verdict-format.md).
func parseReviewVerdict(msg string) db.ReviewVerdict {
	var v db.ReviewVerdict
	if m := verdictRe.FindAllStringSubmatch(msg, -1); len(m) > 0 {
		v.Verdict = strings.ToUpper(m[len(m)-1][1])
	}
	v.MustFix = len(mustFixRe.FindAllString(msg, -1))
	v.ShouldFix = len(shouldFixRe.FindAllString(msg, -1))
	v.Suggestion = len(suggestionRe.FindAllString(msg, -1))
	return v
}
