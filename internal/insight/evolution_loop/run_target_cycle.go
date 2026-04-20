package evolution_loop

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/google/uuid"
)

// RunTargetCycle executes one target-project analysis cycle with full
// evolution_runs / evolution_hypotheses bookkeeping.
//
// It is the default dispatch path (StratusSelfEnabled=false). It wraps
// RunCycle and persists the run record plus one db.EvolutionHypothesis row per
// scored hypothesis.
//
// timeoutOverrideMs, when > 0, overrides cfg.TimeoutMs for this run only.
// categoriesOverride, when non-empty, replaces cfg.AllowedEvolutionCategories
// for this run only; generators outside the override list are skipped.
func (l *EvolutionLoop) RunTargetCycle(ctx context.Context, triggerType string, timeoutOverrideMs int64, categoriesOverride []string) (*RunResult, error) {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return nil, fmt.Errorf("evolution loop: run target cycle: already running")
	}
	l.running = true
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.running = false
		l.mu.Unlock()
	}()

	cfg := l.configFn()

	// Per-request category override: replaces cfg.AllowedEvolutionCategories
	// when non-empty. Mirrors how Run() consumes `categories` in loop.go.
	if len(categoriesOverride) > 0 {
		cfg.AllowedEvolutionCategories = categoriesOverride
	}

	effectiveTimeoutMs := cfg.TimeoutMs
	if timeoutOverrideMs > 0 {
		effectiveTimeoutMs = timeoutOverrideMs
	}

	start := time.Now()

	run := &db.EvolutionRun{
		ID:          uuid.NewString(),
		TriggerType: triggerType,
		Status:      "running",
		TimeoutMs:   effectiveTimeoutMs,
		Metadata:    map[string]any{},
		StartedAt:   start.UTC().Format(time.RFC3339Nano),
	}

	if err := l.store.SaveRun(run); err != nil {
		return nil, fmt.Errorf("run target cycle: save run: %w", err)
	}

	slog.Info("evolution loop: target cycle started", "run_id", run.ID, "trigger", triggerType)

	tctx, cancel := context.WithTimeout(ctx, time.Duration(effectiveTimeoutMs)*time.Millisecond)
	defer cancel()

	_, scored, cycleErr := l.runCycleWithConfig(tctx, cfg)

	// Persist each scored hypothesis.
	for _, sh := range scored {
		h := &db.EvolutionHypothesis{
			ID:          uuid.NewString(),
			RunID:       run.ID,
			Category:    sh.Hypothesis.Category,
			Description: sh.Hypothesis.Title,
			Evidence: map[string]any{
				"signal_refs": sh.Hypothesis.SignalRefs,
				"rationale":   sh.Hypothesis.Rationale,
				"scores": map[string]any{
					"final":  sh.Final,
					"static": sh.Static,
					"llm":    sh.LLM,
				},
			},
		}
		if err := l.store.SaveHypothesis(h); err != nil {
			slog.Error("evolution loop: target cycle: save hypothesis failed",
				"run_id", run.ID, "category", sh.Hypothesis.Category, "err", err)
		}
	}

	// Finalise run record.
	durationMs := time.Since(start).Milliseconds()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	run.DurationMs = durationMs
	run.CompletedAt = &now
	run.HypothesesCount = len(scored)

	if cycleErr != nil {
		run.Status = "failed"
		run.ErrorMessage = cycleErr.Error()
	} else {
		run.Status = "completed"
	}

	if err := l.store.UpdateRun(run); err != nil {
		slog.Error("evolution loop: target cycle: update run failed", "run_id", run.ID, "err", err)
	}

	slog.Info("evolution loop: target cycle finished",
		"run_id", run.ID, "status", run.Status, "duration_ms", durationMs)

	if cycleErr != nil {
		return nil, fmt.Errorf("run target cycle: %w", cycleErr)
	}
	return &RunResult{
		RunID:            run.ID,
		HypothesesTested: len(scored),
		DurationMs:       durationMs,
	}, nil
}
