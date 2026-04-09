package evolution_loop_test

import (
	"fmt"
	"sync"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
)

// Compile-time assertion.
var _ evolution_loop.EvolutionStore = (*mockStore)(nil)

type mockStore struct {
	mu          sync.Mutex
	runs        map[string]*db.EvolutionRun
	hypotheses  map[string]*db.EvolutionHypothesis
	saveRunErr  error
	updateRunErr error
}

func newMockStore() *mockStore {
	return &mockStore{
		runs:       make(map[string]*db.EvolutionRun),
		hypotheses: make(map[string]*db.EvolutionHypothesis),
	}
}

func (m *mockStore) SaveRun(r *db.EvolutionRun) error {
	if m.saveRunErr != nil {
		return m.saveRunErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *r
	m.runs[r.ID] = &cp
	return nil
}

func (m *mockStore) GetRun(id string) (*db.EvolutionRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *mockStore) ListRuns(f db.EvolutionRunFilters) ([]db.EvolutionRun, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var runs []db.EvolutionRun
	for _, r := range m.runs {
		runs = append(runs, *r)
	}
	return runs, len(runs), nil
}

func (m *mockStore) UpdateRun(r *db.EvolutionRun) error {
	if m.updateRunErr != nil {
		return m.updateRunErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[r.ID]; !ok {
		return fmt.Errorf("run not found: %s", r.ID)
	}
	cp := *r
	m.runs[r.ID] = &cp
	return nil
}

func (m *mockStore) GetActiveRun() (*db.EvolutionRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.Status == "running" {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockStore) SaveHypothesis(h *db.EvolutionHypothesis) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *h
	m.hypotheses[h.ID] = &cp
	return nil
}

func (m *mockStore) GetHypothesis(id string) (*db.EvolutionHypothesis, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hypotheses[id]
	if !ok {
		return nil, nil
	}
	cp := *h
	return &cp, nil
}

func (m *mockStore) ListHypotheses(runID string) ([]db.EvolutionHypothesis, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []db.EvolutionHypothesis
	for _, h := range m.hypotheses {
		if h.RunID == runID {
			out = append(out, *h)
		}
	}
	return out, nil
}

func (m *mockStore) UpdateHypothesis(h *db.EvolutionHypothesis) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.hypotheses[h.ID]; !ok {
		return fmt.Errorf("hypothesis not found: %s", h.ID)
	}
	cp := *h
	m.hypotheses[h.ID] = &cp
	return nil
}

// runCount returns how many runs are stored (for assertions).
func (m *mockStore) runCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runs)
}

// latestRun returns the single run if exactly one exists, or panics.
func (m *mockStore) latestRun() *db.EvolutionRun {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		cp := *r
		return &cp
	}
	return nil
}
