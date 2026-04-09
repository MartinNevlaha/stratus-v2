package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

// EvolutionRun represents a single time-bounded evolution cycle.
type EvolutionRun struct {
	ID               string         `json:"id"`
	TriggerType      string         `json:"trigger_type"`      // scheduled | manual | event_driven
	Status           string         `json:"status"`            // running | completed | failed | timeout
	HypothesesCount  int            `json:"hypotheses_count"`
	ExperimentsRun   int            `json:"experiments_run"`
	AutoApplied      int            `json:"auto_applied"`
	ProposalsCreated int            `json:"proposals_created"`
	WikiPagesUpdated int            `json:"wiki_pages_updated"`
	DurationMs       int64          `json:"duration_ms"`
	TimeoutMs        int64          `json:"timeout_ms"`
	ErrorMessage     string         `json:"error_message,omitempty"`
	Metadata         map[string]any `json:"metadata"`
	StartedAt        string         `json:"started_at"`
	CompletedAt      *string        `json:"completed_at,omitempty"`
	CreatedAt        string         `json:"created_at"`
}

// EvolutionHypothesis represents an individual experiment within a run.
type EvolutionHypothesis struct {
	ID               string         `json:"id"`
	RunID            string         `json:"run_id"`
	Category         string         `json:"category"`         // prompt_tuning | workflow_routing | agent_selection | threshold_adjustment
	Description      string         `json:"description"`
	BaselineValue    string         `json:"baseline_value"`
	ProposedValue    string         `json:"proposed_value"`
	Metric           string         `json:"metric"`
	BaselineMetric   float64        `json:"baseline_metric"`
	ExperimentMetric float64        `json:"experiment_metric"`
	Confidence       float64        `json:"confidence"`
	Decision         string         `json:"decision"`         // auto_applied | proposal_created | rejected | inconclusive
	DecisionReason   string         `json:"decision_reason"`
	WikiPageID       *string        `json:"wiki_page_id,omitempty"`
	Evidence         map[string]any `json:"evidence"`
	CreatedAt        string         `json:"created_at"`
}

// EvolutionRunFilters controls listing behaviour for evolution runs.
type EvolutionRunFilters struct {
	Status string
	Limit  int
	Offset int
}

// SaveEvolutionRun inserts a new evolution run into the database.
// If r.ID is empty a UUID is generated.
func (d *DB) SaveEvolutionRun(r *EvolutionRun) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}

	metadata := r.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("save evolution run: marshal metadata: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	startedAt := r.StartedAt
	if startedAt == "" {
		startedAt = now
	}

	var completedAt sql.NullString
	if r.CompletedAt != nil {
		completedAt = sql.NullString{String: *r.CompletedAt, Valid: true}
	}

	_, err = d.sql.Exec(`
		INSERT INTO evolution_runs
			(id, trigger_type, status, hypotheses_count, experiments_run, auto_applied,
			 proposals_created, wiki_pages_updated, duration_ms, timeout_ms, error_message,
			 metadata_json, started_at, completed_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ID, r.TriggerType, r.Status, r.HypothesesCount, r.ExperimentsRun, r.AutoApplied,
		r.ProposalsCreated, r.WikiPagesUpdated, r.DurationMs, r.TimeoutMs, r.ErrorMessage,
		string(metadataBytes), startedAt, completedAt, now,
	)
	if err != nil {
		return fmt.Errorf("save evolution run: %w", err)
	}

	if r.CreatedAt == "" {
		r.CreatedAt = now
	}
	r.StartedAt = startedAt
	return nil
}

// GetEvolutionRun retrieves a single evolution run by ID.
// Returns (nil, nil) if not found.
func (d *DB) GetEvolutionRun(id string) (*EvolutionRun, error) {
	row := d.sql.QueryRow(`
		SELECT id, trigger_type, status, hypotheses_count, experiments_run, auto_applied,
		       proposals_created, wiki_pages_updated, duration_ms, timeout_ms, error_message,
		       metadata_json, started_at, completed_at, created_at
		FROM evolution_runs
		WHERE id = ?
	`, id)
	return scanEvolutionRun(row)
}

// ListEvolutionRuns returns a paginated list of evolution runs and the total count.
func (d *DB) ListEvolutionRuns(f EvolutionRunFilters) ([]EvolutionRun, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}

	countQuery := `SELECT COUNT(*) FROM evolution_runs WHERE 1=1`
	listQuery := `
		SELECT id, trigger_type, status, hypotheses_count, experiments_run, auto_applied,
		       proposals_created, wiki_pages_updated, duration_ms, timeout_ms, error_message,
		       metadata_json, started_at, completed_at, created_at
		FROM evolution_runs
		WHERE 1=1
	`
	args := []any{}

	if f.Status != "" {
		countQuery += " AND status = ?"
		listQuery += " AND status = ?"
		args = append(args, f.Status)
	}

	var total int
	if err := d.sql.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("list evolution runs: count: %w", err)
	}

	listQuery += " ORDER BY started_at DESC LIMIT ? OFFSET ?"
	args = append(args, f.Limit, f.Offset)

	rows, err := d.sql.Query(listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list evolution runs: query: %w", err)
	}
	defer rows.Close()

	runs, err := scanEvolutionRuns(rows)
	if err != nil {
		return nil, 0, err
	}
	return runs, total, nil
}

// UpdateEvolutionRun updates the mutable fields of an existing evolution run.
func (d *DB) UpdateEvolutionRun(r *EvolutionRun) error {
	metadata := r.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("update evolution run: marshal metadata: %w", err)
	}

	var completedAt sql.NullString
	if r.CompletedAt != nil {
		completedAt = sql.NullString{String: *r.CompletedAt, Valid: true}
	}

	_, err = d.sql.Exec(`
		UPDATE evolution_runs SET
			status             = ?,
			hypotheses_count   = ?,
			experiments_run    = ?,
			auto_applied       = ?,
			proposals_created  = ?,
			wiki_pages_updated = ?,
			duration_ms        = ?,
			error_message      = ?,
			completed_at       = ?,
			metadata_json      = ?
		WHERE id = ?
	`,
		r.Status, r.HypothesesCount, r.ExperimentsRun, r.AutoApplied,
		r.ProposalsCreated, r.WikiPagesUpdated, r.DurationMs, r.ErrorMessage,
		completedAt, string(metadataBytes), r.ID,
	)
	if err != nil {
		return fmt.Errorf("update evolution run: %w", err)
	}
	return nil
}

// GetActiveEvolutionRun returns the currently running evolution run, or nil if none is active.
func (d *DB) GetActiveEvolutionRun() (*EvolutionRun, error) {
	row := d.sql.QueryRow(`
		SELECT id, trigger_type, status, hypotheses_count, experiments_run, auto_applied,
		       proposals_created, wiki_pages_updated, duration_ms, timeout_ms, error_message,
		       metadata_json, started_at, completed_at, created_at
		FROM evolution_runs
		WHERE status = 'running'
		LIMIT 1
	`)
	return scanEvolutionRun(row)
}

// SaveEvolutionHypothesis inserts a new hypothesis into the database.
// If h.ID is empty a UUID is generated.
func (d *DB) SaveEvolutionHypothesis(h *EvolutionHypothesis) error {
	if h.ID == "" {
		h.ID = uuid.NewString()
	}

	evidence := h.Evidence
	if evidence == nil {
		evidence = map[string]any{}
	}
	evidenceBytes, err := json.Marshal(evidence)
	if err != nil {
		return fmt.Errorf("save evolution hypothesis: marshal evidence: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	var wikiPageID sql.NullString
	if h.WikiPageID != nil {
		wikiPageID = sql.NullString{String: *h.WikiPageID, Valid: true}
	}

	_, err = d.sql.Exec(`
		INSERT INTO evolution_hypotheses
			(id, run_id, category, description, baseline_value, proposed_value,
			 metric, baseline_metric, experiment_metric, confidence, decision,
			 decision_reason, wiki_page_id, evidence_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		h.ID, h.RunID, h.Category, h.Description, h.BaselineValue, h.ProposedValue,
		h.Metric, h.BaselineMetric, h.ExperimentMetric, h.Confidence, h.Decision,
		h.DecisionReason, wikiPageID, string(evidenceBytes), now,
	)
	if err != nil {
		return fmt.Errorf("save evolution hypothesis: %w", err)
	}

	if h.CreatedAt == "" {
		h.CreatedAt = now
	}
	return nil
}

// GetEvolutionHypothesis retrieves a single hypothesis by ID.
// Returns (nil, nil) if not found.
func (d *DB) GetEvolutionHypothesis(id string) (*EvolutionHypothesis, error) {
	row := d.sql.QueryRow(`
		SELECT id, run_id, category, description, baseline_value, proposed_value,
		       metric, baseline_metric, experiment_metric, confidence, decision,
		       decision_reason, wiki_page_id, evidence_json, created_at
		FROM evolution_hypotheses
		WHERE id = ?
	`, id)
	return scanEvolutionHypothesis(row)
}

// ListEvolutionHypotheses returns all hypotheses belonging to a given run, ordered by created_at.
func (d *DB) ListEvolutionHypotheses(runID string) ([]EvolutionHypothesis, error) {
	rows, err := d.sql.Query(`
		SELECT id, run_id, category, description, baseline_value, proposed_value,
		       metric, baseline_metric, experiment_metric, confidence, decision,
		       decision_reason, wiki_page_id, evidence_json, created_at
		FROM evolution_hypotheses
		WHERE run_id = ?
		ORDER BY created_at
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list evolution hypotheses: %w", err)
	}
	defer rows.Close()
	return scanEvolutionHypotheses(rows)
}

// UpdateEvolutionHypothesis updates the experiment result fields of an existing hypothesis.
func (d *DB) UpdateEvolutionHypothesis(h *EvolutionHypothesis) error {
	evidence := h.Evidence
	if evidence == nil {
		evidence = map[string]any{}
	}
	evidenceBytes, err := json.Marshal(evidence)
	if err != nil {
		return fmt.Errorf("update evolution hypothesis: marshal evidence: %w", err)
	}

	var wikiPageID sql.NullString
	if h.WikiPageID != nil {
		wikiPageID = sql.NullString{String: *h.WikiPageID, Valid: true}
	}

	_, err = d.sql.Exec(`
		UPDATE evolution_hypotheses SET
			experiment_metric = ?,
			confidence        = ?,
			decision          = ?,
			decision_reason   = ?,
			wiki_page_id      = ?,
			evidence_json     = ?
		WHERE id = ?
	`,
		h.ExperimentMetric, h.Confidence, h.Decision, h.DecisionReason,
		wikiPageID, string(evidenceBytes), h.ID,
	)
	if err != nil {
		return fmt.Errorf("update evolution hypothesis: %w", err)
	}
	return nil
}

// scanEvolutionRun scans a single *sql.Row into an EvolutionRun.
// Returns (nil, nil) when no row is found.
func scanEvolutionRun(row *sql.Row) (*EvolutionRun, error) {
	var r EvolutionRun
	var metadataJSON string
	var completedAt sql.NullString

	err := row.Scan(
		&r.ID, &r.TriggerType, &r.Status, &r.HypothesesCount, &r.ExperimentsRun, &r.AutoApplied,
		&r.ProposalsCreated, &r.WikiPagesUpdated, &r.DurationMs, &r.TimeoutMs, &r.ErrorMessage,
		&metadataJSON, &r.StartedAt, &completedAt, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan evolution run: %w", err)
	}

	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &r.Metadata); err != nil {
			log.Printf("warning: failed to parse metadata for evolution run %s: %v", r.ID, err)
		}
	}
	if r.Metadata == nil {
		r.Metadata = map[string]any{}
	}

	if completedAt.Valid {
		r.CompletedAt = &completedAt.String
	}

	return &r, nil
}

// scanEvolutionRuns scans multiple rows into a slice of EvolutionRun.
func scanEvolutionRuns(rows *sql.Rows) ([]EvolutionRun, error) {
	var runs []EvolutionRun
	for rows.Next() {
		var r EvolutionRun
		var metadataJSON string
		var completedAt sql.NullString

		if err := rows.Scan(
			&r.ID, &r.TriggerType, &r.Status, &r.HypothesesCount, &r.ExperimentsRun, &r.AutoApplied,
			&r.ProposalsCreated, &r.WikiPagesUpdated, &r.DurationMs, &r.TimeoutMs, &r.ErrorMessage,
			&metadataJSON, &r.StartedAt, &completedAt, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan evolution run: %w", err)
		}

		if metadataJSON != "" {
			if err := json.Unmarshal([]byte(metadataJSON), &r.Metadata); err != nil {
				log.Printf("warning: failed to parse metadata for evolution run %s: %v", r.ID, err)
			}
		}
		if r.Metadata == nil {
			r.Metadata = map[string]any{}
		}

		if completedAt.Valid {
			r.CompletedAt = &completedAt.String
		}

		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// scanEvolutionHypothesis scans a single *sql.Row into an EvolutionHypothesis.
// Returns (nil, nil) when no row is found.
func scanEvolutionHypothesis(row *sql.Row) (*EvolutionHypothesis, error) {
	var h EvolutionHypothesis
	var evidenceJSON string
	var wikiPageID sql.NullString

	err := row.Scan(
		&h.ID, &h.RunID, &h.Category, &h.Description, &h.BaselineValue, &h.ProposedValue,
		&h.Metric, &h.BaselineMetric, &h.ExperimentMetric, &h.Confidence, &h.Decision,
		&h.DecisionReason, &wikiPageID, &evidenceJSON, &h.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan evolution hypothesis: %w", err)
	}

	if evidenceJSON != "" {
		if err := json.Unmarshal([]byte(evidenceJSON), &h.Evidence); err != nil {
			log.Printf("warning: failed to parse evidence for evolution hypothesis %s: %v", h.ID, err)
		}
	}
	if h.Evidence == nil {
		h.Evidence = map[string]any{}
	}

	if wikiPageID.Valid {
		h.WikiPageID = &wikiPageID.String
	}

	return &h, nil
}

// scanEvolutionHypotheses scans multiple rows into a slice of EvolutionHypothesis.
func scanEvolutionHypotheses(rows *sql.Rows) ([]EvolutionHypothesis, error) {
	var hypotheses []EvolutionHypothesis
	for rows.Next() {
		var h EvolutionHypothesis
		var evidenceJSON string
		var wikiPageID sql.NullString

		if err := rows.Scan(
			&h.ID, &h.RunID, &h.Category, &h.Description, &h.BaselineValue, &h.ProposedValue,
			&h.Metric, &h.BaselineMetric, &h.ExperimentMetric, &h.Confidence, &h.Decision,
			&h.DecisionReason, &wikiPageID, &evidenceJSON, &h.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan evolution hypothesis: %w", err)
		}

		if evidenceJSON != "" {
			if err := json.Unmarshal([]byte(evidenceJSON), &h.Evidence); err != nil {
				log.Printf("warning: failed to parse evidence for evolution hypothesis %s: %v", h.ID, err)
			}
		}
		if h.Evidence == nil {
			h.Evidence = map[string]any{}
		}

		if wikiPageID.Valid {
			h.WikiPageID = &wikiPageID.String
		}

		hypotheses = append(hypotheses, h)
	}
	return hypotheses, rows.Err()
}
