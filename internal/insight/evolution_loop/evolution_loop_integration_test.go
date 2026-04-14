package evolution_loop_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
)

// ---------------------------------------------------------------------------
// Integration stubs — distinct from loop_test.go mocks to avoid coupling.
// ---------------------------------------------------------------------------

// stubBuilder returns a pre-configured Bundle; never errors unless errToReturn is set.
type stubBuilder struct {
	mu          sync.Mutex
	bundle      baseline.Bundle
	errToReturn error
}

func (s *stubBuilder) Build(_ context.Context, _ string, _ config.BaselineLimits) (baseline.Bundle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.errToReturn != nil {
		return baseline.Bundle{}, s.errToReturn
	}
	return s.bundle, nil
}

// stubJudge returns canned LLMScores and tokensUsed per call.
type stubJudge struct {
	mu         sync.Mutex
	calls      int
	scores     scoring.LLMScores
	tokensEach int
}

func (s *stubJudge) Score(
	_ context.Context,
	_ scoring.Hypothesis,
	_ baseline.Bundle,
	perCallCap int,
) (scoring.LLMScores, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	used := s.tokensEach
	if used > perCallCap {
		used = perCallCap
	}
	return s.scores, used, nil
}

func (s *stubJudge) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// integrationCycleConfig returns a config suited for full-cycle integration tests.
func integrationCycleConfig() config.EvolutionConfig {
	return config.EvolutionConfig{
		Enabled:             true,
		MaxTokensPerCycle:   5000,
		MaxHypothesesPerRun: 5,
		AllowedEvolutionCategories: []string{
			"refactor_opportunity",
			"test_gap",
			"architecture_drift",
			"feature_idea",
			"dx_improvement",
			"doc_drift",
		},
		StratusSelfEnabled: false,
		ScoringWeights: config.ScoringWeights{
			Churn:                 0.15,
			TestGap:               0.15,
			TODO:                  0.05,
			Staleness:             0.05,
			ADRViolation:          0.10,
			LLMImpact:             0.20,
			LLMEffort:             0.10,
			LLMConfidence:         0.10,
			LLMNovelty:            0.10,
			MaxTokensPerJudgeCall: 1000,
		},
		BaselineLimits: config.BaselineLimits{
			VexorTopK:     5,
			GitLogCommits: 10,
			TODOMax:       10,
		},
	}
}

// bundleWithFeatureIdeaTriggers returns a Bundle with at least one TODO that
// matches the forward-looking regex ("would be nice") and one high-churn file,
// ensuring feature_idea and other generators are triggered.
func bundleWithFeatureIdeaTriggers() baseline.Bundle {
	return baseline.Bundle{
		ProjectRoot: ".",
		TODOs: []baseline.TODOItem{
			{
				Path: "api/handler.go",
				Line: 42,
				Text: "TODO: would be nice to add pagination here",
				Kind: "TODO",
			},
			{
				Path: "db/store.go",
				Line: 17,
				Text: "FIXME: someday refactor this to use interfaces",
				Kind: "FIXME",
			},
		},
		WikiTitles: []baseline.WikiTitle{
			{ID: "wiki-stale-1", Title: "Architecture Overview", Staleness: 0.8},
			{ID: "wiki-stale-2", Title: "API Design", Staleness: 0.9},
		},
		TestRatios: []baseline.TestRatio{
			{Dir: "api", SourceFiles: 10, TestFiles: 1, Ratio: 0.1},
			{Dir: "db", SourceFiles: 8, TestFiles: 0, Ratio: 0.0},
		},
		GitCommits: []baseline.GitCommit{
			{Hash: "abc123", Subject: "fix: major rewrite of api/handler.go", Files: []string{"api/handler.go"}, At: time.Now().Add(-24 * time.Hour)},
			{Hash: "def456", Subject: "feat: added new endpoint", Files: []string{"api/routes.go"}, At: time.Now().Add(-48 * time.Hour)},
		},
		GeneratedAt: time.Now(),
	}
}

// queryProposalCount returns the number of rows in insight_proposals.
func queryProposalCount(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM insight_proposals`).Scan(&n); err != nil {
		t.Fatalf("count insight_proposals: %v", err)
	}
	return n
}

// queryWikiPageCount returns the number of rows in wiki_pages.
func queryWikiPageCount(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM wiki_pages`).Scan(&n); err != nil {
		t.Fatalf("count wiki_pages: %v", err)
	}
	return n
}

// queryProposalsByType returns a map of type → count for all rows in insight_proposals.
func queryProposalsByType(t *testing.T, sqlDB *sql.DB) map[string]int {
	t.Helper()
	rows, err := sqlDB.Query(`SELECT type, COUNT(*) FROM insight_proposals GROUP BY type`)
	if err != nil {
		t.Fatalf("query proposals by type: %v", err)
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var typ string
		var count int
		if err := rows.Scan(&typ, &count); err != nil {
			t.Fatalf("scan proposals by type: %v", err)
		}
		result[typ] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}
	return result
}

// buildIntegrationLoop constructs an EvolutionLoop wired with a real DB writer
// plus the provided stub builder and judge.
func buildIntegrationLoop(
	t *testing.T,
	database *db.DB,
	cfg config.EvolutionConfig,
	bldr *stubBuilder,
	judge *stubJudge,
) *evolution_loop.EvolutionLoop {
	t.Helper()
	store := newMockStore()
	writer := evolution_loop.NewProposalWriter(database)

	opts := []evolution_loop.LoopOption{
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithProposalWriter(writer),
		evolution_loop.WithProjectRoot("."),
	}
	if judge != nil {
		opts = append(opts, evolution_loop.WithLLMJudge(judge))
	}

	return evolution_loop.NewEvolutionLoop(
		store,
		func() config.EvolutionConfig { return cfg },
		nil,
		opts...,
	)
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

// TestIntegration_FullCycle_WritesProposalsAndWikiPage exercises the full
// RunCycle pipeline against a real file-backed SQLite DB (required so that
// cross-pool queries on wiki_pages work correctly) and asserts that:
//   - at least 1 row is inserted into insight_proposals
//   - feature_idea proposals have a matching wiki_pages row with page_type='idea'
//   - every non-null wiki_page_id FK resolves to an existing wiki_pages.id
func TestIntegration_FullCycle_WritesProposalsAndWikiPage(t *testing.T) {
	// Use file-backed DB: in-memory SQLite with MaxOpenConns(4) may serve
	// different connections to the writer (which pins via Conn()) and to our
	// test queries, causing "no such table" errors on connections that received
	// no schema migration.
	database := openFileTestDatabase(t)
	cfg := integrationCycleConfig()

	bldr := &stubBuilder{bundle: bundleWithFeatureIdeaTriggers()}
	judge := &stubJudge{
		scores: scoring.LLMScores{
			Impact:     0.7,
			Effort:     0.3,
			Confidence: 0.8,
			Novelty:    0.6,
		},
		tokensEach: 200,
	}

	loop := buildIntegrationLoop(t, database, cfg, bldr, judge)

	result, err := loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}

	proposalCount := queryProposalCount(t, database.SQL())
	if proposalCount == 0 {
		t.Error("expected at least 1 row in insight_proposals after full cycle")
	}
	if result.HypothesesGenerated == 0 {
		t.Error("expected HypothesesGenerated > 0")
	}

	wikiCount := queryWikiPageCount(t, database.SQL())
	t.Logf("proposals=%d wiki_pages=%d hypotheses=%d", proposalCount, wikiCount, result.HypothesesGenerated)

	// FK integrity: every non-null wiki_page_id must resolve to a wiki_pages row.
	rows, err := database.SQL().Query(
		`SELECT id, wiki_page_id FROM insight_proposals WHERE wiki_page_id IS NOT NULL`,
	)
	if err != nil {
		t.Fatalf("query proposals with wiki_page_id: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var proposalID, wikiPageID string
		if err := rows.Scan(&proposalID, &wikiPageID); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		var count int
		if err := database.SQL().QueryRow(
			`SELECT COUNT(*) FROM wiki_pages WHERE id = ?`, wikiPageID,
		).Scan(&count); err != nil {
			t.Fatalf("check wiki_pages FK for proposal %s: %v", proposalID, err)
		}
		if count == 0 {
			t.Errorf("broken FK: insight_proposals.wiki_page_id=%q has no matching wiki_pages row (proposal_id=%s)",
				wikiPageID, proposalID)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	// Feature idea wiki pages must have page_type='idea' and proposal_id in frontmatter.
	wikiRows, err := database.SQL().Query(
		`SELECT id, page_type, content FROM wiki_pages WHERE page_type = 'idea'`,
	)
	if err != nil {
		t.Fatalf("query wiki pages of type idea: %v", err)
	}
	defer wikiRows.Close()
	for wikiRows.Next() {
		var id, pageType, content string
		if err := wikiRows.Scan(&id, &pageType, &content); err != nil {
			t.Fatalf("scan wiki row: %v", err)
		}
		if pageType != "idea" {
			t.Errorf("wiki_pages.page_type=%q, want 'idea'", pageType)
		}
		if !strings.Contains(content, "proposal_id:") {
			t.Errorf("wiki page %q content missing 'proposal_id:' frontmatter", id)
		}
	}
}

// TestIntegration_SecondCycle_IsIdempotent verifies that running two identical
// cycles results in the same number of insight_proposals rows, and that
// last_seen_at is not regressed on the second cycle.
func TestIntegration_SecondCycle_IsIdempotent(t *testing.T) {
	database := openTestDatabase(t)
	cfg := integrationCycleConfig()

	bldr := &stubBuilder{bundle: bundleWithFeatureIdeaTriggers()}
	judge := &stubJudge{
		scores:     scoring.LLMScores{Impact: 0.6, Effort: 0.4, Confidence: 0.7, Novelty: 0.5},
		tokensEach: 150,
	}

	loop := buildIntegrationLoop(t, database, cfg, bldr, judge)

	// First cycle.
	res1, err := loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("first RunCycle: %v", err)
	}
	countAfterFirst := queryProposalCount(t, database.SQL())
	if countAfterFirst == 0 {
		t.Skip("no proposals written in first cycle — bundle may not trigger any generator")
	}

	// Capture last_seen_at values before second cycle.
	type seenRow struct {
		id         string
		lastSeenAt int64
	}
	var firstSeenRows []seenRow
	rows, err := database.SQL().Query(`SELECT id, last_seen_at FROM insight_proposals`)
	if err != nil {
		t.Fatalf("query last_seen_at before second cycle: %v", err)
	}
	for rows.Next() {
		var r seenRow
		if err := rows.Scan(&r.id, &r.lastSeenAt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		firstSeenRows = append(firstSeenRows, r)
	}
	rows.Close()

	// Sleep 1s so Unix timestamp advances and second cycle can set a larger value.
	time.Sleep(1 * time.Second)

	// Second cycle — same bundle, same config.
	res2, err := loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("second RunCycle: %v", err)
	}
	countAfterSecond := queryProposalCount(t, database.SQL())

	// Row counts must not change.
	if countAfterSecond != countAfterFirst {
		t.Errorf("proposal count changed from %d to %d after second cycle (expected idempotency)",
			countAfterFirst, countAfterSecond)
	}

	// last_seen_at must not regress on any row.
	for _, before := range firstSeenRows {
		var after int64
		if err := database.SQL().QueryRow(
			`SELECT last_seen_at FROM insight_proposals WHERE id = ?`, before.id,
		).Scan(&after); err != nil {
			t.Fatalf("read last_seen_at after second cycle for %s: %v", before.id, err)
		}
		if after < before.lastSeenAt {
			t.Errorf("last_seen_at regressed for proposal %s: before=%d after=%d",
				before.id, before.lastSeenAt, after)
		}
	}

	t.Logf("first cycle: %d hypotheses; second cycle: %d hypotheses",
		res1.HypothesesGenerated, res2.HypothesesGenerated)
}

// TestIntegration_TokenCap_ProducesPartialScoring verifies that when
// MaxTokensPerCycle is tight, remaining hypotheses get static-only scoring
// and CycleResult.PartialScoring is true. All proposals are still written.
func TestIntegration_TokenCap_ProducesPartialScoring(t *testing.T) {
	database := openTestDatabase(t)
	cfg := integrationCycleConfig()

	// Each call uses 600 tokens; budget is 1000.
	// First call: 600 used, 400 remaining.
	// Second call: perCall = min(1000, 400) = 400; judge returns min(600,400)=400; total=1000.
	// Third call: remaining=0 → perCall=0 → PartialScoring=true.
	cfg.MaxTokensPerCycle = 1000
	cfg.ScoringWeights.MaxTokensPerJudgeCall = 0 // 0 means use remaining budget

	bldr := &stubBuilder{bundle: bundleWithFeatureIdeaTriggers()}
	judge := &stubJudge{
		scores:     scoring.LLMScores{Impact: 0.5, Effort: 0.5, Confidence: 0.5, Novelty: 0.5},
		tokensEach: 600,
	}

	loop := buildIntegrationLoop(t, database, cfg, bldr, judge)

	result, err := loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}

	t.Logf("HypothesesGenerated=%d TokensUsed=%d PartialScoring=%v JudgeCalls=%d",
		result.HypothesesGenerated, result.TokensUsed, result.PartialScoring, judge.callCount())

	if result.TokensUsed > cfg.MaxTokensPerCycle {
		t.Errorf("TokensUsed (%d) exceeded MaxTokensPerCycle (%d)",
			result.TokensUsed, cfg.MaxTokensPerCycle)
	}

	// If 3 or more hypotheses were generated, partial scoring must have fired.
	if result.HypothesesGenerated >= 3 && !result.PartialScoring {
		t.Error("expected PartialScoring=true with token cap tight and 3+ hypotheses")
	}

	// All proposals must be written (static-only fallback still produces a proposal).
	proposalCount := queryProposalCount(t, database.SQL())
	if proposalCount != result.HypothesesGenerated {
		t.Errorf("DB proposal count (%d) != HypothesesGenerated (%d)",
			proposalCount, result.HypothesesGenerated)
	}
}

// TestIntegration_CategoryBreakdown_MatchesWrittenRows asserts that
// CycleResult.CategoryBreakdown matches what's actually written in the DB.
func TestIntegration_CategoryBreakdown_MatchesWrittenRows(t *testing.T) {
	database := openTestDatabase(t)
	cfg := integrationCycleConfig()

	bldr := &stubBuilder{bundle: bundleWithFeatureIdeaTriggers()}
	judge := &stubJudge{
		scores:     scoring.LLMScores{Impact: 0.6, Effort: 0.2, Confidence: 0.9, Novelty: 0.4},
		tokensEach: 100,
	}

	loop := buildIntegrationLoop(t, database, cfg, bldr, judge)

	result, err := loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}

	if result.HypothesesGenerated == 0 {
		t.Skip("no hypotheses generated — check bundleWithFeatureIdeaTriggers")
	}

	dbCounts := queryProposalsByType(t, database.SQL())

	// Sum of CategoryBreakdown must equal HypothesesGenerated.
	breakdownTotal := 0
	for _, cnt := range result.CategoryBreakdown {
		breakdownTotal += cnt
	}
	if breakdownTotal != result.HypothesesGenerated {
		t.Errorf("CategoryBreakdown sum (%d) != HypothesesGenerated (%d)",
			breakdownTotal, result.HypothesesGenerated)
	}

	// DB counts by type must match CategoryBreakdown.
	for category, breakdownCount := range result.CategoryBreakdown {
		dbCount := dbCounts[category]
		if dbCount != breakdownCount {
			t.Errorf("category %q: CategoryBreakdown=%d but DB has %d rows",
				category, breakdownCount, dbCount)
		}
	}

	// DB must not have extra categories that weren't in the breakdown.
	for category, dbCount := range dbCounts {
		if _, ok := result.CategoryBreakdown[category]; !ok {
			t.Errorf("DB has %d rows for category %q not present in CategoryBreakdown",
				dbCount, category)
		}
	}

	t.Logf("CategoryBreakdown: %v", result.CategoryBreakdown)
}

// TestIntegration_LegacyRowsCoexist verifies that pre-existing rows with
// legacy categories (workflow_routing) coexist with new rows without any
// schema violations, and the legacy rows remain readable after a new cycle.
func TestIntegration_LegacyRowsCoexist(t *testing.T) {
	database := openTestDatabase(t)

	// Pre-insert a legacy row directly via SQL.
	_, err := database.SQL().Exec(`
		INSERT INTO insight_proposals (
			id, type, status, title, description,
			confidence, risk_level, source_pattern_id, evidence, recommendation,
			created_at, updated_at
		) VALUES (
			'legacy-001', 'workflow_routing', 'pending',
			'Legacy routing proposal', '{"legacy":true}',
			0.5, 'low', '', '{}', '{}',
			datetime('now'), datetime('now')
		)`,
	)
	if err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	// Verify legacy row is readable before the cycle.
	var legacyID, legacyType string
	if err := database.SQL().QueryRow(
		`SELECT id, type FROM insight_proposals WHERE id = 'legacy-001'`,
	).Scan(&legacyID, &legacyType); err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if legacyType != "workflow_routing" {
		t.Errorf("legacy row type = %q, want 'workflow_routing'", legacyType)
	}

	// Run a cycle producing only new-category proposals.
	cfg := integrationCycleConfig()
	cfg.AllowedEvolutionCategories = []string{"test_gap", "doc_drift"}

	bldr := &stubBuilder{
		bundle: baseline.Bundle{
			ProjectRoot: ".",
			TestRatios: []baseline.TestRatio{
				{Dir: "api", SourceFiles: 10, TestFiles: 0, Ratio: 0.0},
			},
			WikiTitles: []baseline.WikiTitle{
				{ID: "wiki-x", Title: "Outdated Guide", Staleness: 0.85},
			},
		},
	}

	loop := buildIntegrationLoop(t, database, cfg, bldr, nil)
	_, err = loop.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}

	// Legacy row must still be readable after the cycle.
	if err := database.SQL().QueryRow(
		`SELECT id FROM insight_proposals WHERE id = 'legacy-001'`,
	).Scan(&legacyID); err != nil {
		t.Fatalf("legacy row disappeared after cycle: %v", err)
	}

	// Total rows must include the legacy row.
	totalProposals := queryProposalCount(t, database.SQL())
	if totalProposals < 1 {
		t.Error("expected at least the legacy row to remain in insight_proposals")
	}

	dbCounts := queryProposalsByType(t, database.SQL())
	if dbCounts["workflow_routing"] != 1 {
		t.Errorf("expected 1 legacy workflow_routing row, got %d", dbCounts["workflow_routing"])
	}

	t.Logf("total proposals=%d by type=%v", totalProposals, dbCounts)
}
