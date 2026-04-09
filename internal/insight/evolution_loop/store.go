package evolution_loop

import (
	"fmt"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// EvolutionStore abstracts evolution persistence for testability.
type EvolutionStore interface {
	SaveRun(r *db.EvolutionRun) error
	GetRun(id string) (*db.EvolutionRun, error)
	ListRuns(f db.EvolutionRunFilters) ([]db.EvolutionRun, int, error)
	UpdateRun(r *db.EvolutionRun) error
	GetActiveRun() (*db.EvolutionRun, error)

	SaveHypothesis(h *db.EvolutionHypothesis) error
	GetHypothesis(id string) (*db.EvolutionHypothesis, error)
	ListHypotheses(runID string) ([]db.EvolutionHypothesis, error)
	UpdateHypothesis(h *db.EvolutionHypothesis) error
}

// DBEvolutionStore delegates to *db.DB.
type DBEvolutionStore struct {
	db *db.DB
}

// NewDBEvolutionStore constructs a DBEvolutionStore backed by the given database.
func NewDBEvolutionStore(database *db.DB) *DBEvolutionStore {
	return &DBEvolutionStore{db: database}
}

func (s *DBEvolutionStore) SaveRun(r *db.EvolutionRun) error {
	if err := s.db.SaveEvolutionRun(r); err != nil {
		return fmt.Errorf("save evolution run: %w", err)
	}
	return nil
}

func (s *DBEvolutionStore) GetRun(id string) (*db.EvolutionRun, error) {
	run, err := s.db.GetEvolutionRun(id)
	if err != nil {
		return nil, fmt.Errorf("get evolution run: %w", err)
	}
	return run, nil
}

func (s *DBEvolutionStore) ListRuns(f db.EvolutionRunFilters) ([]db.EvolutionRun, int, error) {
	runs, total, err := s.db.ListEvolutionRuns(f)
	if err != nil {
		return nil, 0, fmt.Errorf("list evolution runs: %w", err)
	}
	return runs, total, nil
}

func (s *DBEvolutionStore) UpdateRun(r *db.EvolutionRun) error {
	if err := s.db.UpdateEvolutionRun(r); err != nil {
		return fmt.Errorf("update evolution run: %w", err)
	}
	return nil
}

func (s *DBEvolutionStore) GetActiveRun() (*db.EvolutionRun, error) {
	run, err := s.db.GetActiveEvolutionRun()
	if err != nil {
		return nil, fmt.Errorf("get active evolution run: %w", err)
	}
	return run, nil
}

func (s *DBEvolutionStore) SaveHypothesis(h *db.EvolutionHypothesis) error {
	if err := s.db.SaveEvolutionHypothesis(h); err != nil {
		return fmt.Errorf("save evolution hypothesis: %w", err)
	}
	return nil
}

func (s *DBEvolutionStore) GetHypothesis(id string) (*db.EvolutionHypothesis, error) {
	h, err := s.db.GetEvolutionHypothesis(id)
	if err != nil {
		return nil, fmt.Errorf("get evolution hypothesis: %w", err)
	}
	return h, nil
}

func (s *DBEvolutionStore) ListHypotheses(runID string) ([]db.EvolutionHypothesis, error) {
	hypotheses, err := s.db.ListEvolutionHypotheses(runID)
	if err != nil {
		return nil, fmt.Errorf("list evolution hypotheses: %w", err)
	}
	return hypotheses, nil
}

func (s *DBEvolutionStore) UpdateHypothesis(h *db.EvolutionHypothesis) error {
	if err := s.db.UpdateEvolutionHypothesis(h); err != nil {
		return fmt.Errorf("update evolution hypothesis: %w", err)
	}
	return nil
}
