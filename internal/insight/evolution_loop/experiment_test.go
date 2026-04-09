package evolution_loop_test

import (
	"context"
	"errors"
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
		Category:       "workflow_routing",
		BaselineMetric: 0.80,
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
		Category:       "threshold_adjustment",
		BaselineMetric: 0.85,
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

func TestExperimentRunner_NonPromptTuning_IgnoresLLM(t *testing.T) {
	mock := &mockLLMClient{err: errors.New("should not be called")}
	runner := evolution_loop.NewExperimentRunner(mock)
	h := &db.EvolutionHypothesis{
		Category:       "workflow_routing",
		BaselineMetric: 0.80,
	}
	result := runner.Execute(context.Background(), h)
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if result.Metric != 0.92 {
		t.Errorf("metric = %f, want 0.92", result.Metric)
	}
}
