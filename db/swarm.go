package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SwarmMission represents a group of coordinated tickets.
type SwarmMission struct {
	ID              string `json:"id"`
	WorkflowID      string `json:"workflow_id"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	BaseBranch      string `json:"base_branch"`
	MergeBranch     string `json:"merge_branch"`
	Strategy        string `json:"strategy"`
	StrategyOutcome string `json:"strategy_outcome"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// FileReservation represents a file lock held by a worker.
type FileReservation struct {
	ID        string `json:"id"`
	MissionID string `json:"mission_id"`
	WorkerID  string `json:"worker_id"`
	Patterns  string `json:"patterns"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

// FileConflict describes an overlap between a reservation and requested patterns.
type FileConflict struct {
	WorkerID string `json:"worker_id"`
	Pattern  string `json:"pattern"`
	Reason   string `json:"reason"`
}

// SwarmCheckpoint stores coordinator state at a progress milestone.
type SwarmCheckpoint struct {
	ID        string `json:"id"`
	MissionID string `json:"mission_id"`
	Progress  int    `json:"progress"`
	StateJSON string `json:"state_json"`
	CreatedAt string `json:"created_at"`
}

// SwarmWorker represents an agent process with its own git worktree.
type SwarmWorker struct {
	ID            string  `json:"id"`
	MissionID     string  `json:"mission_id"`
	AgentType     string  `json:"agent_type"`
	WorktreePath  string  `json:"worktree_path"`
	BranchName    string  `json:"branch_name"`
	Status        string  `json:"status"`
	SessionID     *string `json:"session_id,omitempty"`
	LastHeartbeat string  `json:"last_heartbeat"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// SwarmTicket represents an atomic work unit within a mission.
type SwarmTicket struct {
	ID          string  `json:"id"`
	MissionID   string  `json:"mission_id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Domain      string  `json:"domain"`
	Priority    int     `json:"priority"`
	Status      string  `json:"status"`
	WorkerID    *string `json:"worker_id,omitempty"`
	DependsOn   string  `json:"depends_on"`
	Result      string  `json:"result"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// SwarmSignal represents an inter-agent message.
type SwarmSignal struct {
	ID         string `json:"id"`
	MissionID  string `json:"mission_id"`
	FromWorker string `json:"from_worker"`
	ToWorker   string `json:"to_worker"`
	Type       string `json:"type"`
	Payload    string `json:"payload"`
	Read       bool   `json:"read"`
	CreatedAt  string `json:"created_at"`
}

// SwarmForgeEntry represents a merge queue item.
type SwarmForgeEntry struct {
	ID            string  `json:"id"`
	MissionID     string  `json:"mission_id"`
	WorkerID      string  `json:"worker_id"`
	BranchName    string  `json:"branch_name"`
	Status        string  `json:"status"`
	ConflictFiles string  `json:"conflict_files"`
	MergedAt      *string `json:"merged_at,omitempty"`
	CreatedAt     string  `json:"created_at"`
}

// --- Missions ---

func (d *DB) CreateMission(id, workflowID, title, baseBranch, mergeBranch, strategy string) error {
	_, err := d.sql.Exec(`
		INSERT INTO missions (id, workflow_id, title, base_branch, merge_branch, strategy)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, workflowID, title, baseBranch, mergeBranch, strategy,
	)
	if err != nil {
		return fmt.Errorf("insert mission: %w", err)
	}
	return nil
}

func (d *DB) GetMission(id string) (*SwarmMission, error) {
	var m SwarmMission
	err := d.sql.QueryRow(`
		SELECT id, workflow_id, title, status, base_branch, merge_branch, strategy, strategy_outcome, created_at, updated_at
		FROM missions WHERE id = ?`, id).
		Scan(&m.ID, &m.WorkflowID, &m.Title, &m.Status, &m.BaseBranch, &m.MergeBranch, &m.Strategy, &m.StrategyOutcome, &m.CreatedAt, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mission not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get mission: %w", err)
	}
	return &m, nil
}

func (d *DB) UpdateMissionStatus(id, status string) error {
	res, err := d.sql.Exec(`
		UPDATE missions SET status = ?, updated_at = ? WHERE id = ?`,
		status, now(), id,
	)
	if err != nil {
		return fmt.Errorf("update mission status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mission not found: %s", id)
	}
	return nil
}

func (d *DB) ListMissions() ([]SwarmMission, error) {
	rows, err := d.sql.Query(`
		SELECT id, workflow_id, title, status, base_branch, merge_branch, strategy, strategy_outcome, created_at, updated_at
		FROM missions ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list missions: %w", err)
	}
	defer rows.Close()
	var missions []SwarmMission
	for rows.Next() {
		var m SwarmMission
		if err := rows.Scan(&m.ID, &m.WorkflowID, &m.Title, &m.Status, &m.BaseBranch, &m.MergeBranch, &m.Strategy, &m.StrategyOutcome, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		missions = append(missions, m)
	}
	return missions, rows.Err()
}

func (d *DB) DeleteMission(id string) error {
	res, err := d.sql.Exec(`DELETE FROM missions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete mission: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mission not found: %s", id)
	}
	return nil
}

// --- Workers ---

func (d *DB) CreateWorker(id, missionID, agentType, worktreePath, branchName string) error {
	_, err := d.sql.Exec(`
		INSERT INTO workers (id, mission_id, agent_type, worktree_path, branch_name)
		VALUES (?, ?, ?, ?, ?)`,
		id, missionID, agentType, worktreePath, branchName,
	)
	if err != nil {
		return fmt.Errorf("insert worker: %w", err)
	}
	return nil
}

func (d *DB) GetWorker(id string) (*SwarmWorker, error) {
	var w SwarmWorker
	var sessionID sql.NullString
	err := d.sql.QueryRow(`
		SELECT id, mission_id, agent_type, worktree_path, branch_name,
		       status, session_id, last_heartbeat, created_at, updated_at
		FROM workers WHERE id = ?`, id).
		Scan(&w.ID, &w.MissionID, &w.AgentType, &w.WorktreePath, &w.BranchName,
			&w.Status, &sessionID, &w.LastHeartbeat, &w.CreatedAt, &w.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("worker not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get worker: %w", err)
	}
	if sessionID.Valid {
		w.SessionID = &sessionID.String
	}
	return &w, nil
}

func (d *DB) ListWorkers(missionID string) ([]SwarmWorker, error) {
	rows, err := d.sql.Query(`
		SELECT id, mission_id, agent_type, worktree_path, branch_name,
		       status, session_id, last_heartbeat, created_at, updated_at
		FROM workers WHERE mission_id = ? ORDER BY created_at ASC`, missionID)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	defer rows.Close()
	return scanWorkers(rows)
}

func (d *DB) ListWorkersByStatus(missionID, status string) ([]SwarmWorker, error) {
	rows, err := d.sql.Query(`
		SELECT id, mission_id, agent_type, worktree_path, branch_name,
		       status, session_id, last_heartbeat, created_at, updated_at
		FROM workers WHERE mission_id = ? AND status = ? ORDER BY created_at ASC`, missionID, status)
	if err != nil {
		return nil, fmt.Errorf("list workers by status: %w", err)
	}
	defer rows.Close()
	return scanWorkers(rows)
}

func (d *DB) WorkerHeartbeat(id string) error {
	// Only update heartbeat if worker is in pending or active status.
	// Do not resurrect failed/done/killed workers.
	res, err := d.sql.Exec(`
		UPDATE workers SET last_heartbeat = ?, status = 'active', updated_at = ?
		WHERE id = ? AND status IN ('pending', 'active', 'stale')`,
		now(), now(), id,
	)
	if err != nil {
		return fmt.Errorf("worker heartbeat: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Check if worker exists but is in a terminal state
		var status string
		err := d.sql.QueryRow(`SELECT status FROM workers WHERE id = ?`, id).Scan(&status)
		if err == sql.ErrNoRows {
			return fmt.Errorf("worker not found: %s", id)
		}
		if err != nil {
			return fmt.Errorf("worker heartbeat: %w", err)
		}
		return fmt.Errorf("worker %s is in terminal state: %s", id, status)
	}
	return nil
}

func (d *DB) UpdateWorkerStatus(id, status string) error {
	res, err := d.sql.Exec(`
		UPDATE workers SET status = ?, updated_at = ? WHERE id = ?`,
		status, now(), id,
	)
	if err != nil {
		return fmt.Errorf("update worker status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("worker not found: %s", id)
	}
	return nil
}

func scanWorkers(rows *sql.Rows) ([]SwarmWorker, error) {
	var workers []SwarmWorker
	for rows.Next() {
		var w SwarmWorker
		var sessionID sql.NullString
		if err := rows.Scan(&w.ID, &w.MissionID, &w.AgentType, &w.WorktreePath, &w.BranchName,
			&w.Status, &sessionID, &w.LastHeartbeat, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		if sessionID.Valid {
			w.SessionID = &sessionID.String
		}
		workers = append(workers, w)
	}
	return workers, rows.Err()
}

// --- Tickets ---

func (d *DB) CreateTicket(id, missionID, title, description, domain string, priority int, dependsOn string) error {
	if dependsOn == "" {
		dependsOn = "[]"
	}
	_, err := d.sql.Exec(`
		INSERT INTO tickets (id, mission_id, title, description, domain, priority, depends_on)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, missionID, title, description, domain, priority, dependsOn,
	)
	if err != nil {
		return fmt.Errorf("insert ticket: %w", err)
	}
	return nil
}

func (d *DB) GetTicket(id string) (*SwarmTicket, error) {
	var t SwarmTicket
	var workerID sql.NullString
	err := d.sql.QueryRow(`
		SELECT id, mission_id, title, description, domain, priority,
		       status, worker_id, depends_on, result, created_at, updated_at
		FROM tickets WHERE id = ?`, id).
		Scan(&t.ID, &t.MissionID, &t.Title, &t.Description, &t.Domain, &t.Priority,
			&t.Status, &workerID, &t.DependsOn, &t.Result, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("ticket not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get ticket: %w", err)
	}
	if workerID.Valid {
		t.WorkerID = &workerID.String
	}
	return &t, nil
}

func (d *DB) ListTickets(missionID string) ([]SwarmTicket, error) {
	rows, err := d.sql.Query(`
		SELECT id, mission_id, title, description, domain, priority,
		       status, worker_id, depends_on, result, created_at, updated_at
		FROM tickets WHERE mission_id = ? ORDER BY priority ASC, created_at ASC`, missionID)
	if err != nil {
		return nil, fmt.Errorf("list tickets: %w", err)
	}
	defer rows.Close()
	return scanTickets(rows)
}

func (d *DB) AssignTicket(ticketID, workerID string) error {
	res, err := d.sql.Exec(`
		UPDATE tickets SET worker_id = ?, status = 'assigned', updated_at = ? WHERE id = ?`,
		workerID, now(), ticketID,
	)
	if err != nil {
		return fmt.Errorf("assign ticket: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("ticket not found: %s", ticketID)
	}
	return nil
}

func (d *DB) UpdateTicketStatus(id, status, result string) error {
	res, err := d.sql.Exec(`
		UPDATE tickets SET status = ?, result = ?, updated_at = ? WHERE id = ?`,
		status, result, now(), id,
	)
	if err != nil {
		return fmt.Errorf("update ticket status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("ticket not found: %s", id)
	}
	return nil
}

// GetDispatchableTickets returns pending tickets whose dependencies are all done.
func (d *DB) GetDispatchableTickets(missionID string) ([]SwarmTicket, error) {
	// Get all tickets for the mission, then filter in Go for dependency resolution.
	// This is simpler than a complex SQL query with JSON parsing.
	all, err := d.ListTickets(missionID)
	if err != nil {
		return nil, err
	}

	// Build done set
	doneSet := make(map[string]bool)
	for _, t := range all {
		if t.Status == "done" {
			doneSet[t.ID] = true
		}
	}

	var dispatchable []SwarmTicket
	for _, t := range all {
		if t.Status != "pending" {
			continue
		}
		// Check all dependencies are done
		deps := parseDependsOn(t.DependsOn)
		allDone := true
		for _, dep := range deps {
			if !doneSet[dep] {
				allDone = false
				break
			}
		}
		if allDone {
			dispatchable = append(dispatchable, t)
		}
	}
	return dispatchable, nil
}

func scanTickets(rows *sql.Rows) ([]SwarmTicket, error) {
	var tickets []SwarmTicket
	for rows.Next() {
		var t SwarmTicket
		var workerID sql.NullString
		if err := rows.Scan(&t.ID, &t.MissionID, &t.Title, &t.Description, &t.Domain, &t.Priority,
			&t.Status, &workerID, &t.DependsOn, &t.Result, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		if workerID.Valid {
			t.WorkerID = &workerID.String
		}
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}

func parseDependsOn(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var deps []string
	if err := json.Unmarshal([]byte(raw), &deps); err != nil {
		return nil
	}
	return deps
}

// --- Signals ---

func (d *DB) CreateSignal(id, missionID, fromWorker, toWorker, sigType, payload string) error {
	if toWorker == "" {
		toWorker = "*"
	}
	if payload == "" {
		payload = "{}"
	}
	_, err := d.sql.Exec(`
		INSERT INTO signals (id, mission_id, from_worker, to_worker, type, payload)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, missionID, fromWorker, toWorker, sigType, payload,
	)
	if err != nil {
		return fmt.Errorf("insert signal: %w", err)
	}
	return nil
}

// GetUnreadSignals returns unread signals for a worker (direct or broadcast).
func (d *DB) GetUnreadSignals(workerID string) ([]SwarmSignal, error) {
	rows, err := d.sql.Query(`
		SELECT id, mission_id, from_worker, to_worker, type, payload, read, created_at
		FROM signals
		WHERE (to_worker = ? OR to_worker = '*') AND read = 0
		ORDER BY created_at ASC`, workerID)
	if err != nil {
		return nil, fmt.Errorf("get unread signals: %w", err)
	}
	defer rows.Close()
	return scanSignals(rows)
}

func (d *DB) MarkSignalRead(id string) error {
	_, err := d.sql.Exec(`UPDATE signals SET read = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mark signal read: %w", err)
	}
	return nil
}

// MarkSignalsRead marks all unread signals for a worker as read.
func (d *DB) MarkSignalsRead(workerID string) error {
	_, err := d.sql.Exec(`UPDATE signals SET read = 1 WHERE (to_worker = ? OR to_worker = '*') AND read = 0`, workerID)
	if err != nil {
		return fmt.Errorf("mark signals read: %w", err)
	}
	return nil
}

// PollAndMarkSignals atomically fetches unread signals and marks them as read.
func (d *DB) PollAndMarkSignals(workerID string) ([]SwarmSignal, error) {
	tx, err := d.sql.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT id, mission_id, from_worker, to_worker, type, payload, read, created_at
		FROM signals
		WHERE (to_worker = ? OR to_worker = '*') AND read = 0
		ORDER BY created_at ASC`, workerID)
	if err != nil {
		return nil, fmt.Errorf("poll signals: %w", err)
	}
	signals, err := scanSignals(rows)
	if err != nil {
		return nil, err
	}

	if len(signals) > 0 {
		_, err = tx.Exec(`UPDATE signals SET read = 1 WHERE (to_worker = ? OR to_worker = '*') AND read = 0`, workerID)
		if err != nil {
			return nil, fmt.Errorf("mark signals read: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return signals, nil
}

func scanSignals(rows *sql.Rows) ([]SwarmSignal, error) {
	var signals []SwarmSignal
	for rows.Next() {
		var s SwarmSignal
		var readInt int
		if err := rows.Scan(&s.ID, &s.MissionID, &s.FromWorker, &s.ToWorker, &s.Type, &s.Payload, &readInt, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.Read = readInt != 0
		signals = append(signals, s)
	}
	return signals, rows.Err()
}

// --- Forge entries ---

func (d *DB) CreateForgeEntry(id, missionID, workerID, branchName string) error {
	_, err := d.sql.Exec(`
		INSERT INTO forge_entries (id, mission_id, worker_id, branch_name)
		VALUES (?, ?, ?, ?)`,
		id, missionID, workerID, branchName,
	)
	if err != nil {
		return fmt.Errorf("insert forge entry: %w", err)
	}
	return nil
}

func (d *DB) UpdateForgeEntry(id, status, conflictFiles string) error {
	var mergedAt *string
	if status == "merged" {
		t := now()
		mergedAt = &t
	}
	_, err := d.sql.Exec(`
		UPDATE forge_entries SET status = ?, conflict_files = ?, merged_at = ? WHERE id = ?`,
		status, conflictFiles, mergedAt, id,
	)
	if err != nil {
		return fmt.Errorf("update forge entry: %w", err)
	}
	return nil
}

func (d *DB) ListForgeEntries(missionID string) ([]SwarmForgeEntry, error) {
	rows, err := d.sql.Query(`
		SELECT id, mission_id, worker_id, branch_name, status, conflict_files, merged_at, created_at
		FROM forge_entries WHERE mission_id = ? ORDER BY created_at ASC`, missionID)
	if err != nil {
		return nil, fmt.Errorf("list forge entries: %w", err)
	}
	defer rows.Close()
	var entries []SwarmForgeEntry
	for rows.Next() {
		var e SwarmForgeEntry
		var mergedAt sql.NullString
		if err := rows.Scan(&e.ID, &e.MissionID, &e.WorkerID, &e.BranchName, &e.Status, &e.ConflictFiles, &mergedAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		if mergedAt.Valid {
			e.MergedAt = &mergedAt.String
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// --- File Reservations ---

// ReserveFilesAtomic checks for conflicts and inserts the reservation in a single transaction.
// Returns (id, conflicts, error). If conflicts is non-empty, no reservation was made and id is "".
func (d *DB) ReserveFilesAtomic(missionID, workerID string, patterns []string, reason string) (string, []FileConflict, error) {
	tx, err := d.sql.Begin()
	if err != nil {
		return "", nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Check existing reservations within the transaction
	rows, err := tx.Query(`
		SELECT id, mission_id, worker_id, patterns, reason, created_at
		FROM file_reservations WHERE mission_id = ?`, missionID)
	if err != nil {
		return "", nil, fmt.Errorf("list reservations: %w", err)
	}
	var existing []FileReservation
	for rows.Next() {
		var r FileReservation
		if err := rows.Scan(&r.ID, &r.MissionID, &r.WorkerID, &r.Patterns, &r.Reason, &r.CreatedAt); err != nil {
			rows.Close()
			return "", nil, err
		}
		existing = append(existing, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", nil, err
	}

	// Check for conflicts
	var conflicts []FileConflict
	for _, res := range existing {
		if res.WorkerID == workerID {
			continue
		}
		var resPatterns []string
		if err := json.Unmarshal([]byte(res.Patterns), &resPatterns); err != nil {
			continue
		}
		for _, rp := range resPatterns {
			for _, newP := range patterns {
				if rp == newP || hasPathOverlap(rp, newP) {
					conflicts = append(conflicts, FileConflict{
						WorkerID: res.WorkerID,
						Pattern:  rp,
						Reason:   res.Reason,
					})
				}
			}
		}
	}
	if len(conflicts) > 0 {
		return "", conflicts, nil
	}

	// No conflicts — insert reservation
	patternsJSON, _ := json.Marshal(patterns)
	id := generateReservationID()
	_, err = tx.Exec(`
		INSERT INTO file_reservations (id, mission_id, worker_id, patterns, reason)
		VALUES (?, ?, ?, ?, ?)`,
		id, missionID, workerID, string(patternsJSON), reason,
	)
	if err != nil {
		return "", nil, fmt.Errorf("reserve files: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", nil, fmt.Errorf("commit tx: %w", err)
	}
	return id, nil, nil
}

func generateReservationID() string {
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

func (d *DB) ReleaseFiles(workerID string) error {
	_, err := d.sql.Exec(`DELETE FROM file_reservations WHERE worker_id = ?`, workerID)
	if err != nil {
		return fmt.Errorf("release files: %w", err)
	}
	return nil
}

func (d *DB) ListFileReservations(missionID string) ([]FileReservation, error) {
	rows, err := d.sql.Query(`
		SELECT id, mission_id, worker_id, patterns, reason, created_at
		FROM file_reservations WHERE mission_id = ? ORDER BY created_at ASC`, missionID)
	if err != nil {
		return nil, fmt.Errorf("list file reservations: %w", err)
	}
	defer rows.Close()
	var reservations []FileReservation
	for rows.Next() {
		var r FileReservation
		if err := rows.Scan(&r.ID, &r.MissionID, &r.WorkerID, &r.Patterns, &r.Reason, &r.CreatedAt); err != nil {
			return nil, err
		}
		reservations = append(reservations, r)
	}
	return reservations, rows.Err()
}

// CheckFileConflicts finds existing reservations in a mission that overlap with the given patterns.
// Pattern matching is simple string prefix/equality — not full glob. Good enough for directory-level locks.
func (d *DB) CheckFileConflicts(missionID string, excludeWorkerID string, patterns []string) ([]FileConflict, error) {
	existing, err := d.ListFileReservations(missionID)
	if err != nil {
		return nil, err
	}
	var conflicts []FileConflict
	for _, res := range existing {
		if res.WorkerID == excludeWorkerID {
			continue
		}
		var resPatterns []string
		if err := json.Unmarshal([]byte(res.Patterns), &resPatterns); err != nil {
			continue
		}
		for _, rp := range resPatterns {
			for _, newP := range patterns {
				// Simple overlap: one is prefix of the other, or exact match
				if rp == newP || hasPathOverlap(rp, newP) {
					conflicts = append(conflicts, FileConflict{
						WorkerID: res.WorkerID,
						Pattern:  rp,
						Reason:   res.Reason,
					})
				}
			}
		}
	}
	return conflicts, nil
}

// hasPathOverlap checks if two path patterns potentially overlap.
// Uses path-boundary-aware prefix matching: "src/api" overlaps "src/api/foo"
// but NOT "src/api-v2".
func hasPathOverlap(a, b string) bool {
	// Strip trailing wildcards for prefix comparison
	aBase := trimGlob(a)
	bBase := trimGlob(b)
	if len(aBase) == 0 || len(bBase) == 0 {
		return false
	}
	return hasPathPrefix(aBase, bBase) || hasPathPrefix(bBase, aBase)
}

func trimGlob(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '*' || s[len(s)-1] == '/') {
		s = s[:len(s)-1]
	}
	return s
}

// hasPathPrefix checks if s starts with prefix at a path boundary.
// "src/api/foo" has path prefix "src/api" (boundary at '/').
// "src/api-v2" does NOT have path prefix "src/api".
func hasPathPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	if s[:len(prefix)] != prefix {
		return false
	}
	// Exact match or the character after the prefix is a path separator
	return len(s) == len(prefix) || s[len(prefix)] == '/'
}

// --- Checkpoints ---

// SaveCheckpointReturningID creates a checkpoint and returns its generated ID.
func (d *DB) SaveCheckpointReturningID(missionID string, progress int, stateJSON string) (string, error) {
	id := generateReservationID()
	if err := d.SaveCheckpoint(id, missionID, progress, stateJSON); err != nil {
		return "", err
	}
	return id, nil
}

func (d *DB) SaveCheckpoint(id, missionID string, progress int, stateJSON string) error {
	_, err := d.sql.Exec(`
		INSERT INTO swarm_checkpoints (id, mission_id, progress, state_json)
		VALUES (?, ?, ?, ?)`,
		id, missionID, progress, stateJSON,
	)
	if err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}
	return nil
}

func (d *DB) GetLatestCheckpoint(missionID string) (*SwarmCheckpoint, error) {
	var c SwarmCheckpoint
	err := d.sql.QueryRow(`
		SELECT id, mission_id, progress, state_json, created_at
		FROM swarm_checkpoints WHERE mission_id = ?
		ORDER BY created_at DESC LIMIT 1`, missionID).
		Scan(&c.ID, &c.MissionID, &c.Progress, &c.StateJSON, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil // no checkpoint yet, not an error
	}
	if err != nil {
		return nil, fmt.Errorf("get latest checkpoint: %w", err)
	}
	return &c, nil
}

func (d *DB) ListCheckpoints(missionID string) ([]SwarmCheckpoint, error) {
	rows, err := d.sql.Query(`
		SELECT id, mission_id, progress, state_json, created_at
		FROM swarm_checkpoints WHERE mission_id = ?
		ORDER BY created_at ASC`, missionID)
	if err != nil {
		return nil, fmt.Errorf("list checkpoints: %w", err)
	}
	defer rows.Close()
	var checkpoints []SwarmCheckpoint
	for rows.Next() {
		var c SwarmCheckpoint
		if err := rows.Scan(&c.ID, &c.MissionID, &c.Progress, &c.StateJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, c)
	}
	return checkpoints, rows.Err()
}

// UpdateMissionStrategyOutcome records the outcome JSON of the decomposition strategy.
func (d *DB) UpdateMissionStrategyOutcome(id, outcome string) error {
	res, err := d.sql.Exec(`
		UPDATE missions SET strategy_outcome = ?, updated_at = ? WHERE id = ?`,
		outcome, now(), id,
	)
	if err != nil {
		return fmt.Errorf("update mission strategy outcome: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mission not found: %s", id)
	}
	return nil
}

// --- helpers ---

func now() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}
