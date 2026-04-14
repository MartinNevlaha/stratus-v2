package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CodeAnalysisRun represents a single code analysis execution.
type CodeAnalysisRun struct {
	ID               string         `json:"id"`
	Status           string         `json:"status"`           // running | completed | failed
	FilesScanned     int            `json:"files_scanned"`
	FilesAnalyzed    int            `json:"files_analyzed"`
	FindingsCount    int            `json:"findings_count"`
	WikiPagesCreated int            `json:"wiki_pages_created"`
	WikiPagesUpdated int            `json:"wiki_pages_updated"`
	DurationMs       int64          `json:"duration_ms"`
	TokensUsed       int64          `json:"tokens_used"`
	GitCommitHash    string         `json:"git_commit_hash"`
	ErrorMessage     string         `json:"error_message,omitempty"`
	Metadata         map[string]any `json:"metadata"`
	StartedAt        string         `json:"started_at"`
	CompletedAt      *string        `json:"completed_at,omitempty"`
	CreatedAt        string         `json:"created_at"`
}

// CodeFinding represents a single finding produced by a code analysis run.
type CodeFinding struct {
	ID          string         `json:"id"`
	RunID       string         `json:"run_id"`
	FilePath    string         `json:"file_path"`
	Category    string         `json:"category"`
	Severity    string         `json:"severity"` // critical | high | medium | low | info
	Title       string         `json:"title"`
	Description string         `json:"description"`
	LineStart   int            `json:"line_start"`
	LineEnd     int            `json:"line_end"`
	Confidence  float64        `json:"confidence"`
	Suggestion  string         `json:"suggestion"`
	WikiPageID  *string        `json:"wiki_page_id,omitempty"`
	Evidence    map[string]any `json:"evidence"`
	Status      string         `json:"status"` // pending | rejected | applied
	CreatedAt   string         `json:"created_at"`
}

// CodeQualityMetric represents aggregated quality metrics for a given date.
type CodeQualityMetric struct {
	ID                    string         `json:"id"`
	MetricDate            string         `json:"metric_date"`
	TotalFiles            int            `json:"total_files"`
	FilesAnalyzed         int            `json:"files_analyzed"`
	FindingsTotal         int            `json:"findings_total"`
	FindingsBySeverity    map[string]int `json:"findings_by_severity"`
	FindingsByCategory    map[string]int `json:"findings_by_category"`
	AvgChurnScore         float64        `json:"avg_churn_score"`
	AvgCoverage           float64        `json:"avg_coverage"`
	CreatedAt             string         `json:"created_at"`
}

// FileCacheEntry represents the analysis cache entry for a single file.
type FileCacheEntry struct {
	FilePath       string  `json:"file_path"`
	GitHash        string  `json:"git_hash"`
	LastAnalyzedAt string  `json:"last_analyzed_at"`
	LastRunID      string  `json:"last_run_id"`
	CompositeScore float64 `json:"composite_score"`
	FindingsCount  int     `json:"findings_count"`
	UpdatedAt      string  `json:"updated_at"`
}

// CodeFindingFilters controls listing behaviour for code findings.
type CodeFindingFilters struct {
	RunID    string
	FilePath string // prefix match
	Category string
	Severity string
	Status   string // pending | rejected | applied; empty = no filter
	Query    string // FTS5 full-text search
	Limit    int
	Offset   int
}

// SaveCodeAnalysisRun inserts a new code analysis run into the database.
// If r.ID is empty a UUID is generated.
func (d *DB) SaveCodeAnalysisRun(r *CodeAnalysisRun) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}

	metadata := r.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("save code analysis run: marshal metadata: %w", err)
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
		INSERT INTO code_analysis_runs
			(id, status, files_scanned, files_analyzed, findings_count,
			 wiki_pages_created, wiki_pages_updated, duration_ms, tokens_used,
			 git_commit_hash, error_message, metadata_json,
			 started_at, completed_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ID, r.Status, r.FilesScanned, r.FilesAnalyzed, r.FindingsCount,
		r.WikiPagesCreated, r.WikiPagesUpdated, r.DurationMs, r.TokensUsed,
		r.GitCommitHash, r.ErrorMessage, string(metadataBytes),
		startedAt, completedAt, now,
	)
	if err != nil {
		return fmt.Errorf("save code analysis run: %w", err)
	}

	if r.CreatedAt == "" {
		r.CreatedAt = now
	}
	r.StartedAt = startedAt
	return nil
}

// GetCodeAnalysisRun retrieves a single code analysis run by ID.
// Returns (nil, nil) if not found.
func (d *DB) GetCodeAnalysisRun(id string) (*CodeAnalysisRun, error) {
	row := d.sql.QueryRow(`
		SELECT id, status, files_scanned, files_analyzed, findings_count,
		       wiki_pages_created, wiki_pages_updated, duration_ms, tokens_used,
		       git_commit_hash, error_message, metadata_json,
		       started_at, completed_at, created_at
		FROM code_analysis_runs
		WHERE id = ?
	`, id)
	return scanCodeAnalysisRun(row)
}

// ListCodeAnalysisRuns returns a paginated list of code analysis runs and the total count,
// ordered by started_at descending.
func (d *DB) ListCodeAnalysisRuns(limit, offset int) ([]CodeAnalysisRun, int, error) {
	if limit <= 0 {
		limit = 50
	}

	var total int
	if err := d.sql.QueryRow(`SELECT COUNT(*) FROM code_analysis_runs`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("list code analysis runs: count: %w", err)
	}

	rows, err := d.sql.Query(`
		SELECT id, status, files_scanned, files_analyzed, findings_count,
		       wiki_pages_created, wiki_pages_updated, duration_ms, tokens_used,
		       git_commit_hash, error_message, metadata_json,
		       started_at, completed_at, created_at
		FROM code_analysis_runs
		ORDER BY started_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list code analysis runs: query: %w", err)
	}
	defer rows.Close()

	runs, err := scanCodeAnalysisRuns(rows)
	if err != nil {
		return nil, 0, err
	}
	return runs, total, nil
}

// UpdateCodeAnalysisRun updates the mutable fields of an existing code analysis run.
func (d *DB) UpdateCodeAnalysisRun(r *CodeAnalysisRun) error {
	metadata := r.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("update code analysis run: marshal metadata: %w", err)
	}

	var completedAt sql.NullString
	if r.CompletedAt != nil {
		completedAt = sql.NullString{String: *r.CompletedAt, Valid: true}
	}

	_, err = d.sql.Exec(`
		UPDATE code_analysis_runs SET
			status             = ?,
			files_scanned      = ?,
			files_analyzed     = ?,
			findings_count     = ?,
			wiki_pages_created = ?,
			wiki_pages_updated = ?,
			duration_ms        = ?,
			tokens_used        = ?,
			git_commit_hash    = ?,
			error_message      = ?,
			metadata_json      = ?,
			completed_at       = ?
		WHERE id = ?
	`,
		r.Status, r.FilesScanned, r.FilesAnalyzed, r.FindingsCount,
		r.WikiPagesCreated, r.WikiPagesUpdated, r.DurationMs, r.TokensUsed,
		r.GitCommitHash, r.ErrorMessage, string(metadataBytes),
		completedAt, r.ID,
	)
	if err != nil {
		return fmt.Errorf("update code analysis run: %w", err)
	}
	return nil
}

// SaveCodeFinding inserts a new code finding into the database.
// If f.ID is empty a UUID is generated.
func (d *DB) SaveCodeFinding(f *CodeFinding) error {
	if f.ID == "" {
		f.ID = uuid.NewString()
	}

	evidence := f.Evidence
	if evidence == nil {
		evidence = map[string]any{}
	}
	evidenceBytes, err := json.Marshal(evidence)
	if err != nil {
		return fmt.Errorf("save code finding: marshal evidence: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	var wikiPageID sql.NullString
	if f.WikiPageID != nil {
		wikiPageID = sql.NullString{String: *f.WikiPageID, Valid: true}
	}

	status := f.Status
	if status == "" {
		status = "pending"
	}

	_, err = d.sql.Exec(`
		INSERT INTO code_findings
			(id, run_id, file_path, category, severity, title, description,
			 line_start, line_end, confidence, suggestion, wiki_page_id,
			 evidence_json, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		f.ID, f.RunID, f.FilePath, f.Category, f.Severity, f.Title, f.Description,
		f.LineStart, f.LineEnd, f.Confidence, f.Suggestion, wikiPageID,
		string(evidenceBytes), status, now,
	)
	if err != nil {
		return fmt.Errorf("save code finding: %w", err)
	}

	f.Status = status
	if f.CreatedAt == "" {
		f.CreatedAt = now
	}
	return nil
}

// UpdateCodeFindingStatus sets the status of a code finding to one of: rejected, applied.
// Returns an error wrapping sql.ErrNoRows if no finding with the given id exists.
func (d *DB) UpdateCodeFindingStatus(ctx context.Context, id string, status string) error {
	if status != "rejected" && status != "applied" {
		return fmt.Errorf("update code finding status: invalid status %q: must be 'rejected' or 'applied'", status)
	}

	res, err := d.sql.ExecContext(ctx, `UPDATE code_findings SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("update code finding status: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update code finding status: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("update code finding status: %w", sql.ErrNoRows)
	}
	return nil
}

// ListCodeFindings returns a filtered, paginated list of code findings and the total count.
func (d *DB) ListCodeFindings(filters CodeFindingFilters) ([]CodeFinding, int, error) {
	if filters.Limit <= 0 {
		filters.Limit = 50
	}

	// FTS5 search takes priority when a query is provided.
	if filters.Query != "" {
		findings, err := d.SearchCodeFindings(filters.Query, filters.Limit)
		if err != nil {
			return nil, 0, err
		}
		return findings, len(findings), nil
	}

	countQuery := `SELECT COUNT(*) FROM code_findings WHERE 1=1`
	listQuery := `
		SELECT id, run_id, file_path, category, severity, title, description,
		       line_start, line_end, confidence, suggestion, wiki_page_id,
		       evidence_json, status, created_at
		FROM code_findings
		WHERE 1=1
	`
	args := []any{}

	if filters.RunID != "" {
		countQuery += " AND run_id = ?"
		listQuery += " AND run_id = ?"
		args = append(args, filters.RunID)
	}
	if filters.FilePath != "" {
		countQuery += " AND file_path LIKE ?"
		listQuery += " AND file_path LIKE ?"
		args = append(args, filters.FilePath+"%")
	}
	if filters.Category != "" {
		countQuery += " AND category = ?"
		listQuery += " AND category = ?"
		args = append(args, filters.Category)
	}
	if filters.Severity != "" {
		countQuery += " AND severity = ?"
		listQuery += " AND severity = ?"
		args = append(args, filters.Severity)
	}
	if filters.Status != "" {
		countQuery += " AND status = ?"
		listQuery += " AND status = ?"
		args = append(args, filters.Status)
	}

	var total int
	if err := d.sql.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("list code findings: count: %w", err)
	}

	listQuery += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, filters.Limit, filters.Offset)

	rows, err := d.sql.Query(listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list code findings: query: %w", err)
	}
	defer rows.Close()

	findings, err := scanCodeFindings(rows)
	if err != nil {
		return nil, 0, err
	}
	return findings, total, nil
}

// SearchCodeFindings performs a full-text search over code findings using FTS5.
func (d *DB) SearchCodeFindings(query string, limit int) ([]CodeFinding, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := d.sql.Query(`
		SELECT cf.id, cf.run_id, cf.file_path, cf.category, cf.severity, cf.title, cf.description,
		       cf.line_start, cf.line_end, cf.confidence, cf.suggestion, cf.wiki_page_id,
		       cf.evidence_json, cf.status, cf.created_at
		FROM code_findings cf
		JOIN code_findings_fts fts ON cf.rowid = fts.rowid
		WHERE code_findings_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search code findings: %w", err)
	}
	defer rows.Close()

	return scanCodeFindings(rows)
}

// SaveCodeQualityMetric inserts or replaces a code quality metric for a given date.
// If m.ID is empty a UUID is generated.
func (d *DB) SaveCodeQualityMetric(m *CodeQualityMetric) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}

	bySeverity := m.FindingsBySeverity
	if bySeverity == nil {
		bySeverity = map[string]int{}
	}
	bySeverityBytes, err := json.Marshal(bySeverity)
	if err != nil {
		return fmt.Errorf("save code quality metric: marshal findings_by_severity: %w", err)
	}

	byCategory := m.FindingsByCategory
	if byCategory == nil {
		byCategory = map[string]int{}
	}
	byCategoryBytes, err := json.Marshal(byCategory)
	if err != nil {
		return fmt.Errorf("save code quality metric: marshal findings_by_category: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = d.sql.Exec(`
		INSERT INTO code_quality_metrics
			(id, metric_date, total_files, files_analyzed, findings_total,
			 findings_by_severity_json, findings_by_category_json,
			 avg_churn_score, avg_coverage, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(metric_date) DO UPDATE SET
			id                        = excluded.id,
			total_files               = excluded.total_files,
			files_analyzed            = excluded.files_analyzed,
			findings_total            = excluded.findings_total,
			findings_by_severity_json = excluded.findings_by_severity_json,
			findings_by_category_json = excluded.findings_by_category_json,
			avg_churn_score           = excluded.avg_churn_score,
			avg_coverage              = excluded.avg_coverage
	`,
		m.ID, m.MetricDate, m.TotalFiles, m.FilesAnalyzed, m.FindingsTotal,
		string(bySeverityBytes), string(byCategoryBytes),
		m.AvgChurnScore, m.AvgCoverage, now,
	)
	if err != nil {
		return fmt.Errorf("save code quality metric: %w", err)
	}

	if m.CreatedAt == "" {
		m.CreatedAt = now
	}
	return nil
}

// ListCodeQualityMetrics returns quality metrics for the last N calendar days,
// ordered by metric_date descending.
func (d *DB) ListCodeQualityMetrics(days int) ([]CodeQualityMetric, error) {
	if days <= 0 {
		days = 30
	}

	rows, err := d.sql.Query(`
		SELECT id, metric_date, total_files, files_analyzed, findings_total,
		       findings_by_severity_json, findings_by_category_json,
		       avg_churn_score, avg_coverage, created_at
		FROM code_quality_metrics
		WHERE metric_date >= date('now', ? || ' days')
		ORDER BY metric_date DESC
	`, fmt.Sprintf("-%d", days))
	if err != nil {
		return nil, fmt.Errorf("list code quality metrics: %w", err)
	}
	defer rows.Close()

	return scanCodeQualityMetrics(rows)
}

// GetFileCacheEntry retrieves the analysis cache entry for a given file path.
// Returns (nil, nil) if not found.
func (d *DB) GetFileCacheEntry(path string) (*FileCacheEntry, error) {
	row := d.sql.QueryRow(`
		SELECT file_path, git_hash, last_analyzed_at, last_run_id,
		       composite_score, findings_count, updated_at
		FROM code_file_cache
		WHERE file_path = ?
	`, path)

	var e FileCacheEntry
	err := row.Scan(
		&e.FilePath, &e.GitHash, &e.LastAnalyzedAt, &e.LastRunID,
		&e.CompositeScore, &e.FindingsCount, &e.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get file cache entry: %w", err)
	}
	return &e, nil
}

// SetFileCacheEntry inserts or updates the analysis cache entry for a file.
func (d *DB) SetFileCacheEntry(path, gitHash, runID string, score float64, findingsCount int) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := d.sql.Exec(`
		INSERT INTO code_file_cache
			(file_path, git_hash, last_analyzed_at, last_run_id,
			 composite_score, findings_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_path) DO UPDATE SET
			git_hash         = excluded.git_hash,
			last_analyzed_at = excluded.last_analyzed_at,
			last_run_id      = excluded.last_run_id,
			composite_score  = excluded.composite_score,
			findings_count   = excluded.findings_count,
			updated_at       = excluded.updated_at
	`, path, gitHash, now, runID, score, findingsCount, now)
	if err != nil {
		return fmt.Errorf("set file cache entry: %w", err)
	}
	return nil
}
