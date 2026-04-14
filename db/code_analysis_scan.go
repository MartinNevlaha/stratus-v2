package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

// --- scan helpers ---

func scanCodeAnalysisRun(row *sql.Row) (*CodeAnalysisRun, error) {
	var r CodeAnalysisRun
	var metadataJSON string
	var completedAt sql.NullString

	err := row.Scan(
		&r.ID, &r.Status, &r.FilesScanned, &r.FilesAnalyzed, &r.FindingsCount,
		&r.WikiPagesCreated, &r.WikiPagesUpdated, &r.DurationMs, &r.TokensUsed,
		&r.GitCommitHash, &r.ErrorMessage, &metadataJSON,
		&r.StartedAt, &completedAt, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan code analysis run: %w", err)
	}

	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &r.Metadata); err != nil {
			log.Printf("warning: failed to parse metadata for code analysis run %s: %v", r.ID, err)
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

func scanCodeAnalysisRuns(rows *sql.Rows) ([]CodeAnalysisRun, error) {
	var runs []CodeAnalysisRun
	for rows.Next() {
		var r CodeAnalysisRun
		var metadataJSON string
		var completedAt sql.NullString

		if err := rows.Scan(
			&r.ID, &r.Status, &r.FilesScanned, &r.FilesAnalyzed, &r.FindingsCount,
			&r.WikiPagesCreated, &r.WikiPagesUpdated, &r.DurationMs, &r.TokensUsed,
			&r.GitCommitHash, &r.ErrorMessage, &metadataJSON,
			&r.StartedAt, &completedAt, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan code analysis run: %w", err)
		}

		if metadataJSON != "" {
			if err := json.Unmarshal([]byte(metadataJSON), &r.Metadata); err != nil {
				log.Printf("warning: failed to parse metadata for code analysis run %s: %v", r.ID, err)
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

func scanCodeFindings(rows *sql.Rows) ([]CodeFinding, error) {
	var findings []CodeFinding
	for rows.Next() {
		var f CodeFinding
		var evidenceJSON string
		var wikiPageID sql.NullString

		if err := rows.Scan(
			&f.ID, &f.RunID, &f.FilePath, &f.Category, &f.Severity, &f.Title, &f.Description,
			&f.LineStart, &f.LineEnd, &f.Confidence, &f.Suggestion, &wikiPageID,
			&evidenceJSON, &f.Status, &f.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan code finding: %w", err)
		}

		if evidenceJSON != "" {
			if err := json.Unmarshal([]byte(evidenceJSON), &f.Evidence); err != nil {
				log.Printf("warning: failed to parse evidence for code finding %s: %v", f.ID, err)
			}
		}
		if f.Evidence == nil {
			f.Evidence = map[string]any{}
		}

		if wikiPageID.Valid {
			f.WikiPageID = &wikiPageID.String
		}

		findings = append(findings, f)
	}
	return findings, rows.Err()
}

func scanCodeQualityMetrics(rows *sql.Rows) ([]CodeQualityMetric, error) {
	var metrics []CodeQualityMetric
	for rows.Next() {
		var m CodeQualityMetric
		var bySeverityJSON, byCategoryJSON string

		if err := rows.Scan(
			&m.ID, &m.MetricDate, &m.TotalFiles, &m.FilesAnalyzed, &m.FindingsTotal,
			&bySeverityJSON, &byCategoryJSON,
			&m.AvgChurnScore, &m.AvgCoverage, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan code quality metric: %w", err)
		}

		if bySeverityJSON != "" {
			if err := json.Unmarshal([]byte(bySeverityJSON), &m.FindingsBySeverity); err != nil {
				log.Printf("warning: failed to parse findings_by_severity for metric %s: %v", m.ID, err)
			}
		}
		if m.FindingsBySeverity == nil {
			m.FindingsBySeverity = map[string]int{}
		}

		if byCategoryJSON != "" {
			if err := json.Unmarshal([]byte(byCategoryJSON), &m.FindingsByCategory); err != nil {
				log.Printf("warning: failed to parse findings_by_category for metric %s: %v", m.ID, err)
			}
		}
		if m.FindingsByCategory == nil {
			m.FindingsByCategory = map[string]int{}
		}

		metrics = append(metrics, m)
	}
	return metrics, rows.Err()
}
