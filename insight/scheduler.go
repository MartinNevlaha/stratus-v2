package insight

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

func normalizeIntervalHours(interval int) int {
	if interval <= 0 {
		return 1
	}
	return interval
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

	interval := normalizeIntervalHours(s.engine.config.Interval)

	s.ticker = time.NewTicker(time.Duration(interval) * time.Hour)
	defer s.ticker.Stop()

	slog.Info("insight: scheduler started", "interval_h", interval)

	for {
		select {
		case <-s.ticker.C:
			slog.Info("insight: scheduled analysis triggered")

			if err := s.engine.RunPatternDetection(ctx); err != nil {
				slog.Error("insight: pattern detection failed", "error", err)
			}

			if err := s.engine.RunProposalGeneration(ctx); err != nil {
				slog.Error("insight: proposal generation failed", "error", err)
			}

			if err := s.engine.RunWorkflowAnalysis(ctx); err != nil {
				slog.Error("insight: workflow analysis failed", "error", err)
			}

			since := time.Now().Add(-time.Duration(interval) * time.Hour)
			if built, err := s.engine.BuildRecentArtifacts(ctx, since); err != nil {
				slog.Error("insight: artifact building failed", "error", err)
			} else if built > 0 {
				slog.Info("insight: artifacts built", "count", built)
			}

			if err := s.engine.RunKnowledgeAnalysis(ctx); err != nil {
				slog.Error("insight: knowledge analysis failed", "error", err)
			}

			if result, err := s.engine.RunTrajectoryAnalysis(ctx); err != nil {
				slog.Error("insight: trajectory analysis failed", "error", err)
			} else if result != nil {
				slog.Info("insight: trajectory analysis complete",
					"trajectories_analyzed", result.TrajectoriesAnalyzed,
					"patterns_extracted", result.PatternsExtracted)
			}

			if synResult, err := s.engine.RunWorkflowSynthesis(ctx); err != nil {
				slog.Error("insight: workflow synthesis failed", "error", err)
			} else if synResult != nil {
				slog.Info("insight: workflow synthesis complete",
					"candidates_generated", synResult.CandidatesGenerated,
					"experiments_started", synResult.ExperimentsStarted,
					"experiments_evaluated", synResult.ExperimentsEvaluated,
					"workflows_promoted", synResult.WorkflowsPromoted,
					"workflows_rejected", synResult.WorkflowsRejected)
			}

			if err := s.engine.RunAnalysis(); err != nil {
				slog.Error("insight: analysis failed", "error", err)
			}

			// Wiki ingest and maintenance
			if s.engine.wikiCfg.Enabled {
				if result, err := s.engine.RunWikiIngest(ctx); err != nil {
					slog.Error("insight: wiki ingest failed", "error", err)
				} else if result != nil {
					slog.Info("insight: wiki ingest complete", "created", result.PagesCreated, "updated", result.PagesUpdated)
				}

				if mResult, err := s.engine.RunWikiMaintenance(ctx); err != nil {
					slog.Error("insight: wiki maintenance failed", "error", err)
				} else if mResult != nil {
					slog.Info("insight: wiki maintenance complete", "scored", mResult.PagesScored, "stale", mResult.PagesMarkedStale)
				}
			}

			// Evolution loop
			if s.engine.evoCfg.Enabled {
				if evoResult, err := s.engine.RunEvolutionCycle(ctx, "scheduled", 0, nil); err != nil {
					slog.Error("insight: evolution cycle failed", "error", err)
				} else if evoResult != nil {
					slog.Info("insight: evolution complete", "hypotheses", evoResult.HypothesesTested, "auto_applied", evoResult.AutoApplied)
				}
			}
		case <-ctx.Done():
			slog.Info("insight: scheduler stopped")
			return ctx.Err()
		}
	}
}
