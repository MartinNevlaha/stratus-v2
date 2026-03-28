package product_intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

type FeatureScorer struct {
	config FeatureScorerConfig
	store  Store
	llm    llm.Client
}

type FeatureScorerConfig struct {
	ImpactWeight       float64 `json:"impact_weight"`
	ComplexityWeight   float64 `json:"complexity_weight"`
	StrategicFitWeight float64 `json:"strategic_fit_weight"`
}

func DefaultFeatureScorerConfig() FeatureScorerConfig {
	return FeatureScorerConfig{
		ImpactWeight:       0.4,
		ComplexityWeight:   0.3,
		StrategicFitWeight: 0.3,
	}
}

func NewFeatureScorer(config FeatureScorerConfig, store Store, llmClient llm.Client) *FeatureScorer {
	return &FeatureScorer{
		config: config,
		store:  store,
		llm:    llmClient,
	}
}

func (s *FeatureScorer) Score(ctx context.Context, gaps []FeatureGap, projectFeatures []ProjectFeature) ([]ScoredFeature, error) {
	if s.llm != nil {
		scored, err := s.scoreWithLLM(ctx, gaps, projectFeatures)
		if err == nil && len(scored) > 0 {
			return scored, nil
		}
		if err != nil {
			slog.Warn("feature_scorer: LLM scoring failed, falling back to rule-based", "error", err)
		}
	}

	return s.scoreRuleBased(gaps, projectFeatures)
}

type llmScoreResponse struct {
	FeatureName     string  `json:"feature_name"`
	ImpactScore     float64 `json:"impact_score"`
	ComplexityScore float64 `json:"complexity_score"`
	StrategicFit    float64 `json:"strategic_fit"`
	OverallScore    float64 `json:"overall_score"`
	Confidence      float64 `json:"confidence"`
	Reasoning       string  `json:"reasoning"`
}

func (s *FeatureScorer) scoreWithLLM(ctx context.Context, gaps []FeatureGap, projectFeatures []ProjectFeature) ([]ScoredFeature, error) {
	gapNames := make([]string, len(gaps))
	for i, g := range gaps {
		gapNames[i] = g.FeatureName
	}

	featureNames := make([]string, len(projectFeatures))
	for i, f := range projectFeatures {
		featureNames[i] = f.FeatureName
	}

	prompt := fmt.Sprintf(`Score these feature gaps based on impact, complexity, and strategic fit.

Gaps to score: %v
Existing project features: %v

For each gap, provide scores. Respond with a JSON array:
[
  {
    "feature_name": "gap feature name",
    "impact_score": 0.0-1.0,
    "complexity_score": 0.0-1.0,
    "strategic_fit": 0.0-1.0,
    "overall_score": 0.0-1.0,
    "confidence": 0.0-1.0,
    "reasoning": "brief explanation"
  }
]

Only respond with the JSON array.`, gapNames, featureNames)

	resp, err := s.llm.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: "You are an expert product manager. Score features objectively. Always respond with valid JSON.",
		Messages:     []llm.Message{llm.UserMessage(prompt)},
		MaxTokens:    2000,
		Temperature:  0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}

	var llmScores []llmScoreResponse
	if err := llm.ParseJSONResponse(resp.Content, &llmScores); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	scored := make([]ScoredFeature, 0, len(llmScores))
	for _, ls := range llmScores {
		scored = append(scored, ScoredFeature{
			FeatureName:     ls.FeatureName,
			ImpactScore:     ls.ImpactScore,
			ComplexityScore: ls.ComplexityScore,
			StrategicFit:    ls.StrategicFit,
			OverallScore:    ls.OverallScore,
			Confidence:      ls.Confidence,
			Reasoning:       ls.Reasoning,
		})
	}

	return scored, nil
}

func (s *FeatureScorer) scoreRuleBased(gaps []FeatureGap, projectFeatures []ProjectFeature) ([]ScoredFeature, error) {
	featureComplexityMap := s.buildComplexityMap(projectFeatures)

	scored := make([]ScoredFeature, 0, len(gaps))

	for _, gap := range gaps {
		score := s.scoreGap(gap, featureComplexityMap)
		scored = append(scored, score)
	}

	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].OverallScore > scored[i].OverallScore {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	return scored, nil
}

func (s *FeatureScorer) scoreGap(gap FeatureGap, complexityMap map[string]float64) ScoredFeature {
	impactScore := gap.ImpactScore

	complexityScore := gap.ComplexityScore
	if adjComplexity, ok := complexityMap[gap.FeatureName]; ok {
		complexityScore = (complexityScore + adjComplexity) / 2
	}

	strategicFit := gap.StrategicFit
	adjustedComplexity := 1.0 - complexityScore

	overallScore := (impactScore*s.config.ImpactWeight +
		adjustedComplexity*s.config.ComplexityWeight +
		strategicFit*s.config.StrategicFitWeight)

	reasoning := s.generateScoreReasoning(gap, impactScore, complexityScore, strategicFit, overallScore)

	return ScoredFeature{
		FeatureName:     gap.FeatureName,
		ImpactScore:     impactScore,
		ComplexityScore: complexityScore,
		StrategicFit:    strategicFit,
		OverallScore:    overallScore,
		Confidence:      gap.Confidence,
		Reasoning:       reasoning,
	}
}

func (s *FeatureScorer) buildComplexityMap(projectFeatures []ProjectFeature) map[string]float64 {
	complexityMap := make(map[string]float64)

	relatedFeatureMap := map[string][]string{
		"ai_risk_prediction":    {"ai", "ml", "analytics", "reporting"},
		"supplier_scoring":      {"supplier", "vendor", "analytics", "reporting"},
		"ai_audit_suggestions":  {"ai", "audit", "checklist"},
		"mobile_capture":        {"mobile", "api", "file"},
		"automated_scheduling":  {"workflow", "scheduling", "automation"},
		"workflow_automation":   {"workflow", "automation", "integration"},
		"recommendation_engine": {"ai", "ml", "analytics", "search"},
		"ai_chatbot":            {"ai", "chat", "messaging", "notification"},
		"forecasting":           {"analytics", "reporting", "ai"},
		"anomaly_detection":     {"ai", "analytics", "monitoring"},
	}

	for feature, related := range relatedFeatureMap {
		relevanceScore := 0.0
		for _, pf := range projectFeatures {
			pfName := strings.ToLower(pf.FeatureName)
			for _, rel := range related {
				if strings.Contains(pfName, rel) {
					relevanceScore += 0.15
				}
			}
		}
		complexityMap[feature] = 1.0 - min(relevanceScore, 0.6)
	}

	return complexityMap
}

func (s *FeatureScorer) generateScoreReasoning(gap FeatureGap, impact, complexity, strategicFit, overall float64) string {
	var reasons []string

	if impact >= 0.8 {
		reasons = append(reasons, "high market impact")
	} else if impact >= 0.6 {
		reasons = append(reasons, "moderate market impact")
	}

	if complexity <= 0.3 {
		reasons = append(reasons, "low implementation complexity")
	} else if complexity <= 0.5 {
		reasons = append(reasons, "moderate implementation complexity")
	} else {
		reasons = append(reasons, "higher implementation complexity")
	}

	if strategicFit >= 0.7 {
		reasons = append(reasons, "strong strategic fit with existing codebase")
	} else if strategicFit >= 0.5 {
		reasons = append(reasons, "reasonable strategic fit")
	}

	if gap.GapType == GapTypeWeak {
		reasons = append(reasons, "opportunity to enhance existing feature")
	}

	return strings.Join(reasons, "; ") + "."
}

func (s *FeatureScorer) GetTopRecommendations(scored []ScoredFeature, limit int) []ScoredFeature {
	if limit <= 0 || limit > len(scored) {
		limit = len(scored)
	}

	return scored[:limit]
}

func (s *FeatureScorer) FilterByScore(scored []ScoredFeature, minOverallScore float64) []ScoredFeature {
	filtered := make([]ScoredFeature, 0)
	for _, s := range scored {
		if s.OverallScore >= minOverallScore {
			filtered = append(filtered, s)
		}
	}
	return filtered
}
