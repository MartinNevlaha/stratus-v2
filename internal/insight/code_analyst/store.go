package code_analyst

import (
	"fmt"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// CodeAnalystStore abstracts persistence for the code analyst subsystem.
type CodeAnalystStore interface {
	// Runs
	SaveRun(r *db.CodeAnalysisRun) error
	GetRun(id string) (*db.CodeAnalysisRun, error)
	ListRuns(limit, offset int) ([]db.CodeAnalysisRun, int, error)
	UpdateRun(r *db.CodeAnalysisRun) error

	// Findings
	SaveFinding(f *db.CodeFinding) error
	ListFindings(filters db.CodeFindingFilters) ([]db.CodeFinding, int, error)
	SearchFindings(query string, limit int) ([]db.CodeFinding, error)

	// Metrics
	SaveMetric(m *db.CodeQualityMetric) error
	ListMetrics(days int) ([]db.CodeQualityMetric, error)

	// File cache
	GetFileCache(path string) (*db.FileCacheEntry, error)
	SetFileCache(path, gitHash, runID string, score float64, findingsCount int) error
}

// DBCodeAnalystStore delegates to db.DB.
type DBCodeAnalystStore struct {
	db *db.DB
}

// NewDBCodeAnalystStore constructs a DBCodeAnalystStore backed by the given database.
func NewDBCodeAnalystStore(d *db.DB) *DBCodeAnalystStore {
	return &DBCodeAnalystStore{db: d}
}

func (s *DBCodeAnalystStore) SaveRun(r *db.CodeAnalysisRun) error {
	if err := s.db.SaveCodeAnalysisRun(r); err != nil {
		return fmt.Errorf("code analyst store: save run: %w", err)
	}
	return nil
}

func (s *DBCodeAnalystStore) GetRun(id string) (*db.CodeAnalysisRun, error) {
	run, err := s.db.GetCodeAnalysisRun(id)
	if err != nil {
		return nil, fmt.Errorf("code analyst store: get run: %w", err)
	}
	return run, nil
}

func (s *DBCodeAnalystStore) ListRuns(limit, offset int) ([]db.CodeAnalysisRun, int, error) {
	runs, total, err := s.db.ListCodeAnalysisRuns(limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("code analyst store: list runs: %w", err)
	}
	return runs, total, nil
}

func (s *DBCodeAnalystStore) UpdateRun(r *db.CodeAnalysisRun) error {
	if err := s.db.UpdateCodeAnalysisRun(r); err != nil {
		return fmt.Errorf("code analyst store: update run: %w", err)
	}
	return nil
}

func (s *DBCodeAnalystStore) SaveFinding(f *db.CodeFinding) error {
	if err := s.db.SaveCodeFinding(f); err != nil {
		return fmt.Errorf("code analyst store: save finding: %w", err)
	}
	return nil
}

func (s *DBCodeAnalystStore) ListFindings(filters db.CodeFindingFilters) ([]db.CodeFinding, int, error) {
	findings, total, err := s.db.ListCodeFindings(filters)
	if err != nil {
		return nil, 0, fmt.Errorf("code analyst store: list findings: %w", err)
	}
	return findings, total, nil
}

func (s *DBCodeAnalystStore) SearchFindings(query string, limit int) ([]db.CodeFinding, error) {
	findings, err := s.db.SearchCodeFindings(query, limit)
	if err != nil {
		return nil, fmt.Errorf("code analyst store: search findings: %w", err)
	}
	return findings, nil
}

func (s *DBCodeAnalystStore) SaveMetric(m *db.CodeQualityMetric) error {
	if err := s.db.SaveCodeQualityMetric(m); err != nil {
		return fmt.Errorf("code analyst store: save metric: %w", err)
	}
	return nil
}

func (s *DBCodeAnalystStore) ListMetrics(days int) ([]db.CodeQualityMetric, error) {
	metrics, err := s.db.ListCodeQualityMetrics(days)
	if err != nil {
		return nil, fmt.Errorf("code analyst store: list metrics: %w", err)
	}
	return metrics, nil
}

func (s *DBCodeAnalystStore) GetFileCache(path string) (*db.FileCacheEntry, error) {
	entry, err := s.db.GetFileCacheEntry(path)
	if err != nil {
		return nil, fmt.Errorf("code analyst store: get file cache: %w", err)
	}
	return entry, nil
}

func (s *DBCodeAnalystStore) SetFileCache(path, gitHash, runID string, score float64, findingsCount int) error {
	if err := s.db.SetFileCacheEntry(path, gitHash, runID, score, findingsCount); err != nil {
		return fmt.Errorf("code analyst store: set file cache: %w", err)
	}
	return nil
}
