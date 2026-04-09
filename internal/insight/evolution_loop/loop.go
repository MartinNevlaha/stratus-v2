package evolution_loop

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/google/uuid"
)

// RunResult summarises the outcome of a completed evolution loop run.
type RunResult struct {
	RunID            string
	HypothesesTested int
	AutoApplied      int
	WikiPagesUpdated int
	DurationMs       int64
}

// EvolutionLoop orchestrates hypothesis generation, experiment execution,
// evaluation, and persistence for a single time-bounded evolution cycle.
type EvolutionLoop struct {
	store     EvolutionStore
	configFn  func() config.EvolutionConfig
	llmClient llm.Client
	mu        sync.Mutex
	running   bool
}

// NewEvolutionLoop constructs an EvolutionLoop.
// llmClient may be nil; a nil value means "no LLM available, use static/simulated behavior".
func NewEvolutionLoop(store EvolutionStore, configFn func() config.EvolutionConfig, llmClient llm.Client) *EvolutionLoop {
	return &EvolutionLoop{store: store, configFn: configFn, llmClient: llmClient}
}

// IsRunning reports whether a run is currently in progress. Thread-safe.
func (l *EvolutionLoop) IsRunning() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running
}

// Run executes one full evolution cycle. It returns an error if a run is
// already in progress, if persisting the run record fails, or if the context
// is cancelled before the cycle begins.
//
// timeoutOverrideMs, when > 0, overrides the configured TimeoutMs for this
// run only. categories, when non-empty, overrides the configured Categories
// for this run only. Pass 0 and nil to use the values from config.
//
// Internally it uses context.WithTimeout derived from the effective TimeoutMs
// so that individual experiments are cancelled automatically when the budget
// expires.
func (l *EvolutionLoop) Run(ctx context.Context, triggerType string, timeoutOverrideMs int64, categories []string) (*RunResult, error) {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return nil, fmt.Errorf("evolution loop: run: already running")
	}
	l.running = true
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.running = false
		l.mu.Unlock()
	}()

	cfg := l.configFn()

	// Apply per-call overrides.
	if timeoutOverrideMs > 0 {
		cfg.TimeoutMs = timeoutOverrideMs
	}
	if len(categories) > 0 {
		cfg.Categories = categories
	}

	start := time.Now()

	run := &db.EvolutionRun{
		ID:          uuid.NewString(),
		TriggerType: triggerType,
		Status:      "running",
		TimeoutMs:   cfg.TimeoutMs,
		Metadata:    map[string]any{},
		StartedAt:   start.UTC().Format(time.RFC3339Nano),
	}

	if err := l.store.SaveRun(run); err != nil {
		return nil, fmt.Errorf("evolution loop: save run: %w", err)
	}

	slog.Info("evolution loop started", "run_id", run.ID, "trigger", triggerType)

	tctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
	defer cancel()

	result, finalStatus, runErr := l.execute(tctx, run, cfg)

	// Always persist final state.
	durationMs := time.Since(start).Milliseconds()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	run.Status = finalStatus
	run.DurationMs = durationMs
	run.CompletedAt = &now
	if runErr != nil {
		run.ErrorMessage = runErr.Error()
	}
	if result != nil {
		run.HypothesesCount = result.HypothesesTested
		run.AutoApplied = result.AutoApplied
		run.WikiPagesUpdated = result.WikiPagesUpdated
	}

	if err := l.store.UpdateRun(run); err != nil {
		slog.Error("evolution loop: update run failed", "run_id", run.ID, "err", err)
	}

	slog.Info("evolution loop finished",
		"run_id", run.ID,
		"status", finalStatus,
		"duration_ms", durationMs,
	)

	if runErr != nil {
		return nil, fmt.Errorf("evolution loop: run: %w", runErr)
	}
	result.RunID = run.ID
	result.DurationMs = durationMs
	return result, nil
}

// execute performs the core hypothesis → experiment → evaluate pipeline.
// It returns a partial RunResult, the final status string, and any error.
func (l *EvolutionLoop) execute(
	ctx context.Context,
	run *db.EvolutionRun,
	cfg config.EvolutionConfig,
) (*RunResult, string, error) {
	generator := NewHypothesisGenerator(l.store, l.llmClient)
	runner := NewExperimentRunner(l.llmClient)
	evaluator := NewEvaluator(l.configFn)

	hypotheses, err := generator.Generate(ctx, run.ID, cfg.Categories, cfg.MaxHypothesesPerRun)
	if err != nil {
		return nil, "failed", fmt.Errorf("generate hypotheses: %w", err)
	}

	result := &RunResult{}

	for i := range hypotheses {
		// Bail out early if the timeout budget is exhausted.
		select {
		case <-ctx.Done():
			slog.Info("evolution loop: timeout reached, stopping early",
				"run_id", run.ID,
				"hypotheses_tested", result.HypothesesTested,
			)
			return result, "timeout", nil
		default:
		}

		h := &hypotheses[i]

		// Persist the hypothesis before running the experiment so we have a
		// record even if the context is cancelled mid-run.
		if err := l.store.SaveHypothesis(h); err != nil {
			slog.Error("evolution loop: save hypothesis failed", "err", err)
			continue
		}

		expResult := runner.Execute(ctx, h)
		if expResult.Error != nil {
			// Context cancelled during experiment — stop the loop.
			if ctx.Err() != nil {
				return result, "timeout", nil
			}
			slog.Error("evolution loop: experiment error", "hypothesis_id", h.ID, "err", expResult.Error)
			continue
		}

		decision, reason, confidence := evaluator.Evaluate(h, expResult, cfg)

		h.ExperimentMetric = expResult.Metric
		h.Confidence = confidence
		h.Decision = decision
		h.DecisionReason = reason

		if err := l.store.UpdateHypothesis(h); err != nil {
			slog.Error("evolution loop: update hypothesis failed", "err", err)
		}

		result.HypothesesTested++

		switch decision {
		case "auto_applied":
			result.AutoApplied++
		case "proposal_created":
			result.WikiPagesUpdated++
		}
	}

	// Update run experiment count while we still have the numbers.
	run.ExperimentsRun = result.HypothesesTested

	return result, "completed", nil
}
