package evolution_loop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

// targetCycleConfig returns a config suited for RunTargetCycle tests.
func targetCycleConfig() config.EvolutionConfig {
	return config.EvolutionConfig{
		Enabled:             true,
		MaxTokensPerCycle:   10000,
		MaxHypothesesPerRun: 5,
		TimeoutMs:           5000,
		AllowedEvolutionCategories: []string{
			"test_gap",
			"refactor_opportunity",
			"doc_drift",
		},
		StratusSelfEnabled: false,
		ScoringWeights: config.ScoringWeights{
			Churn:                 0.2,
			TestGap:               0.2,
			TODO:                  0.1,
			Staleness:             0.1,
			ADRViolation:          0.1,
			LLMImpact:             0.15,
			LLMEffort:             0.05,
			LLMConfidence:         0.05,
			LLMNovelty:            0.05,
			MaxTokensPerJudgeCall: 4000,
		},
		BaselineLimits: config.BaselineLimits{
			VexorTopK:     5,
			GitLogCommits: 10,
			TODOMax:       5,
		},
	}
}

// bundleForTargetCycle returns a bundle that triggers at least one test_gap hypothesis.
func bundleForTargetCycle() baseline.Bundle {
	return baseline.Bundle{
		ProjectRoot: ".",
		TestRatios: []baseline.TestRatio{
			{Dir: "api", SourceFiles: 10, TestFiles: 0, Ratio: 0.0},
		},
	}
}

// TestRunTargetCycle_CreatesCompletedRun verifies that RunTargetCycle persists
// an evolution_runs row with Status=="completed" and a non-empty ID.
func TestRunTargetCycle_CreatesCompletedRun(t *testing.T) {
	store := newMockStore()
	cfg := targetCycleConfig()

	bldr := &mockBuilder{bundle: bundleForTargetCycle()}
	writer := &mockWriter{}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	result, err := loop.RunTargetCycle(context.Background(), "manual", 0, nil)
	if err != nil {
		t.Fatalf("RunTargetCycle: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil RunResult")
	}
	if result.RunID == "" {
		t.Error("expected non-empty RunID")
	}
	if result.DurationMs < 0 {
		t.Errorf("expected DurationMs >= 0, got %d", result.DurationMs)
	}

	run := store.latestRun()
	if run == nil {
		t.Fatal("expected run to be saved in store")
	}
	if run.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", run.Status)
	}
	if run.ID == "" {
		t.Error("expected non-empty run.ID")
	}
}

// TestRunTargetCycle_PersistsHypothesesRows verifies that one db.EvolutionHypothesis
// row is saved per scored hypothesis, with correct RunID, non-empty Category and Description.
func TestRunTargetCycle_PersistsHypothesesRows(t *testing.T) {
	store := newMockStore()
	cfg := targetCycleConfig()

	bldr := &mockBuilder{bundle: bundleForTargetCycle()}
	writer := &mockWriter{}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	result, err := loop.RunTargetCycle(context.Background(), "manual", 0, nil)
	if err != nil {
		t.Fatalf("RunTargetCycle: %v", err)
	}

	hypotheses := store.allHypotheses()
	if len(hypotheses) != result.HypothesesTested {
		t.Errorf("stored hypotheses count (%d) != HypothesesTested (%d)",
			len(hypotheses), result.HypothesesTested)
	}

	for _, h := range hypotheses {
		if h.RunID != result.RunID {
			t.Errorf("hypothesis RunID %q != run ID %q", h.RunID, result.RunID)
		}
		if h.Category == "" {
			t.Error("expected non-empty Category on hypothesis")
		}
		if h.Description == "" {
			t.Error("expected non-empty Description on hypothesis")
		}
	}
}

// TestRunTargetCycle_HypothesesCountMatchesRun verifies that the persisted run
// row has HypothesesCount matching len(scored).
func TestRunTargetCycle_HypothesesCountMatchesRun(t *testing.T) {
	store := newMockStore()
	cfg := targetCycleConfig()

	bldr := &mockBuilder{bundle: bundleForTargetCycle()}
	writer := &mockWriter{}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	result, err := loop.RunTargetCycle(context.Background(), "scheduled", 0, nil)
	if err != nil {
		t.Fatalf("RunTargetCycle: %v", err)
	}

	run := store.latestRun()
	if run == nil {
		t.Fatal("expected run record")
	}
	if run.HypothesesCount != result.HypothesesTested {
		t.Errorf("run.HypothesesCount=%d != HypothesesTested=%d",
			run.HypothesesCount, result.HypothesesTested)
	}
}

// TestRunTargetCycle_ZeroPromptTuningRows verifies that with StratusSelfEnabled=false,
// no hypothesis with Category=="prompt_tuning" is persisted.
func TestRunTargetCycle_ZeroPromptTuningRows(t *testing.T) {
	store := newMockStore()
	cfg := targetCycleConfig()
	cfg.StratusSelfEnabled = false

	bldr := &mockBuilder{bundle: bundleForTargetCycle()}
	writer := &mockWriter{}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	_, err := loop.RunTargetCycle(context.Background(), "manual", 0, nil)
	if err != nil {
		t.Fatalf("RunTargetCycle: %v", err)
	}

	for _, h := range store.allHypotheses() {
		if h.Category == "prompt_tuning" {
			t.Errorf("found prompt_tuning hypothesis with StratusSelfEnabled=false: %s", h.ID)
		}
	}
}

// TestRunTargetCycle_FailsWhenRunCycleErrors verifies that when the baseline
// builder returns an error, the run row is marked "failed" and ErrorMessage is set.
func TestRunTargetCycle_FailsWhenRunCycleErrors(t *testing.T) {
	store := newMockStore()
	cfg := targetCycleConfig()

	bldr := &mockBuilder{err: errors.New("baseline unavailable")}
	writer := &mockWriter{}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	_, err := loop.RunTargetCycle(context.Background(), "manual", 0, nil)
	if err == nil {
		t.Fatal("expected error when RunCycle fails")
	}

	run := store.latestRun()
	if run == nil {
		t.Fatal("expected run record to be saved even on failure")
	}
	if run.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", run.Status)
	}
	if run.ErrorMessage == "" {
		t.Error("expected non-empty ErrorMessage on failed run")
	}
}

// TestRunTargetCycle_HonorsCategoriesOverride verifies that when categoriesOverride
// is non-empty, only hypotheses belonging to the overridden categories are persisted.
func TestRunTargetCycle_HonorsCategoriesOverride(t *testing.T) {
	store := newMockStore()
	cfg := targetCycleConfig()
	// cfg has AllowedEvolutionCategories with test_gap, refactor_opportunity, doc_drift.
	// The override should restrict to test_gap only.

	bldr := &mockBuilder{bundle: bundleForTargetCycle()}
	writer := &mockWriter{}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	_, err := loop.RunTargetCycle(context.Background(), "manual", 0, []string{"test_gap"})
	if err != nil {
		t.Fatalf("RunTargetCycle: %v", err)
	}

	hypotheses := store.allHypotheses()
	if len(hypotheses) == 0 {
		t.Fatal("expected at least one hypothesis")
	}
	for _, h := range hypotheses {
		if h.Category != "test_gap" {
			t.Errorf("expected only test_gap hypotheses, got category %q (id=%s)", h.Category, h.ID)
		}
	}
}
