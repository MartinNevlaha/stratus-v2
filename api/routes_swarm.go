package api

import (
	"encoding/json"
	"net/http"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/swarm"
)

// --- Missions ---

func (s *Server) handleCreateMission(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkflowID string `json:"workflow_id"`
		Title      string `json:"title"`
		BaseBranch string `json:"base_branch"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.WorkflowID == "" || body.Title == "" {
		jsonErr(w, http.StatusBadRequest, "workflow_id and title are required")
		return
	}
	mission, err := s.swarm.CreateMission(body.WorkflowID, body.Title, body.BaseBranch)
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
	mission, _ := s.swarm.GetMission(id)
	s.hub.BroadcastJSON("mission_status", mission)
	json200(w, mission)
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
