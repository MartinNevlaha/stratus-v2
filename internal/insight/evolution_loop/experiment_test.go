package evolution_loop_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

// mockLLMClient is a test double for llm.Client.
// Priority: completeFn > err > response.
type mockLLMClient struct {
	response   string
	err        error
	completeFn func(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error)
}

func (m *mockLLMClient) Complete(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.completeFn != nil {
		return m.completeFn(ctx, req)
	}
	if m.err != nil {
		return nil, m.err
	}
	return &llm.CompletionResponse{Content: m.response}, nil
}

func (m *mockLLMClient) Provider() string { return "mock" }
func (m *mockLLMClient) Model() string    { return "mock" }

func TestExecute_ReturnsResult(t *testing.T) {
	runner := evolution_loop.NewExperimentRunner(nil)
	h := &db.EvolutionHypothesis{
		Category:       "prompt_tuning",
		BaselineMetric: 0.68,
	}

	result := runner.Execute(context.Background(), h)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.SampleSize <= 0 {
		t.Errorf("expected SampleSize > 0, got %d", result.SampleSize)
	}
	if result.Metric <= 0 {
		t.Errorf("expected Metric > 0, got %f", result.Metric)
	}
}

func TestExecute_RespectsContextCancellation(t *testing.T) {
	runner := evolution_loop.NewExperimentRunner(nil)
	h := &db.EvolutionHypothesis{
		Category:       "prompt_tuning",
		BaselineMetric: 0.68,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	result := runner.Execute(ctx, h)

	if result.Error == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestExecute_UnknownCategory_ReturnsResult(t *testing.T) {
	runner := evolution_loop.NewExperimentRunner(nil)
	h := &db.EvolutionHypothesis{
		Category:       "unknown_category",
		BaselineMetric: 0.60,
	}

	result := runner.Execute(context.Background(), h)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	// For unknown category the metric should be above baseline.
	if result.Metric <= h.BaselineMetric {
		t.Errorf("expected Metric > BaselineMetric (%.4f), got %.4f", h.BaselineMetric, result.Metric)
	}
}

func TestExperimentRunner_PromptTuning_WithLLM(t *testing.T) {
	callCount := 0
	mock := &mockLLMClient{
		completeFn: func(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
			callCount++
			switch callCount {
			case 1:
				return &llm.CompletionResponse{Content: "baseline response"}, nil
			case 2:
				return &llm.CompletionResponse{Content: "proposed response"}, nil
			case 3:
				return &llm.CompletionResponse{Content: `{"baseline_score": 0.6, "proposed_score": 0.8, "reasoning": "better"}`}, nil
			}
			return nil, errors.New("unexpected call")
		},
	}
	runner := evolution_loop.NewExperimentRunner(mock)
	h := &db.EvolutionHypothesis{
		Category:      "prompt_tuning",
		BaselineValue: "standard",
		ProposedValue: "chain_of_thought",
	}
	result := runner.Execute(context.Background(), h)
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if result.Metric != 0.8 {
		t.Errorf("metric = %f, want 0.8", result.Metric)
	}
	if result.SampleSize != 1 {
		t.Errorf("sample size = %d, want 1", result.SampleSize)
	}
	if callCount != 3 {
		t.Errorf("expected 3 LLM calls, got %d", callCount)
	}
}

func TestExperimentRunner_PromptTuning_LLMError_FallsBack(t *testing.T) {
	mock := &mockLLMClient{err: errors.New("timeout")}
	runner := evolution_loop.NewExperimentRunner(mock)
	h := &db.EvolutionHypothesis{
		Category:       "prompt_tuning",
		BaselineMetric: 0.68,
	}
	result := runner.Execute(context.Background(), h)
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	// Should fall back to categoryBaselines["prompt_tuning"] = 0.75
	if result.Metric != 0.75 {
		t.Errorf("metric = %f, want 0.75", result.Metric)
	}
}

// TestExecute_LegacyCategories_UsesFallbackMetric verifies that the three
// categories removed in T9 (workflow_routing, agent_selection,
// threshold_adjustment) are no longer in categoryBaselines and therefore hit
// the neutral-fallback path (BaselineMetric * 1.05 → above baseline, below 1.0).
func TestExecute_LegacyCategories_UsesFallbackMetric(t *testing.T) {
	runner := evolution_loop.NewExperimentRunner(nil)

	for _, cat := range []string{"workflow_routing", "agent_selection", "threshold_adjustment"} {
		h := &db.EvolutionHypothesis{
			Category:       cat,
			BaselineMetric: 0.80,
		}
		result := runner.Execute(context.Background(), h)
		if result.Error != nil {
			t.Fatalf("[%s] unexpected error: %v", cat, result.Error)
		}
		// Neutral fallback: metric > baseline and < 1.0 (not one of the removed hard-coded values).
		if result.Metric <= h.BaselineMetric {
			t.Errorf("[%s] metric %f should be above baseline %f (neutral fallback)", cat, result.Metric, h.BaselineMetric)
		}
		// The removed hard-coded values were 0.92, 0.88, 0.95 — none of them should appear.
		hardcoded := map[float64]bool{0.92: true, 0.88: true, 0.95: true}
		if hardcoded[result.Metric] {
			t.Errorf("[%s] metric %f matches a removed hard-coded baseline value — category was not cleaned up", cat, result.Metric)
		}
	}
}

// TestExperimentRunner_EvaluatorCallUsesJSONResponseFormatOnly verifies that the
// three LLM calls in executePromptTuningWithLang set ResponseFormat correctly:
// baseline (call 1) and proposed (call 2) must be plaintext (""), while the
// evaluator (call 3) must use ResponseFormat="json".
func TestExperimentRunner_EvaluatorCallUsesJSONResponseFormatOnly(t *testing.T) {
	var mu sync.Mutex
	var captured []llm.CompletionRequest

	mock := &mockLLMClient{
		completeFn: func(_ context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
			mu.Lock()
			captured = append(captured, req)
			n := len(captured)
			mu.Unlock()

			switch n {
			case 1:
				return &llm.CompletionResponse{Content: "baseline output"}, nil
			case 2:
				return &llm.CompletionResponse{Content: "proposed output"}, nil
			case 3:
				return &llm.CompletionResponse{Content: `{"baseline_score":0.4,"proposed_score":0.8,"reasoning":"x"}`}, nil
			}
			return nil, errors.New("unexpected call")
		},
	}

	runner := evolution_loop.NewExperimentRunner(mock)
	hypothesis := &db.EvolutionHypothesis{
		Category:      "prompt_tuning",
		BaselineValue: "standard",
		ProposedValue: "cot",
	}

	result := runner.ExecuteWithLang(context.Background(), hypothesis, "en")
	if result.Error != nil {
		t.Fatalf("ExecuteWithLang: %v", result.Error)
	}

	mu.Lock()
	reqs := make([]llm.CompletionRequest, len(captured))
	copy(reqs, captured)
	mu.Unlock()

	if len(reqs) != 3 {
		t.Fatalf("expected 3 LLM calls, got %d", len(reqs))
	}
	if reqs[0].ResponseFormat != "" {
		t.Errorf("call 1 (baseline): ResponseFormat = %q, want %q", reqs[0].ResponseFormat, "")
	}
	if reqs[1].ResponseFormat != "" {
		t.Errorf("call 2 (proposed): ResponseFormat = %q, want %q", reqs[1].ResponseFormat, "")
	}
	if reqs[2].ResponseFormat != "json" {
		t.Errorf("call 3 (evaluator): ResponseFormat = %q, want %q", reqs[2].ResponseFormat, "json")
	}
	if result.Metric != 0.8 {
		t.Errorf("metric = %f, want 0.8 (evaluator was parsed, not fallback)", result.Metric)
	}
}
