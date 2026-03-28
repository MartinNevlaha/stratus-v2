package product_intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

type GapAnalyzer struct {
	config GapAnalyzerConfig
	store  Store
	llm    llm.Client
}

type GapAnalyzerConfig struct {
	MinPrevalenceThreshold float64 `json:"min_prevalence_threshold"`
	MinImportanceThreshold float64 `json:"min_importance_threshold"`
}

func DefaultGapAnalyzerConfig() GapAnalyzerConfig {
	return GapAnalyzerConfig{
		MinPrevalenceThreshold: 0.5,
		MinImportanceThreshold: 0.6,
	}
}

func NewGapAnalyzer(config GapAnalyzerConfig, store Store, llmClient llm.Client) *GapAnalyzer {
	return &GapAnalyzer{
		config: config,
		store:  store,
		llm:    llmClient,
	}
}

func (a *GapAnalyzer) Analyze(ctx context.Context, projectID string, projectFeatures []ProjectFeature, marketFeatures []MarketFeature) ([]FeatureGap, error) {
	if a.llm != nil {
		gaps, err := a.analyzeWithLLM(ctx, projectID, projectFeatures, marketFeatures)
		if err == nil && len(gaps) > 0 {
			return gaps, nil
		}
		if err != nil {
			slog.Warn("gap_analyzer: LLM analysis failed, falling back to rule-based", "error", err)
		}
	}

	return a.analyzeRuleBased(projectID, projectFeatures, marketFeatures)
}

type llmGapResponse struct {
	FeatureName     string  `json:"feature_name"`
	GapType         string  `json:"gap_type"`
	ImpactScore     float64 `json:"impact_score"`
	ComplexityScore float64 `json:"complexity_score"`
	StrategicFit    float64 `json:"strategic_fit"`
	Confidence      float64 `json:"confidence"`
	Reasoning       string  `json:"reasoning"`
}

func (a *GapAnalyzer) analyzeWithLLM(ctx context.Context, projectID string, projectFeatures []ProjectFeature, marketFeatures []MarketFeature) ([]FeatureGap, error) {
	projectFeatureNames := make([]string, len(projectFeatures))
	for i, f := range projectFeatures {
		projectFeatureNames[i] = f.FeatureName
	}

	marketFeatureNames := make([]string, len(marketFeatures))
	for i, f := range marketFeatures {
		marketFeatureNames[i] = f.FeatureName
	}

	prompt := fmt.Sprintf(`Analyze feature gaps between a project and market standards.

Project features: %v
Market standard features: %v

Identify gaps where the project is missing features that are common in the market.
Respond with a JSON array:
[
  {
    "feature_name": "missing feature name",
    "gap_type": "one of: missing, weak, enhancement",
    "impact_score": 0.0-1.0,
    "complexity_score": 0.0-1.0,
    "strategic_fit": 0.0-1.0,
    "confidence": 0.0-1.0,
    "reasoning": "brief explanation"
  }
]

Only respond with the JSON array, no additional text.`, projectFeatureNames, marketFeatureNames)

	resp, err := a.llm.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: "You are an expert product manager. Analyze feature gaps with high accuracy. Always respond with valid JSON.",
		Messages:     []llm.Message{llm.UserMessage(prompt)},
		MaxTokens:    2000,
		Temperature:  0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}

	var llmGaps []llmGapResponse
	if err := llm.ParseJSONResponse(resp.Content, &llmGaps); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	gaps := make([]FeatureGap, 0, len(llmGaps))

	for _, lg := range llmGaps {
		gaps = append(gaps, FeatureGap{
			ID:              generateGapID(projectID, lg.FeatureName),
			ProjectID:       projectID,
			FeatureName:     lg.FeatureName,
			GapType:         GapType(lg.GapType),
			ImpactScore:     lg.ImpactScore,
			ComplexityScore: lg.ComplexityScore,
			StrategicFit:    lg.StrategicFit,
			Confidence:      lg.Confidence,
			Reasoning:       lg.Reasoning,
			Status:          GapStatusIdentified,
			CreatedAt:       now,
		})
	}

	return gaps, nil
}

func (a *GapAnalyzer) analyzeRuleBased(projectID string, projectFeatures []ProjectFeature, marketFeatures []MarketFeature) ([]FeatureGap, error) {
	projectFeatureSet := make(map[string]ProjectFeature)
	for _, f := range projectFeatures {
		normalized := normalizeFeatureName(f.FeatureName)
		projectFeatureSet[normalized] = f
	}

	var gaps []FeatureGap

	for _, mf := range marketFeatures {
		if mf.Prevalence < a.config.MinPrevalenceThreshold && mf.Importance < a.config.MinImportanceThreshold {
			continue
		}

		normalized := normalizeFeatureName(mf.FeatureName)

		if _, exists := projectFeatureSet[normalized]; !exists {
			found := false
			for pfName := range projectFeatureSet {
				if isSimilarFeature(normalized, pfName) {
					found = true
					break
				}
			}

			if !found {
				gap := a.createMissingGap(projectID, mf)
				gaps = append(gaps, gap)
			}
		}
	}

	for _, pf := range projectFeatures {
		if pf.Confidence < 0.5 {
			for _, mf := range marketFeatures {
				if isSimilarFeature(normalizeFeatureName(pf.FeatureName), normalizeFeatureName(mf.FeatureName)) {
					if mf.Prevalence > 0.7 {
						gap := a.createWeakGap(projectID, pf, mf)
						gaps = append(gaps, gap)
						break
					}
				}
			}
		}
	}

	for i := 0; i < len(gaps); i++ {
		for j := i + 1; j < len(gaps); j++ {
			if gaps[j].ImpactScore > gaps[i].ImpactScore {
				gaps[i], gaps[j] = gaps[j], gaps[i]
			}
		}
	}

	return gaps, nil
}

func (a *GapAnalyzer) createMissingGap(projectID string, mf MarketFeature) FeatureGap {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	impactScore := mf.Importance * 0.8
	if mf.Prevalence > 0.8 {
		impactScore += 0.1
	}
	if mf.Prevalence > 0.9 {
		impactScore += 0.1
	}

	complexityScore := 0.5
	switch mf.FeatureType {
	case FeatureTypeAuth:
		complexityScore = 0.7
	case FeatureTypeIntegration:
		complexityScore = 0.65
	case FeatureTypeAnalytics:
		complexityScore = 0.6
	case FeatureTypeUI:
		complexityScore = 0.45
	case FeatureTypeReporting:
		complexityScore = 0.5
	}

	reasoning := a.generateMissingReasoning(mf)

	return FeatureGap{
		ID:              generateGapID(projectID, mf.FeatureName),
		ProjectID:       projectID,
		FeatureName:     mf.FeatureName,
		GapType:         GapTypeMissing,
		ImpactScore:     impactScore,
		ComplexityScore: complexityScore,
		StrategicFit:    mf.Prevalence,
		Confidence:      0.7 + mf.Importance*0.2,
		Reasoning:       reasoning,
		Status:          GapStatusIdentified,
		CreatedAt:       now,
	}
}

func (a *GapAnalyzer) createWeakGap(projectID string, pf ProjectFeature, mf MarketFeature) FeatureGap {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	impactScore := mf.Importance * 0.6
	complexityScore := 0.35

	reasoning := a.generateWeakReasoning(pf, mf)

	return FeatureGap{
		ID:              generateGapID(projectID, "enhance_"+pf.FeatureName),
		ProjectID:       projectID,
		FeatureName:     pf.FeatureName,
		GapType:         GapTypeWeak,
		ImpactScore:     impactScore,
		ComplexityScore: complexityScore,
		StrategicFit:    mf.Prevalence * 0.8,
		Confidence:      0.6,
		Reasoning:       reasoning,
		Status:          GapStatusIdentified,
		CreatedAt:       now,
	}
}

func (a *GapAnalyzer) generateMissingReasoning(mf MarketFeature) string {
	prevalence := "moderate"
	if mf.Prevalence >= 0.8 {
		prevalence = "high"
	}
	if mf.Prevalence >= 0.9 {
		prevalence = "very high"
	}

	return "This feature is present in " + prevalence + " prevalence (" +
		formatPercent(mf.Prevalence) + ") of comparable products in the market. " +
		"Market importance rating: " + formatPercent(mf.Importance) + "."
}

func (a *GapAnalyzer) generateWeakReasoning(pf ProjectFeature, mf MarketFeature) string {
	return "Current implementation has low confidence (" + formatPercent(pf.Confidence) +
		"). Market analysis shows " + formatPercent(mf.Prevalence) +
		" prevalence with " + formatPercent(mf.Importance) + " importance. " +
		"Consider enhancing this feature."
}

func normalizeFeatureName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	return strings.TrimSpace(name)
}

func isSimilarFeature(name1, name2 string) bool {
	if name1 == name2 {
		return true
	}

	similarGroups := [][]string{
		{"auth", "authentication", "login", "signin"},
		{"user", "users", "user management", "account"},
		{"report", "reports", "reporting", "analytics"},
		{"notification", "notifications", "alerts", "alerting"},
		{"dashboard", "dashboards", "analytics dashboard"},
		{"search", "searching", "filter", "query"},
		{"api", "api access", "rest api", "graphql"},
		{"chat", "messaging", "messages", "communication"},
		{"task", "tasks", "todo", "todos"},
		{"workflow", "workflows", "automation", "process"},
		{"export", "import", "data export", "data import"},
		{"mobile", "mobile app", "mobile support"},
		{"ai", "artificial intelligence", "machine learning", "ml"},
		{"file", "files", "document", "documents", "attachment"},
		{"integration", "integrations", "webhook", "api integration"},
	}

	for _, group := range similarGroups {
		found1 := false
		found2 := false
		for _, term := range group {
			if strings.Contains(name1, term) {
				found1 = true
			}
			if strings.Contains(name2, term) {
				found2 = true
			}
		}
		if found1 && found2 {
			return true
		}
	}

	return false
}

func formatPercent(v float64) string {
	return fmt.Sprintf("%.0f%%", v*100)
}

func generateGapID(projectID, featureName string) string {
	return projectID + "_gap_" + strings.ReplaceAll(strings.ToLower(featureName), " ", "_")
}
