package evolution_loop

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/prompts"
	"github.com/google/uuid"
)

// allCategories is the full set of hypothesis categories the generator knows about.
var allCategories = []string{
	"workflow_routing",
	"agent_selection",
	"threshold_adjustment",
	"prompt_tuning",
}

// seedHypotheses is a catalogue of simple MVP hypotheses indexed by category.
var seedHypotheses = map[string][]struct {
	desc          string
	baselineValue string
	proposedValue string
	metric        string
	baselineMetric float64
}{
	"workflow_routing": {
		{
			desc:           "Lower routing confidence threshold from 0.80 to 0.75 to reduce fallbacks",
			baselineValue:  "0.80",
			proposedValue:  "0.75",
			metric:         "routing_accuracy",
			baselineMetric: 0.80,
		},
		{
			desc:           "Raise routing confidence threshold from 0.80 to 0.85 to improve precision",
			baselineValue:  "0.80",
			proposedValue:  "0.85",
			metric:         "routing_accuracy",
			baselineMetric: 0.80,
		},
	},
	"agent_selection": {
		{
			desc:           "Prefer specialist agents for tasks with >3 files touched",
			baselineValue:  "generalist",
			proposedValue:  "specialist_above_3_files",
			metric:         "task_success_rate",
			baselineMetric: 0.72,
		},
	},
	"threshold_adjustment": {
		{
			desc:           "Reduce auto-apply confidence threshold from 0.85 to 0.80",
			baselineValue:  "0.85",
			proposedValue:  "0.80",
			metric:         "auto_apply_accuracy",
			baselineMetric: 0.85,
		},
		{
			desc:           "Increase min sample size from 10 to 15 for higher statistical confidence",
			baselineValue:  "10",
			proposedValue:  "15",
			metric:         "decision_reliability",
			baselineMetric: 0.78,
		},
	},
	"prompt_tuning": {
		{
			desc:           "Add chain-of-thought prefix to planning prompts",
			baselineValue:  "standard",
			proposedValue:  "chain_of_thought",
			metric:         "plan_quality_score",
			baselineMetric: 0.68,
		},
	},
}

// HypothesisGenerator creates candidate hypotheses for an evolution run.
type HypothesisGenerator struct {
	store     EvolutionStore
	llmClient llm.Client
}

// NewHypothesisGenerator constructs a HypothesisGenerator.
// llmClient may be nil; a nil value means "no LLM available, use static/simulated behavior".
func NewHypothesisGenerator(store EvolutionStore, llmClient llm.Client) *HypothesisGenerator {
	return &HypothesisGenerator{store: store, llmClient: llmClient}
}

// Generate returns up to maxCount hypotheses, optionally filtered by categories.
// When categories is empty all known categories are used.
// It tries LLM-powered generation first and falls back to static seeds on error.
func (g *HypothesisGenerator) Generate(
	ctx context.Context,
	runID string,
	categories []string,
	maxCount int,
) ([]db.EvolutionHypothesis, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("evolution hypothesis generator: context cancelled: %w", err)
	}

	// Try LLM-powered generation first.
	if g.llmClient != nil {
		hypotheses, err := g.generateWithLLM(ctx, runID, categories, maxCount)
		if err != nil {
			slog.Warn("evolution hypothesis generator: LLM generation failed, falling back to seeds", "err", err)
		} else if len(hypotheses) > 0 {
			return hypotheses, nil
		}
	}

	// Fall back to seed hypotheses.
	active := categories
	if len(active) == 0 {
		active = allCategories
	}

	var result []db.EvolutionHypothesis

	for _, cat := range active {
		if len(result) >= maxCount {
			break
		}
		seeds, ok := seedHypotheses[cat]
		if !ok {
			continue
		}
		for _, s := range seeds {
			if len(result) >= maxCount {
				break
			}
			result = append(result, db.EvolutionHypothesis{
				ID:             uuid.NewString(),
				RunID:          runID,
				Category:       cat,
				Description:    s.desc,
				BaselineValue:  s.baselineValue,
				ProposedValue:  s.proposedValue,
				Metric:         s.metric,
				BaselineMetric: s.baselineMetric,
				Evidence:       map[string]any{},
			})
		}
	}

	return result, nil
}

// generateWithLLM calls the LLM to produce hypotheses and returns the parsed results.
func (g *HypothesisGenerator) generateWithLLM(
	ctx context.Context,
	runID string,
	categories []string,
	maxCount int,
) ([]db.EvolutionHypothesis, error) {
	active := categories
	if len(active) == 0 {
		active = allCategories
	}

	userPrompt := fmt.Sprintf(`Current system state:
- Categories to explore: %v
- Max hypotheses: %d

Generate up to %d hypotheses as a JSON array:
[{"category": "...", "description": "...", "baseline_value": "...", "proposed_value": "...", "metric": "...", "baseline_metric": 0.0, "rationale": "..."}]`, active, maxCount, maxCount)

	resp, err := g.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: prompts.HypothesisGeneration,
		Messages:     []llm.Message{{Role: "user", Content: userPrompt}},
		MaxTokens:    2048,
		Temperature:  0.7,
	})
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}

	var raw []struct {
		Category       string  `json:"category"`
		Description    string  `json:"description"`
		BaselineValue  string  `json:"baseline_value"`
		ProposedValue  string  `json:"proposed_value"`
		Metric         string  `json:"metric"`
		BaselineMetric float64 `json:"baseline_metric"`
		Rationale      string  `json:"rationale"`
	}

	if err := llm.ParseJSONResponse(resp.Content, &raw); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	var hypotheses []db.EvolutionHypothesis
	for _, r := range raw {
		if len(hypotheses) >= maxCount {
			break
		}
		// Validate that the category is one we recognise.
		validCat := false
		for _, c := range allCategories {
			if r.Category == c {
				validCat = true
				break
			}
		}
		if !validCat {
			continue
		}

		hypotheses = append(hypotheses, db.EvolutionHypothesis{
			ID:             uuid.NewString(),
			RunID:          runID,
			Category:       r.Category,
			Description:    r.Description,
			BaselineValue:  r.BaselineValue,
			ProposedValue:  r.ProposedValue,
			Metric:         r.Metric,
			BaselineMetric: r.BaselineMetric,
			Evidence:       map[string]any{"llm_rationale": r.Rationale},
		})
	}

	return hypotheses, nil
}
