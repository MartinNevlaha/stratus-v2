// Package scheduler provides a shared ticker-loop primitive used by long-lived
// background observers (Guardian, Insight). It centralises the concerns those
// observers all had to re-implement: hot-reloading the interval, single-flight
// protection, and graceful shutdown on ctx cancellation.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// IntervalFn returns the desired tick interval at the moment it is called.
// The scheduler invokes it once at startup and again on every tick so that
// config changes take effect without restarting the caller. A non-positive
// return value is treated as "no change" — the scheduler keeps its current
// interval. The function MUST NOT block.
type IntervalFn func() time.Duration

// TickFn is the work the scheduler performs on every tick. The scheduler
// passes its own ctx so the tick can cooperate with shutdown. A panic in
// TickFn is not recovered; callers that need panic safety should wrap their
// tick themselves.
type TickFn func(ctx context.Context)

// Scheduler runs TickFn on an interval. Instances are reusable: after Run
// returns you may call Run again. Instances are NOT safe for concurrent Run
// calls — the second caller will receive ErrAlreadyRunning.
type Scheduler struct {
	name     string
	interval IntervalFn
	tick     TickFn

	mu      sync.Mutex
	running bool
}

// ErrAlreadyRunning is returned by Run when another goroutine is already
// executing the same Scheduler instance.
var ErrAlreadyRunning = errors.New("scheduler: already running")

// ErrInvalidInterval is returned by Run when IntervalFn returns a
// non-positive value on startup.
var ErrInvalidInterval = errors.New("scheduler: initial interval must be positive")

// New constructs a Scheduler. The name is used as a log attribute only.
func New(name string, interval IntervalFn, tick TickFn) *Scheduler {
	return &Scheduler{
		name:     name,
		interval: interval,
		tick:     tick,
	}
}

// Run blocks until ctx is cancelled. It ticks on the interval returned by
// IntervalFn, invoking TickFn each time. If IntervalFn returns a different
// positive value on a tick, the ticker is reset to the new interval.
//
// Run returns ctx.Err() when ctx is cancelled, or one of the Err* sentinels
// if the instance is misused.
func (s *Scheduler) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrAlreadyRunning
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	cur := s.interval()
	if cur <= 0 {
		return ErrInvalidInterval
	}

	t := time.NewTicker(cur)
	defer t.Stop()

	slog.Info("scheduler: started", "name", s.name, "interval", cur)

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler: stopped", "name", s.name)
			return ctx.Err()
		case <-t.C:
			if next := s.interval(); next > 0 && next != cur {
				cur = next
				t.Reset(cur)
				slog.Info("scheduler: interval changed", "name", s.name, "interval", cur)
			}
			s.tick(ctx)
		}
	}
}
