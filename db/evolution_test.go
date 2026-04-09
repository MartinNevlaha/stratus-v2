package db

import (
	"testing"
	"time"
)

// helpers

func makeRun(triggerType, status string) *EvolutionRun {
	return &EvolutionRun{
		TriggerType: triggerType,
		Status:      status,
		TimeoutMs:   30000,
		Metadata:    map[string]any{"source": "test"},
	}
}

func makeHypothesis(runID, category, decision string) *EvolutionHypothesis {
	return &EvolutionHypothesis{
		RunID:          runID,
		Category:       category,
		Description:    "hypothesis description",
		BaselineValue:  "baseline",
		ProposedValue:  "proposed",
		Metric:         "success_rate",
		BaselineMetric: 0.70,
		Decision:       decision,
		Evidence:       map[string]any{"samples": 100},
	}
}

// TestSaveEvolutionRun_GeneratesIDAndRoundTrips verifies that SaveEvolutionRun
// assigns a UUID when ID is empty and that GetEvolutionRun returns identical data.
func TestSaveEvolutionRun_GeneratesIDAndRoundTrips(t *testing.T) {
	db := openTestDB(t)

	run := makeRun("scheduled", "running")
	if err := db.SaveEvolutionRun(run); err != nil {
		t.Fatalf("SaveEvolutionRun: %v", err)
	}
	if run.ID == "" {
		t.Fatal("expected ID to be generated, got empty string")
	}

	got, err := db.GetEvolutionRun(run.ID)
	if err != nil {
		t.Fatalf("GetEvolutionRun: %v", err)
	}
	if got == nil {
		t.Fatal("expected evolution run, got nil")
	}

	if got.ID != run.ID {
		t.Errorf("ID mismatch: got %q want %q", got.ID, run.ID)
	}
	if got.TriggerType != run.TriggerType {
		t.Errorf("TriggerType mismatch: got %q want %q", got.TriggerType, run.TriggerType)
	}
	if got.Status != run.Status {
		t.Errorf("Status mismatch: got %q want %q", got.Status, run.Status)
	}
	if got.TimeoutMs != run.TimeoutMs {
		t.Errorf("TimeoutMs mismatch: got %d want %d", got.TimeoutMs, run.TimeoutMs)
	}
	if got.Metadata["source"] != "test" {
		t.Errorf("Metadata not round-tripped: got %v", got.Metadata)
	}
	if got.CompletedAt != nil {
		t.Errorf("CompletedAt expected nil, got %v", got.CompletedAt)
	}
}

// TestGetEvolutionRun_NotFound verifies that a missing ID returns (nil, nil).
func TestGetEvolutionRun_NotFound(t *testing.T) {
	db := openTestDB(t)

	got, err := db.GetEvolutionRun("nonexistent-id")
	if err != nil {
		t.Fatalf("GetEvolutionRun: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// TestListEvolutionRuns_StatusFilter verifies that status filtering and total count work correctly.
func TestListEvolutionRuns_StatusFilter(t *testing.T) {
	db := openTestDB(t)

	for _, status := range []string{"running", "completed", "completed", "failed"} {
		r := makeRun("scheduled", status)
		if err := db.SaveEvolutionRun(r); err != nil {
			t.Fatalf("SaveEvolutionRun(%s): %v", status, err)
		}
	}

	// All runs
	all, total, err := db.ListEvolutionRuns(EvolutionRunFilters{Limit: 10})
	if err != nil {
		t.Fatalf("ListEvolutionRuns (all): %v", err)
	}
	if total != 4 {
		t.Errorf("total count: got %d want 4", total)
	}
	if len(all) != 4 {
		t.Errorf("list length: got %d want 4", len(all))
	}

	// Filter by completed
	completed, totalCompleted, err := db.ListEvolutionRuns(EvolutionRunFilters{Status: "completed", Limit: 10})
	if err != nil {
		t.Fatalf("ListEvolutionRuns (completed): %v", err)
	}
	if totalCompleted != 2 {
		t.Errorf("completed total: got %d want 2", totalCompleted)
	}
	if len(completed) != 2 {
		t.Errorf("completed list length: got %d want 2", len(completed))
	}
	for _, r := range completed {
		if r.Status != "completed" {
			t.Errorf("unexpected status in filtered list: %q", r.Status)
		}
	}
}

// TestListEvolutionRuns_PaginationOffset verifies that Offset skips rows correctly.
func TestListEvolutionRuns_PaginationOffset(t *testing.T) {
	db := openTestDB(t)

	for i := 0; i < 5; i++ {
		if err := db.SaveEvolutionRun(makeRun("manual", "completed")); err != nil {
			t.Fatalf("SaveEvolutionRun: %v", err)
		}
	}

	page, total, err := db.ListEvolutionRuns(EvolutionRunFilters{Limit: 3, Offset: 3})
	if err != nil {
		t.Fatalf("ListEvolutionRuns (page 2): %v", err)
	}
	if total != 5 {
		t.Errorf("total mismatch: got %d want 5", total)
	}
	if len(page) != 2 {
		t.Errorf("page length: got %d want 2", len(page))
	}
}

// TestUpdateEvolutionRun_StatusAndCounters verifies that mutable fields are persisted.
func TestUpdateEvolutionRun_StatusAndCounters(t *testing.T) {
	db := openTestDB(t)

	run := makeRun("manual", "running")
	if err := db.SaveEvolutionRun(run); err != nil {
		t.Fatalf("SaveEvolutionRun: %v", err)
	}

	completedAt := time.Now().UTC().Format(time.RFC3339Nano)
	run.Status = "completed"
	run.HypothesesCount = 5
	run.ExperimentsRun = 4
	run.AutoApplied = 2
	run.ProposalsCreated = 1
	run.WikiPagesUpdated = 3
	run.DurationMs = 12345
	run.ErrorMessage = ""
	run.CompletedAt = &completedAt
	run.Metadata = map[string]any{"updated": true}

	if err := db.UpdateEvolutionRun(run); err != nil {
		t.Fatalf("UpdateEvolutionRun: %v", err)
	}

	got, err := db.GetEvolutionRun(run.ID)
	if err != nil {
		t.Fatalf("GetEvolutionRun after update: %v", err)
	}
	if got == nil {
		t.Fatal("expected evolution run after update, got nil")
	}
	if got.Status != "completed" {
		t.Errorf("Status: got %q want completed", got.Status)
	}
	if got.HypothesesCount != 5 {
		t.Errorf("HypothesesCount: got %d want 5", got.HypothesesCount)
	}
	if got.ExperimentsRun != 4 {
		t.Errorf("ExperimentsRun: got %d want 4", got.ExperimentsRun)
	}
	if got.AutoApplied != 2 {
		t.Errorf("AutoApplied: got %d want 2", got.AutoApplied)
	}
	if got.ProposalsCreated != 1 {
		t.Errorf("ProposalsCreated: got %d want 1", got.ProposalsCreated)
	}
	if got.WikiPagesUpdated != 3 {
		t.Errorf("WikiPagesUpdated: got %d want 3", got.WikiPagesUpdated)
	}
	if got.DurationMs != 12345 {
		t.Errorf("DurationMs: got %d want 12345", got.DurationMs)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt: expected non-nil")
	}
	if got.Metadata["updated"] != true {
		t.Errorf("Metadata not updated: got %v", got.Metadata)
	}
}

// TestSaveEvolutionHypothesis_RoundTrip verifies INSERT and SELECT by ID.
func TestSaveEvolutionHypothesis_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	run := makeRun("scheduled", "running")
	if err := db.SaveEvolutionRun(run); err != nil {
		t.Fatalf("SaveEvolutionRun: %v", err)
	}

	h := makeHypothesis(run.ID, "prompt_tuning", "inconclusive")
	h.ExperimentMetric = 0.75
	h.Confidence = 0.60

	if err := db.SaveEvolutionHypothesis(h); err != nil {
		t.Fatalf("SaveEvolutionHypothesis: %v", err)
	}
	if h.ID == "" {
		t.Fatal("expected ID to be generated, got empty string")
	}

	got, err := db.GetEvolutionHypothesis(h.ID)
	if err != nil {
		t.Fatalf("GetEvolutionHypothesis: %v", err)
	}
	if got == nil {
		t.Fatal("expected hypothesis, got nil")
	}
	if got.RunID != run.ID {
		t.Errorf("RunID mismatch: got %q want %q", got.RunID, run.ID)
	}
	if got.Category != "prompt_tuning" {
		t.Errorf("Category: got %q want prompt_tuning", got.Category)
	}
	if got.BaselineMetric != 0.70 {
		t.Errorf("BaselineMetric: got %f want 0.70", got.BaselineMetric)
	}
	if got.ExperimentMetric != 0.75 {
		t.Errorf("ExperimentMetric: got %f want 0.75", got.ExperimentMetric)
	}
	if got.Evidence["samples"] == nil {
		t.Errorf("Evidence not round-tripped: got %v", got.Evidence)
	}
	if got.WikiPageID != nil {
		t.Errorf("WikiPageID expected nil, got %v", got.WikiPageID)
	}
}

// TestGetEvolutionHypothesis_NotFound verifies that a missing ID returns (nil, nil).
func TestGetEvolutionHypothesis_NotFound(t *testing.T) {
	db := openTestDB(t)

	got, err := db.GetEvolutionHypothesis("nonexistent-id")
	if err != nil {
		t.Fatalf("GetEvolutionHypothesis: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// TestListEvolutionHypotheses_ByRunID verifies ordering and run scoping.
func TestListEvolutionHypotheses_ByRunID(t *testing.T) {
	db := openTestDB(t)

	run1 := makeRun("scheduled", "running")
	run2 := makeRun("manual", "running")
	if err := db.SaveEvolutionRun(run1); err != nil {
		t.Fatalf("SaveEvolutionRun run1: %v", err)
	}
	if err := db.SaveEvolutionRun(run2); err != nil {
		t.Fatalf("SaveEvolutionRun run2: %v", err)
	}

	categories := []string{"prompt_tuning", "workflow_routing", "agent_selection"}
	for _, cat := range categories {
		h := makeHypothesis(run1.ID, cat, "inconclusive")
		if err := db.SaveEvolutionHypothesis(h); err != nil {
			t.Fatalf("SaveEvolutionHypothesis (%s): %v", cat, err)
		}
	}
	// Hypothesis for a different run — should not appear in run1's list
	other := makeHypothesis(run2.ID, "threshold_adjustment", "rejected")
	if err := db.SaveEvolutionHypothesis(other); err != nil {
		t.Fatalf("SaveEvolutionHypothesis (other run): %v", err)
	}

	list, err := db.ListEvolutionHypotheses(run1.ID)
	if err != nil {
		t.Fatalf("ListEvolutionHypotheses: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 hypotheses for run1, got %d", len(list))
	}
	for _, h := range list {
		if h.RunID != run1.ID {
			t.Errorf("unexpected RunID %q in list for run1", h.RunID)
		}
	}
}

// TestListEvolutionHypotheses_Empty verifies that an empty result returns a nil/empty slice without error.
func TestListEvolutionHypotheses_Empty(t *testing.T) {
	db := openTestDB(t)

	list, err := db.ListEvolutionHypotheses("nonexistent-run-id")
	if err != nil {
		t.Fatalf("ListEvolutionHypotheses (empty): %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 hypotheses, got %d", len(list))
	}
}

// TestUpdateEvolutionHypothesis_DecisionAndConfidence verifies that experiment
// result fields, wiki_page_id, and evidence are persisted correctly.
func TestUpdateEvolutionHypothesis_DecisionAndConfidence(t *testing.T) {
	db := openTestDB(t)

	run := makeRun("event_driven", "running")
	if err := db.SaveEvolutionRun(run); err != nil {
		t.Fatalf("SaveEvolutionRun: %v", err)
	}

	h := makeHypothesis(run.ID, "agent_selection", "inconclusive")
	if err := db.SaveEvolutionHypothesis(h); err != nil {
		t.Fatalf("SaveEvolutionHypothesis: %v", err)
	}

	wikiPage := "wiki-page-42"
	h.ExperimentMetric = 0.88
	h.Confidence = 0.91
	h.Decision = "auto_applied"
	h.DecisionReason = "significant improvement"
	h.WikiPageID = &wikiPage
	h.Evidence = map[string]any{"samples": 200, "p_value": 0.03}

	if err := db.UpdateEvolutionHypothesis(h); err != nil {
		t.Fatalf("UpdateEvolutionHypothesis: %v", err)
	}

	got, err := db.GetEvolutionHypothesis(h.ID)
	if err != nil {
		t.Fatalf("GetEvolutionHypothesis after update: %v", err)
	}
	if got == nil {
		t.Fatal("expected hypothesis after update, got nil")
	}
	if got.ExperimentMetric != 0.88 {
		t.Errorf("ExperimentMetric: got %f want 0.88", got.ExperimentMetric)
	}
	if got.Confidence != 0.91 {
		t.Errorf("Confidence: got %f want 0.91", got.Confidence)
	}
	if got.Decision != "auto_applied" {
		t.Errorf("Decision: got %q want auto_applied", got.Decision)
	}
	if got.DecisionReason != "significant improvement" {
		t.Errorf("DecisionReason: got %q want 'significant improvement'", got.DecisionReason)
	}
	if got.WikiPageID == nil || *got.WikiPageID != wikiPage {
		t.Errorf("WikiPageID: got %v want %q", got.WikiPageID, wikiPage)
	}
	if got.Evidence["samples"] == nil {
		t.Errorf("Evidence not updated: got %v", got.Evidence)
	}
}

// TestGetActiveEvolutionRun_ReturnsRunningRun verifies that the active run query
// returns the currently running evolution run.
func TestGetActiveEvolutionRun_ReturnsRunningRun(t *testing.T) {
	db := openTestDB(t)

	completed := makeRun("scheduled", "completed")
	if err := db.SaveEvolutionRun(completed); err != nil {
		t.Fatalf("SaveEvolutionRun (completed): %v", err)
	}

	active := makeRun("manual", "running")
	if err := db.SaveEvolutionRun(active); err != nil {
		t.Fatalf("SaveEvolutionRun (running): %v", err)
	}

	got, err := db.GetActiveEvolutionRun()
	if err != nil {
		t.Fatalf("GetActiveEvolutionRun: %v", err)
	}
	if got == nil {
		t.Fatal("expected active run, got nil")
	}
	if got.ID != active.ID {
		t.Errorf("ID mismatch: got %q want %q", got.ID, active.ID)
	}
	if got.Status != "running" {
		t.Errorf("Status: got %q want running", got.Status)
	}
}

// TestGetActiveEvolutionRun_NilWhenNoneRunning verifies that (nil, nil) is returned
// when there is no active evolution run.
func TestGetActiveEvolutionRun_NilWhenNoneRunning(t *testing.T) {
	db := openTestDB(t)

	// Insert only a completed run
	r := makeRun("scheduled", "completed")
	if err := db.SaveEvolutionRun(r); err != nil {
		t.Fatalf("SaveEvolutionRun: %v", err)
	}

	got, err := db.GetActiveEvolutionRun()
	if err != nil {
		t.Fatalf("GetActiveEvolutionRun: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil when no active run, got %+v", got)
	}
}

// TestSaveEvolutionRun_NilMetadataDefaultsToEmptyMap verifies that nil Metadata
// is stored as an empty JSON object and unmarshalled back to an initialised map.
func TestSaveEvolutionRun_NilMetadataDefaultsToEmptyMap(t *testing.T) {
	db := openTestDB(t)

	run := &EvolutionRun{
		TriggerType: "scheduled",
		Status:      "running",
		// Metadata deliberately left nil
	}
	if err := db.SaveEvolutionRun(run); err != nil {
		t.Fatalf("SaveEvolutionRun: %v", err)
	}

	got, err := db.GetEvolutionRun(run.ID)
	if err != nil {
		t.Fatalf("GetEvolutionRun: %v", err)
	}
	if got.Metadata == nil {
		t.Error("expected Metadata to be initialised to empty map, got nil")
	}
}

// TestSaveEvolutionHypothesis_NilEvidenceDefaultsToEmptyMap mirrors the Metadata
// nil-safety check for hypothesis Evidence.
func TestSaveEvolutionHypothesis_NilEvidenceDefaultsToEmptyMap(t *testing.T) {
	db := openTestDB(t)

	run := makeRun("scheduled", "running")
	if err := db.SaveEvolutionRun(run); err != nil {
		t.Fatalf("SaveEvolutionRun: %v", err)
	}

	h := &EvolutionHypothesis{
		RunID:       run.ID,
		Category:    "threshold_adjustment",
		Description: "no evidence provided",
		Decision:    "inconclusive",
		// Evidence deliberately left nil
	}
	if err := db.SaveEvolutionHypothesis(h); err != nil {
		t.Fatalf("SaveEvolutionHypothesis: %v", err)
	}

	got, err := db.GetEvolutionHypothesis(h.ID)
	if err != nil {
		t.Fatalf("GetEvolutionHypothesis: %v", err)
	}
	if got.Evidence == nil {
		t.Error("expected Evidence to be initialised to empty map, got nil")
	}
}
