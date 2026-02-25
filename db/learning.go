package db

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Candidate represents a learning pattern candidate.
type Candidate struct {
	ID            string   `json:"id"`
	DetectionType string   `json:"detection_type"`
	Count         int      `json:"count"`
	Confidence    float64  `json:"confidence"`
	Files         []string `json:"files"`
	Description   string   `json:"description"`
	Status        string   `json:"status"` // pending | proposed | decided
	DetectedAt    string   `json:"detected_at"`
}

// Proposal represents a learning proposal.
type Proposal struct {
	ID              string   `json:"id"`
	CandidateID     string   `json:"candidate_id"`
	Type            string   `json:"type"` // rule | skill | adr | template
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	ProposedContent string   `json:"proposed_content"`
	ProposedPath    *string  `json:"proposed_path,omitempty"`
	Confidence      float64  `json:"confidence"`
	Status          string   `json:"status"` // pending | presented | accepted | rejected | ignored | snoozed
	Decision        *string  `json:"decision,omitempty"`
	DecidedAt       *string  `json:"decided_at,omitempty"`
	SessionID       *string  `json:"session_id,omitempty"`
	CreatedAt       string   `json:"created_at"`
}

// SaveCandidate inserts a new pattern candidate.
func (d *DB) SaveCandidate(c Candidate) (string, error) {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	files, _ := json.Marshal(c.Files)
	if c.Files == nil {
		files = []byte("[]")
	}
	_, err := d.sql.Exec(`
		INSERT OR IGNORE INTO candidates (id, detection_type, count, confidence, files, description, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.DetectionType, c.Count, c.Confidence, string(files), c.Description, "pending",
	)
	if err != nil {
		return "", fmt.Errorf("save candidate: %w", err)
	}
	return c.ID, nil
}

// ListCandidates returns candidates filtered by status.
func (d *DB) ListCandidates(status string, limit int) ([]Candidate, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = d.sql.Query(`SELECT id, detection_type, count, confidence, files, description, status, detected_at FROM candidates WHERE status = ? ORDER BY detected_at DESC LIMIT ?`, status, limit)
	} else {
		rows, err = d.sql.Query(`SELECT id, detection_type, count, confidence, files, description, status, detected_at FROM candidates ORDER BY detected_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCandidates(rows)
}

func scanCandidates(rows *sql.Rows) ([]Candidate, error) {
	var candidates []Candidate
	for rows.Next() {
		var c Candidate
		var filesJSON string
		if err := rows.Scan(&c.ID, &c.DetectionType, &c.Count, &c.Confidence, &filesJSON, &c.Description, &c.Status, &c.DetectedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(filesJSON), &c.Files)
		if c.Files == nil {
			c.Files = []string{}
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// SaveProposal inserts a new proposal.
func (d *DB) SaveProposal(p Proposal) (string, error) {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	_, err := d.sql.Exec(`
		INSERT OR IGNORE INTO proposals (id, candidate_id, type, title, description, proposed_content, proposed_path, confidence, status, session_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.CandidateID, p.Type, p.Title, p.Description, p.ProposedContent, p.ProposedPath, p.Confidence, "pending", p.SessionID,
	)
	if err != nil {
		return "", fmt.Errorf("save proposal: %w", err)
	}
	// Update candidate status
	_, _ = d.sql.Exec(`UPDATE candidates SET status = 'proposed' WHERE id = ?`, p.CandidateID)
	return p.ID, nil
}

// GetProposal returns a single proposal by ID.
func (d *DB) GetProposal(id string) (*Proposal, error) {
	row := d.sql.QueryRow(`SELECT id, candidate_id, type, title, description, proposed_content, proposed_path, confidence, status, decision, decided_at, session_id, created_at FROM proposals WHERE id = ?`, id)
	var p Proposal
	var proposedPath, decision, decidedAt, sessionID sql.NullString
	if err := row.Scan(&p.ID, &p.CandidateID, &p.Type, &p.Title, &p.Description,
		&p.ProposedContent, &proposedPath, &p.Confidence, &p.Status,
		&decision, &decidedAt, &sessionID, &p.CreatedAt); err != nil {
		return nil, fmt.Errorf("get proposal %q: %w", id, err)
	}
	if proposedPath.Valid {
		p.ProposedPath = &proposedPath.String
	}
	if decision.Valid {
		p.Decision = &decision.String
	}
	if decidedAt.Valid {
		p.DecidedAt = &decidedAt.String
	}
	if sessionID.Valid {
		p.SessionID = &sessionID.String
	}
	return &p, nil
}

// ListProposals returns proposals filtered by status.
// When status = "pending", also includes snoozed proposals older than 7 days
// so they resurface for reconsideration.
func (d *DB) ListProposals(status string, limit int) ([]Proposal, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if status == "pending" {
		rows, err = d.sql.Query(`
			SELECT id, candidate_id, type, title, description, proposed_content, proposed_path,
			       confidence, status, decision, decided_at, session_id, created_at
			FROM proposals
			WHERE status = 'pending'
			   OR (status = 'snoozed' AND decided_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-7 days'))
			ORDER BY created_at DESC LIMIT ?`, limit)
	} else if status != "" {
		rows, err = d.sql.Query(`SELECT id, candidate_id, type, title, description, proposed_content, proposed_path, confidence, status, decision, decided_at, session_id, created_at FROM proposals WHERE status = ? ORDER BY created_at DESC LIMIT ?`, status, limit)
	} else {
		rows, err = d.sql.Query(`SELECT id, candidate_id, type, title, description, proposed_content, proposed_path, confidence, status, decision, decided_at, session_id, created_at FROM proposals ORDER BY created_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProposals(rows)
}

func scanProposals(rows *sql.Rows) ([]Proposal, error) {
	var proposals []Proposal
	for rows.Next() {
		var p Proposal
		var proposedPath, decision, decidedAt, sessionID sql.NullString
		if err := rows.Scan(&p.ID, &p.CandidateID, &p.Type, &p.Title, &p.Description,
			&p.ProposedContent, &proposedPath, &p.Confidence, &p.Status,
			&decision, &decidedAt, &sessionID, &p.CreatedAt); err != nil {
			return nil, err
		}
		if proposedPath.Valid {
			p.ProposedPath = &proposedPath.String
		}
		if decision.Valid {
			p.Decision = &decision.String
		}
		if decidedAt.Valid {
			p.DecidedAt = &decidedAt.String
		}
		if sessionID.Valid {
			p.SessionID = &sessionID.String
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

// DecideProposal records a decision on a proposal.
func (d *DB) DecideProposal(id, decision string) error {
	validDecisions := map[string]bool{"accept": true, "reject": true, "ignore": true, "snooze": true}
	if !validDecisions[decision] {
		return fmt.Errorf("invalid decision %q: must be accept|reject|ignore|snooze", decision)
	}
	res, err := d.sql.Exec(`
		UPDATE proposals SET status = ?, decision = ?, decided_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`,
		decision, decision, id,
	)
	if err != nil {
		return fmt.Errorf("decide proposal: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("proposal %q not found", id)
	}
	// Update candidate status on accept/reject
	if decision == "accept" || decision == "reject" {
		_, _ = d.sql.Exec(`UPDATE candidates SET status = 'decided' WHERE id = (SELECT candidate_id FROM proposals WHERE id = ?)`, id)
	}
	return nil
}
