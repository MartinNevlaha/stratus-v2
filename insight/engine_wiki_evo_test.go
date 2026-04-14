package insight

import (
	"context"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/insight/events"
)

func makeEngineWithWikiEvo(t *testing.T) *Engine {
	t.Helper()
	database := setupTestDB(t)
	insightCfg := config.InsightConfig{Enabled: true, Interval: 1}
	wikiCfg := config.WikiConfig{
		Enabled:            true,
		MaxPagesPerIngest:  10,
		StalenessThreshold: 0.5,
	}
	evoCfg := config.EvolutionConfig{
		Enabled:             true,
		TimeoutMs:           5000,
		MaxHypothesesPerRun: 3,
	}
	return NewEngineWithConfig(database, insightCfg, wikiCfg, evoCfg)
}

func TestNewEngineWithConfig_WikiEngineInitialized(t *testing.T) {
	e := makeEngineWithWikiEvo(t)
	if e.WikiEngine() == nil {
		t.Fatal("expected WikiEngine to be non-nil after NewEngineWithConfig")
	}
}

func TestNewEngineWithConfig_EvolutionLoopInitialized(t *testing.T) {
	e := makeEngineWithWikiEvo(t)
	if e.EvolutionLoop() == nil {
		t.Fatal("expected EvolutionLoop to be non-nil after NewEngineWithConfig")
	}
}

func TestNewEngineWithConfig_WikiSynthesizerInitialized(t *testing.T) {
	e := makeEngineWithWikiEvo(t)
	if e.WikiSynthesizer() == nil {
		t.Fatal("expected WikiSynthesizer to be non-nil after NewEngineWithConfig")
	}
}

func TestEngine_RunWikiIngest_NilEngineReturnsNil(t *testing.T) {
	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	// NewEngine without wiki config — wikiEngine stays nil
	e := NewEngine(database, cfg)

	result, err := e.RunWikiIngest(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when wikiEngine is nil, got: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result when wikiEngine is nil, got: %+v", result)
	}
}

func TestEngine_RunWikiMaintenance_NilEngineReturnsNil(t *testing.T) {
	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	e := NewEngine(database, cfg)

	result, err := e.RunWikiMaintenance(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when wikiEngine is nil, got: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result when wikiEngine is nil, got: %+v", result)
	}
}

func TestEngine_RunEvolutionCycle_NilEngineReturnsNil(t *testing.T) {
	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	e := NewEngine(database, cfg)

	result, err := e.RunEvolutionCycle(context.Background(), "scheduled", 0, nil)
	if err != nil {
		t.Fatalf("expected nil error when evolutionLoop is nil, got: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result when evolutionLoop is nil, got: %+v", result)
	}
}

func TestEngine_SynthesizeWikiAnswer_NilSynthReturnsError(t *testing.T) {
	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	e := NewEngine(database, cfg)

	_, err := e.SynthesizeWikiAnswer(context.Background(), "test query", 5, false)
	if err == nil {
		t.Fatal("expected error when wikiSynth is nil, got nil")
	}
}

// TestNewEngineWithConfig_WikiDisabled_SynthesizerStillInitialized verifies that
// passing wiki.enabled=false to NewEngineWithConfig still constructs wikiSynth so
// that SynthesizeWikiAnswer would not return "wiki synthesizer not initialized".
// This is the regression guard for the bug where NewEngine (no wiki config) was
// used unconditionally when wiki.enabled=false, leaving wikiSynth nil.
func TestNewEngineWithConfig_WikiDisabled_SynthesizerStillInitialized(t *testing.T) {
	database := setupTestDB(t)
	insightCfg := config.InsightConfig{Enabled: true, Interval: 1}
	wikiCfg := config.WikiConfig{Enabled: false}
	evoCfg := config.EvolutionConfig{Enabled: false}

	e := NewEngineWithConfig(database, insightCfg, wikiCfg, evoCfg)

	if e.WikiSynthesizer() == nil {
		t.Fatal("wikiSynth must not be nil even when wiki.enabled=false; SynthesizeWikiAnswer would return 'wiki synthesizer not initialized'")
	}
}

func TestEngine_RunWikiIngest_WithWikiEngine(t *testing.T) {
	e := makeEngineWithWikiEvo(t)
	ctx := context.Background()

	// Wiki engine has no LLM client — should return empty result (fail-open)
	result, err := e.RunWikiIngest(ctx)
	if err != nil {
		t.Fatalf("RunWikiIngest returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil IngestResult")
	}
}

func TestEngine_RunWikiMaintenance_WithWikiEngine(t *testing.T) {
	e := makeEngineWithWikiEvo(t)
	ctx := context.Background()

	// No pages in DB — maintenance should succeed and score 0 pages
	result, err := e.RunWikiMaintenance(ctx)
	if err != nil {
		t.Fatalf("RunWikiMaintenance returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil MaintenanceResult")
	}
	if result.PagesScored != 0 {
		t.Errorf("expected 0 pages scored on empty DB, got %d", result.PagesScored)
	}
}

func TestEngine_RunEvolutionCycle_WithEvolutionLoop(t *testing.T) {
	e := makeEngineWithWikiEvo(t)
	ctx := context.Background()

	result, err := e.RunEvolutionCycle(ctx, "scheduled", 0, nil)
	if err != nil {
		t.Fatalf("RunEvolutionCycle returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil RunResult")
	}
}

// TestHandleWorkflowWikiStaleness_NoFiles verifies that events without a
// "files" payload key are handled gracefully without touching the DB.
func TestHandleWorkflowWikiStaleness_NoFiles(t *testing.T) {
	e := makeEngineWithWikiEvo(t)

	// Should not panic or error — no files means early return.
	e.handleWorkflowWikiStaleness(map[string]any{
		"workflow_id": "wf-1",
	})
}

// TestHandleWorkflowWikiStaleness_WithFiles verifies that when the event
// payload contains a "files" key, wiki pages referencing those files have
// their staleness score boosted by 0.2 (clamped to 1.0).
func TestHandleWorkflowWikiStaleness_WithFiles(t *testing.T) {
	e := makeEngineWithWikiEvo(t)

	// Seed a wiki page that references "api/handler.go".
	page := &db.WikiPage{
		PageType:       "summary",
		Title:          "API Handler",
		Status:         "published",
		GeneratedBy:    "ingest",
		StalenessScore: 0.4,
	}
	if err := e.database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}
	ref := &db.WikiPageRef{PageID: page.ID, SourceType: "artifact", SourceID: "api/handler.go"}
	if err := e.database.SaveWikiPageRef(ref); err != nil {
		t.Fatalf("SaveWikiPageRef: %v", err)
	}

	e.handleWorkflowWikiStaleness(map[string]any{
		"workflow_id": "wf-2",
		"files":       []any{"api/handler.go", "api/server.go"},
	})

	updated, err := e.database.GetWikiPage(page.ID)
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	want := 0.6 // 0.4 + 0.2
	if updated.StalenessScore < want-0.001 || updated.StalenessScore > want+0.001 {
		t.Errorf("StalenessScore = %.3f, want %.3f", updated.StalenessScore, want)
	}
}

// TestHandleWorkflowWikiStaleness_Clamped verifies that the staleness score
// is clamped to 1.0 and does not exceed it.
func TestHandleWorkflowWikiStaleness_Clamped(t *testing.T) {
	e := makeEngineWithWikiEvo(t)

	page := &db.WikiPage{
		PageType:       "summary",
		Title:          "Near Full Stale",
		Status:         "published",
		GeneratedBy:    "ingest",
		StalenessScore: 0.95,
	}
	if err := e.database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}
	ref := &db.WikiPageRef{PageID: page.ID, SourceType: "artifact", SourceID: "cmd/main.go"}
	if err := e.database.SaveWikiPageRef(ref); err != nil {
		t.Fatalf("SaveWikiPageRef: %v", err)
	}

	e.handleWorkflowWikiStaleness(map[string]any{
		"files": []any{"cmd/main.go"},
	})

	updated, err := e.database.GetWikiPage(page.ID)
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if updated.StalenessScore > 1.0 {
		t.Errorf("StalenessScore %.3f exceeds max 1.0", updated.StalenessScore)
	}
	if updated.StalenessScore < 0.999 {
		t.Errorf("StalenessScore %.3f should be 1.0", updated.StalenessScore)
	}
}

// TestHandleWorkflowWikiStaleness_NilDB verifies that the method returns
// immediately when the engine has no database (nil guard).
func TestHandleWorkflowWikiStaleness_NilDB(t *testing.T) {
	e := &Engine{} // no database
	// Must not panic.
	e.handleWorkflowWikiStaleness(map[string]any{
		"files": []any{"some/file.go"},
	})
}

// TestHandleEvent_WorkflowCompleted_TriggersWikiStaleness verifies that
// HandleEvent dispatches handleWorkflowWikiStaleness for EventWorkflowCompleted.
func TestHandleEvent_WorkflowCompleted_TriggersWikiStaleness(t *testing.T) {
	e := makeEngineWithWikiEvo(t)

	// Seed a wiki page with a matching file reference.
	page := &db.WikiPage{
		PageType:       "summary",
		Title:          "Event-Triggered",
		Status:         "published",
		GeneratedBy:    "ingest",
		StalenessScore: 0.0,
	}
	if err := e.database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}
	ref := &db.WikiPageRef{PageID: page.ID, SourceType: "artifact", SourceID: "insight/engine.go"}
	if err := e.database.SaveWikiPageRef(ref); err != nil {
		t.Fatalf("SaveWikiPageRef: %v", err)
	}

	// Start the engine so HandleEvent does not early-return.
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop()

	evt := events.Event{
		ID:     "evt-1",
		Type:   events.EventWorkflowCompleted,
		Source: "test",
		Payload: map[string]any{
			"workflow_id": "wf-ev-1",
			"files":       []any{"insight/engine.go"},
		},
	}
	e.HandleEvent(ctx, evt)

	// HandleEvent spawns a goroutine; give it time to finish.
	deadline := context.Background()
	_ = deadline

	// Poll briefly.
	var updated *db.WikiPage
	for i := 0; i < 50; i++ {
		var err error
		updated, err = e.database.GetWikiPage(page.ID)
		if err != nil {
			t.Fatalf("GetWikiPage: %v", err)
		}
		if updated.StalenessScore > 0.0 {
			break
		}
		// sleep via a channel wait equivalent — use a small busy wait
		for j := 0; j < 1000000; j++ {
		}
	}

	if updated.StalenessScore < 0.19 {
		t.Errorf("expected staleness boost after EventWorkflowCompleted, got %.3f", updated.StalenessScore)
	}
}
