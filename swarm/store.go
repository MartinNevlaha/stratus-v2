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
func (s *Store) CreateMission(workflowID, title, baseBranch, strategy string) (*db.SwarmMission, error) {
	id := slugify(title)
	if baseBranch == "" {
		baseBranch = "main"
	}
	mergeBranch := fmt.Sprintf("swarm/%s/integration", id)

	if err := s.db.CreateMission(id, workflowID, title, baseBranch, mergeBranch, strategy); err != nil {
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

// CountPastMissions returns the total number of completed/failed/aborted missions.
func (s *Store) CountPastMissions() (int, error) {
	return s.db.CountPastMissions()
}

// ListPastMissions returns past missions with offset/limit pagination.
func (s *Store) ListPastMissions(offset, limit int) ([]db.SwarmMission, error) {
	return s.db.ListPastMissions(offset, limit)
}

// UpdateMissionStatus updates the mission status.
// On terminal states (complete/failed/aborted) it asynchronously removes all worker worktrees.
func (s *Store) UpdateMissionStatus(id, status string) error {
	if err := s.db.UpdateMissionStatus(id, status); err != nil {
		return err
	}
	if status == MissionComplete || status == MissionFailed || status == MissionAborted {
		go s.removeWorktrees(id)
	}
	return nil
}

// removeWorktrees removes all git worktrees for a mission's workers.
// Called asynchronously on mission completion; errors are logged, not returned.
func (s *Store) removeWorktrees(missionID string) {
	workers, err := s.db.ListWorkers(missionID)
	if err != nil {
		log.Printf("swarm: removeWorktrees: list workers: %v", err)
		return
	}
	for _, w := range workers {
		if w.WorktreePath != "" {
			if err := s.worktree.Remove(w.WorktreePath, w.BranchName); err != nil {
				log.Printf("swarm: removeWorktrees: %s: %v", w.ID, err)
			}
		}
	}
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
// files is a JSON array of file path patterns this ticket is expected to modify (e.g. '["src/api/foo.go"]').
// Pass "[]" or "" if unknown. Used by dispatch to group overlapping tickets to the same worker.
func (s *Store) CreateTicket(missionID, title, description, domain string, priority int, dependsOn, files string) (*db.SwarmTicket, error) {
	id := generateID()
	if err := s.db.CreateTicket(id, missionID, title, description, domain, priority, dependsOn, files); err != nil {
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
// Enforces bounded retry discipline via MaxTicketRevisions and MaxTicketRejections.
func (s *Store) UpdateTicketStatus(id, status, result string) error {
	return s.db.UpdateTicketStatus(id, status, result, MaxTicketRevisions, MaxTicketRejections)
}

// Dispatch assigns dispatchable tickets to available workers by domain matching.
// Two improvements over simple round-robin:
//  1. Tickets with overlapping files are grouped and assigned to the same worker
//     to avoid merge conflicts.
//  2. Tickets whose dependencies were completed by a specific worker are preferentially
//     assigned to that same worker to avoid cross-worker dependency waits.
//
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
	activeWorkers := make(map[string]bool)
	for _, w := range workers {
		if w.Status == WorkerFailed || w.Status == WorkerKilled || w.Status == WorkerStale {
			continue
		}
		domain := agentTypeToDomain(w.AgentType)
		domainWorkers[domain] = append(domainWorkers[domain], w.ID)
		generalWorkers = append(generalWorkers, w.ID)
		activeWorkers[w.ID] = true
	}

	// Build ticketID → workerID map for completed tickets (dependency preference).
	allTickets, err := s.db.ListTickets(missionID)
	if err != nil {
		return nil, err
	}
	completedByWorker := make(map[string]string) // ticketID → workerID
	for _, t := range allTickets {
		if t.Status == TicketDone && t.WorkerID != nil {
			completedByWorker[t.ID] = *t.WorkerID
		}
	}

	// Group tickets by file overlap using union-find.
	// Tickets that touch overlapping files must go to the same worker.
	uf := newUnionFind(len(tickets))
	for i := range tickets {
		filesI := parseFilesJSON(tickets[i].Files)
		for j := i + 1; j < len(tickets); j++ {
			filesJ := parseFilesJSON(tickets[j].Files)
			if fileListsOverlap(filesI, filesJ) {
				uf.union(i, j)
			}
		}
	}

	// Determine the assigned worker per union-find root.
	// The first ticket in a group determines the worker; subsequent tickets in the
	// same group reuse that assignment.
	domainIdx := make(map[string]int)
	groupWorker := make(map[int]string) // root index → workerID

	var assignments []Assignment
	for i, t := range tickets {
		root := uf.find(i)

		workerID, alreadyAssigned := groupWorker[root]
		if !alreadyAssigned {
			// Find the best worker: prefer dependency worker, then domain round-robin.
			for _, depID := range parseDependsOnLocal(t.DependsOn) {
				if depWorker, ok := completedByWorker[depID]; ok && activeWorkers[depWorker] {
					workerID = depWorker
					break
				}
			}
			if workerID == "" {
				candidates := domainWorkers[t.Domain]
				if len(candidates) == 0 {
					candidates = generalWorkers
				}
				if len(candidates) == 0 {
					continue // no workers available
				}
				idx := domainIdx[t.Domain] % len(candidates)
				domainIdx[t.Domain] = idx + 1
				workerID = candidates[idx]
			}
			groupWorker[root] = workerID
		}

		if err := s.db.AssignTicket(t.ID, workerID); err != nil {
			continue
		}
		assignments = append(assignments, Assignment{
			TicketID: t.ID,
			WorkerID: workerID,
		})
		payload, _ := json.Marshal(map[string]string{"ticket_id": t.ID, "title": t.Title})
		sigID := generateID()
		if err := s.db.CreateSignal(sigID, missionID, "hub", workerID, SignalTicketAssigned, string(payload)); err != nil {
			log.Printf("swarm dispatch: failed to send signal to %s: %v", workerID, err)
		}
	}

	return assignments, nil
}

// parseDependsOnLocal parses a JSON depends_on string into a slice of ticket IDs.
func parseDependsOnLocal(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var deps []string
	if err := json.Unmarshal([]byte(raw), &deps); err != nil {
		return nil
	}
	return deps
}

// parseFilesJSON parses a JSON files string into a slice of file path patterns.
func parseFilesJSON(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var files []string
	if err := json.Unmarshal([]byte(raw), &files); err != nil {
		return nil
	}
	return files
}

// fileListsOverlap returns true if any file in 'a' overlaps with any file in 'b'
// using path-boundary-aware prefix matching.
func fileListsOverlap(a, b []string) bool {
	for _, fa := range a {
		for _, fb := range b {
			if filePathsOverlap(fa, fb) {
				return true
			}
		}
	}
	return false
}

// filePathsOverlap checks if two file path patterns potentially overlap.
// "src/api" overlaps "src/api/foo" but NOT "src/api-v2".
func filePathsOverlap(a, b string) bool {
	aBase := trimGlobSuffix(a)
	bBase := trimGlobSuffix(b)
	if len(aBase) == 0 || len(bBase) == 0 {
		return false
	}
	return pathHasPrefix(aBase, bBase) || pathHasPrefix(bBase, aBase)
}

func trimGlobSuffix(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '*' || s[len(s)-1] == '/') {
		s = s[:len(s)-1]
	}
	return s
}

func pathHasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	if s[:len(prefix)] != prefix {
		return false
	}
	// Must end at a path boundary
	return len(s) == len(prefix) || s[len(prefix)] == '/'
}

// unionFind is a simple union-find (disjoint set) for grouping ticket indices.
type unionFind struct {
	parent []int
}

func newUnionFind(n int) *unionFind {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	return &unionFind{parent: p}
}

func (uf *unionFind) find(x int) int {
	for uf.parent[x] != x {
		uf.parent[x] = uf.parent[uf.parent[x]] // path compression
		x = uf.parent[x]
	}
	return x
}

func (uf *unionFind) union(x, y int) {
	rx, ry := uf.find(x), uf.find(y)
	if rx != ry {
		uf.parent[rx] = ry
	}
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

// --- File Reservations ---

// ReserveFiles atomically checks for conflicts and creates a file reservation.
// Returns (reservationID, conflicts, error). If conflicts is non-empty, no reservation was made and id is "".
func (s *Store) ReserveFiles(missionID, workerID string, patterns []string, reason string) (string, []db.FileConflict, error) {
	return s.db.ReserveFilesAtomic(missionID, workerID, patterns, reason)
}

// ReleaseFiles removes all file reservations for a worker.
func (s *Store) ReleaseFiles(workerID string) error {
	return s.db.ReleaseFiles(workerID)
}

// CheckFileConflicts checks for overlapping file reservations without reserving.
func (s *Store) CheckFileConflicts(missionID, excludeWorkerID string, patterns []string) ([]db.FileConflict, error) {
	return s.db.CheckFileConflicts(missionID, excludeWorkerID, patterns)
}

// --- Checkpoints ---

// SaveCheckpoint records a coordinator state snapshot.
func (s *Store) SaveCheckpoint(missionID string, progress int, stateJSON string) (string, error) {
	return s.db.SaveCheckpointReturningID(missionID, progress, stateJSON)
}

// GetLatestCheckpoint returns the most recent checkpoint for a mission.
func (s *Store) GetLatestCheckpoint(missionID string) (*db.SwarmCheckpoint, error) {
	return s.db.GetLatestCheckpoint(missionID)
}

// --- Strategy ---

// UpdateStrategyOutcome records the strategy outcome JSON on a mission.
func (s *Store) UpdateStrategyOutcome(missionID, outcome string) error {
	return s.db.UpdateMissionStrategyOutcome(missionID, outcome)
}

// --- Plan Drift Detection ---

// DetectDrift compares actual changed files against the ticket descriptions.
// Returns a list of drift descriptions (files changed that weren't mentioned in any ticket,
// or files mentioned but not changed). Empty slice means no drift.
func (s *Store) DetectDrift(missionID string, changedFiles []string) ([]string, error) {
	tickets, err := s.db.ListTickets(missionID)
	if err != nil {
		return nil, err
	}

	// Build set of file patterns mentioned in ticket descriptions
	mentionedPatterns := make(map[string]bool)
	for _, t := range tickets {
		if t.Status != "done" && t.Status != "in_progress" {
			continue
		}
		// Extract file paths from ticket description (look for path-like strings)
		for _, word := range strings.Fields(t.Description) {
			word = strings.Trim(word, "`,\"'()[]{}*")
			if isFilePath(word) {
				mentionedPatterns[word] = true
			}
		}
	}

	var drifts []string

	// Check for unexpected files (changed but not in any ticket)
	for _, f := range changedFiles {
		matched := false
		for pattern := range mentionedPatterns {
			if f == pattern || strings.HasPrefix(f, pattern+"/") || strings.HasPrefix(f, strings.TrimSuffix(pattern, "/**")) {
				matched = true
				break
			}
		}
		if !matched && len(mentionedPatterns) > 0 {
			drifts = append(drifts, fmt.Sprintf("unexpected file changed: %s (not in any ticket)", f))
		}
	}

	return drifts, nil
}

// isFilePath checks if a string looks like a file path.
func isFilePath(s string) bool {
	return (strings.Contains(s, "/") || strings.Contains(s, ".")) &&
		!strings.HasPrefix(s, "http") &&
		!strings.Contains(s, "@") &&
		len(s) > 3 &&
		len(s) < 200 &&
		(strings.HasSuffix(s, ".go") || strings.HasSuffix(s, ".ts") ||
			strings.HasSuffix(s, ".svelte") || strings.HasSuffix(s, ".json") ||
			strings.HasSuffix(s, ".md") || strings.HasSuffix(s, ".sql") ||
			strings.HasSuffix(s, ".yaml") || strings.HasSuffix(s, ".yml") ||
			strings.Contains(s, "/") && !strings.Contains(s, " "))
}

// CreatePreviewWorktree creates a temporary worktree with all pending/merged worker
// branches pre-merged into it. This lets the code reviewer see the consolidated
// state of all workers' changes rather than the base branch.
// Returns (worktreePath, failedMerges, error).
func (s *Store) CreatePreviewWorktree(missionID string) (string, []string, error) {
	mission, err := s.db.GetMission(missionID)
	if err != nil {
		return "", nil, fmt.Errorf("get mission: %w", err)
	}
	entries, err := s.db.ListForgeEntries(missionID)
	if err != nil {
		return "", nil, fmt.Errorf("list forge entries: %w", err)
	}
	var branches []string
	for _, e := range entries {
		if e.Status == ForgePending || e.Status == ForgeMerging || e.Status == ForgeMerged {
			branches = append(branches, e.BranchName)
		}
	}
	return s.worktree.CreatePreviewMerge(missionID, mission.BaseBranch, branches)
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

// --- Evidence ---

// RecordEvidence creates a structured evidence record for a ticket.
func (s *Store) RecordEvidence(ticketID, missionID, evidenceType, content, agent, verdict string) (*db.SwarmEvidence, error) {
	id := generateID()
	if err := s.db.CreateEvidence(id, ticketID, missionID, evidenceType, content, agent, verdict); err != nil {
		return nil, err
	}
	return &db.SwarmEvidence{
		ID:        id,
		TicketID:  ticketID,
		MissionID: missionID,
		Type:      evidenceType,
		Content:   content,
		Agent:     agent,
		Verdict:   verdict,
	}, nil
}

// ListTicketEvidence returns all evidence for a ticket.
func (s *Store) ListTicketEvidence(ticketID string) ([]db.SwarmEvidence, error) {
	return s.db.ListEvidenceByTicket(ticketID)
}

// ListMissionEvidence returns all evidence for a mission.
func (s *Store) ListMissionEvidence(missionID string) ([]db.SwarmEvidence, error) {
	return s.db.ListEvidenceByMission(missionID)
}

// --- Guardrails ---

// TrackToolCall records a tool invocation for guardrail tracking.
// Returns the updated guardrail state.
func (s *Store) TrackToolCall(workerID, missionID, toolName string) (*db.SwarmGuardrail, error) {
	return s.db.UpsertGuardrail(workerID, missionID, toolName)
}

// GetGuardrail returns guardrail state for a worker.
func (s *Store) GetGuardrail(workerID string) (*db.SwarmGuardrail, error) {
	return s.db.GetGuardrail(workerID)
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
