package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/swarm"
)

// --- Missions ---

func (s *Server) handleCreateMission(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkflowID string `json:"workflow_id"`
		Title      string `json:"title"`
		BaseBranch string `json:"base_branch"`
		Strategy   string `json:"strategy"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.WorkflowID == "" || body.Title == "" {
		jsonErr(w, http.StatusBadRequest, "workflow_id and title are required")
		return
	}
	mission, err := s.swarm.CreateMission(body.WorkflowID, body.Title, body.BaseBranch, body.Strategy)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("mission_status", mission)
	json200(w, mission)
}

func (s *Server) handleListMissions(w http.ResponseWriter, r *http.Request) {
	missions, err := s.swarm.ListMissions()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if missions == nil {
		missions = []db.SwarmMission{}
	}
	json200(w, missions)
}

func (s *Server) handleGetMission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mission, err := s.swarm.GetMission(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	// Enrich with workers, tickets, and forge entries
	workers, err := s.swarm.ListWorkers(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list workers: "+err.Error())
		return
	}
	tickets, err := s.swarm.ListTickets(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list tickets: "+err.Error())
		return
	}
	forge, err := s.swarm.ListForgeEntries(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list forge: "+err.Error())
		return
	}
	if workers == nil {
		workers = []db.SwarmWorker{}
	}
	if tickets == nil {
		tickets = []db.SwarmTicket{}
	}
	if forge == nil {
		forge = []db.SwarmForgeEntry{}
	}

	json200(w, map[string]any{
		"mission": mission,
		"workers": workers,
		"tickets": tickets,
		"forge":   forge,
	})
}

func (s *Server) handleUpdateMissionStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Status string `json:"status"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if !swarm.ValidMissionStatuses[body.Status] {
		jsonErr(w, http.StatusBadRequest, "invalid mission status: "+body.Status)
		return
	}
	if err := s.swarm.UpdateMissionStatus(id, body.Status); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Run automated quality gates when transitioning to verifying
	if body.Status == swarm.MissionVerifying {
		go s.runQualityGates(id)
	}

	mission, _ := s.swarm.GetMission(id)
	s.hub.BroadcastJSON("mission_status", mission)
	json200(w, mission)
}

// runQualityGates executes Stage A automated checks (build, vet, placeholder scan)
// and records results as evidence on the mission's tickets.
func (s *Server) runQualityGates(missionID string) {
	tickets, err := s.swarm.ListTickets(missionID)
	if err != nil || len(tickets) == 0 {
		return
	}
	// Use the first ticket as the evidence anchor for mission-level gates
	ticketID := tickets[0].ID

	// Gate 1: go build
	if out, err := runGateCmd(s.projectRoot, "go", "build", "./..."); err != nil {
		s.swarm.RecordEvidence(ticketID, missionID, swarm.EvidenceGate, "go build FAILED:\n"+out, "quality-gate", "fail")
		s.hub.BroadcastJSON("gate_result", map[string]any{"mission_id": missionID, "gate": "build", "passed": false, "output": truncate(out, 500)})
	} else {
		s.swarm.RecordEvidence(ticketID, missionID, swarm.EvidenceGate, "go build: OK", "quality-gate", "pass")
		s.hub.BroadcastJSON("gate_result", map[string]any{"mission_id": missionID, "gate": "build", "passed": true})
	}

	// Gate 2: go vet
	if out, err := runGateCmd(s.projectRoot, "go", "vet", "./..."); err != nil {
		s.swarm.RecordEvidence(ticketID, missionID, swarm.EvidenceGate, "go vet FAILED:\n"+out, "quality-gate", "fail")
		s.hub.BroadcastJSON("gate_result", map[string]any{"mission_id": missionID, "gate": "vet", "passed": false, "output": truncate(out, 500)})
	} else {
		s.swarm.RecordEvidence(ticketID, missionID, swarm.EvidenceGate, "go vet: OK", "quality-gate", "pass")
		s.hub.BroadcastJSON("gate_result", map[string]any{"mission_id": missionID, "gate": "vet", "passed": true})
	}

	// Gate 3: placeholder scan (TODO/FIXME/PLACEHOLDER in recently changed lines)
	if out, _ := runGateCmd(s.projectRoot, "git", "diff", "--unified=0", "HEAD~5"); len(out) > 0 {
		placeholders := scanPlaceholders(out)
		if len(placeholders) > 0 {
			content := fmt.Sprintf("Found %d placeholder markers in recent changes:\n%s", len(placeholders), strings.Join(placeholders, "\n"))
			s.swarm.RecordEvidence(ticketID, missionID, swarm.EvidenceGate, content, "quality-gate", "fail")
			s.hub.BroadcastJSON("gate_result", map[string]any{"mission_id": missionID, "gate": "placeholder_scan", "passed": false, "count": len(placeholders)})
		} else {
			s.swarm.RecordEvidence(ticketID, missionID, swarm.EvidenceGate, "Placeholder scan: clean", "quality-gate", "pass")
			s.hub.BroadcastJSON("gate_result", map[string]any{"mission_id": missionID, "gate": "placeholder_scan", "passed": true})
		}
	}
}

func (s *Server) handleDeleteMission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.swarm.CleanupMission(id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("mission_status", map[string]any{"id": id, "status": "deleted"})
	json200(w, map[string]bool{"deleted": true})
}

// --- Workers ---

func (s *Server) handleSpawnWorker(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	var body struct {
		AgentType string `json:"agent_type"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.AgentType == "" {
		jsonErr(w, http.StatusBadRequest, "agent_type is required")
		return
	}
	worker, err := s.swarm.SpawnWorker(missionID, body.AgentType)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("worker_spawned", worker)
	json200(w, worker)
}

func (s *Server) handleListWorkers(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	workers, err := s.swarm.ListWorkers(missionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if workers == nil {
		workers = []db.SwarmWorker{}
	}
	json200(w, workers)
}

func (s *Server) handleWorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	workerID := r.PathValue("id")
	if err := s.swarm.RecordHeartbeat(workerID); err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	s.hub.BroadcastJSON("worker_heartbeat", map[string]string{"id": workerID})
	json200(w, map[string]string{"status": "ok"})
}

func (s *Server) handleUpdateWorkerStatus(w http.ResponseWriter, r *http.Request) {
	workerID := r.PathValue("id")
	var body struct {
		Status string `json:"status"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if !swarm.ValidWorkerStatuses[body.Status] {
		jsonErr(w, http.StatusBadRequest, "invalid worker status: "+body.Status)
		return
	}
	if err := s.swarm.UpdateWorkerStatus(workerID, body.Status); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	worker, _ := s.swarm.GetWorker(workerID)
	s.hub.BroadcastJSON("worker_status", worker)
	json200(w, worker)
}

// --- Tickets ---

func (s *Server) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	var body struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Domain      string   `json:"domain"`
		Priority    int      `json:"priority"`
		DependsOn   []string `json:"depends_on"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Title == "" {
		jsonErr(w, http.StatusBadRequest, "title is required")
		return
	}
	if body.Domain == "" {
		body.Domain = "general"
	}
	deps := "[]"
	if len(body.DependsOn) > 0 {
		b, _ := json.Marshal(body.DependsOn)
		deps = string(b)
	}
	ticket, err := s.swarm.CreateTicket(missionID, body.Title, body.Description, body.Domain, body.Priority, deps)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("ticket_status", ticket)
	json200(w, ticket)
}

func (s *Server) handleBatchCreateTickets(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	var body struct {
		Tickets []struct {
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Domain      string   `json:"domain"`
			Priority    int      `json:"priority"`
			DependsOn   []string `json:"depends_on"`
		} `json:"tickets"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	var created []db.SwarmTicket
	for _, t := range body.Tickets {
		domain := t.Domain
		if domain == "" {
			domain = "general"
		}
		deps := "[]"
		if len(t.DependsOn) > 0 {
			b, _ := json.Marshal(t.DependsOn)
			deps = string(b)
		}
		ticket, err := s.swarm.CreateTicket(missionID, t.Title, t.Description, domain, t.Priority, deps)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		created = append(created, *ticket)
	}

	s.hub.BroadcastJSON("tickets_created", created)
	json200(w, created)
}

func (s *Server) handleListTickets(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	tickets, err := s.swarm.ListTickets(missionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tickets == nil {
		tickets = []db.SwarmTicket{}
	}
	json200(w, tickets)
}

func (s *Server) handleUpdateTicketStatus(w http.ResponseWriter, r *http.Request) {
	ticketID := r.PathValue("id")
	var body struct {
		Status string `json:"status"`
		Result string `json:"result"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if !swarm.ValidTicketStatuses[body.Status] {
		jsonErr(w, http.StatusBadRequest, "invalid ticket status: "+body.Status)
		return
	}
	if err := s.swarm.UpdateTicketStatus(ticketID, body.Status, body.Result); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ticket, _ := s.swarm.GetTicket(ticketID)
	s.hub.BroadcastJSON("ticket_status", ticket)
	json200(w, ticket)
}

func (s *Server) handleSwarmDispatch(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	assignments, err := s.swarm.Dispatch(missionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if assignments == nil {
		assignments = []swarm.Assignment{}
	}
	json200(w, map[string]any{"assignments": assignments})
}

// --- Signals ---

func (s *Server) handleSendSignal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MissionID  string `json:"mission_id"`
		FromWorker string `json:"from_worker"`
		ToWorker   string `json:"to_worker"`
		Type       string `json:"type"`
		Payload    string `json:"payload"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Type == "" {
		jsonErr(w, http.StatusBadRequest, "type is required")
		return
	}
	if err := s.swarm.SendSignal(body.MissionID, body.FromWorker, body.ToWorker, body.Type, body.Payload); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("signal_sent", body)
	json200(w, map[string]string{"status": "sent"})
}

func (s *Server) handlePollSignals(w http.ResponseWriter, r *http.Request) {
	workerID := r.PathValue("id")
	signals, err := s.swarm.PollSignals(workerID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if signals == nil {
		signals = []db.SwarmSignal{}
	}
	json200(w, signals)
}

// --- Forge ---

func (s *Server) handleSubmitToForge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkerID string `json:"worker_id"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.WorkerID == "" {
		jsonErr(w, http.StatusBadRequest, "worker_id is required")
		return
	}
	entry, err := s.swarm.SubmitToForge(body.WorkerID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("forge_update", entry)
	json200(w, entry)
}

// handleSubmitToForgeByWorker is a convenience alias for handleSubmitToForge.
func (s *Server) handleSubmitToForgeByWorker(w http.ResponseWriter, r *http.Request) {
	s.handleSubmitToForge(w, r)
}

// handleUpdateForgeEntry updates the status of a forge entry.
func (s *Server) handleUpdateForgeEntry(w http.ResponseWriter, r *http.Request) {
	entryID := r.PathValue("id")
	var body struct {
		Status        string `json:"status"`
		ConflictFiles string `json:"conflict_files"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if !swarm.ValidForgeStatuses[body.Status] {
		jsonErr(w, http.StatusBadRequest, "invalid forge status: "+body.Status)
		return
	}
	if err := s.db.UpdateForgeEntry(entryID, body.Status, body.ConflictFiles); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("forge_update", map[string]string{"id": entryID, "status": body.Status})
	json200(w, map[string]string{"id": entryID, "status": body.Status})
}

// handleGetWorker returns a single worker by ID.
func (s *Server) handleGetWorker(w http.ResponseWriter, r *http.Request) {
	workerID := r.PathValue("id")
	worker, err := s.swarm.GetWorker(workerID)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	json200(w, worker)
}

func (s *Server) handleListForgeEntries(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	entries, err := s.swarm.ListForgeEntries(missionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entries == nil {
		entries = []db.SwarmForgeEntry{}
	}
	json200(w, entries)
}

// --- File Reservations ---

func (s *Server) handleListMissionFiles(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	reservations, err := s.db.ListFileReservations(missionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if reservations == nil {
		reservations = []db.FileReservation{}
	}
	json200(w, reservations)
}

func (s *Server) handleReserveFiles(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MissionID string   `json:"mission_id"`
		WorkerID  string   `json:"worker_id"`
		Patterns  []string `json:"patterns"`
		Reason    string   `json:"reason"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.WorkerID == "" || len(body.Patterns) == 0 {
		jsonErr(w, http.StatusBadRequest, "worker_id and patterns are required")
		return
	}
	// Auto-detect mission_id from worker if not provided
	if body.MissionID == "" {
		worker, err := s.swarm.GetWorker(body.WorkerID)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "worker not found: "+err.Error())
			return
		}
		body.MissionID = worker.MissionID
	}
	// Atomic check + reserve (prevents TOCTOU race)
	resID, conflicts, err := s.swarm.ReserveFiles(body.MissionID, body.WorkerID, body.Patterns, body.Reason)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(conflicts) > 0 {
		json200(w, map[string]any{"reserved": false, "conflicts": conflicts})
		return
	}
	s.hub.BroadcastJSON("files_reserved", map[string]any{"worker_id": body.WorkerID, "patterns": body.Patterns})
	json200(w, map[string]any{"reserved": true, "id": resID})
}

func (s *Server) handleReleaseFiles(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkerID string `json:"worker_id"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.WorkerID == "" {
		jsonErr(w, http.StatusBadRequest, "worker_id is required")
		return
	}
	if err := s.swarm.ReleaseFiles(body.WorkerID); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("files_released", map[string]any{"worker_id": body.WorkerID})
	json200(w, map[string]string{"status": "released"})
}

func (s *Server) handleCheckFileConflicts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MissionID string   `json:"mission_id"`
		WorkerID  string   `json:"worker_id"`
		Patterns  []string `json:"patterns"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.MissionID == "" || len(body.Patterns) == 0 {
		jsonErr(w, http.StatusBadRequest, "mission_id and patterns are required")
		return
	}
	conflicts, err := s.swarm.CheckFileConflicts(body.MissionID, body.WorkerID, body.Patterns)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if conflicts == nil {
		conflicts = []db.FileConflict{}
	}
	json200(w, map[string]any{"conflicts": conflicts})
}

// --- Checkpoints ---

func (s *Server) handleSaveCheckpoint(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	var body struct {
		Progress  int    `json:"progress"`
		StateJSON string `json:"state_json"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.StateJSON == "" {
		body.StateJSON = "{}"
	}
	id, err := s.swarm.SaveCheckpoint(missionID, body.Progress, body.StateJSON)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	json200(w, map[string]any{"id": id, "progress": body.Progress})
}

func (s *Server) handleGetLatestCheckpoint(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	checkpoint, err := s.swarm.GetLatestCheckpoint(missionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if checkpoint == nil {
		json200(w, map[string]any{"checkpoint": nil})
		return
	}
	json200(w, checkpoint)
}

// --- Evidence ---

func (s *Server) handleRecordEvidence(w http.ResponseWriter, r *http.Request) {
	ticketID := r.PathValue("id")
	var body struct {
		Type    string `json:"type"`
		Content string `json:"content"`
		Agent   string `json:"agent"`
		Verdict string `json:"verdict"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if !swarm.ValidEvidenceTypes[body.Type] {
		jsonErr(w, http.StatusBadRequest, "invalid evidence type: "+body.Type+". Valid: diff, test_result, review, build, note, gate")
		return
	}
	// Get ticket to find mission_id
	ticket, err := s.swarm.GetTicket(ticketID)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "ticket not found: "+err.Error())
		return
	}
	evidence, err := s.swarm.RecordEvidence(ticketID, ticket.MissionID, body.Type, body.Content, body.Agent, body.Verdict)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("evidence_recorded", evidence)
	json200(w, evidence)
}

func (s *Server) handleListTicketEvidence(w http.ResponseWriter, r *http.Request) {
	ticketID := r.PathValue("id")
	evidence, err := s.swarm.ListTicketEvidence(ticketID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if evidence == nil {
		evidence = []db.SwarmEvidence{}
	}
	json200(w, evidence)
}

func (s *Server) handleListMissionEvidence(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	evidence, err := s.swarm.ListMissionEvidence(missionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if evidence == nil {
		evidence = []db.SwarmEvidence{}
	}
	json200(w, evidence)
}

// --- Guardrails ---

func (s *Server) handleTrackToolCall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkerID  string `json:"worker_id"`
		MissionID string `json:"mission_id"`
		ToolName  string `json:"tool_name"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.WorkerID == "" || body.ToolName == "" {
		jsonErr(w, http.StatusBadRequest, "worker_id and tool_name are required")
		return
	}
	// Auto-detect mission_id from worker if not provided
	if body.MissionID == "" {
		worker, err := s.swarm.GetWorker(body.WorkerID)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "worker not found: "+err.Error())
			return
		}
		body.MissionID = worker.MissionID
	}
	guardrail, err := s.swarm.TrackToolCall(body.WorkerID, body.MissionID, body.ToolName)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Check guardrail limits and emit signals
	const toolCallCeiling = 200
	const repetitionWarn = 3
	const repetitionBlock = 5

	if guardrail.RepetitionCount >= repetitionBlock {
		s.hub.BroadcastJSON("guardrail_block", guardrail)
		json200(w, map[string]any{"guardrail": guardrail, "action": "block", "reason": "loop detected: same tool called " + string(rune('0'+guardrail.RepetitionCount)) + " times consecutively"})
		return
	}
	if guardrail.RepetitionCount >= repetitionWarn {
		s.hub.BroadcastJSON("guardrail_warn", guardrail)
	}
	if guardrail.ToolCalls >= toolCallCeiling {
		s.hub.BroadcastJSON("guardrail_block", guardrail)
		json200(w, map[string]any{"guardrail": guardrail, "action": "block", "reason": "tool call ceiling exceeded"})
		return
	}

	json200(w, map[string]any{"guardrail": guardrail, "action": "allow"})
}

func (s *Server) handleGetGuardrail(w http.ResponseWriter, r *http.Request) {
	workerID := r.PathValue("id")
	guardrail, err := s.swarm.GetGuardrail(workerID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if guardrail == nil {
		json200(w, map[string]any{"guardrail": nil})
		return
	}
	json200(w, guardrail)
}

// --- Plan Drift Detection ---

func (s *Server) handleCheckDrift(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	var body struct {
		ChangedFiles []string `json:"changed_files"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	// If no files provided, try to get from git diff
	if len(body.ChangedFiles) == 0 {
		out, err := runGateCmd(s.projectRoot, "git", "diff", "--name-only", "HEAD~5")
		if err == nil && len(out) > 0 {
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					body.ChangedFiles = append(body.ChangedFiles, line)
				}
			}
		}
	}
	drifts, err := s.swarm.DetectDrift(missionID, body.ChangedFiles)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(drifts) > 0 {
		s.hub.BroadcastJSON("plan_drift", map[string]any{"mission_id": missionID, "drifts": drifts})
	}
	json200(w, map[string]any{"drifts": drifts, "drift_detected": len(drifts) > 0})
}

// --- Strategy outcome ---

func (s *Server) handleUpdateStrategyOutcome(w http.ResponseWriter, r *http.Request) {
	missionID := r.PathValue("id")
	var body struct {
		StrategyOutcome string `json:"strategy_outcome"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.StrategyOutcome == "" {
		jsonErr(w, http.StatusBadRequest, "strategy_outcome is required")
		return
	}
	if err := s.swarm.UpdateStrategyOutcome(missionID, body.StrategyOutcome); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	json200(w, map[string]string{"status": "updated"})
}

// --- Quality gate helpers ---

// runGateCmd executes a command and returns its combined output.
func runGateCmd(dir string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// scanPlaceholders finds TODO/FIXME/PLACEHOLDER markers in diff output.
func scanPlaceholders(diff string) []string {
	var found []string
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "TODO") || strings.Contains(upper, "FIXME") || strings.Contains(upper, "PLACEHOLDER") || strings.Contains(upper, "XXX") {
			if len(line) > 120 {
				line = line[:120] + "..."
			}
			found = append(found, line)
		}
	}
	return found
}

