package evolution_loop_test

import (
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
)

func openTestDatabase(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func makeTestRun() *db.EvolutionRun {
	return &db.EvolutionRun{
		TriggerType: "test",
		Status:      "running",
		TimeoutMs:   10000,
		Metadata:    map[string]any{},
	}
}

// ---------------------------------------------------------------------------
// Run CRUD via DBEvolutionStore
// ---------------------------------------------------------------------------

func TestDBEvolutionStore_SaveAndGetRun(t *testing.T) {
	database := openTestDatabase(t)
	store := evolution_loop.NewDBEvolutionStore(database)

	run := makeTestRun()
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if run.ID == "" {
		t.Fatal("expected ID to be generated after save")
	}

	got, err := store.GetRun(run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil run")
	}
	if got.TriggerType != "test" {
		t.Errorf("trigger_type = %q, want test", got.TriggerType)
	}
}

func TestDBEvolutionStore_ListRuns(t *testing.T) {
	database := openTestDatabase(t)
	store := evolution_loop.NewDBEvolutionStore(database)

	for i := 0; i < 3; i++ {
		run := makeTestRun()
		if err := store.SaveRun(run); err != nil {
			t.Fatalf("SaveRun[%d]: %v", i, err)
		}
	}

	runs, total, err := store.ListRuns(db.EvolutionRunFilters{Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Errorf("len = %d, want 3", len(runs))
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
}

func TestDBEvolutionStore_UpdateRun(t *testing.T) {
	database := openTestDatabase(t)
	store := evolution_loop.NewDBEvolutionStore(database)

	run := makeTestRun()
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	run.Status = "completed"
	if err := store.UpdateRun(run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	got, err := store.GetRun(run.ID)
	if err != nil {
		t.Fatalf("GetRun after update: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("status = %q, want completed", got.Status)
	}
}

func TestDBEvolutionStore_GetActiveRun_NoneRunning(t *testing.T) {
	database := openTestDatabase(t)
	store := evolution_loop.NewDBEvolutionStore(database)

	// No runs in DB — GetActiveRun should return nil, nil (not an error).
	got, err := store.GetActiveRun()
	if err != nil {
		t.Fatalf("GetActiveRun: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil when no active run, got: %+v", got)
	}
}

func TestDBEvolutionStore_GetActiveRun_WithRunning(t *testing.T) {
	database := openTestDatabase(t)
	store := evolution_loop.NewDBEvolutionStore(database)

	run := makeTestRun()
	run.Status = "running"
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	got, err := store.GetActiveRun()
	if err != nil {
		t.Fatalf("GetActiveRun: %v", err)
	}
	if got == nil {
		t.Fatal("expected active run, got nil")
	}
	if got.ID != run.ID {
		t.Errorf("active run ID = %q, want %q", got.ID, run.ID)
	}
}

// ---------------------------------------------------------------------------
// Hypothesis CRUD via DBEvolutionStore
// ---------------------------------------------------------------------------

func TestDBEvolutionStore_SaveAndGetHypothesis(t *testing.T) {
	database := openTestDatabase(t)

	run := makeTestRun()
	if err := database.SaveEvolutionRun(run); err != nil {
		t.Fatalf("SaveEvolutionRun: %v", err)
	}

	store := evolution_loop.NewDBEvolutionStore(database)

	h := &db.EvolutionHypothesis{
		RunID:          run.ID,
		Category:       "workflow_routing",
		Description:    "test hypothesis",
		BaselineValue:  "0.80",
		ProposedValue:  "0.75",
		Metric:         "accuracy",
		BaselineMetric: 0.80,
		Evidence:       map[string]any{"key": "value"},
	}

	if err := store.SaveHypothesis(h); err != nil {
		t.Fatalf("SaveHypothesis: %v", err)
	}
	if h.ID == "" {
		t.Fatal("expected ID to be generated after save")
	}

	got, err := store.GetHypothesis(h.ID)
	if err != nil {
		t.Fatalf("GetHypothesis: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil hypothesis")
	}
	if got.Category != "workflow_routing" {
		t.Errorf("category = %q, want workflow_routing", got.Category)
	}
	if got.Description != "test hypothesis" {
		t.Errorf("description = %q, want test hypothesis", got.Description)
	}
}

func TestDBEvolutionStore_ListHypotheses(t *testing.T) {
	database := openTestDatabase(t)

	run := makeTestRun()
	if err := database.SaveEvolutionRun(run); err != nil {
		t.Fatalf("SaveEvolutionRun: %v", err)
	}

	store := evolution_loop.NewDBEvolutionStore(database)

	for i := 0; i < 3; i++ {
		h := &db.EvolutionHypothesis{
			RunID:          run.ID,
			Category:       "agent_selection",
			Description:    "hypothesis",
			BaselineValue:  "v1",
			ProposedValue:  "v2",
			Metric:         "accuracy",
			BaselineMetric: 0.70,
			Evidence:       map[string]any{},
		}
		if err := store.SaveHypothesis(h); err != nil {
			t.Fatalf("SaveHypothesis[%d]: %v", i, err)
		}
	}

	hypotheses, err := store.ListHypotheses(run.ID)
	if err != nil {
		t.Fatalf("ListHypotheses: %v", err)
	}
	if len(hypotheses) != 3 {
		t.Errorf("len = %d, want 3", len(hypotheses))
	}
}

func TestDBEvolutionStore_UpdateHypothesis(t *testing.T) {
	database := openTestDatabase(t)

	run := makeTestRun()
	if err := database.SaveEvolutionRun(run); err != nil {
		t.Fatalf("SaveEvolutionRun: %v", err)
	}

	store := evolution_loop.NewDBEvolutionStore(database)

	h := &db.EvolutionHypothesis{
		RunID:          run.ID,
		Category:       "threshold_adjustment",
		Description:    "original",
		BaselineValue:  "0.85",
		ProposedValue:  "0.80",
		Metric:         "accuracy",
		BaselineMetric: 0.85,
		Evidence:       map[string]any{},
	}
	if err := store.SaveHypothesis(h); err != nil {
		t.Fatalf("SaveHypothesis: %v", err)
	}

	h.Decision = "accepted"
	h.ExperimentMetric = 0.88
	if err := store.UpdateHypothesis(h); err != nil {
		t.Fatalf("UpdateHypothesis: %v", err)
	}

	got, err := store.GetHypothesis(h.ID)
	if err != nil {
		t.Fatalf("GetHypothesis after update: %v", err)
	}
	if got.Decision != "accepted" {
		t.Errorf("decision = %q, want accepted", got.Decision)
	}
}
