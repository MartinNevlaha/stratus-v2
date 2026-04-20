package product_intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

type ProposalEngine struct {
	config ProposalEngineConfig
	store  Store
	llm    llm.Client
}

type ProposalEngineConfig struct {
	MaxProposals    int     `json:"max_proposals"`
	MinConfidence   float64 `json:"min_confidence"`
	MinOverallScore float64 `json:"min_overall_score"`
}

func DefaultProposalEngineConfig() ProposalEngineConfig {
	return ProposalEngineConfig{
		MaxProposals:    10,
		MinConfidence:   0.5,
		MinOverallScore: 0.5,
	}
}

func NewProposalEngine(config ProposalEngineConfig, store Store, llmClient llm.Client) *ProposalEngine {
	return &ProposalEngine{
		config: config,
		store:  store,
		llm:    llmClient,
	}
}

func (e *ProposalEngine) Generate(ctx context.Context, projectID string, gaps []FeatureGap, scored []ScoredFeature, projectFeatures []ProjectFeature) ([]FeatureProposal, error) {
	if e.llm != nil {
		proposals, err := e.generateWithLLM(ctx, projectID, gaps, scored, projectFeatures)
		if err == nil && len(proposals) > 0 {
			if len(proposals) > e.config.MaxProposals {
				proposals = proposals[:e.config.MaxProposals]
			}
			return proposals, nil
		}
		if err != nil {
			slog.Warn("proposal_engine: LLM generation failed, falling back to rule-based", "error", err)
		}
	}

	return e.generateRuleBased(projectID, gaps, scored, projectFeatures)
}

type llmProposalResponse struct {
	FeatureName         string   `json:"feature_name"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	Rationale           string   `json:"rationale"`
	ImpactScore         int      `json:"impact_score"`
	ComplexityScore     int      `json:"complexity_score"`
	StrategicFit        float64  `json:"strategic_fit"`
	Confidence          float64  `json:"confidence"`
	ImplementationHints []string `json:"implementation_hints"`
}

func (e *ProposalEngine) generateWithLLM(ctx context.Context, projectID string, gaps []FeatureGap, scored []ScoredFeature, projectFeatures []ProjectFeature) ([]FeatureProposal, error) {
	gapNames := make([]string, len(gaps))
	for i, g := range gaps {
		gapNames[i] = g.FeatureName
	}

	featureNames := make([]string, len(projectFeatures))
	for i, f := range projectFeatures {
		featureNames[i] = f.FeatureName
	}

	prompt := fmt.Sprintf(`Generate implementation proposals for these feature gaps.

Gaps to address: %v
Existing features: %v

Create proposals for implementing these features. Respond with a JSON array:
[
  {
    "feature_name": "feature name",
    "title": "Implementation Proposal: Feature Name",
    "description": "Brief description of the implementation",
    "rationale": "Why this feature should be implemented",
    "impact_score": 1-10,
    "complexity_score": 1-10,
    "strategic_fit": 0.0-1.0,
    "confidence": 0.0-1.0,
    "implementation_hints": ["hint1", "hint2"]
  }
]

Only respond with the JSON array.`, gapNames, featureNames)

	resp, err := e.llm.Complete(ctx, llm.CompletionRequest{
		SystemPrompt:   "You are an expert software architect. Generate implementation proposals. Always respond with valid JSON.",
		Messages:       []llm.Message{llm.UserMessage(prompt)},
		MaxTokens:      100000,
		Temperature:    0.7,
		ResponseFormat: "json",
	})
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}

	var llmProposals []llmProposalResponse
	if err := llm.ParseJSONResponse(resp.Content, &llmProposals); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	proposals := make([]FeatureProposal, 0, len(llmProposals))

	for _, lp := range llmProposals {
		if lp.Confidence < e.config.MinConfidence {
			continue
		}
		proposals = append(proposals, FeatureProposal{
			ID:                  generateProposalID(projectID, lp.FeatureName),
			ProjectID:           projectID,
			FeatureName:         lp.FeatureName,
			Title:               lp.Title,
			Description:         lp.Description,
			Rationale:           lp.Rationale,
			ImpactScore:         lp.ImpactScore,
			ComplexityScore:     lp.ComplexityScore,
			StrategicFit:        lp.StrategicFit,
			Confidence:          lp.Confidence,
			ImplementationHints: lp.ImplementationHints,
			Status:              ProposalStatusProposed,
			CreatedAt:           now,
			UpdatedAt:           now,
		})
	}

	return proposals, nil
}

func (e *ProposalEngine) generateRuleBased(projectID string, gaps []FeatureGap, scored []ScoredFeature, projectFeatures []ProjectFeature) ([]FeatureProposal, error) {
	scoreMap := make(map[string]ScoredFeature)
	for _, s := range scored {
		scoreMap[s.FeatureName] = s
	}

	var proposals []FeatureProposal

	for _, gap := range gaps {
		score, hasScore := scoreMap[gap.FeatureName]
		if !hasScore {
			continue
		}

		if score.OverallScore < e.config.MinOverallScore {
			continue
		}

		if gap.Confidence < e.config.MinConfidence {
			continue
		}

		proposal := e.createProposal(projectID, gap, score, projectFeatures)
		proposals = append(proposals, proposal)

		if len(proposals) >= e.config.MaxProposals {
			break
		}
	}

	for i := 0; i < len(proposals); i++ {
		for j := i + 1; j < len(proposals); j++ {
			if proposals[j].ImpactScore > proposals[i].ImpactScore ||
				(proposals[j].ImpactScore == proposals[i].ImpactScore && proposals[j].Confidence > proposals[i].Confidence) {
				proposals[i], proposals[j] = proposals[j], proposals[i]
			}
		}
	}

	return proposals, nil
}

func (e *ProposalEngine) createProposal(projectID string, gap FeatureGap, score ScoredFeature, projectFeatures []ProjectFeature) FeatureProposal {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	title := e.generateTitle(gap.FeatureName)
	description := e.generateDescription(gap.FeatureName, gap.GapType)
	rationale := e.generateRationale(gap, score)
	hints := e.generateImplementationHints(gap.FeatureName, projectFeatures)
	evidence := e.buildEvidence(gap, score)

	return FeatureProposal{
		ID:                  generateProposalID(projectID, gap.FeatureName),
		ProjectID:           projectID,
		GapID:               gap.ID,
		FeatureName:         gap.FeatureName,
		Title:               title,
		Description:         description,
		Rationale:           rationale,
		ImpactScore:         int(score.ImpactScore * 10),
		ComplexityScore:     int(score.ComplexityScore * 10),
		StrategicFit:        score.StrategicFit,
		Confidence:          score.Confidence,
		Evidence:            evidence,
		ImplementationHints: hints,
		Status:              ProposalStatusProposed,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func (e *ProposalEngine) generateTitle(featureName string) string {
	words := strings.Split(featureName, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func (e *ProposalEngine) generateDescription(featureName string, gapType GapType) string {
	descriptions := map[string]string{
		"ai_risk_prediction":      "Implement AI-powered risk prediction to identify potential issues before they occur, leveraging historical data and pattern recognition",
		"supplier_scoring":        "Add supplier scoring and analytics to evaluate vendor performance and make data-driven procurement decisions",
		"ai_audit_suggestions":    "Integrate AI suggestions during audit processes to improve efficiency and catch common issues automatically",
		"mobile_capture":          "Develop mobile app capabilities for on-the-go data capture, field work, and offline functionality",
		"automated_scheduling":    "Implement automated scheduling with intelligent resource allocation and conflict resolution",
		"workflow_automation":     "Add workflow automation engine to streamline processes and reduce manual intervention",
		"recommendation_engine":   "Build a recommendation engine to provide personalized suggestions based on user behavior and preferences",
		"ai_chatbot":              "Integrate AI-powered chatbot for customer support and user assistance",
		"forecasting":             "Add forecasting capabilities using historical data and predictive models",
		"anomaly_detection":       "Implement anomaly detection to automatically identify unusual patterns and potential issues",
		"compliance_dashboards":   "Create comprehensive compliance dashboards with real-time monitoring and alerts",
		"evidence_attachments":    "Enhance evidence management with rich attachments, versioning, and metadata",
		"offline_mode":            "Add offline mode support for uninterrupted work without network connectivity",
		"regulatory_mapping":      "Implement regulatory requirement mapping to track compliance obligations",
		"report_templates":        "Add customizable report templates with branding and export options",
		"finding_management":      "Enhance finding management with categorization, prioritization, and resolution tracking",
		"corrective_actions":      "Implement corrective action workflows with assignment, tracking, and verification",
		"ai_search":               "Add AI-powered search with natural language queries and semantic understanding",
		"loyalty_program":         "Implement customer loyalty program with points, tiers, and rewards",
		"abandoned_cart_recovery": "Add abandoned cart recovery with automated email sequences and incentives",
	}

	if desc, ok := descriptions[featureName]; ok {
		return desc
	}

	prefix := "Add"
	if gapType == GapTypeEnhancement {
		prefix = "Enhance"
	} else if gapType == GapTypeWeak {
		prefix = "Strengthen"
	}

	return prefix + " " + strings.ReplaceAll(featureName, "_", " ") + " functionality"
}

func (e *ProposalEngine) generateRationale(gap FeatureGap, score ScoredFeature) string {
	rationaleParts := []string{}

	if gap.GapType == GapTypeMissing {
		rationaleParts = append(rationaleParts, "This feature is currently missing from the product.")
	} else if gap.GapType == GapTypeWeak {
		rationaleParts = append(rationaleParts, "The existing implementation could be enhanced.")
	}

	if score.ImpactScore >= 0.7 {
		rationaleParts = append(rationaleParts, "High potential impact on user value and competitive positioning.")
	} else if score.ImpactScore >= 0.5 {
		rationaleParts = append(rationaleParts, "Moderate impact expected on user satisfaction.")
	}

	if score.StrategicFit >= 0.7 {
		rationaleParts = append(rationaleParts, "Strong alignment with existing codebase and architecture.")
	}

	if score.ComplexityScore <= 0.4 {
		rationaleParts = append(rationaleParts, "Implementation complexity is manageable.")
	}

	rationaleParts = append(rationaleParts, score.Reasoning)

	return strings.Join(rationaleParts, " ")
}

func (e *ProposalEngine) generateImplementationHints(featureName string, projectFeatures []ProjectFeature) []string {
	hints := []string{}

	featureHints := map[string][]string{
		"ai_risk_prediction": {
			"Leverage existing audit data for training models",
			"Consider integrating with existing analytics infrastructure",
			"Start with simple statistical models before advanced ML",
		},
		"supplier_scoring": {
			"Build on existing supplier/vendor data models",
			"Create scoring algorithms based on performance metrics",
			"Add visualization for score trends over time",
		},
		"ai_audit_suggestions": {
			"Use historical audit findings for training",
			"Implement suggestion API alongside existing audit workflows",
			"Consider rule-based approach as fallback",
		},
		"mobile_capture": {
			"Create mobile-friendly API endpoints",
			"Implement offline data synchronization",
			"Consider React Native or Flutter for cross-platform",
		},
		"workflow_automation": {
			"Design flexible workflow state machine",
			"Build on existing notification system",
			"Consider integration with external automation tools",
		},
		"recommendation_engine": {
			"Start with collaborative filtering approach",
			"Leverage existing user activity tracking",
			"Consider third-party ML services for quick start",
		},
		"ai_chatbot": {
			"Integrate with existing messaging/notification system",
			"Use existing knowledge base for context",
			"Consider LLM APIs for natural language understanding",
		},
		"forecasting": {
			"Utilize existing reporting data pipeline",
			"Implement time series analysis",
			"Add forecast visualization to dashboards",
		},
	}

	if h, ok := featureHints[featureName]; ok {
		hints = append(hints, h...)
	}

	for _, pf := range projectFeatures {
		if strings.Contains(strings.ToLower(pf.FeatureName), "api") {
			hints = append(hints, "Can leverage existing API infrastructure")
			break
		}
	}

	for _, pf := range projectFeatures {
		if strings.Contains(strings.ToLower(pf.FeatureName), "auth") {
			hints = append(hints, "Authentication already in place - can focus on feature logic")
			break
		}
	}

	if len(hints) == 0 {
		hints = append(hints, "Review existing codebase for reusable components")
		hints = append(hints, "Consider incremental rollout with feature flags")
	}

	return hints
}

func (e *ProposalEngine) buildEvidence(gap FeatureGap, score ScoredFeature) map[string]any {
	return map[string]any{
		"gap_type":                  string(gap.GapType),
		"market_prevalence":         score.StrategicFit,
		"market_importance":         score.ImpactScore,
		"implementation_complexity": score.ComplexityScore,
		"confidence_score":          score.Confidence,
		"reasoning":                 score.Reasoning,
	}
}

func generateProposalID(projectID, featureName string) string {
	return projectID + "_proposal_" + strings.ReplaceAll(featureName, "_", "-")
}
