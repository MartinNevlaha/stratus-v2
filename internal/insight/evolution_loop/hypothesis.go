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

// allCategories is the set of hypothesis categories the legacy generator knows about.
// NOTE: workflow_routing, agent_selection, and threshold_adjustment were removed in T9.
// Rows with those categories already persisted in insight_proposals remain readable
// but no new code path emits them. Only prompt_tuning is retained here because
// execute() in loop.go still drives the legacy HypothesisGenerator for the Run()
// code path; RunCycle() uses the generators/ sub-package instead.
var allCategories = []string{
	"prompt_tuning",
}

// SeedHypothesis holds the data for a single seed hypothesis entry.
// Exported so that tests can inspect fields without reflection.
type SeedHypothesis struct {
	Desc          string
	BaselineValue string
	ProposedValue string
	Metric        string
	BaselineMetric float64
}

// seedHypothesesByLang is a catalogue of seed hypotheses per language, indexed
// first by language code then by category.
// NOTE: workflow_routing, agent_selection, and threshold_adjustment seeds were
// removed in T9. Only prompt_tuning seeds remain for the legacy Run() path.
var seedHypothesesByLang = map[string]map[string][]SeedHypothesis{
	"en": {
		"prompt_tuning": {
			{
				Desc:           "Add chain-of-thought prefix to planning prompts",
				BaselineValue:  "standard",
				ProposedValue:  "chain_of_thought",
				Metric:         "plan_quality_score",
				BaselineMetric: 0.68,
			},
		},
	},
	"sk": {
		"prompt_tuning": {
			{
				Desc:           "Pridať predponu reťazca myšlienok do plánovacích výziev",
				BaselineValue:  "standard",
				ProposedValue:  "chain_of_thought",
				Metric:         "plan_quality_score",
				BaselineMetric: 0.68,
			},
		},
	},
}

// SeedsFor returns the seed hypothesis map for the given language code.
// If lang is not a known language, it logs a warning and falls back to "en".
func SeedsFor(lang string) map[string][]SeedHypothesis {
	if seeds, ok := seedHypothesesByLang[lang]; ok {
		return seeds
	}
	slog.Warn("evolution hypothesis: unknown language, falling back to English", "lang", lang)
	return seedHypothesesByLang["en"]
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
// lang controls the language of descriptions and the LLM system prompt.
func (g *HypothesisGenerator) Generate(
	ctx context.Context,
	runID string,
	categories []string,
	maxCount int,
) ([]db.EvolutionHypothesis, error) {
	return g.GenerateWithLang(ctx, runID, categories, maxCount, "en")
}

// GenerateWithLang is like Generate but accepts an explicit language code.
func (g *HypothesisGenerator) GenerateWithLang(
	ctx context.Context,
	runID string,
	categories []string,
	maxCount int,
	lang string,
) ([]db.EvolutionHypothesis, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("evolution hypothesis generator: context cancelled: %w", err)
	}

	// Try LLM-powered generation first.
	if g.llmClient != nil {
		hypotheses, err := g.generateWithLLM(ctx, runID, categories, maxCount, lang)
		if err != nil {
			slog.Warn("evolution hypothesis generator: LLM generation failed, falling back to seeds", "err", err)
		} else if len(hypotheses) > 0 {
			return hypotheses, nil
		}
	}

	// Fall back to seed hypotheses in the requested language.
	active := categories
	if len(active) == 0 {
		active = allCategories
	}

	seeds := SeedsFor(lang)
	var result []db.EvolutionHypothesis

	for _, cat := range active {
		if len(result) >= maxCount {
			break
		}
		catSeeds, ok := seeds[cat]
		if !ok {
			continue
		}
		for _, s := range catSeeds {
			if len(result) >= maxCount {
				break
			}
			result = append(result, db.EvolutionHypothesis{
				ID:             uuid.NewString(),
				RunID:          runID,
				Category:       cat,
				Description:    s.Desc,
				BaselineValue:  s.BaselineValue,
				ProposedValue:  s.ProposedValue,
				Metric:         s.Metric,
				BaselineMetric: s.BaselineMetric,
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
	lang string,
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
		SystemPrompt:  prompts.WithLanguage(prompts.HypothesisGeneration, lang),
		Messages:      []llm.Message{{Role: "user", Content: userPrompt}},
		MaxTokens:     8192,
		Temperature:   0.7,
		ResponseFormat: "json",
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
