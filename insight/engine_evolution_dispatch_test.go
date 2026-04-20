package insight

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	evolution_scoring "github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
)

// spyBuilder is a baseline.Builder that records how many times Build was called.
type spyBuilder struct {
	calls int64
	bundle baseline.Bundle
}

func (s *spyBuilder) Build(_ context.Context, _ string, _ config.BaselineLimits) (baseline.Bundle, error) {
	atomic.AddInt64(&s.calls, 1)
	return s.bundle, nil
}

// dispatchBundle returns a Bundle that causes at least one test_gap hypothesis
// (dir with zero test ratio triggers the test_gap generator).
func dispatchBundle() baseline.Bundle {
	return baseline.Bundle{
		ProjectRoot: ".",
		TestRatios: []baseline.TestRatio{
			{Dir: "api", SourceFiles: 10, TestFiles: 0, Ratio: 0.0},
		},
	}
}

// dispatchEvoCfg returns a EvolutionConfig for dispatch tests.
func dispatchEvoCfg(stratusSelf bool) config.EvolutionConfig {
	return config.EvolutionConfig{
		Enabled:             true,
		StratusSelfEnabled:  stratusSelf,
		MaxTokensPerCycle:   1000,
		MaxHypothesesPerRun: 5,
		TimeoutMs:           5000,
		AllowedEvolutionCategories: []string{
			"test_gap",
			"refactor_opportunity",
			"doc_drift",
			"architecture_drift",
			"feature_idea",
			"dx_improvement",
		},
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

// makeDispatchEngine builds a minimal Engine with the given evoCfg and injects
// an EvolutionLoop that uses the supplied spy builder.
func makeDispatchEngine(t *testing.T, evoCfg config.EvolutionConfig, spy *spyBuilder) *Engine {
	t.Helper()
	database := setupTestDB(t)

	store := evolution_loop.NewDBEvolutionStore(database)
	writer := evolution_loop.NewProposalWriter(database)
	staticScorer := evolution_scoring.NewStaticScorer()

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return evoCfg },
		nil, // no LLM client — static-only scoring
		evolution_loop.WithBaselineBuilder(spy),
		evolution_loop.WithStaticScorer(staticScorer),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	e := &Engine{
		database:      database,
		evoCfg:        evoCfg,
		evolutionLoop: loop,
	}
	return e
}

// TestRunEvolutionCycle_TargetPathDispatch verifies that with StratusSelfEnabled=false
// RunEvolutionCycle routes to RunTargetCycle: result is non-nil,
// HypothesesTested >= 1, and the spy builder was called.
func TestRunEvolutionCycle_TargetPathDispatch(t *testing.T) {
	evoCfg := dispatchEvoCfg(false)
	spy := &spyBuilder{bundle: dispatchBundle()}
	e := makeDispatchEngine(t, evoCfg, spy)

	result, err := e.RunEvolutionCycle(context.Background(), "manual", 0, nil)
	if err != nil {
		t.Fatalf("RunEvolutionCycle: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil RunResult")
	}
	if result.HypothesesTested < 1 {
		t.Errorf("expected HypothesesTested >= 1, got %d", result.HypothesesTested)
	}

	// Target path calls RunCycle which calls the builder.
	if atomic.LoadInt64(&spy.calls) == 0 {
		t.Error("expected spy builder to be called at least once on target path")
	}

	// Target path never produces prompt_tuning hypotheses.
	hyps, err := e.database.ListEvolutionHypotheses(result.RunID)
	if err != nil {
		t.Fatalf("ListEvolutionHypotheses: %v", err)
	}
	for _, h := range hyps {
		if h.Category == "prompt_tuning" {
			t.Errorf("found prompt_tuning hypothesis on target path: id=%s", h.ID)
		}
	}
}

// TestRunEvolutionCycle_StratusSelfEnabledRoutesToLegacyPath verifies that with
// StratusSelfEnabled=true RunEvolutionCycle routes to the legacy Run() path:
// the baseline builder is NOT called (spy.calls == 0).
func TestRunEvolutionCycle_StratusSelfEnabledRoutesToLegacyPath(t *testing.T) {
	evoCfg := dispatchEvoCfg(true)
	spy := &spyBuilder{bundle: dispatchBundle()}
	e := makeDispatchEngine(t, evoCfg, spy)

	// The legacy Run() path requires HypothesisGenerator/Evaluator/ExperimentRunner
	// which are nil here — it will fail or return an empty result; we only care
	// that the builder was NOT called (proving target path was skipped).
	_, _ = e.RunEvolutionCycle(context.Background(), "manual", 0, nil)

	if atomic.LoadInt64(&spy.calls) != 0 {
		t.Errorf("expected spy builder NOT to be called on legacy path, got %d calls", atomic.LoadInt64(&spy.calls))
	}
}
