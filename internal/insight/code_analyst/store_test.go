package code_analyst_test

import (
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/code_analyst"
)

func openTestStore(t *testing.T) code_analyst.CodeAnalystStore {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})

	return code_analyst.NewDBCodeAnalystStore(database)
}

// --- helpers ---

func makeRun(status string) *db.CodeAnalysisRun {
	return &db.CodeAnalysisRun{
		Status:        status,
		FilesScanned:  100,
		FilesAnalyzed: 80,
		FindingsCount: 5,
		GitCommitHash: "abc123",
		Metadata:      map[string]any{"source": "test"},
	}
}

func makeFinding(runID, category, severity string) *db.CodeFinding {
	return &db.CodeFinding{
		RunID:       runID,
		FilePath:    "internal/pkg/foo.go",
		Category:    category,
		Severity:    severity,
		Title:       "Example finding",
		Description: "This is a test finding description",
		LineStart:   10,
		LineEnd:     20,
		Confidence:  0.85,
		Suggestion:  "Refactor this function",
		Evidence:    map[string]any{"count": 3},
	}
}

// --- SaveRun + GetRun ---

// TestSaveRun_GetRun_RoundTrip verifies that SaveRun assigns an ID and GetRun returns the same data.
func TestSaveRun_GetRun_RoundTrip(t *testing.T) {
	store := openTestStore(t)

	run := makeRun("running")
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if run.ID == "" {
		t.Fatal("expected ID to be generated, got empty string")
	}

	got, err := store.GetRun(run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got == nil {
		t.Fatal("expected run, got nil")
	}

	if got.ID != run.ID {
		t.Errorf("ID: got %q want %q", got.ID, run.ID)
	}
	if got.Status != "running" {
		t.Errorf("Status: got %q want running", got.Status)
	}
	if got.FilesScanned != 100 {
		t.Errorf("FilesScanned: got %d want 100", got.FilesScanned)
	}
	if got.GitCommitHash != "abc123" {
		t.Errorf("GitCommitHash: got %q want abc123", got.GitCommitHash)
	}
	if got.Metadata["source"] != "test" {
		t.Errorf("Metadata not round-tripped: got %v", got.Metadata)
	}
}

// TestGetRun_NotFound verifies that a missing ID returns (nil, nil).
func TestGetRun_NotFound(t *testing.T) {
	store := openTestStore(t)

	got, err := store.GetRun("nonexistent-id")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// --- ListRuns pagination ---

// TestListRuns_Pagination verifies total count and page slicing.
func TestListRuns_Pagination(t *testing.T) {
	store := openTestStore(t)

	for i := 0; i < 5; i++ {
		r := makeRun("completed")
		if err := store.SaveRun(r); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
	}

	all, total, err := store.ListRuns(10, 0)
	if err != nil {
		t.Fatalf("ListRuns (all): %v", err)
	}
	if total != 5 {
		t.Errorf("total count: got %d want 5", total)
	}
	if len(all) != 5 {
		t.Errorf("list length: got %d want 5", len(all))
	}

	page, total2, err := store.ListRuns(3, 3)
	if err != nil {
		t.Fatalf("ListRuns (page 2): %v", err)
	}
	if total2 != 5 {
		t.Errorf("total on page 2: got %d want 5", total2)
	}
	if len(page) != 2 {
		t.Errorf("page 2 length: got %d want 2", len(page))
	}
}

// --- UpdateRun ---

// TestUpdateRun_StatusAndCounters verifies that mutable fields are persisted.
func TestUpdateRun_StatusAndCounters(t *testing.T) {
	store := openTestStore(t)

	run := makeRun("running")
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	completedAt := time.Now().UTC().Format(time.RFC3339Nano)
	run.Status = "completed"
	run.FilesScanned = 200
	run.FindingsCount = 12
	run.DurationMs = 45000
	run.CompletedAt = &completedAt

	if err := store.UpdateRun(run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	got, err := store.GetRun(run.ID)
	if err != nil {
		t.Fatalf("GetRun after update: %v", err)
	}
	if got == nil {
		t.Fatal("expected run after update, got nil")
	}
	if got.Status != "completed" {
		t.Errorf("Status: got %q want completed", got.Status)
	}
	if got.FilesScanned != 200 {
		t.Errorf("FilesScanned: got %d want 200", got.FilesScanned)
	}
	if got.DurationMs != 45000 {
		t.Errorf("DurationMs: got %d want 45000", got.DurationMs)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt: expected non-nil after update")
	}
}

// --- SaveFinding + ListFindings with filters ---

// TestSaveFinding_ListFindings_RoundTrip verifies INSERT and retrieval via ListFindings.
func TestSaveFinding_ListFindings_RoundTrip(t *testing.T) {
	store := openTestStore(t)

	run := makeRun("completed")
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	f := makeFinding(run.ID, "security", "high")
	if err := store.SaveFinding(f); err != nil {
		t.Fatalf("SaveFinding: %v", err)
	}
	if f.ID == "" {
		t.Fatal("expected ID to be generated, got empty string")
	}

	findings, total, err := store.ListFindings(db.CodeFindingFilters{RunID: run.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListFindings: %v", err)
	}
	if total != 1 {
		t.Errorf("total: got %d want 1", total)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}

	got := findings[0]
	if got.ID != f.ID {
		t.Errorf("ID: got %q want %q", got.ID, f.ID)
	}
	if got.Category != "security" {
		t.Errorf("Category: got %q want security", got.Category)
	}
	if got.Severity != "high" {
		t.Errorf("Severity: got %q want high", got.Severity)
	}
	if got.Confidence != 0.85 {
		t.Errorf("Confidence: got %f want 0.85", got.Confidence)
	}
}

// TestListFindings_FilterByCategory verifies category filtering.
func TestListFindings_FilterByCategory(t *testing.T) {
	store := openTestStore(t)

	run := makeRun("completed")
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	for _, cat := range []string{"security", "security", "performance"} {
		f := makeFinding(run.ID, cat, "medium")
		if err := store.SaveFinding(f); err != nil {
			t.Fatalf("SaveFinding (%s): %v", cat, err)
		}
	}

	sec, total, err := store.ListFindings(db.CodeFindingFilters{Category: "security", Limit: 10})
	if err != nil {
		t.Fatalf("ListFindings (security): %v", err)
	}
	if total != 2 {
		t.Errorf("security total: got %d want 2", total)
	}
	if len(sec) != 2 {
		t.Errorf("security list length: got %d want 2", len(sec))
	}
}

// TestListFindings_FilterBySeverity verifies severity filtering.
func TestListFindings_FilterBySeverity(t *testing.T) {
	store := openTestStore(t)

	run := makeRun("completed")
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	for _, sev := range []string{"critical", "high", "high", "info"} {
		f := makeFinding(run.ID, "security", sev)
		if err := store.SaveFinding(f); err != nil {
			t.Fatalf("SaveFinding (%s): %v", sev, err)
		}
	}

	highs, total, err := store.ListFindings(db.CodeFindingFilters{Severity: "high", Limit: 10})
	if err != nil {
		t.Fatalf("ListFindings (high): %v", err)
	}
	if total != 2 {
		t.Errorf("high total: got %d want 2", total)
	}
	if len(highs) != 2 {
		t.Errorf("high list length: got %d want 2", len(highs))
	}
}

// TestSearchFindings_FTS5 verifies full-text search delegation.
func TestSearchFindings_FTS5(t *testing.T) {
	store := openTestStore(t)

	run := makeRun("completed")
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	findings := []*db.CodeFinding{
		{
			RunID:       run.ID,
			FilePath:    "pkg/auth.go",
			Category:    "security",
			Severity:    "high",
			Title:       "SQL injection vulnerability detected",
			Description: "User input is concatenated directly into SQL query",
			Suggestion:  "Use parameterized queries instead",
		},
		{
			RunID:       run.ID,
			FilePath:    "pkg/handler.go",
			Category:    "performance",
			Severity:    "medium",
			Title:       "N+1 database query pattern",
			Description: "Loop executes a query for each iteration",
			Suggestion:  "Batch load with a single JOIN query",
		},
	}
	for _, f := range findings {
		if err := store.SaveFinding(f); err != nil {
			t.Fatalf("SaveFinding: %v", err)
		}
	}

	results, err := store.SearchFindings("injection", 10)
	if err != nil {
		t.Fatalf("SearchFindings: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'injection', got %d", len(results))
	}
	if results[0].Category != "security" {
		t.Errorf("unexpected category: got %q want security", results[0].Category)
	}
}

// --- SaveMetric + ListMetrics ---

// TestSaveMetric_ListMetrics_RoundTrip verifies metric INSERT and retrieval.
func TestSaveMetric_ListMetrics_RoundTrip(t *testing.T) {
	store := openTestStore(t)

	m := &db.CodeQualityMetric{
		MetricDate:         "2026-04-12",
		TotalFiles:         500,
		FilesAnalyzed:      450,
		FindingsTotal:      30,
		FindingsBySeverity: map[string]int{"high": 5, "medium": 15},
		FindingsByCategory: map[string]int{"security": 8, "performance": 12},
		AvgChurnScore:      0.35,
		AvgCoverage:        0.72,
	}

	if err := store.SaveMetric(m); err != nil {
		t.Fatalf("SaveMetric: %v", err)
	}
	if m.ID == "" {
		t.Fatal("expected ID to be generated, got empty string")
	}

	list, err := store.ListMetrics(30)
	if err != nil {
		t.Fatalf("ListMetrics: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(list))
	}

	got := list[0]
	if got.MetricDate != "2026-04-12" {
		t.Errorf("MetricDate: got %q want 2026-04-12", got.MetricDate)
	}
	if got.TotalFiles != 500 {
		t.Errorf("TotalFiles: got %d want 500", got.TotalFiles)
	}
	if got.FindingsTotal != 30 {
		t.Errorf("FindingsTotal: got %d want 30", got.FindingsTotal)
	}
	if got.FindingsBySeverity["high"] != 5 {
		t.Errorf("FindingsBySeverity[high]: got %d want 5", got.FindingsBySeverity["high"])
	}
	if got.AvgChurnScore != 0.35 {
		t.Errorf("AvgChurnScore: got %f want 0.35", got.AvgChurnScore)
	}
}

// TestListMetrics_Empty verifies that no results returns an empty slice without error.
func TestListMetrics_Empty(t *testing.T) {
	store := openTestStore(t)

	list, err := store.ListMetrics(30)
	if err != nil {
		t.Fatalf("ListMetrics (empty): %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 metrics, got %d", len(list))
	}
}

// --- GetFileCache + SetFileCache ---

// TestSetFileCache_GetFileCache_RoundTrip verifies cache INSERT and retrieval.
func TestSetFileCache_GetFileCache_RoundTrip(t *testing.T) {
	store := openTestStore(t)

	path := "internal/auth/handler.go"
	gitHash := "deadbeef"
	runID := "run-123"

	if err := store.SetFileCache(path, gitHash, runID, 0.75, 3); err != nil {
		t.Fatalf("SetFileCache: %v", err)
	}

	got, err := store.GetFileCache(path)
	if err != nil {
		t.Fatalf("GetFileCache: %v", err)
	}
	if got == nil {
		t.Fatal("expected entry, got nil")
	}

	if got.FilePath != path {
		t.Errorf("FilePath: got %q want %q", got.FilePath, path)
	}
	if got.GitHash != gitHash {
		t.Errorf("GitHash: got %q want %q", got.GitHash, gitHash)
	}
	if got.LastRunID != runID {
		t.Errorf("LastRunID: got %q want %q", got.LastRunID, runID)
	}
	if got.CompositeScore != 0.75 {
		t.Errorf("CompositeScore: got %f want 0.75", got.CompositeScore)
	}
	if got.FindingsCount != 3 {
		t.Errorf("FindingsCount: got %d want 3", got.FindingsCount)
	}
	if got.LastAnalyzedAt == "" {
		t.Error("expected LastAnalyzedAt to be set")
	}
}

// TestSetFileCache_Upsert verifies that calling SetFileCache twice for the same path updates the row.
func TestSetFileCache_Upsert(t *testing.T) {
	store := openTestStore(t)

	path := "pkg/service.go"

	if err := store.SetFileCache(path, "hash1", "run-1", 0.50, 2); err != nil {
		t.Fatalf("SetFileCache (insert): %v", err)
	}
	if err := store.SetFileCache(path, "hash2", "run-2", 0.90, 7); err != nil {
		t.Fatalf("SetFileCache (update): %v", err)
	}

	got, err := store.GetFileCache(path)
	if err != nil {
		t.Fatalf("GetFileCache after upsert: %v", err)
	}
	if got == nil {
		t.Fatal("expected entry after upsert, got nil")
	}

	if got.GitHash != "hash2" {
		t.Errorf("GitHash after upsert: got %q want hash2", got.GitHash)
	}
	if got.CompositeScore != 0.90 {
		t.Errorf("CompositeScore after upsert: got %f want 0.90", got.CompositeScore)
	}
	if got.FindingsCount != 7 {
		t.Errorf("FindingsCount after upsert: got %d want 7", got.FindingsCount)
	}
}

// TestGetFileCache_NotFound verifies that a missing path returns (nil, nil).
func TestGetFileCache_NotFound(t *testing.T) {
	store := openTestStore(t)

	got, err := store.GetFileCache("no/such/file.go")
	if err != nil {
		t.Fatalf("GetFileCache (not found): %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing entry, got %+v", got)
	}
}

// Ensure the time import is used.
var _ = time.RFC3339Nano
