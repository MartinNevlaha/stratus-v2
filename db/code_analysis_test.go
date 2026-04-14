package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// --- helpers ---

func makeCodeAnalysisRun(status string) *CodeAnalysisRun {
	return &CodeAnalysisRun{
		Status:        status,
		FilesScanned:  100,
		FilesAnalyzed: 80,
		FindingsCount: 5,
		GitCommitHash: "abc123",
		Metadata:      map[string]any{"source": "test"},
	}
}

func makeCodeFinding(runID, category, severity string) *CodeFinding {
	return &CodeFinding{
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

// --- CodeAnalysisRun tests ---

// TestSaveCodeAnalysisRun_GeneratesIDAndRoundTrips verifies that SaveCodeAnalysisRun
// assigns a UUID when ID is empty and that GetCodeAnalysisRun returns identical data.
func TestSaveCodeAnalysisRun_GeneratesIDAndRoundTrips(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("running")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}
	if run.ID == "" {
		t.Fatal("expected ID to be generated, got empty string")
	}

	got, err := db.GetCodeAnalysisRun(run.ID)
	if err != nil {
		t.Fatalf("GetCodeAnalysisRun: %v", err)
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
	if got.CompletedAt != nil {
		t.Errorf("CompletedAt expected nil, got %v", got.CompletedAt)
	}
}

// TestGetCodeAnalysisRun_NotFound verifies that a missing ID returns (nil, nil).
func TestGetCodeAnalysisRun_NotFound(t *testing.T) {
	db := openTestDB(t)

	got, err := db.GetCodeAnalysisRun("nonexistent-id")
	if err != nil {
		t.Fatalf("GetCodeAnalysisRun: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// TestListCodeAnalysisRuns_Pagination verifies pagination and total count.
func TestListCodeAnalysisRuns_Pagination(t *testing.T) {
	db := openTestDB(t)

	for i := 0; i < 5; i++ {
		r := makeCodeAnalysisRun("completed")
		if err := db.SaveCodeAnalysisRun(r); err != nil {
			t.Fatalf("SaveCodeAnalysisRun: %v", err)
		}
	}

	all, total, err := db.ListCodeAnalysisRuns(10, 0)
	if err != nil {
		t.Fatalf("ListCodeAnalysisRuns (all): %v", err)
	}
	if total != 5 {
		t.Errorf("total count: got %d want 5", total)
	}
	if len(all) != 5 {
		t.Errorf("list length: got %d want 5", len(all))
	}

	page, total2, err := db.ListCodeAnalysisRuns(3, 3)
	if err != nil {
		t.Fatalf("ListCodeAnalysisRuns (page 2): %v", err)
	}
	if total2 != 5 {
		t.Errorf("total on page 2: got %d want 5", total2)
	}
	if len(page) != 2 {
		t.Errorf("page 2 length: got %d want 2", len(page))
	}
}

// TestUpdateCodeAnalysisRun_StatusAndCounters verifies that mutable fields are persisted.
func TestUpdateCodeAnalysisRun_StatusAndCounters(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("running")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	completedAt := time.Now().UTC().Format(time.RFC3339Nano)
	run.Status = "completed"
	run.FilesScanned = 200
	run.FilesAnalyzed = 180
	run.FindingsCount = 12
	run.WikiPagesCreated = 3
	run.WikiPagesUpdated = 7
	run.DurationMs = 45000
	run.TokensUsed = 50000
	run.CompletedAt = &completedAt
	run.Metadata = map[string]any{"updated": true}

	if err := db.UpdateCodeAnalysisRun(run); err != nil {
		t.Fatalf("UpdateCodeAnalysisRun: %v", err)
	}

	got, err := db.GetCodeAnalysisRun(run.ID)
	if err != nil {
		t.Fatalf("GetCodeAnalysisRun after update: %v", err)
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
	if got.FindingsCount != 12 {
		t.Errorf("FindingsCount: got %d want 12", got.FindingsCount)
	}
	if got.WikiPagesCreated != 3 {
		t.Errorf("WikiPagesCreated: got %d want 3", got.WikiPagesCreated)
	}
	if got.DurationMs != 45000 {
		t.Errorf("DurationMs: got %d want 45000", got.DurationMs)
	}
	if got.TokensUsed != 50000 {
		t.Errorf("TokensUsed: got %d want 50000", got.TokensUsed)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt: expected non-nil")
	}
	if got.Metadata["updated"] != true {
		t.Errorf("Metadata not updated: got %v", got.Metadata)
	}
}

// TestSaveCodeAnalysisRun_NilMetadataDefaultsToEmptyMap verifies nil Metadata
// is stored as an empty JSON object and unmarshalled to an initialised map.
func TestSaveCodeAnalysisRun_NilMetadataDefaultsToEmptyMap(t *testing.T) {
	db := openTestDB(t)

	run := &CodeAnalysisRun{
		Status: "running",
		// Metadata deliberately left nil
	}
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	got, err := db.GetCodeAnalysisRun(run.ID)
	if err != nil {
		t.Fatalf("GetCodeAnalysisRun: %v", err)
	}
	if got.Metadata == nil {
		t.Error("expected Metadata to be initialised to empty map, got nil")
	}
}

// --- CodeFinding tests ---

// TestSaveCodeFinding_GeneratesIDAndRoundTrips verifies INSERT and SELECT via ListCodeFindings.
func TestSaveCodeFinding_GeneratesIDAndRoundTrips(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("running")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	f := makeCodeFinding(run.ID, "security", "high")
	if err := db.SaveCodeFinding(f); err != nil {
		t.Fatalf("SaveCodeFinding: %v", err)
	}
	if f.ID == "" {
		t.Fatal("expected ID to be generated, got empty string")
	}

	findings, total, err := db.ListCodeFindings(CodeFindingFilters{RunID: run.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings: %v", err)
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
	if got.Evidence["count"] == nil {
		t.Errorf("Evidence not round-tripped: got %v", got.Evidence)
	}
	if got.WikiPageID != nil {
		t.Errorf("WikiPageID expected nil, got %v", got.WikiPageID)
	}
}

// TestListCodeFindings_FilterByCategory verifies category filtering.
func TestListCodeFindings_FilterByCategory(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("completed")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	categories := []string{"security", "security", "performance", "style"}
	for _, cat := range categories {
		f := makeCodeFinding(run.ID, cat, "medium")
		if err := db.SaveCodeFinding(f); err != nil {
			t.Fatalf("SaveCodeFinding (%s): %v", cat, err)
		}
	}

	sec, total, err := db.ListCodeFindings(CodeFindingFilters{Category: "security", Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings (security): %v", err)
	}
	if total != 2 {
		t.Errorf("security total: got %d want 2", total)
	}
	if len(sec) != 2 {
		t.Errorf("security list length: got %d want 2", len(sec))
	}
	for _, f := range sec {
		if f.Category != "security" {
			t.Errorf("unexpected category %q in filtered list", f.Category)
		}
	}
}

// TestListCodeFindings_FilterBySeverity verifies severity filtering.
func TestListCodeFindings_FilterBySeverity(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("completed")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	severities := []string{"critical", "high", "high", "info"}
	for _, sev := range severities {
		f := makeCodeFinding(run.ID, "security", sev)
		if err := db.SaveCodeFinding(f); err != nil {
			t.Fatalf("SaveCodeFinding (%s): %v", sev, err)
		}
	}

	highs, total, err := db.ListCodeFindings(CodeFindingFilters{Severity: "high", Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings (high): %v", err)
	}
	if total != 2 {
		t.Errorf("high total: got %d want 2", total)
	}
	if len(highs) != 2 {
		t.Errorf("high list length: got %d want 2", len(highs))
	}
}

// TestListCodeFindings_FilterByFilePath verifies prefix-match file path filtering.
func TestListCodeFindings_FilterByFilePath(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("completed")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	paths := []string{
		"internal/pkg/auth.go",
		"internal/pkg/user.go",
		"cmd/main.go",
	}
	for _, p := range paths {
		f := makeCodeFinding(run.ID, "security", "medium")
		f.FilePath = p
		if err := db.SaveCodeFinding(f); err != nil {
			t.Fatalf("SaveCodeFinding (%s): %v", p, err)
		}
	}

	list, total, err := db.ListCodeFindings(CodeFindingFilters{FilePath: "internal/pkg/", Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings (file prefix): %v", err)
	}
	if total != 2 {
		t.Errorf("file prefix total: got %d want 2", total)
	}
	for _, f := range list {
		if !strings.HasPrefix(f.FilePath, "internal/pkg/") {
			t.Errorf("unexpected file_path %q in filtered list", f.FilePath)
		}
	}
}

// TestSaveCodeFinding_NilEvidenceDefaultsToEmptyMap verifies nil Evidence
// is stored as an empty JSON object and unmarshalled to an initialised map.
func TestSaveCodeFinding_NilEvidenceDefaultsToEmptyMap(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("running")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	f := &CodeFinding{
		RunID:    run.ID,
		FilePath: "pkg/foo.go",
		Category: "style",
		Severity: "info",
		Title:    "No evidence",
		// Evidence deliberately left nil
	}
	if err := db.SaveCodeFinding(f); err != nil {
		t.Fatalf("SaveCodeFinding: %v", err)
	}

	findings, _, err := db.ListCodeFindings(CodeFindingFilters{RunID: run.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected at least one finding")
	}
	if findings[0].Evidence == nil {
		t.Error("expected Evidence to be initialised to empty map, got nil")
	}
}

// TestSearchCodeFindings_FTS5 verifies full-text search over title/description.
func TestSearchCodeFindings_FTS5(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("completed")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	findings := []*CodeFinding{
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
		if err := db.SaveCodeFinding(f); err != nil {
			t.Fatalf("SaveCodeFinding: %v", err)
		}
	}

	results, err := db.SearchCodeFindings("injection", 10)
	if err != nil {
		t.Fatalf("SearchCodeFindings: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'injection', got %d", len(results))
	}
	if results[0].Category != "security" {
		t.Errorf("unexpected category: got %q want security", results[0].Category)
	}

	results2, err := db.SearchCodeFindings("query", 10)
	if err != nil {
		t.Fatalf("SearchCodeFindings (query): %v", err)
	}
	if len(results2) < 1 {
		t.Errorf("expected at least 1 result for 'query', got %d", len(results2))
	}
}

// --- CodeQualityMetric tests ---

// TestSaveCodeQualityMetric_RoundTrip verifies INSERT and retrieval via ListCodeQualityMetrics.
func TestSaveCodeQualityMetric_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	m := &CodeQualityMetric{
		MetricDate:         "2026-04-12",
		TotalFiles:         500,
		FilesAnalyzed:      450,
		FindingsTotal:      30,
		FindingsBySeverity: map[string]int{"high": 5, "medium": 15, "low": 10},
		FindingsByCategory: map[string]int{"security": 8, "performance": 12, "style": 10},
		AvgChurnScore:      0.35,
		AvgCoverage:        0.72,
	}

	if err := db.SaveCodeQualityMetric(m); err != nil {
		t.Fatalf("SaveCodeQualityMetric: %v", err)
	}
	if m.ID == "" {
		t.Fatal("expected ID to be generated, got empty string")
	}

	list, err := db.ListCodeQualityMetrics(30)
	if err != nil {
		t.Fatalf("ListCodeQualityMetrics: %v", err)
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
	if got.FindingsByCategory["security"] != 8 {
		t.Errorf("FindingsByCategory[security]: got %d want 8", got.FindingsByCategory["security"])
	}
	if got.AvgChurnScore != 0.35 {
		t.Errorf("AvgChurnScore: got %f want 0.35", got.AvgChurnScore)
	}
}

// TestSaveCodeQualityMetric_Upsert verifies that saving a second metric for the same date
// overwrites the existing row (UNIQUE constraint on metric_date).
func TestSaveCodeQualityMetric_Upsert(t *testing.T) {
	db := openTestDB(t)

	m1 := &CodeQualityMetric{
		MetricDate:    "2026-04-11",
		TotalFiles:    100,
		FindingsTotal: 10,
	}
	if err := db.SaveCodeQualityMetric(m1); err != nil {
		t.Fatalf("SaveCodeQualityMetric (first): %v", err)
	}

	m2 := &CodeQualityMetric{
		MetricDate:    "2026-04-11", // same date
		TotalFiles:    200,
		FindingsTotal: 20,
	}
	if err := db.SaveCodeQualityMetric(m2); err != nil {
		t.Fatalf("SaveCodeQualityMetric (upsert): %v", err)
	}

	list, err := db.ListCodeQualityMetrics(30)
	if err != nil {
		t.Fatalf("ListCodeQualityMetrics: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 metric after upsert, got %d", len(list))
	}
	if list[0].TotalFiles != 200 {
		t.Errorf("TotalFiles after upsert: got %d want 200", list[0].TotalFiles)
	}
}

// TestListCodeQualityMetrics_Empty verifies that no results returns an empty slice without error.
func TestListCodeQualityMetrics_Empty(t *testing.T) {
	db := openTestDB(t)

	list, err := db.ListCodeQualityMetrics(30)
	if err != nil {
		t.Fatalf("ListCodeQualityMetrics (empty): %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 metrics, got %d", len(list))
	}
}

// --- FileCacheEntry tests ---

// TestSetFileCacheEntry_InsertAndGet verifies that SetFileCacheEntry inserts and
// GetFileCacheEntry retrieves identical data.
func TestSetFileCacheEntry_InsertAndGet(t *testing.T) {
	db := openTestDB(t)

	path := "internal/auth/handler.go"
	gitHash := "deadbeef"
	runID := "run-123"

	if err := db.SetFileCacheEntry(path, gitHash, runID, 0.75, 3); err != nil {
		t.Fatalf("SetFileCacheEntry: %v", err)
	}

	got, err := db.GetFileCacheEntry(path)
	if err != nil {
		t.Fatalf("GetFileCacheEntry: %v", err)
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

// TestSetFileCacheEntry_Upsert verifies that calling SetFileCacheEntry twice
// for the same path updates the existing row.
func TestSetFileCacheEntry_Upsert(t *testing.T) {
	db := openTestDB(t)

	path := "pkg/service.go"

	if err := db.SetFileCacheEntry(path, "hash1", "run-1", 0.50, 2); err != nil {
		t.Fatalf("SetFileCacheEntry (insert): %v", err)
	}

	if err := db.SetFileCacheEntry(path, "hash2", "run-2", 0.90, 7); err != nil {
		t.Fatalf("SetFileCacheEntry (update): %v", err)
	}

	got, err := db.GetFileCacheEntry(path)
	if err != nil {
		t.Fatalf("GetFileCacheEntry after upsert: %v", err)
	}
	if got == nil {
		t.Fatal("expected entry after upsert, got nil")
	}

	if got.GitHash != "hash2" {
		t.Errorf("GitHash after upsert: got %q want hash2", got.GitHash)
	}
	if got.LastRunID != "run-2" {
		t.Errorf("LastRunID after upsert: got %q want run-2", got.LastRunID)
	}
	if got.CompositeScore != 0.90 {
		t.Errorf("CompositeScore after upsert: got %f want 0.90", got.CompositeScore)
	}
	if got.FindingsCount != 7 {
		t.Errorf("FindingsCount after upsert: got %d want 7", got.FindingsCount)
	}
}

// TestGetFileCacheEntry_NotFound verifies that a missing path returns (nil, nil).
func TestGetFileCacheEntry_NotFound(t *testing.T) {
	db := openTestDB(t)

	got, err := db.GetFileCacheEntry("no/such/file.go")
	if err != nil {
		t.Fatalf("GetFileCacheEntry (not found): %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing entry, got %+v", got)
	}
}

// TestSaveCodeFinding_WithWikiPageID verifies that a non-nil WikiPageID round-trips correctly.
func TestSaveCodeFinding_WithWikiPageID(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("completed")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	wikiID := "wiki-page-42"
	f := makeCodeFinding(run.ID, "security", "critical")
	f.WikiPageID = &wikiID

	if err := db.SaveCodeFinding(f); err != nil {
		t.Fatalf("SaveCodeFinding: %v", err)
	}

	findings, _, err := db.ListCodeFindings(CodeFindingFilters{RunID: run.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected findings, got none")
	}
	if findings[0].WikiPageID == nil {
		t.Error("WikiPageID: expected non-nil")
	} else if *findings[0].WikiPageID != wikiID {
		t.Errorf("WikiPageID: got %q want %q", *findings[0].WikiPageID, wikiID)
	}
}

// TestListCodeAnalysisRuns_DefaultLimit verifies that a zero limit defaults to 50.
func TestListCodeAnalysisRuns_DefaultLimit(t *testing.T) {
	db := openTestDB(t)

	// insert 3 runs
	for i := 0; i < 3; i++ {
		if err := db.SaveCodeAnalysisRun(makeCodeAnalysisRun("completed")); err != nil {
			t.Fatalf("SaveCodeAnalysisRun: %v", err)
		}
	}

	list, total, err := db.ListCodeAnalysisRuns(0, 0) // limit=0 should default to 50
	if err != nil {
		t.Fatalf("ListCodeAnalysisRuns: %v", err)
	}
	if total != 3 {
		t.Errorf("total: got %d want 3", total)
	}
	if len(list) != 3 {
		t.Errorf("list length: got %d want 3", len(list))
	}
}

// --- Status lifecycle tests ---

// TestSaveCodeFinding_DefaultsStatusToPending verifies that saving a finding with empty
// Status results in the persisted status being "pending".
func TestSaveCodeFinding_DefaultsStatusToPending(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("running")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	f := makeCodeFinding(run.ID, "security", "high")
	f.Status = "" // explicitly empty to exercise the default

	if err := db.SaveCodeFinding(f); err != nil {
		t.Fatalf("SaveCodeFinding: %v", err)
	}

	// Status field on struct should be updated in-place.
	if f.Status != "pending" {
		t.Errorf("in-memory Status after save: got %q want pending", f.Status)
	}

	findings, _, err := db.ListCodeFindings(CodeFindingFilters{RunID: run.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected at least one finding")
	}
	if findings[0].Status != "pending" {
		t.Errorf("persisted Status: got %q want pending", findings[0].Status)
	}
}

// TestListCodeFindings_FilterByStatus verifies that the Status filter correctly
// returns only findings matching the requested status value.
func TestListCodeFindings_FilterByStatus(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("completed")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	ctx := context.Background()

	// Save 3 findings (all default to "pending").
	var ids [3]string
	for i := range ids {
		f := makeCodeFinding(run.ID, "security", "high")
		if err := db.SaveCodeFinding(f); err != nil {
			t.Fatalf("SaveCodeFinding %d: %v", i, err)
		}
		ids[i] = f.ID
	}

	// Transition finding[1] → rejected, finding[2] → applied.
	if err := db.UpdateCodeFindingStatus(ctx, ids[1], "rejected"); err != nil {
		t.Fatalf("UpdateCodeFindingStatus (rejected): %v", err)
	}
	if err := db.UpdateCodeFindingStatus(ctx, ids[2], "applied"); err != nil {
		t.Fatalf("UpdateCodeFindingStatus (applied): %v", err)
	}

	// Filter: pending — should return exactly ids[0].
	pendingList, pendingTotal, err := db.ListCodeFindings(CodeFindingFilters{Status: "pending", Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings (pending): %v", err)
	}
	if pendingTotal != 1 {
		t.Errorf("pending total: got %d want 1", pendingTotal)
	}
	if len(pendingList) != 1 {
		t.Fatalf("pending list length: got %d want 1", len(pendingList))
	}
	if pendingList[0].ID != ids[0] {
		t.Errorf("pending finding ID: got %q want %q", pendingList[0].ID, ids[0])
	}

	// Filter: rejected — should return exactly ids[1].
	rejectedList, rejectedTotal, err := db.ListCodeFindings(CodeFindingFilters{Status: "rejected", Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings (rejected): %v", err)
	}
	if rejectedTotal != 1 {
		t.Errorf("rejected total: got %d want 1", rejectedTotal)
	}
	if len(rejectedList) != 1 {
		t.Fatalf("rejected list length: got %d want 1", len(rejectedList))
	}
	if rejectedList[0].ID != ids[1] {
		t.Errorf("rejected finding ID: got %q want %q", rejectedList[0].ID, ids[1])
	}

	// Filter: applied — should return exactly ids[2].
	appliedList, appliedTotal, err := db.ListCodeFindings(CodeFindingFilters{Status: "applied", Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings (applied): %v", err)
	}
	if appliedTotal != 1 {
		t.Errorf("applied total: got %d want 1", appliedTotal)
	}
	if len(appliedList) != 1 {
		t.Fatalf("applied list length: got %d want 1", len(appliedList))
	}
	if appliedList[0].ID != ids[2] {
		t.Errorf("applied finding ID: got %q want %q", appliedList[0].ID, ids[2])
	}
}

// TestUpdateCodeFindingStatus_Valid verifies that a pending finding can be transitioned
// first to "rejected" and then to "applied", with each change persisted correctly.
func TestUpdateCodeFindingStatus_Valid(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("running")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	f := makeCodeFinding(run.ID, "style", "info")
	if err := db.SaveCodeFinding(f); err != nil {
		t.Fatalf("SaveCodeFinding: %v", err)
	}

	ctx := context.Background()

	// Transition to rejected.
	if err := db.UpdateCodeFindingStatus(ctx, f.ID, "rejected"); err != nil {
		t.Fatalf("UpdateCodeFindingStatus (rejected): %v", err)
	}
	findings, _, err := db.ListCodeFindings(CodeFindingFilters{RunID: run.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings after rejected: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected finding after rejected transition")
	}
	if findings[0].Status != "rejected" {
		t.Errorf("Status after rejected transition: got %q want rejected", findings[0].Status)
	}

	// Transition to applied.
	if err := db.UpdateCodeFindingStatus(ctx, f.ID, "applied"); err != nil {
		t.Fatalf("UpdateCodeFindingStatus (applied): %v", err)
	}
	findings2, _, err := db.ListCodeFindings(CodeFindingFilters{RunID: run.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings after applied: %v", err)
	}
	if len(findings2) == 0 {
		t.Fatal("expected finding after applied transition")
	}
	if findings2[0].Status != "applied" {
		t.Errorf("Status after applied transition: got %q want applied", findings2[0].Status)
	}
}

// TestUpdateCodeFindingStatus_InvalidStatus verifies that an invalid status value is
// rejected with an error containing "invalid status" and that the DB row is unchanged.
func TestUpdateCodeFindingStatus_InvalidStatus(t *testing.T) {
	db := openTestDB(t)

	run := makeCodeAnalysisRun("running")
	if err := db.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}

	f := makeCodeFinding(run.ID, "security", "medium")
	if err := db.SaveCodeFinding(f); err != nil {
		t.Fatalf("SaveCodeFinding: %v", err)
	}

	ctx := context.Background()
	err := db.UpdateCodeFindingStatus(ctx, f.ID, "foo")
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Errorf("error message should contain 'invalid status': got %q", err.Error())
	}

	// Verify the row was not modified.
	findings, _, listErr := db.ListCodeFindings(CodeFindingFilters{RunID: run.ID, Limit: 10})
	if listErr != nil {
		t.Fatalf("ListCodeFindings: %v", listErr)
	}
	if len(findings) == 0 {
		t.Fatal("expected finding to still exist")
	}
	if findings[0].Status != "pending" {
		t.Errorf("Status should remain pending after invalid update: got %q", findings[0].Status)
	}
}

// TestUpdateCodeFindingStatus_NotFound verifies that updating a non-existent finding ID
// returns an error wrapping sql.ErrNoRows.
func TestUpdateCodeFindingStatus_NotFound(t *testing.T) {
	db := openTestDB(t)

	ctx := context.Background()
	err := db.UpdateCodeFindingStatus(ctx, "does-not-exist", "rejected")
	if err == nil {
		t.Fatal("expected error for non-existent ID, got nil")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected error to wrap sql.ErrNoRows: got %v", err)
	}
}

// Ensure the time import is used to avoid an "imported and not used" error.
var _ = time.RFC3339Nano
