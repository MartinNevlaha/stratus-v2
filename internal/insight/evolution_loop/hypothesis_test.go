package evolution_loop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

func TestGenerate_ReturnsHypotheses(t *testing.T) {
	store := newMockStore()
	gen := evolution_loop.NewHypothesisGenerator(store, nil)

	hypotheses, err := gen.Generate(context.Background(), "run-1", nil, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hypotheses) == 0 {
		t.Fatal("expected at least one hypothesis, got 0")
	}
	if len(hypotheses) > 5 {
		t.Errorf("expected at most 5 hypotheses, got %d", len(hypotheses))
	}
}

func TestGenerate_FiltersCategories(t *testing.T) {
	store := newMockStore()
	gen := evolution_loop.NewHypothesisGenerator(store, nil)

	hypotheses, err := gen.Generate(context.Background(), "run-2", []string{"prompt_tuning"}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hypotheses) == 0 {
		t.Fatal("expected hypotheses for prompt_tuning category")
	}
	for _, h := range hypotheses {
		if h.Category != "prompt_tuning" {
			t.Errorf("expected category prompt_tuning, got %q", h.Category)
		}
	}
}

func TestGenerate_EmptyCategories_ReturnsAll(t *testing.T) {
	store := newMockStore()
	gen := evolution_loop.NewHypothesisGenerator(store, nil)

	// Empty categories → all known categories used (only prompt_tuning post-T9).
	hypotheses, err := gen.Generate(context.Background(), "run-3", []string{}, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// At minimum we expect at least one hypothesis.
	if len(hypotheses) == 0 {
		t.Errorf("expected at least one hypothesis, got 0")
	}
	for _, h := range hypotheses {
		if h.Category != "prompt_tuning" {
			t.Errorf("unexpected category %q; only prompt_tuning is a known seed category post-T9", h.Category)
		}
	}
}

func TestGenerate_MaxCount_Respected(t *testing.T) {
	store := newMockStore()
	gen := evolution_loop.NewHypothesisGenerator(store, nil)

	max := 1
	hypotheses, err := gen.Generate(context.Background(), "run-4", nil, max)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hypotheses) > max {
		t.Errorf("expected at most %d hypotheses, got %d", max, len(hypotheses))
	}
}

func TestGenerate_AssignsRunID(t *testing.T) {
	store := newMockStore()
	gen := evolution_loop.NewHypothesisGenerator(store, nil)

	const runID = "run-42"
	hypotheses, err := gen.Generate(context.Background(), runID, nil, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, h := range hypotheses {
		if h.RunID != runID {
			t.Errorf("expected RunID %q, got %q", runID, h.RunID)
		}
	}
}

func TestHypothesisGenerator_WithLLM(t *testing.T) {
	const jsonResp = `[{"category":"prompt_tuning","description":"test hyp","baseline_value":"standard","proposed_value":"chain_of_thought","metric":"plan_quality_score","baseline_metric":0.68,"rationale":"testing"}]`
	mock := &mockLLMClient{
		completeFn: func(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{Content: jsonResp}, nil
		},
	}
	store := newMockStore()
	gen := evolution_loop.NewHypothesisGenerator(store, mock)

	hypotheses, err := gen.Generate(context.Background(), "run-1", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hypotheses) != 1 {
		t.Fatalf("got %d hypotheses, want 1", len(hypotheses))
	}
	if hypotheses[0].Category != "prompt_tuning" {
		t.Errorf("wrong category: got %q", hypotheses[0].Category)
	}
	if hypotheses[0].Evidence["llm_rationale"] != "testing" {
		t.Errorf("rationale not stored in evidence: got %v", hypotheses[0].Evidence["llm_rationale"])
	}
}

func TestHypothesisGenerator_LLMError_FallsBackToSeeds(t *testing.T) {
	mock := &mockLLMClient{err: errors.New("connection refused")}
	store := newMockStore()
	gen := evolution_loop.NewHypothesisGenerator(store, mock)

	hypotheses, err := gen.Generate(context.Background(), "run-1", []string{"prompt_tuning"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hypotheses) == 0 {
		t.Fatal("should fall back to seed hypotheses")
	}
}

func TestHypothesisGenerator_LLMInvalidJSON_FallsBackToSeeds(t *testing.T) {
	mock := &mockLLMClient{
		completeFn: func(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{Content: "not valid json at all"}, nil
		},
	}
	store := newMockStore()
	gen := evolution_loop.NewHypothesisGenerator(store, mock)

	hypotheses, err := gen.Generate(context.Background(), "run-1", []string{"prompt_tuning"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hypotheses) == 0 {
		t.Fatal("should fall back to seed hypotheses")
	}
}

// TestHypothesisGenerator_LegacyCategoryDropped verifies that the legacy
// categories removed in T9 are not produced by the seed fallback path even when
// explicitly requested.
func TestHypothesisGenerator_LegacyCategoryDropped(t *testing.T) {
	store := newMockStore()
	gen := evolution_loop.NewHypothesisGenerator(store, nil)

	legacyCategories := []string{"workflow_routing", "agent_selection", "threshold_adjustment"}
	hypotheses, err := gen.Generate(context.Background(), "run-legacy", legacyCategories, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Seeds no longer exist for these categories; the generator must return nothing.
	if len(hypotheses) != 0 {
		t.Errorf("expected 0 hypotheses for legacy categories, got %d", len(hypotheses))
	}
}

func TestSeedsFor_Slovak(t *testing.T) {
	seeds := evolution_loop.SeedsFor("sk")
	if len(seeds) == 0 {
		t.Fatal("expected non-empty seeds for 'sk'")
	}
	// Slovak seeds must have non-empty descriptions.
	for cat, entries := range seeds {
		for _, s := range entries {
			if s.Desc == "" {
				t.Errorf("category %q: empty Desc in Slovak seeds", cat)
			}
		}
	}
}

func TestSeedsFor_UnknownFallsBackToEnglish(t *testing.T) {
	skSeeds := evolution_loop.SeedsFor("sk")
	enSeeds := evolution_loop.SeedsFor("en")
	unknownSeeds := evolution_loop.SeedsFor("zz")

	// Unknown lang must return same number of categories as "en".
	if len(unknownSeeds) != len(enSeeds) {
		t.Errorf("unknown lang: got %d categories, want %d (same as 'en')", len(unknownSeeds), len(enSeeds))
	}

	// Slovak and English must have the same category keys.
	for cat := range enSeeds {
		if _, ok := skSeeds[cat]; !ok {
			t.Errorf("category %q missing from 'sk' seeds", cat)
		}
	}
}

func TestSeedsFor_SKAndENHaveSameIDs(t *testing.T) {
	// Not checking IDs directly (they are descriptions), but the same categories
	// and same number of entries per category ensures structural parity.
	skSeeds := evolution_loop.SeedsFor("sk")
	enSeeds := evolution_loop.SeedsFor("en")

	for cat, enEntries := range enSeeds {
		skEntries, ok := skSeeds[cat]
		if !ok {
			t.Errorf("category %q missing from sk seeds", cat)
			continue
		}
		if len(skEntries) != len(enEntries) {
			t.Errorf("category %q: sk has %d entries, en has %d", cat, len(skEntries), len(enEntries))
		}
	}
}
