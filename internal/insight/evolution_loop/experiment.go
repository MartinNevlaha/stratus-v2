package evolution_loop

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/prompts"
)

// ExperimentResult holds the outcome of a single experiment execution.
type ExperimentResult struct {
	Metric     float64
	SampleSize int
	Error      error
}

// ExperimentRunner executes hypothesis experiments.
type ExperimentRunner struct {
	llmClient llm.Client
}

// NewExperimentRunner constructs an ExperimentRunner.
// llmClient may be nil; a nil value means "no LLM available, use static/simulated behavior".
func NewExperimentRunner(llmClient llm.Client) *ExperimentRunner {
	return &ExperimentRunner{llmClient: llmClient}
}

// categoryBaselines maps hypothesis categories to simulated baseline improvements.
// Values represent the simulated metric uplift factor used in MVP mode.
var categoryBaselines = map[string]float64{
	"workflow_routing":     0.92,
	"agent_selection":      0.88,
	"threshold_adjustment": 0.95,
	"prompt_tuning":        0.75,
}

// Execute runs an experiment for the given hypothesis.
// For prompt_tuning with an LLM client available it performs a real A/B comparison.
// All other categories (and fallback on LLM error) use simulated metrics.
func (r *ExperimentRunner) Execute(ctx context.Context, hypothesis *db.EvolutionHypothesis) *ExperimentResult {
	// Honour cancellation before doing any work.
	select {
	case <-ctx.Done():
		return &ExperimentResult{Error: ctx.Err()}
	default:
	}

	// For prompt_tuning with LLM available, run real A/B comparison.
	if hypothesis.Category == "prompt_tuning" && r.llmClient != nil {
		result := r.executePromptTuning(ctx, hypothesis)
		if result.Error == nil {
			return result
		}
		// Fall back to simulated on LLM error.
		slog.Warn("experiment runner: LLM prompt_tuning failed, falling back to simulated",
			"hypothesis_id", hypothesis.ID, "err", result.Error)
	}

	// Simulated execution for all other categories (and fallback).
	metric, ok := categoryBaselines[hypothesis.Category]
	if !ok {
		// Unknown category: return a neutral metric slightly above baseline.
		metric = hypothesis.BaselineMetric * 1.05
	}

	// Check again after the (simulated) computation.
	select {
	case <-ctx.Done():
		return &ExperimentResult{Error: ctx.Err()}
	default:
	}

	return &ExperimentResult{
		Metric:     metric,
		SampleSize: 20, // fixed simulated sample size for MVP
	}
}

// executePromptTuning performs a real A/B prompt comparison using the LLM client.
func (r *ExperimentRunner) executePromptTuning(ctx context.Context, hypothesis *db.EvolutionHypothesis) *ExperimentResult {
	testScenario := "Analyze the following code change and provide a structured implementation plan with clear steps."

	// Run baseline prompt.
	baselineResp, err := r.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: fmt.Sprintf("You are a software development assistant. Style: %s", hypothesis.BaselineValue),
		Messages:     []llm.Message{{Role: "user", Content: testScenario}},
		MaxTokens:    1024,
		Temperature:  0.3,
	})
	if err != nil {
		return &ExperimentResult{Error: fmt.Errorf("baseline prompt: %w", err)}
	}

	// Run proposed prompt.
	proposedResp, err := r.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: fmt.Sprintf("You are a software development assistant. Style: %s", hypothesis.ProposedValue),
		Messages:     []llm.Message{{Role: "user", Content: testScenario}},
		MaxTokens:    1024,
		Temperature:  0.3,
	})
	if err != nil {
		return &ExperimentResult{Error: fmt.Errorf("proposed prompt: %w", err)}
	}

	// Score using LLM evaluator.
	evalPrompt := fmt.Sprintf(`Evaluate these two responses to the same prompt.

Baseline response (length %d chars):
%s

Proposed response (length %d chars):
%s

Return a JSON object: {"baseline_score": 0.0-1.0, "proposed_score": 0.0-1.0, "reasoning": "..."}`,
		len(baselineResp.Content), truncate(baselineResp.Content, 2000),
		len(proposedResp.Content), truncate(proposedResp.Content, 2000))

	evalResp, err := r.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: prompts.ExperimentEvaluation,
		Messages:     []llm.Message{{Role: "user", Content: evalPrompt}},
		MaxTokens:    512,
		Temperature:  0.2,
	})
	if err != nil {
		return &ExperimentResult{Error: fmt.Errorf("evaluation: %w", err)}
	}

	var scores struct {
		BaselineScore float64 `json:"baseline_score"`
		ProposedScore float64 `json:"proposed_score"`
		Reasoning     string  `json:"reasoning"`
	}
	if err := llm.ParseJSONResponse(evalResp.Content, &scores); err != nil {
		return &ExperimentResult{Error: fmt.Errorf("parse evaluation: %w", err)}
	}

	return &ExperimentResult{
		Metric:     scores.ProposedScore,
		SampleSize: 1, // single A/B comparison
	}
}

// truncate shortens s to at most maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
