package evolution_loop_test

import (
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
)

func defaultEvolutionConfig() config.EvolutionConfig {
	return config.EvolutionConfig{
		Enabled:             true,
		TimeoutMs:           5000,
		MaxHypothesesPerRun: 10,
		AutoApplyThreshold:  0.85,
		ProposalThreshold:   0.65,
		MinSampleSize:       10,
		DailyTokenBudget:    100000,
		Categories:          []string{},
	}
}

// TestEvaluate_AutoApplied_NeverTriggered verifies that auto_applied is never
// returned after T9 removed all internal categories. Any high-confidence
// experiment now results in proposal_created, not auto_applied.
func TestEvaluate_AutoApplied_NeverTriggered(t *testing.T) {
	cfg := defaultEvolutionConfig()
	evaluator := evolution_loop.NewEvaluator(func() config.EvolutionConfig { return cfg })

	// baseline=0.50, experiment=1.00 → effectRatio=1.0, sampleFactor=1.0, confidence=1.0
	// Even with confidence=1.0, internalCategories is empty so auto_applied cannot fire.
	h := &db.EvolutionHypothesis{
		Category:       "prompt_tuning",
		BaselineMetric: 0.50,
	}
	result := &evolution_loop.ExperimentResult{
		Metric:     1.00,
		SampleSize: 20,
	}

	decision, _, confidence := evaluator.Evaluate(h, result, cfg)

	if decision == "auto_applied" {
		t.Errorf("auto_applied must never be returned after T9 (confidence=%.4f)", confidence)
	}
	// High confidence → should be proposal_created.
	if decision != "proposal_created" {
		t.Errorf("expected proposal_created, got %q (confidence=%.4f)", decision, confidence)
	}
}

func TestEvaluate_ProposalCreated(t *testing.T) {
	cfg := defaultEvolutionConfig()
	evaluator := evolution_loop.NewEvaluator(func() config.EvolutionConfig { return cfg })

	// baseline=0.50, experiment=0.90 → effectRatio=0.80, sampleFactor=1.0, confidence=0.80
	// 0.80 >= ProposalThreshold(0.65).
	h := &db.EvolutionHypothesis{
		Category:       "prompt_tuning",
		BaselineMetric: 0.50,
	}
	result := &evolution_loop.ExperimentResult{
		Metric:     0.90,
		SampleSize: 20,
	}

	decision, _, confidence := evaluator.Evaluate(h, result, cfg)

	if decision != "proposal_created" {
		t.Errorf("expected proposal_created, got %q (confidence=%.4f)", decision, confidence)
	}
	if confidence < cfg.ProposalThreshold {
		t.Errorf("confidence %.4f should be >= ProposalThreshold %.4f", confidence, cfg.ProposalThreshold)
	}
}

func TestEvaluate_Rejected(t *testing.T) {
	cfg := defaultEvolutionConfig()
	evaluator := evolution_loop.NewEvaluator(func() config.EvolutionConfig { return cfg })

	// baseline=0.80, experiment=0.82 → effectRatio=0.025, sampleFactor=1.0, confidence=0.025 < 0.65
	h := &db.EvolutionHypothesis{
		Category:       "prompt_tuning",
		BaselineMetric: 0.80,
	}
	result := &evolution_loop.ExperimentResult{
		Metric:     0.82,
		SampleSize: 20,
	}

	decision, _, confidence := evaluator.Evaluate(h, result, cfg)

	if decision != "rejected" {
		t.Errorf("expected rejected, got %q (confidence=%.4f)", decision, confidence)
	}
	if confidence >= cfg.ProposalThreshold {
		t.Errorf("confidence %.4f should be < ProposalThreshold %.4f", confidence, cfg.ProposalThreshold)
	}
}

func TestEvaluate_Inconclusive(t *testing.T) {
	cfg := defaultEvolutionConfig()
	evaluator := evolution_loop.NewEvaluator(func() config.EvolutionConfig { return cfg })

	h := &db.EvolutionHypothesis{
		Category:       "prompt_tuning",
		BaselineMetric: 0.50,
	}
	// SampleSize < MinSampleSize(10)
	result := &evolution_loop.ExperimentResult{
		Metric:     1.00,
		SampleSize: 3,
	}

	decision, _, confidence := evaluator.Evaluate(h, result, cfg)

	if decision != "inconclusive" {
		t.Errorf("expected inconclusive, got %q", decision)
	}
	if confidence != 0 {
		t.Errorf("expected confidence 0 for inconclusive, got %.4f", confidence)
	}
}

// TestEvaluate_AutoApply_BlockedForAllCategories verifies that auto_applied is
// never produced for any category now that internalCategories is empty (T9).
func TestEvaluate_AutoApply_BlockedForAllCategories(t *testing.T) {
	cfg := defaultEvolutionConfig()
	evaluator := evolution_loop.NewEvaluator(func() config.EvolutionConfig { return cfg })

	categories := []string{
		"prompt_tuning",
		"workflow_routing",    // legacy
		"agent_selection",     // legacy
		"threshold_adjustment", // legacy
	}

	for _, cat := range categories {
		h := &db.EvolutionHypothesis{
			Category:       cat,
			BaselineMetric: 0.10,
		}
		result := &evolution_loop.ExperimentResult{
			Metric:     1.00,
			SampleSize: 100,
		}

		decision, _, _ := evaluator.Evaluate(h, result, cfg)

		if decision == "auto_applied" {
			t.Errorf("[%s] auto_applied must not be returned after T9", cat)
		}
	}
}
