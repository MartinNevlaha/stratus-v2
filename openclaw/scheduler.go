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

			if err := s.engine.RunAnalysis(); err != nil {
				slog.Error("openclaw: analysis failed", "error", err)
			}
		case <-ctx.Done():
			slog.Info("openclaw: scheduler stopped")
			return ctx.Err()
		}
	}
}
