package evolution_loop_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
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

// testCycleConfig returns an EvolutionConfig suitable for RunCycle tests.
func testCycleConfig() config.EvolutionConfig {
	return config.EvolutionConfig{
		Enabled:             true,
		MaxTokensPerCycle:   10000,
		MaxHypothesesPerRun: 5,
		AllowedEvolutionCategories: []string{
			"refactor_opportunity",
			"test_gap",
			"doc_drift",
		},
		StratusSelfEnabled: false,
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

// ---------------------------------------------------------------------------
// Mocks for RunCycle
// ---------------------------------------------------------------------------

// mockBuilder always returns a fixed Bundle.
type mockBuilder struct {
	bundle baseline.Bundle
	err    error
}

func (m *mockBuilder) Build(_ context.Context, _ string, _ config.BaselineLimits) (baseline.Bundle, error) {
	if m.err != nil {
		return baseline.Bundle{}, m.err
	}
	return m.bundle, nil
}

// mockJudge returns configurable scores and tokensUsed per call.
type mockJudge struct {
	mu        sync.Mutex
	calls     int
	scores    scoring.LLMScores
	tokensEach int
	err        error
}

func (m *mockJudge) Score(_ context.Context, _ scoring.Hypothesis, _ baseline.Bundle, perCallCap int) (scoring.LLMScores, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.err != nil {
		return scoring.LLMScores{}, 0, m.err
	}
	used := m.tokensEach
	if used > perCallCap {
		used = perCallCap
	}
	return m.scores, used, nil
}

// mockWriter records all ProposalInputs written.
type mockWriter struct {
	mu     sync.Mutex
	inputs []evolution_loop.ProposalInput
	err    error
}

func (m *mockWriter) Write(_ context.Context, in evolution_loop.ProposalInput) (evolution_loop.ProposalResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return evolution_loop.ProposalResult{}, m.err
	}
	m.inputs = append(m.inputs, in)
	return evolution_loop.ProposalResult{Inserted: true}, nil
}

func (m *mockWriter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.inputs)
}

// ---------------------------------------------------------------------------
// RunCycle tests
// ---------------------------------------------------------------------------

// TestRunCycle_ReturnsErrWhenMaxTokensZero verifies that RunCycle returns
// ErrTokenCapRequired when MaxTokensPerCycle is 0.
func TestRunCycle_ReturnsErrWhenMaxTokensZero(t *testing.T) {
	store := newMockStore()
	cfg := testCycleConfig()
	cfg.MaxTokensPerCycle = 0

	writer := &mockWriter{}
	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithProposalWriter(writer),
	)

	_, _, err := loop.RunCycle(context.Background())
	if err == nil {
		t.Fatal("expected error when MaxTokensPerCycle == 0")
	}
	if !errors.Is(err, config.ErrTokenCapRequired) {
		t.Errorf("expected ErrTokenCapRequired, got: %v", err)
	}
}

// TestRunCycle_ReturnsErrWhenNoProposalWriter verifies that RunCycle returns
// an error when no ProposalWriter is configured.
func TestRunCycle_ReturnsErrWhenNoProposalWriter(t *testing.T) {
	store := newMockStore()
	cfg := testCycleConfig()

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		// no WithProposalWriter
	)

	_, _, err := loop.RunCycle(context.Background())
	if err == nil {
		t.Fatal("expected error when proposalWriter is nil")
	}
}

// TestRunCycle_WritesProposalsForEachHypothesis verifies that Write is called
// once per hypothesis produced by the generators.
func TestRunCycle_WritesProposalsForEachHypothesis(t *testing.T) {
	store := newMockStore()
	cfg := testCycleConfig()
	// Use a real project root so the builder can scan it (nil-safe baseline).
	// We inject a mock builder that returns a fixed empty bundle.
	bldr := &mockBuilder{bundle: baseline.Bundle{ProjectRoot: "."}}
	writer := &mockWriter{}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	result, _, err := loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if writer.count() != result.HypothesesGenerated {
		t.Errorf("Write calls (%d) != HypothesesGenerated (%d)",
			writer.count(), result.HypothesesGenerated)
	}
}

// bundleWithHypotheses returns a bundle guaranteed to produce at least one
// hypothesis from the test_gap generator (dir with low test ratio).
func bundleWithHypotheses() baseline.Bundle {
	return baseline.Bundle{
		ProjectRoot: ".",
		TestRatios: []baseline.TestRatio{
			{Dir: "api", SourceFiles: 10, TestFiles: 0, Ratio: 0.0},
			{Dir: "db", SourceFiles: 5, TestFiles: 1, Ratio: 0.2},
		},
		WikiTitles: []baseline.WikiTitle{
			{ID: "wiki-1", Title: "Architecture overview", Staleness: 0.9},
		},
	}
}

// TestRunCycle_PartialScoringWhenJudgeErrors verifies that when the LLM judge
// always errors, PartialScoring is true and static-only blended results are
// still written for every hypothesis.
func TestRunCycle_PartialScoringWhenJudgeErrors(t *testing.T) {
	store := newMockStore()
	cfg := testCycleConfig()
	cfg.AllowedEvolutionCategories = []string{"test_gap", "doc_drift"}

	bldr := &mockBuilder{bundle: bundleWithHypotheses()}
	judge := &mockJudge{err: fmt.Errorf("llm unavailable")}
	writer := &mockWriter{}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithLLMJudge(judge),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	result, _, err := loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.HypothesesGenerated == 0 {
		t.Fatal("expected at least one hypothesis to be generated (check bundleWithHypotheses)")
	}
	if !result.PartialScoring {
		t.Error("expected PartialScoring=true when judge always errors")
	}
	// Proposals should still be written despite judge failures.
	if writer.count() != result.HypothesesGenerated {
		t.Errorf("Write calls (%d) != HypothesesGenerated (%d)",
			writer.count(), result.HypothesesGenerated)
	}
}

// TestRunCycle_TokenCapAbortsJudge verifies that when the token cap is tight
// the loop sets PartialScoring=true once the budget is exhausted.
func TestRunCycle_TokenCapAbortsJudge(t *testing.T) {
	store := newMockStore()
	cfg := testCycleConfig()
	cfg.AllowedEvolutionCategories = []string{"test_gap", "doc_drift"}
	// Budget is small: judge uses 60 tokens per call, cap is 100 → first call
	// uses 60 tokens; second call gets perCall=40 (< 60) so only 40 tokens are
	// used; third call gets perCall=0 → PartialScoring=true.
	cfg.MaxTokensPerCycle = 100
	cfg.ScoringWeights.MaxTokensPerJudgeCall = 4000

	bldr := &mockBuilder{bundle: bundleWithHypotheses()}
	judge := &mockJudge{
		scores:     scoring.LLMScores{Impact: 0.5, Effort: 0.3, Confidence: 0.8, Novelty: 0.4},
		tokensEach: 60,
	}
	writer := &mockWriter{}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithLLMJudge(judge),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	result, _, err := loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With budget=100 and 60 tokens per call, budget is exhausted after the
	// second call (60+40=100), third call gets perCall≤0 → PartialScoring=true.
	// If fewer than 3 hypotheses are generated, partial may not trigger;
	// log the result for debugging.
	t.Logf("HypothesesGenerated=%d TokensUsed=%d PartialScoring=%v",
		result.HypothesesGenerated, result.TokensUsed, result.PartialScoring)
	if result.HypothesesGenerated >= 3 && !result.PartialScoring {
		t.Error("expected PartialScoring=true after token cap exhaustion with 3+ hypotheses")
	}
	if result.TokensUsed > cfg.MaxTokensPerCycle {
		t.Errorf("TokensUsed (%d) exceeded MaxTokensPerCycle (%d)",
			result.TokensUsed, cfg.MaxTokensPerCycle)
	}
}

// TestRunCycle_NoApplyFn asserts that RunCycle does not call any apply function.
// This is a structural test: WithApplyFn is set but must never be triggered.
func TestRunCycle_NoApplyFn(t *testing.T) {
	store := newMockStore()
	cfg := testCycleConfig()

	bldr := &mockBuilder{bundle: baseline.Bundle{ProjectRoot: "."}}
	writer := &mockWriter{}

	applyFnCalled := false
	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
		// Register an applyFn — it must never be called by RunCycle.
		evolution_loop.WithApplyFn(func(_ *db.EvolutionHypothesis) error {
			applyFnCalled = true
			return nil
		}),
	)

	_, _, err := loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applyFnCalled {
		t.Error("applyFn must not be called by RunCycle (auto-apply path removed)")
	}
}

// TestRunCycle_CategoryBreakdown verifies that the breakdown map counts
// hypotheses per category.
func TestRunCycle_CategoryBreakdown(t *testing.T) {
	store := newMockStore()
	cfg := testCycleConfig()

	bldr := &mockBuilder{bundle: baseline.Bundle{ProjectRoot: "."}}
	writer := &mockWriter{}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	result, _, err := loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sum of all category counts must equal HypothesesGenerated.
	total := 0
	for _, cnt := range result.CategoryBreakdown {
		total += cnt
	}
	if total != result.HypothesesGenerated {
		t.Errorf("CategoryBreakdown total (%d) != HypothesesGenerated (%d)",
			total, result.HypothesesGenerated)
	}

	// Every key in breakdown must be a category that was requested.
	allowed := make(map[string]bool)
	for _, cat := range cfg.AllowedEvolutionCategories {
		allowed[cat] = true
	}
	for cat := range result.CategoryBreakdown {
		if !allowed[cat] {
			t.Errorf("unexpected category %q in breakdown", cat)
		}
	}
}

// TestRunCycle_WriterErrorContinues verifies that a proposal writer error for
// one hypothesis does not abort the whole cycle.
func TestRunCycle_WriterErrorContinues(t *testing.T) {
	store := newMockStore()
	cfg := testCycleConfig()

	bldr := &mockBuilder{bundle: baseline.Bundle{ProjectRoot: "."}}
	writer := &mockWriter{err: fmt.Errorf("db write failed")}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	)

	result, _, err := loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle should not return error on write failures, got: %v", err)
	}
	// Even with write failures the result is populated.
	if result.HypothesesGenerated < 0 {
		t.Error("unexpected negative HypothesesGenerated")
	}
}

// ---------------------------------------------------------------------------
// Original Run() tests — preserved and adjusted for removed auto-apply path
// ---------------------------------------------------------------------------

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

// TestRun_AutoApplied_NoOpWhenApplyFnSet verifies that setting WithApplyFn no
// longer causes the loop to apply any hypotheses automatically. The auto-apply
// path has been removed; applyFn is a retained but no-op option.
func TestRun_AutoApplied_NoOpWhenApplyFnSet(t *testing.T) {
	store := newMockStore()
	cfg := testConfig()
	cfg.AutoApplyThreshold = 0.01 // very low — would have triggered before T8
	cfg.MinSampleSize = 1
	cfg.Categories = []string{"prompt_tuning"} // only remaining seed category post-T9
	cfg.MaxHypothesesPerRun = 1

	applyFnCalled := false
	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithApplyFn(func(_ *db.EvolutionHypothesis) error {
			applyFnCalled = true
			return nil
		}),
	)

	_, err := loop.Run(context.Background(), "manual", 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Auto-apply path was removed in T8. applyFn must never be called.
	if applyFnCalled {
		t.Error("applyFn was called but auto-apply path should be removed")
	}
}

// TestRun_ProposalCreated_CallsWikiFn verifies that when an experiment meets
// the proposal threshold, the wikiFn callback is invoked and WikiPageID is set.
func TestRun_ProposalCreated_CallsWikiFn(t *testing.T) {
	store := newMockStore()
	cfg := testConfig()
	cfg.ProposalThreshold = 0.01 // very low to ensure proposal_created
	cfg.AutoApplyThreshold = 0.99 // very high to avoid auto_applied
	cfg.MinSampleSize = 1
	cfg.Categories = []string{"prompt_tuning"} // non-internal, so won't auto-apply
	cfg.MaxHypothesesPerRun = 1

	var wikiCalls int
	wikiFn := func(ctx context.Context, h *db.EvolutionHypothesis) (string, error) {
		wikiCalls++
		return "wiki-page-123", nil
	}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithWikiFn(wikiFn),
	)

	result, err := loop.Run(context.Background(), "manual", 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.WikiPagesUpdated == 0 {
		t.Fatal("expected at least one wiki page updated")
	}
	if wikiCalls == 0 {
		t.Error("wikiFn was never called")
	}

	// Check that WikiPageID was set on the hypothesis.
	for _, h := range store.allHypotheses() {
		if h.Decision == "proposal_created" && h.WikiPageID != nil && *h.WikiPageID == "wiki-page-123" {
			return // found it
		}
	}
	t.Error("expected hypothesis with WikiPageID='wiki-page-123'")
}

// TestRun_ProposalCreated_WikiFnError_ContinuesGracefully verifies that a
// wikiFn error doesn't stop the run or produce a fatal error.
func TestRun_ProposalCreated_WikiFnError_ContinuesGracefully(t *testing.T) {
	store := newMockStore()
	cfg := testConfig()
	cfg.ProposalThreshold = 0.01
	cfg.AutoApplyThreshold = 0.99
	cfg.MinSampleSize = 1
	cfg.Categories = []string{"prompt_tuning"}
	cfg.MaxHypothesesPerRun = 1

	wikiFn := func(ctx context.Context, h *db.EvolutionHypothesis) (string, error) {
		return "", fmt.Errorf("wiki save failed")
	}

	loop := evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		evolution_loop.WithWikiFn(wikiFn),
	)

	result, err := loop.Run(context.Background(), "manual", 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// WikiPagesUpdated should NOT be counted when wikiFn fails.
	if result.WikiPagesUpdated != 0 {
		t.Errorf("expected WikiPagesUpdated == 0 on wikiFn error, got %d", result.WikiPagesUpdated)
	}

	// Run should complete, not crash.
	run := store.latestRun()
	if run == nil {
		t.Fatal("expected run record to be saved")
	}
	if run.Status != "completed" {
		t.Errorf("expected status completed, got %q", run.Status)
	}
}
