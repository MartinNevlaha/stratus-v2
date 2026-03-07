package openclaw

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type Scheduler struct {
	engine  *Engine
	ticker  *time.Ticker
	running bool
	mu      sync.Mutex
}

func newScheduler(engine *Engine) *Scheduler {
	return &Scheduler{
		engine: engine,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("scheduler already running")
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	interval := s.engine.config.Interval
	if interval <= 0 {
		interval = 1
	}

	s.ticker = time.NewTicker(time.Duration(interval) * time.Hour)
	defer s.ticker.Stop()

	slog.Info("openclaw: scheduler started", "interval_h", interval)

	for {
		select {
		case <-s.ticker.C:
			slog.Info("openclaw: scheduled analysis triggered")

			if err := s.engine.RunPatternDetection(ctx); err != nil {
				slog.Error("openclaw: pattern detection failed", "error", err)
			}

			if err := s.engine.RunProposalGeneration(ctx); err != nil {
				slog.Error("openclaw: proposal generation failed", "error", err)
			}

			if err := s.engine.RunWorkflowAnalysis(ctx); err != nil {
				slog.Error("openclaw: workflow analysis failed", "error", err)
			}

			since := time.Now().Add(-time.Duration(s.engine.config.Interval) * time.Hour)
			if built, err := s.engine.BuildRecentArtifacts(ctx, since); err != nil {
				slog.Error("openclaw: artifact building failed", "error", err)
			} else if built > 0 {
				slog.Info("openclaw: artifacts built", "count", built)
			}

			if err := s.engine.RunKnowledgeAnalysis(ctx); err != nil {
				slog.Error("openclaw: knowledge analysis failed", "error", err)
			}

			if result, err := s.engine.RunTrajectoryAnalysis(ctx); err != nil {
				slog.Error("openclaw: trajectory analysis failed", "error", err)
			} else if result != nil {
				slog.Info("openclaw: trajectory analysis complete",
					"trajectories_analyzed", result.TrajectoriesAnalyzed,
					"patterns_extracted", result.PatternsExtracted)
			}

			if synResult, err := s.engine.RunWorkflowSynthesis(ctx); err != nil {
				slog.Error("openclaw: workflow synthesis failed", "error", err)
			} else if synResult != nil {
				slog.Info("openclaw: workflow synthesis complete",
					"candidates_generated", synResult.CandidatesGenerated,
					"experiments_started", synResult.ExperimentsStarted,
					"experiments_evaluated", synResult.ExperimentsEvaluated,
					"workflows_promoted", synResult.WorkflowsPromoted,
					"workflows_rejected", synResult.WorkflowsRejected)
			}

			if err := s.engine.RunAnalysis(); err != nil {
				slog.Error("openclaw: analysis failed", "error", err)
			}
		case <-ctx.Done():
			slog.Info("openclaw: scheduler stopped")
			return ctx.Err()
		}
	}
}
