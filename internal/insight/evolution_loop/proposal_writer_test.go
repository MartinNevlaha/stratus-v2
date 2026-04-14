package evolution_loop_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeFeatureIdeaInput(title string, signals []string) evolution_loop.ProposalInput {
	return evolution_loop.ProposalInput{
		Hypothesis: scoring.Hypothesis{
			Category:   "feature_idea",
			Title:      title,
			Rationale:  "This is a test rationale.",
			FileRefs:   []string{"pkg/foo/bar.go"},
			SymbolRefs: []string{"Bar"},
			SignalRefs: signals,
		},
		Final: 0.75,
		Static: scoring.StaticScores{
			Churn:        0.6,
			TestGap:      0.4,
			TODO:         0.2,
			Staleness:    0.1,
			ADRViolation: 0.0,
		},
		LLM: scoring.LLMScores{
			Impact:     0.8,
			Effort:     0.3,
			Confidence: 0.9,
			Novelty:    0.7,
		},
		Breakdown: map[string]float64{
			"churn": 0.12,
			"llm":   0.63,
		},
	}
}

func makeRefactorInput(title string, signals []string) evolution_loop.ProposalInput {
	in := makeFeatureIdeaInput(title, signals)
	in.Hypothesis.Category = "refactor_opportunity"
	return in
}

// countProposals returns the total number of rows in insight_proposals.
func countProposals(t *testing.T, database interface{ SQL() *sql.DB }) int {
	t.Helper()
	var n int
	if err := database.SQL().QueryRow(`SELECT COUNT(*) FROM insight_proposals`).Scan(&n); err != nil {
		t.Fatalf("count proposals: %v", err)
	}
	return n
}

// countWikiPages returns the total number of rows in wiki_pages.
func countWikiPages(t *testing.T, database interface{ SQL() *sql.DB }) int {
	t.Helper()
	var n int
	if err := database.SQL().QueryRow(`SELECT COUNT(*) FROM wiki_pages`).Scan(&n); err != nil {
		t.Fatalf("count wiki_pages: %v", err)
	}
	return n
}

// ---------------------------------------------------------------------------
// Test cases
// ---------------------------------------------------------------------------

// TestProposalWriter_FirstWriteInserts verifies that the first write creates a
// new row and returns Inserted=true.
func TestProposalWriter_FirstWriteInserts(t *testing.T) {
	database := openTestDatabase(t)
	w := evolution_loop.NewProposalWriter(database)

	in := makeFeatureIdeaInput("Add dark mode toggle", []string{"churn:ui.go", "todo:dark"})
	res, err := w.Write(context.Background(), in)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !res.Inserted {
		t.Error("expected Inserted=true on first write")
	}
	if res.ProposalID == "" {
		t.Error("expected non-empty ProposalID")
	}
	if res.LastSeenAt.IsZero() {
		t.Error("expected non-zero LastSeenAt")
	}
	if countProposals(t, database) != 1 {
		t.Errorf("expected 1 proposal row, got %d", countProposals(t, database))
	}
}

// TestProposalWriter_SecondWriteSameSignals verifies that a duplicate write
// (same idempotency hash) takes the ON CONFLICT path: Inserted=false and
// last_seen_at is updated, but no new row is created.
func TestProposalWriter_SecondWriteSameSignals(t *testing.T) {
	database := openTestDatabase(t)
	w := evolution_loop.NewProposalWriter(database)

	in := makeRefactorInput("Refactor DB layer", []string{"churn:db.go", "todo_count:5"})

	res1, err := w.Write(context.Background(), in)
	if err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if !res1.Inserted {
		t.Fatal("expected first write to be inserted")
	}

	res2, err := w.Write(context.Background(), in)
	if err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if res2.Inserted {
		t.Error("expected Inserted=false on second write with same signals")
	}
	if res2.ProposalID != res1.ProposalID {
		t.Errorf("ProposalID mismatch: got %q, want %q", res2.ProposalID, res1.ProposalID)
	}
	if countProposals(t, database) != 1 {
		t.Errorf("expected exactly 1 proposal row after duplicate write, got %d", countProposals(t, database))
	}

	// last_seen_at should be updated (not guaranteed to differ in sub-second
	// tests, but the column should be set to a non-zero value).
	var lastSeen int64
	if err := database.SQL().QueryRow(
		`SELECT last_seen_at FROM insight_proposals WHERE id = ?`, res1.ProposalID,
	).Scan(&lastSeen); err != nil {
		t.Fatalf("read last_seen_at: %v", err)
	}
	if lastSeen == 0 {
		t.Error("expected last_seen_at to be non-zero after update")
	}
}

// TestProposalWriter_SignalOrderIrrelevant verifies that signal order does not
// affect the idempotency hash — writing with shuffled signals still hits the
// ON CONFLICT path.
func TestProposalWriter_SignalOrderIrrelevant(t *testing.T) {
	database := openTestDatabase(t)
	w := evolution_loop.NewProposalWriter(database)

	signals1 := []string{"adr:001", "churn:foo.go", "todo:3"}
	signals2 := []string{"todo:3", "adr:001", "churn:foo.go"}

	in1 := makeRefactorInput("Reduce coupling", signals1)
	in2 := makeRefactorInput("Reduce coupling", signals2)

	res1, err := w.Write(context.Background(), in1)
	if err != nil {
		t.Fatalf("first Write: %v", err)
	}
	res2, err := w.Write(context.Background(), in2)
	if err != nil {
		t.Fatalf("second Write: %v", err)
	}

	if res2.Inserted {
		t.Error("expected ON CONFLICT path when signals reordered")
	}
	if res1.ProposalID != res2.ProposalID {
		t.Errorf("ProposalID mismatch: got %q, want %q", res2.ProposalID, res1.ProposalID)
	}
	if countProposals(t, database) != 1 {
		t.Errorf("expected 1 proposal, got %d", countProposals(t, database))
	}
}

// TestProposalWriter_FeatureIdeaWritesWikiPage verifies that a feature_idea on
// the insert path creates a wiki_pages row whose content contains the proposal id.
func TestProposalWriter_FeatureIdeaWritesWikiPage(t *testing.T) {
	database := openTestDatabase(t)
	w := evolution_loop.NewProposalWriter(database)

	in := makeFeatureIdeaInput("Add real-time collaboration", []string{"churn:collab.go"})
	res, err := w.Write(context.Background(), in)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if res.WikiPageID == "" {
		t.Fatal("expected non-empty WikiPageID for feature_idea")
	}
	if !strings.HasPrefix(res.WikiPageID, "ideas/") {
		t.Errorf("WikiPageID should start with 'ideas/', got %q", res.WikiPageID)
	}
	if countWikiPages(t, database) != 1 {
		t.Errorf("expected 1 wiki_pages row, got %d", countWikiPages(t, database))
	}

	// Content must contain the proposal_id in the frontmatter.
	var content string
	if err := database.SQL().QueryRow(
		`SELECT content FROM wiki_pages WHERE id = ?`, res.WikiPageID,
	).Scan(&content); err != nil {
		t.Fatalf("read wiki content: %v", err)
	}
	if !strings.Contains(content, res.ProposalID) {
		t.Errorf("wiki content does not contain proposal_id %q", res.ProposalID)
	}
	if !strings.Contains(content, "# Add real-time collaboration") {
		t.Error("wiki content missing h1 title")
	}
}

// TestProposalWriter_NonIdeaCategorySkipsWiki verifies that non-feature_idea
// categories do not create a wiki page.
func TestProposalWriter_NonIdeaCategorySkipsWiki(t *testing.T) {
	database := openTestDatabase(t)
	w := evolution_loop.NewProposalWriter(database)

	in := makeRefactorInput("Extract service layer", []string{"churn:svc.go"})
	res, err := w.Write(context.Background(), in)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if res.WikiPageID != "" {
		t.Errorf("expected empty WikiPageID for refactor_opportunity, got %q", res.WikiPageID)
	}
	if countWikiPages(t, database) != 0 {
		t.Errorf("expected 0 wiki_pages rows for non-feature_idea, got %d", countWikiPages(t, database))
	}
}

// openFileTestDatabase opens a temp-file-backed SQLite DB for tests that need
// concurrent connections (in-memory DBs are per-connection and cannot be shared).
func openFileTestDatabase(t *testing.T) *db.DB {
	t.Helper()
	tmpFile := t.TempDir() + "/test.db"
	database, err := db.Open(tmpFile)
	if err != nil {
		t.Fatalf("open file test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// TestProposalWriter_ConcurrentWriters spawns two goroutines writing the exact
// same hypothesis and asserts that exactly one insert and one update result are
// returned (or both see Inserted=false after the first commit), and no UNIQUE
// constraint panic occurs.
func TestProposalWriter_ConcurrentWriters(t *testing.T) {
	database := openFileTestDatabase(t)
	w := evolution_loop.NewProposalWriter(database)

	in := makeRefactorInput("Concurrent hypothesis", []string{"signal:x", "signal:y"})

	type outcome struct {
		res evolution_loop.ProposalResult
		err error
	}
	results := make([]outcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		i := i
		go func() {
			defer wg.Done()
			res, err := w.Write(context.Background(), in)
			results[i] = outcome{res, err}
		}()
	}
	wg.Wait()

	// Both must succeed (no panics, no unhandled errors).
	for i, o := range results {
		if o.err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, o.err)
		}
	}

	// Exactly one proposal row must exist.
	if n := countProposals(t, database); n != 1 {
		t.Errorf("expected 1 proposal row after concurrent writes, got %d", n)
	}

	// At most one of them should be Inserted=true (the other races and sees the
	// conflict path).  It is also valid for both to see Inserted=false if the
	// first commit is visible before the second BEGIN IMMEDIATE.
	insertedCount := 0
	for _, o := range results {
		if o.res.Inserted {
			insertedCount++
		}
	}
	if insertedCount > 1 {
		t.Errorf("at most 1 goroutine should see Inserted=true, got %d", insertedCount)
	}
}

// TestProposalWriter_JSONDescriptionRoundTrip inserts a proposal and reads back
// the description column, asserting that the key fields survive JSON marshalling.
func TestProposalWriter_JSONDescriptionRoundTrip(t *testing.T) {
	database := openTestDatabase(t)
	w := evolution_loop.NewProposalWriter(database)

	in := makeFeatureIdeaInput("Improve search UX", []string{"churn:search.go"})
	res, err := w.Write(context.Background(), in)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	var descRaw string
	if err := database.SQL().QueryRow(
		`SELECT description FROM insight_proposals WHERE id = ?`, res.ProposalID,
	).Scan(&descRaw); err != nil {
		t.Fatalf("read description: %v", err)
	}

	var parsed struct {
		Rationale  string             `json:"rationale"`
		FileRefs   []string           `json:"file_refs"`
		SymbolRefs []string           `json:"symbol_refs"`
		Scores     map[string]any     `json:"scores"`
		Breakdown  map[string]float64 `json:"breakdown"`
	}
	if err := json.Unmarshal([]byte(descRaw), &parsed); err != nil {
		t.Fatalf("unmarshal description: %v", err)
	}

	if parsed.Rationale != in.Hypothesis.Rationale {
		t.Errorf("rationale = %q, want %q", parsed.Rationale, in.Hypothesis.Rationale)
	}
	if len(parsed.FileRefs) != 1 || parsed.FileRefs[0] != "pkg/foo/bar.go" {
		t.Errorf("file_refs = %v, want [pkg/foo/bar.go]", parsed.FileRefs)
	}
	if parsed.Scores == nil {
		t.Error("scores field should not be nil")
	}
	finalScore, ok := parsed.Scores["final"].(float64)
	if !ok || finalScore != in.Final {
		t.Errorf("scores.final = %v, want %v", parsed.Scores["final"], in.Final)
	}
	if len(parsed.Breakdown) == 0 {
		t.Error("breakdown should not be empty")
	}
}

// TestProposalWriter_FeatureIdeaSecondWriteFetchesWikiID verifies that on the
// update path for a feature_idea the existing wiki_page_id is returned in the
// result rather than creating a second wiki page.
func TestProposalWriter_FeatureIdeaSecondWriteFetchesWikiID(t *testing.T) {
	database := openTestDatabase(t)
	w := evolution_loop.NewProposalWriter(database)

	in := makeFeatureIdeaInput("Add push notifications", []string{"churn:notif.go"})

	res1, err := w.Write(context.Background(), in)
	if err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if res1.WikiPageID == "" {
		t.Fatal("expected WikiPageID on first write")
	}

	res2, err := w.Write(context.Background(), in)
	if err != nil {
		t.Fatalf("second Write: %v", err)
	}

	if res2.WikiPageID != res1.WikiPageID {
		t.Errorf("second write WikiPageID = %q, want %q", res2.WikiPageID, res1.WikiPageID)
	}
	if countWikiPages(t, database) != 1 {
		t.Errorf("expected exactly 1 wiki_pages row after duplicate feature_idea write, got %d", countWikiPages(t, database))
	}
}
