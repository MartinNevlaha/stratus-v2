package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_TicksAndStopsOnContextCancel(t *testing.T) {
	var ticks int32
	s := New("test",
		func() time.Duration { return 5 * time.Millisecond },
		func(ctx context.Context) { atomic.AddInt32(&ticks, 1) },
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	time.Sleep(40 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return after cancel")
	}

	if got := atomic.LoadInt32(&ticks); got < 3 {
		t.Fatalf("expected >=3 ticks in 40ms at 5ms interval, got %d", got)
	}
}

func TestScheduler_RejectsConcurrentRun(t *testing.T) {
	s := New("test",
		func() time.Duration { return time.Hour },
		func(ctx context.Context) {},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	time.Sleep(10 * time.Millisecond)
	err := s.Run(ctx)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("want ErrAlreadyRunning, got %v", err)
	}
}

func TestScheduler_RejectsNonPositiveInitialInterval(t *testing.T) {
	s := New("test",
		func() time.Duration { return 0 },
		func(ctx context.Context) {},
	)
	err := s.Run(context.Background())
	if !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("want ErrInvalidInterval, got %v", err)
	}
}

func TestScheduler_HotReloadsInterval(t *testing.T) {
	var current atomic.Int64
	current.Store(int64(5 * time.Millisecond))

	var ticks int32
	s := New("test",
		func() time.Duration { return time.Duration(current.Load()) },
		func(ctx context.Context) { atomic.AddInt32(&ticks, 1) },
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// Let a few ticks elapse at the fast interval.
	time.Sleep(30 * time.Millisecond)
	fastTicks := atomic.LoadInt32(&ticks)

	// Slow the interval down dramatically.
	current.Store(int64(time.Hour))

	// Wait long enough for one more tick to land AND the interval change to
	// be observed, then assert the tick rate dropped.
	time.Sleep(30 * time.Millisecond)
	midTicks := atomic.LoadInt32(&ticks)

	time.Sleep(60 * time.Millisecond)
	endTicks := atomic.LoadInt32(&ticks)

	if fastTicks < 3 {
		t.Fatalf("expected fast-phase ticks >= 3, got %d", fastTicks)
	}
	if endTicks > midTicks+1 {
		t.Fatalf("expected tick rate to drop after interval change: mid=%d end=%d", midTicks, endTicks)
	}
}

func TestScheduler_IgnoresNonPositiveIntervalOnTick(t *testing.T) {
	var current atomic.Int64
	current.Store(int64(5 * time.Millisecond))

	var ticks int32
	s := New("test",
		func() time.Duration { return time.Duration(current.Load()) },
		func(ctx context.Context) { atomic.AddInt32(&ticks, 1) },
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	time.Sleep(25 * time.Millisecond)

	// Flip interval to zero — scheduler must NOT reset to zero and must keep ticking.
	current.Store(0)
	time.Sleep(25 * time.Millisecond)
	got := atomic.LoadInt32(&ticks)
	if got < 6 {
		t.Fatalf("expected scheduler to keep ticking despite zero interval; ticks=%d", got)
	}
}

func TestScheduler_ReusableAfterRun(t *testing.T) {
	var ticks int32
	s := New("test",
		func() time.Duration { return 5 * time.Millisecond },
		func(ctx context.Context) { atomic.AddInt32(&ticks, 1) },
	)

	run := func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = s.Run(ctx) }()
		time.Sleep(25 * time.Millisecond)
	}
	run()
	first := atomic.LoadInt32(&ticks)

	// Wait for first Run to fully release the singleton before starting the second.
	time.Sleep(20 * time.Millisecond)
	run()
	second := atomic.LoadInt32(&ticks)

	if second <= first {
		t.Fatalf("expected second run to add ticks; first=%d second=%d", first, second)
	}
}
