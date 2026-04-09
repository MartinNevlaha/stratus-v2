package insight

import (
	"context"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
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
