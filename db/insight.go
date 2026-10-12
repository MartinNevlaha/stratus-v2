package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

type InsightProposal struct {
	ID              string                 `json:"id"`
	Type            string                 `json:"type"`
	Status          string                 `json:"status"`
	Title           string                 `json:"title"`
	Description     string                 `json:"description"`
	Confidence      float64                `json:"confidence"`
	RiskLevel       string                 `json:"risk_level"`
	SourcePatternID string                 `json:"source_pattern_id"`
	Evidence        map[string]interface{} `json:"evidence"`
	Recommendation  map[string]interface{} `json:"recommendation"`
	DecisionReason  string                 `json:"decision_reason,omitempty"`
	CreatedAt       string                 `json:"created_at"`
	UpdatedAt       string                 `json:"updated_at"`
}

func (d *DB) SaveInsightProposal(proposal *InsightProposal) error {
	evidenceBytes, err := json.Marshal(proposal.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	recommendationBytes, err := json.Marshal(proposal.Recommendation)
	if err != nil {
		return fmt.Errorf("marshal recommendation: %w", err)
	}
	if proposal.Evidence == nil {
		evidenceBytes = []byte("{}")
	}
	if proposal.Recommendation == nil {
		recommendationBytes = []byte("{}")
	}

	var decisionReason interface{}
	if proposal.DecisionReason != "" {
		decisionReason = proposal.DecisionReason
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.sql.Exec(`
		INSERT INTO insight_proposals 
		(id, type, status, title, description, confidence, risk_level, 
		 source_pattern_id, evidence, recommendation, decision_reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		proposal.ID, proposal.Type, proposal.Status, proposal.Title,
		proposal.Description, proposal.Confidence, proposal.RiskLevel,
		proposal.SourcePatternID, string(evidenceBytes), string(recommendationBytes),
		decisionReason, now, now,
	)

	return err
}

func (d *DB) GetInsightProposalByID(id string) (*InsightProposal, error) {
	row := d.sql.QueryRow(`
		SELECT id, type, status, title, description, confidence, risk_level,
		       source_pattern_id, evidence, recommendation, decision_reason, created_at, updated_at
		FROM insight_proposals
		WHERE id = ?
	`, id)

	var p InsightProposal
	var evidenceJSON, recommendationJSON string
	var decisionReason sql.NullString
	err := row.Scan(
		&p.ID, &p.Type, &p.Status, &p.Title, &p.Description, &p.Confidence,
		&p.RiskLevel, &p.SourcePatternID, &evidenceJSON, &recommendationJSON,
		&decisionReason, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if decisionReason.Valid {
		p.DecisionReason = decisionReason.String
	}
	if evidenceJSON != "" {
		if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
			log.Printf("warning: failed to parse evidence for proposal %s: %v", p.ID, err)
		}
	}
	if recommendationJSON != "" {
		if err := json.Unmarshal([]byte(recommendationJSON), &p.Recommendation); err != nil {
			log.Printf("warning: failed to parse recommendation for proposal %s: %v", p.ID, err)
		}
	}

	return &p, nil
}

func (d *DB) ListInsightProposals(proposalType string, status string, riskLevel string, minConfidence float64, limit int, offset int) ([]InsightProposal, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var rows *sql.Rows
	var err error

	query := `
		SELECT id, type, status, title, description, confidence, risk_level,
		       source_pattern_id, evidence, recommendation, decision_reason, created_at, updated_at
		FROM insight_proposals
		WHERE confidence >= ?
	`
	args := []interface{}{minConfidence}

	if proposalType != "" {
		if strings.HasSuffix(proposalType, "*") {
			query += " AND type LIKE ?"
			args = append(args, strings.TrimSuffix(proposalType, "*")+"%")
		} else {
			query += " AND type = ?"
			args = append(args, proposalType)
		}
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	if riskLevel != "" {
		query += " AND risk_level = ?"
		args = append(args, riskLevel)
	}

	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err = d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proposals []InsightProposal
	for rows.Next() {
		var p InsightProposal
		var evidenceJSON, recommendationJSON string
		var decisionReason sql.NullString
		if err := rows.Scan(
			&p.ID, &p.Type, &p.Status, &p.Title, &p.Description, &p.Confidence,
			&p.RiskLevel, &p.SourcePatternID, &evidenceJSON, &recommendationJSON,
			&decisionReason, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if decisionReason.Valid {
			p.DecisionReason = decisionReason.String
		}
		if evidenceJSON != "" {
			if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
				log.Printf("warning: failed to parse evidence for proposal %s: %v", p.ID, err)
			}
		}
		if recommendationJSON != "" {
			if err := json.Unmarshal([]byte(recommendationJSON), &p.Recommendation); err != nil {
				log.Printf("warning: failed to parse recommendation for proposal %s: %v", p.ID, err)
			}
		}

		proposals = append(proposals, p)
	}

	return proposals, nil
}

// CountInsightProposalsByType returns a map of proposal type → count for all
// proposals in the database (no time filter applied, so callers can decide).
func (d *DB) CountInsightProposalsByType() (map[string]int, error) {
	rows, err := d.sql.Query(`
		SELECT type, COUNT(*) AS cnt
		FROM insight_proposals
		GROUP BY type
	`)
	if err != nil {
		return nil, fmt.Errorf("count insight proposals by type: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var t string
		var cnt int
		if err := rows.Scan(&t, &cnt); err != nil {
			return nil, fmt.Errorf("scan proposal type count: %w", err)
		}
		result[t] = cnt
	}
	return result, rows.Err()
}

// CreateInsightProposalFull inserts a new proposal row with the extended columns
// (wiki_page_id, idempotency_hash, last_seen_at, signal_refs) added in T1.
// If a row with the same idempotency_hash already exists, last_seen_at and
// updated_at are bumped and the existing ID is returned (idempotent upsert).
// The idempotency_hash MUST be pre-computed by the caller (server-authoritative).
func (d *DB) CreateInsightProposalFull(p *InsightProposal, idempotencyHash string, signalRefs []string, wikiPageID *string) (string, bool, error) {
	signalRefsJSON, err := json.Marshal(signalRefs)
	if err != nil {
		return "", false, fmt.Errorf("create insight proposal full: marshal signal_refs: %w", err)
	}

	now := time.Now().UTC()
	nowISO := now.Format(time.RFC3339Nano)
	nowUnix := now.Unix()

	conn, err := d.sql.Conn(context.Background())
	if err != nil {
		return "", false, fmt.Errorf("create insight proposal full: acquire conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		return "", false, fmt.Errorf("create insight proposal full: begin immediate: %w", err)
	}

	var existingID string
	scanErr := conn.QueryRowContext(context.Background(),
		`SELECT id FROM insight_proposals WHERE idempotency_hash = ? LIMIT 1`,
		idempotencyHash,
	).Scan(&existingID)

	var resultID string
	var inserted bool

	switch {
	case scanErr == nil:
		// Row exists — bump last_seen_at.
		if _, err := conn.ExecContext(context.Background(),
			`UPDATE insight_proposals SET last_seen_at = ?, updated_at = ? WHERE id = ?`,
			nowUnix, nowISO, existingID,
		); err != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
			return "", false, fmt.Errorf("create insight proposal full: update existing: %w", err)
		}
		resultID = existingID
		inserted = false

	case scanErr == sql.ErrNoRows:
		if p.ID == "" {
			p.ID = uuid.NewString()
		}
		evidenceBytes, _ := json.Marshal(p.Evidence)
		if p.Evidence == nil {
			evidenceBytes = []byte("{}")
		}
		recommendationBytes, _ := json.Marshal(p.Recommendation)
		if p.Recommendation == nil {
			recommendationBytes = []byte("{}")
		}

		var wikiID interface{}
		if wikiPageID != nil {
			wikiID = *wikiPageID
		}

		if _, err := conn.ExecContext(context.Background(), `
INSERT INTO insight_proposals (
  id, type, status, title, description,
  confidence, risk_level, source_pattern_id, evidence, recommendation,
  created_at, updated_at,
  wiki_page_id, idempotency_hash, last_seen_at, signal_refs
)
VALUES (?, ?, ?, ?, ?,
        ?, ?, ?, ?, ?,
        ?, ?,
        ?, ?, ?, ?)`,
			p.ID, p.Type, p.Status, p.Title, p.Description,
			p.Confidence, p.RiskLevel, p.SourcePatternID, string(evidenceBytes), string(recommendationBytes),
			nowISO, nowISO,
			wikiID, idempotencyHash, nowUnix, string(signalRefsJSON),
		); err != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
			return "", false, fmt.Errorf("create insight proposal full: insert: %w", err)
		}
		resultID = p.ID
		inserted = true

	default:
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		return "", false, fmt.Errorf("create insight proposal full: check existing: %w", scanErr)
	}

	if _, err := conn.ExecContext(context.Background(), "COMMIT"); err != nil {
		return "", false, fmt.Errorf("create insight proposal full: commit: %w", err)
	}
	return resultID, inserted, nil
}

func (d *DB) UpdateInsightProposalStatus(id string, status string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		UPDATE insight_proposals
		SET status = ?, updated_at = ?
		WHERE id = ?
	`, status, now, id)

	return err
}

func (d *DB) UpdateInsightProposalStatusWithReason(id string, status string, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		UPDATE insight_proposals
		SET status = ?, decision_reason = ?, updated_at = ?
		WHERE id = ?
	`, status, reason, now, id)

	return err
}

// ListAssetProposalPaths returns the proposed_path values from all asset.* proposals
// that are in detected, drafted, or approved status.
func (d *DB) ListAssetProposalPaths() ([]string, error) {
	rows, err := d.sql.Query(`
		SELECT json_extract(recommendation, '$.proposed_path')
		FROM insight_proposals
		WHERE type LIKE 'asset.%'
		  AND status IN ('detected', 'drafted', 'approved')
		  AND json_extract(recommendation, '$.proposed_path') IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("list asset proposal paths: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("list asset proposal paths: scan: %w", err)
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}
