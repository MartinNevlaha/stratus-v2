package swarm

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// Store provides business-logic operations over the swarm tables.
type Store struct {
	db       *db.DB
	worktree *WorktreeManager
}

// NewStore creates a swarm store.
func NewStore(database *db.DB, projectRoot string) *Store {
	return &Store{
		db:       database,
		worktree: NewWorktreeManager(projectRoot),
	}
}

// --- Mission lifecycle ---

// CreateMission creates a new mission linked to a workflow.
// The merge branch is named swarm/<missionID>/integration.
func (s *Store) CreateMission(workflowID, title, baseBranch string) (*db.SwarmMission, error) {
	id := slugify(title)
	if baseBranch == "" {
		baseBranch = "main"
	}
	mergeBranch := fmt.Sprintf("swarm/%s/integration", id)

	if err := s.db.CreateMission(id, workflowID, title, baseBranch, mergeBranch); err != nil {
		return nil, err
	}
	return s.db.GetMission(id)
}

// GetMission returns a mission by ID.
func (s *Store) GetMission(id string) (*db.SwarmMission, error) {
	return s.db.GetMission(id)
}

// ListMissions returns all missions.
func (s *Store) ListMissions() ([]db.SwarmMission, error) {
	return s.db.ListMissions()
}

// UpdateMissionStatus updates the mission status.
func (s *Store) UpdateMissionStatus(id, status string) error {
	return s.db.UpdateMissionStatus(id, status)
}

// --- Worker lifecycle ---

// SpawnWorker creates a worker with its own git worktree.
func (s *Store) SpawnWorker(missionID, agentType string) (*db.SwarmWorker, error) {
	workerID := generateID()
	branch := fmt.Sprintf("swarm/%s/%s", missionID, workerID)

	wtPath, err := s.worktree.Create(branch)
	if err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}

	if err := s.db.CreateWorker(workerID, missionID, agentType, wtPath, branch); err != nil {
		// Cleanup worktree on DB failure
		s.worktree.Remove(wtPath, branch)
		return nil, err
	}

	return s.db.GetWorker(workerID)
}

// GetWorker returns a worker by ID.
func (s *Store) GetWorker(id string) (*db.SwarmWorker, error) {
	return s.db.GetWorker(id)
}

// ListWorkers returns all workers for a mission.
func (s *Store) ListWorkers(missionID string) ([]db.SwarmWorker, error) {
	return s.db.ListWorkers(missionID)
}

// RecordHeartbeat updates a worker's heartbeat.
func (s *Store) RecordHeartbeat(workerID string) error {
	return s.db.WorkerHeartbeat(workerID)
}

// UpdateWorkerStatus updates a worker's status.
func (s *Store) UpdateWorkerStatus(id, status string) error {
	return s.db.UpdateWorkerStatus(id, status)
}

// --- Tickets ---

// CreateTicket creates a new ticket in a mission.
func (s *Store) CreateTicket(missionID, title, description, domain string, priority int, dependsOn string) (*db.SwarmTicket, error) {
	id := generateID()
	if err := s.db.CreateTicket(id, missionID, title, description, domain, priority, dependsOn); err != nil {
		return nil, err
	}
	return s.db.GetTicket(id)
}

// GetTicket returns a ticket by ID.
func (s *Store) GetTicket(id string) (*db.SwarmTicket, error) {
	return s.db.GetTicket(id)
}

// ListTickets returns all tickets for a mission.
func (s *Store) ListTickets(missionID string) ([]db.SwarmTicket, error) {
	return s.db.ListTickets(missionID)
}

// UpdateTicketStatus updates a ticket's status and optional result.
func (s *Store) UpdateTicketStatus(id, status, result string) error {
	return s.db.UpdateTicketStatus(id, status, result)
}

// Dispatch assigns dispatchable tickets to available workers by domain matching.
// Returns the list of assignments made.
func (s *Store) Dispatch(missionID string) ([]Assignment, error) {
	tickets, err := s.db.GetDispatchableTickets(missionID)
	if err != nil {
		return nil, err
	}
	workers, err := s.db.ListWorkers(missionID)
	if err != nil {
		return nil, err
	}

	// Build domain → worker list (only active/pending workers, skip stale/failed/killed)
	domainWorkers := make(map[string][]string)
	var generalWorkers []string
	for _, w := range workers {
		if w.Status == WorkerFailed || w.Status == WorkerKilled || w.Status == WorkerStale {
			continue
		}
		domain := agentTypeToDomain(w.AgentType)
		domainWorkers[domain] = append(domainWorkers[domain], w.ID)
		generalWorkers = append(generalWorkers, w.ID)
	}

	// Round-robin counters per domain for load balancing
	domainIdx := make(map[string]int)

	var assignments []Assignment
	for _, t := range tickets {
		candidates := domainWorkers[t.Domain]
		if len(candidates) == 0 {
			candidates = generalWorkers // fallback
		}
		if len(candidates) == 0 {
			continue // no workers available
		}
		// Round-robin: pick next worker for this domain
		idx := domainIdx[t.Domain] % len(candidates)
		domainIdx[t.Domain] = idx + 1
		workerID := candidates[idx]

		if err := s.db.AssignTicket(t.ID, workerID); err != nil {
			continue
		}
		assignments = append(assignments, Assignment{
			TicketID: t.ID,
			WorkerID: workerID,
		})
		// Send signal to worker (safe JSON via Marshal)
		payload, _ := json.Marshal(map[string]string{"ticket_id": t.ID, "title": t.Title})
		sigID := generateID()
		if err := s.db.CreateSignal(sigID, missionID, "hub", workerID, SignalTicketAssigned, string(payload)); err != nil {
			log.Printf("swarm dispatch: failed to send signal to %s: %v", workerID, err)
		}
	}

	return assignments, nil
}

// --- Signals ---

// SendSignal creates a new inter-agent signal.
func (s *Store) SendSignal(missionID, from, to, sigType, payload string) error {
	id := generateID()
	return s.db.CreateSignal(id, missionID, from, to, sigType, payload)
}

// PollSignals returns unread signals for a worker and marks them as read atomically.
func (s *Store) PollSignals(workerID string) ([]db.SwarmSignal, error) {
	return s.db.PollAndMarkSignals(workerID)
}

// --- Forge ---

// SubmitToForge creates a forge entry for a worker's branch.
func (s *Store) SubmitToForge(workerID string) (*db.SwarmForgeEntry, error) {
	worker, err := s.db.GetWorker(workerID)
	if err != nil {
		return nil, err
	}
	id := generateID()
	if err := s.db.CreateForgeEntry(id, worker.MissionID, workerID, worker.BranchName); err != nil {
		return nil, err
	}
	entry := &db.SwarmForgeEntry{
		ID:         id,
		MissionID:  worker.MissionID,
		WorkerID:   workerID,
		BranchName: worker.BranchName,
		Status:     ForgePending,
		CreatedAt:  worker.CreatedAt,
	}
	return entry, nil
}

// ListForgeEntries returns all forge entries for a mission.
func (s *Store) ListForgeEntries(missionID string) ([]db.SwarmForgeEntry, error) {
	return s.db.ListForgeEntries(missionID)
}

// --- Cleanup ---

// CleanupMission removes all worktrees and data for a mission.
func (s *Store) CleanupMission(missionID string) error {
	workers, err := s.db.ListWorkers(missionID)
	if err != nil {
		return err
	}
	for _, w := range workers {
		if w.WorktreePath != "" {
			s.worktree.Remove(w.WorktreePath, w.BranchName)
		}
	}
	return s.db.DeleteMission(missionID)
}

// --- helpers ---

func agentTypeToDomain(agentType string) string {
	switch {
	case strings.Contains(agentType, "backend"):
		return "backend"
	case strings.Contains(agentType, "frontend"):
		return "frontend"
	case strings.Contains(agentType, "database"):
		return "database"
	case strings.Contains(agentType, "qa"), strings.Contains(agentType, "test"):
		return "tests"
	case strings.Contains(agentType, "devops"), strings.Contains(agentType, "infra"):
		return "infra"
	case strings.Contains(agentType, "architect"):
		return "architecture"
	default:
		return "general"
	}
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			b.WriteRune('-')
		}
	}
	result := b.String()
	// Collapse multiple hyphens
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	if len(result) > 40 {
		result = result[:40]
	}
	return result
}

func generateID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	const hex = "0123456789abcdef"
	s := make([]byte, 16)
	for i, v := range b {
		s[i*2] = hex[v>>4]
		s[i*2+1] = hex[v&0x0f]
	}
	return string(s)
}
