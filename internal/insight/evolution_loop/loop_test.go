package evolution_loop_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
)

func testConfig() config.EvolutionConfig {
	return config.EvolutionConfig{
		Enabled:             true,
		TimeoutMs:           5000,
		MaxHypothesesPerRun: 5,
		AutoApplyThreshold:  0.85,
		ProposalThreshold:   0.65,
		MinSampleSize:       10,
		DailyTokenBudget:    100000,
		Categories:          []string{},
	}
}

// TestRun_CompletesSuccessfully verifies that a normal run saves a completed
// run record and returns a non-empty result.
func TestRun_CompletesSuccessfully(t *testing.T) {
	store := newMockStore()
	cfg := testConfig()
	loop := evolution_loop.NewEvolutionLoop(store, func() config.EvolutionConfig { return cfg }, nil)

	result, err := loop.Run(context.Background(), "manual", 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil RunResult")
	}
	if result.RunID == "" {
		t.Error("expected non-empty RunID")
	}
	if result.HypothesesTested == 0 {
		t.Error("expected at least one hypothesis tested")
	}

	// Verify the run record was persisted with completed status.
	run := store.latestRun()
	if run == nil {
		t.Fatal("expected run to be saved in store")
	}
	if run.Status != "completed" {
		t.Errorf("expected status completed, got %q", run.Status)
	}
	if run.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

// TestRun_RejectsWhenAlreadyRunning verifies that a second concurrent call
// returns an error immediately without waiting.
func TestRun_RejectsWhenAlreadyRunning(t *testing.T) {
	// blockingStore signals after SaveRun and then blocks UpdateRun.
	// This keeps the first run alive long enough for the second call.
	block := make(chan struct{})
	savedRun := make(chan struct{}, 1)

	bs := &blockingStore{
		inner:    newMockStore(),
		block:    block,
		savedRun: savedRun,
	}

	cfg := testConfig()
	cfg.MaxHypothesesPerRun = 1
	cfg.TimeoutMs = 10_000

	loop := evolution_loop.NewEvolutionLoop(bs, func() config.EvolutionConfig { return cfg }, nil)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		loop.Run(context.Background(), "scheduled", 0, nil) //nolint:errcheck
	}()

	// Wait until the first run has saved its initial run record.
	select {
	case <-savedRun:
	case <-time.After(3 * time.Second):
		t.Fatal("first run did not start in time")
	}

	// The second call must be rejected immediately.
	_, secondErr := loop.Run(context.Background(), "manual", 0, nil)
	if secondErr == nil {
		t.Error("expected error when second run started while first is running")
	}

	// Let the first run complete.
	close(block)
	wg.Wait()
}

// TestRun_RespectsTimeout verifies that a very short timeout budget causes the
// loop to terminate early without blocking.
func TestRun_RespectsTimeout(t *testing.T) {
	store := newMockStore()
	cfg := testConfig()
	cfg.TimeoutMs = 1   // 1 ms — budget expires immediately
	cfg.MaxHypothesesPerRun = 10

	loop := evolution_loop.NewEvolutionLoop(store, func() config.EvolutionConfig { return cfg }, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Must return without blocking indefinitely.
	loop.Run(ctx, "scheduled", 0, nil) //nolint:errcheck

	run := store.latestRun()
	if run == nil {
		t.Fatal("expected run record to be saved")
	}
	// With a 1 ms budget the run status should be timeout or completed (0 done).
	if run.Status != "timeout" && run.Status != "completed" {
		t.Errorf("expected timeout or completed status, got %q", run.Status)
	}
}

func TestIsRunning_FalseBeforeRun(t *testing.T) {
	store := newMockStore()
	cfg := testConfig()
	loop := evolution_loop.NewEvolutionLoop(store, func() config.EvolutionConfig { return cfg }, nil)
	if loop.IsRunning() {
		t.Error("expected IsRunning to be false before any run")
	}
}

func TestIsRunning_FalseAfterRun(t *testing.T) {
	store := newMockStore()
	cfg := testConfig()
	loop := evolution_loop.NewEvolutionLoop(store, func() config.EvolutionConfig { return cfg }, nil)
	loop.Run(context.Background(), "manual", 0, nil) //nolint:errcheck
	if loop.IsRunning() {
		t.Error("expected IsRunning to be false after run completes")
	}
}

// TestRun_TimeoutOverride verifies that a positive timeoutOverrideMs replaces
// the configured TimeoutMs and is recorded in the persisted run record.
func TestRun_TimeoutOverride(t *testing.T) {
	store := newMockStore()
	cfg := testConfig()
	cfg.TimeoutMs = 30000 // configured value

	loop := evolution_loop.NewEvolutionLoop(store, func() config.EvolutionConfig { return cfg }, nil)

	// Pass an override smaller than the configured value.
	loop.Run(context.Background(), "manual", 5000, nil) //nolint:errcheck

	run := store.latestRun()
	if run == nil {
		t.Fatal("expected run record to be saved")
	}
	if run.TimeoutMs != 5000 {
		t.Errorf("expected TimeoutMs 5000 (override), got %d", run.TimeoutMs)
	}
}

// TestRun_ZeroTimeoutUsesConfig verifies that a zero timeoutOverrideMs falls
// back to the configured TimeoutMs.
func TestRun_ZeroTimeoutUsesConfig(t *testing.T) {
	store := newMockStore()
	cfg := testConfig()
	cfg.TimeoutMs = 12000

	loop := evolution_loop.NewEvolutionLoop(store, func() config.EvolutionConfig { return cfg }, nil)

	loop.Run(context.Background(), "manual", 0, nil) //nolint:errcheck

	run := store.latestRun()
	if run == nil {
		t.Fatal("expected run record to be saved")
	}
	if run.TimeoutMs != 12000 {
		t.Errorf("expected TimeoutMs 12000 (from config), got %d", run.TimeoutMs)
	}
}

// blockingStore satisfies EvolutionStore; it signals on first SaveRun and then
// blocks on UpdateRun until block is closed.
type blockingStore struct {
	inner    *mockStore
	block    chan struct{}
	savedRun chan struct{}
	once     sync.Once
}

var _ evolution_loop.EvolutionStore = (*blockingStore)(nil)

func (b *blockingStore) SaveRun(r *db.EvolutionRun) error {
	err := b.inner.SaveRun(r)
	b.once.Do(func() { close(b.savedRun) })
	return err
}

func (b *blockingStore) GetRun(id string) (*db.EvolutionRun, error) {
	return b.inner.GetRun(id)
}

func (b *blockingStore) ListRuns(f db.EvolutionRunFilters) ([]db.EvolutionRun, int, error) {
	return b.inner.ListRuns(f)
}

func (b *blockingStore) UpdateRun(r *db.EvolutionRun) error {
	// Block until the test signals that the second run has been attempted.
	<-b.block
	return b.inner.UpdateRun(r)
}

func (b *blockingStore) GetActiveRun() (*db.EvolutionRun, error) {
	return b.inner.GetActiveRun()
}

func (b *blockingStore) SaveHypothesis(h *db.EvolutionHypothesis) error {
	return b.inner.SaveHypothesis(h)
}

func (b *blockingStore) GetHypothesis(id string) (*db.EvolutionHypothesis, error) {
	return b.inner.GetHypothesis(id)
}

func (b *blockingStore) ListHypotheses(runID string) ([]db.EvolutionHypothesis, error) {
	return b.inner.ListHypotheses(runID)
}

func (b *blockingStore) UpdateHypothesis(h *db.EvolutionHypothesis) error {
	return b.inner.UpdateHypothesis(h)
}
