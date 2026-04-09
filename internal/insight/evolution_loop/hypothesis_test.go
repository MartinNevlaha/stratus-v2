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

	hypotheses, err := gen.Generate(context.Background(), "run-2", []string{"agent_selection"}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hypotheses) == 0 {
		t.Fatal("expected hypotheses for agent_selection category")
	}
	for _, h := range hypotheses {
		if h.Category != "agent_selection" {
			t.Errorf("expected category agent_selection, got %q", h.Category)
		}
	}
}

func TestGenerate_EmptyCategories_ReturnsAll(t *testing.T) {
	store := newMockStore()
	gen := evolution_loop.NewHypothesisGenerator(store, nil)

	// Empty categories → all known categories used.
	hypotheses, err := gen.Generate(context.Background(), "run-3", []string{}, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	categories := map[string]bool{}
	for _, h := range hypotheses {
		categories[h.Category] = true
	}

	// At minimum we expect more than one category.
	if len(categories) < 2 {
		t.Errorf("expected hypotheses from multiple categories, got: %v", categories)
	}
}

func TestGenerate_MaxCount_Respected(t *testing.T) {
	store := newMockStore()
	gen := evolution_loop.NewHypothesisGenerator(store, nil)

	max := 2
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
	const jsonResp = `[{"category":"workflow_routing","description":"test hyp","baseline_value":"0.8","proposed_value":"0.75","metric":"accuracy","baseline_metric":0.8,"rationale":"testing"}]`
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
	if hypotheses[0].Category != "workflow_routing" {
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

	hypotheses, err := gen.Generate(context.Background(), "run-1", []string{"workflow_routing"}, 10)
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

	hypotheses, err := gen.Generate(context.Background(), "run-1", []string{"threshold_adjustment"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hypotheses) == 0 {
		t.Fatal("should fall back to seed hypotheses")
	}
}
