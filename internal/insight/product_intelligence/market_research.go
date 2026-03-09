package product_intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

type MarketResearchEngine struct {
	config MarketResearchConfig
	store  Store
	llm    llm.Client
}

type MarketResearchConfig struct {
	CacheTTLHours int     `json:"cache_ttl_hours"`
	MinConfidence float64 `json:"min_confidence"`
}

func DefaultMarketResearchConfig() MarketResearchConfig {
	return MarketResearchConfig{
		CacheTTLHours: 168,
		MinConfidence: 0.5,
	}
}

func NewMarketResearchEngine(config MarketResearchConfig, store Store, llmClient llm.Client) *MarketResearchEngine {
	return &MarketResearchEngine{
		config: config,
		store:  store,
		llm:    llmClient,
	}
}

func (e *MarketResearchEngine) Research(ctx context.Context, domain string) ([]MarketFeature, error) {
	cached, err := e.store.GetMarketFeaturesByDomain(ctx, domain)
	if err == nil && len(cached) > 0 && e.isCacheValid(cached) {
		slog.Info("product_intelligence: using cached market features", "domain", domain, "count", len(cached))
		return cached, nil
	}

	if e.llm != nil {
		features, err := e.researchWithLLM(ctx, domain)
		if err == nil && len(features) > 0 {
			e.saveFeatures(ctx, features)
			return features, nil
		}
		if err != nil {
			slog.Warn("market_research: LLM research failed, falling back to knowledge base", "error", err)
		}
	}

	features := e.getDomainKnowledge(domain)
	e.saveFeatures(ctx, features)

	slog.Info("product_intelligence: market research complete", "domain", domain, "features", len(features))
	return features, nil
}

type llmMarketFeature struct {
	FeatureName string  `json:"feature_name"`
	FeatureType string  `json:"feature_type"`
	Prevalence  float64 `json:"prevalence"`
	Importance  float64 `json:"importance"`
}

func (e *MarketResearchEngine) researchWithLLM(ctx context.Context, domain string) ([]MarketFeature, error) {
	prompt := fmt.Sprintf(`Research standard features for the "%s" software domain.

List all features that are commonly found in products in this domain, along with their market prevalence and importance.

Respond with a JSON array:
[
  {
    "feature_name": "descriptive_snake_case_name",
    "feature_type": "one of: capability, module, api, ui, integration, auth, analytics, reporting",
    "prevalence": 0.0-1.0 (how common this feature is in the market),
    "importance": 0.0-1.0 (how important this feature is for product success)
  }
]

Only respond with the JSON array, no additional text.`, domain)

	resp, err := e.llm.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: "You are an expert product researcher. Analyze market standards with high accuracy. Always respond with valid JSON.",
		Messages:     []llm.Message{llm.UserMessage(prompt)},
		MaxTokens:    3000,
		Temperature:  0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}

	var llmFeatures []llmMarketFeature
	if err := llm.ParseJSONResponse(resp.Content, &llmFeatures); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	features := make([]MarketFeature, 0, len(llmFeatures))

	for _, lf := range llmFeatures {
		features = append(features, MarketFeature{
			ID:           generateMarketFeatureID(domain, lf.FeatureName),
			Domain:       domain,
			FeatureName:  lf.FeatureName,
			FeatureType:  FeatureType(lf.FeatureType),
			Prevalence:   lf.Prevalence,
			Importance:   lf.Importance,
			Sources:      []string{"llm_analysis"},
			DiscoveredAt: now,
		})
	}

	return features, nil
}

func (e *MarketResearchEngine) saveFeatures(ctx context.Context, features []MarketFeature) {
	for _, f := range features {
		if err := e.store.SaveMarketFeature(ctx, f); err != nil {
			slog.Warn("market_research: failed to save feature", "feature", f.FeatureName, "error", err)
		}
	}
}

func (e *MarketResearchEngine) RefreshResearch(ctx context.Context, domain string) error {
	_, err := e.Research(ctx, domain)
	return err
}

func (e *MarketResearchEngine) isCacheValid(features []MarketFeature) bool {
	if e.config.CacheTTLHours <= 0 || len(features) == 0 {
		return false
	}

	cutoff := time.Now().Add(-time.Duration(e.config.CacheTTLHours) * time.Hour)

	for _, f := range features {
		t, err := time.Parse(time.RFC3339Nano, f.DiscoveredAt)
		if err != nil {
			t, _ = time.Parse(time.RFC3339, f.DiscoveredAt)
		}
		if t.Before(cutoff) {
			return false
		}
	}

	return true
}

func (e *MarketResearchEngine) getDomainKnowledge(domain string) []MarketFeature {
	knowledge := map[string][]MarketFeature{
		"audit_management": {
			{FeatureName: "audit_checklists", FeatureType: FeatureTypeCapability, Prevalence: 0.95, Importance: 0.9, Sources: []string{"market_analysis"}},
			{FeatureName: "compliance_dashboards", FeatureType: FeatureTypeUI, Prevalence: 0.85, Importance: 0.85, Sources: []string{"market_analysis"}},
			{FeatureName: "automated_scheduling", FeatureType: FeatureTypeCapability, Prevalence: 0.75, Importance: 0.8, Sources: []string{"market_analysis"}},
			{FeatureName: "mobile_capture", FeatureType: FeatureTypeCapability, Prevalence: 0.7, Importance: 0.75, Sources: []string{"market_analysis"}},
			{FeatureName: "ai_risk_prediction", FeatureType: FeatureTypeCapability, Prevalence: 0.45, Importance: 0.9, Sources: []string{"market_analysis"}},
			{FeatureName: "workflow_automation", FeatureType: FeatureTypeCapability, Prevalence: 0.8, Importance: 0.85, Sources: []string{"market_analysis"}},
			{FeatureName: "evidence_attachments", FeatureType: FeatureTypeCapability, Prevalence: 0.9, Importance: 0.8, Sources: []string{"market_analysis"}},
			{FeatureName: "audit_trail_logging", FeatureType: FeatureTypeCapability, Prevalence: 0.95, Importance: 0.9, Sources: []string{"market_analysis"}},
			{FeatureName: "finding_management", FeatureType: FeatureTypeCapability, Prevalence: 0.9, Importance: 0.85, Sources: []string{"market_analysis"}},
		},
		"ecommerce": {
			{FeatureName: "product_catalog", FeatureType: FeatureTypeCapability, Prevalence: 0.98, Importance: 0.95, Sources: []string{"market_analysis"}},
			{FeatureName: "shopping_cart", FeatureType: FeatureTypeCapability, Prevalence: 0.98, Importance: 0.95, Sources: []string{"market_analysis"}},
			{FeatureName: "checkout_flow", FeatureType: FeatureTypeCapability, Prevalence: 0.98, Importance: 0.95, Sources: []string{"market_analysis"}},
			{FeatureName: "inventory_management", FeatureType: FeatureTypeCapability, Prevalence: 0.85, Importance: 0.9, Sources: []string{"market_analysis"}},
			{FeatureName: "order_tracking", FeatureType: FeatureTypeCapability, Prevalence: 0.9, Importance: 0.85, Sources: []string{"market_analysis"}},
			{FeatureName: "recommendation_engine", FeatureType: FeatureTypeCapability, Prevalence: 0.6, Importance: 0.85, Sources: []string{"market_analysis"}},
			{FeatureName: "loyalty_program", FeatureType: FeatureTypeCapability, Prevalence: 0.5, Importance: 0.7, Sources: []string{"market_analysis"}},
			{FeatureName: "ai_search", FeatureType: FeatureTypeCapability, Prevalence: 0.45, Importance: 0.8, Sources: []string{"market_analysis"}},
		},
		"crm": {
			{FeatureName: "contact_management", FeatureType: FeatureTypeCapability, Prevalence: 0.98, Importance: 0.95, Sources: []string{"market_analysis"}},
			{FeatureName: "lead_tracking", FeatureType: FeatureTypeCapability, Prevalence: 0.95, Importance: 0.9, Sources: []string{"market_analysis"}},
			{FeatureName: "sales_pipeline", FeatureType: FeatureTypeCapability, Prevalence: 0.9, Importance: 0.9, Sources: []string{"market_analysis"}},
			{FeatureName: "email_integration", FeatureType: FeatureTypeIntegration, Prevalence: 0.85, Importance: 0.85, Sources: []string{"market_analysis"}},
			{FeatureName: "reporting_dashboards", FeatureType: FeatureTypeReporting, Prevalence: 0.85, Importance: 0.8, Sources: []string{"market_analysis"}},
			{FeatureName: "ai_lead_scoring", FeatureType: FeatureTypeCapability, Prevalence: 0.45, Importance: 0.85, Sources: []string{"market_analysis"}},
			{FeatureName: "workflow_automation", FeatureType: FeatureTypeCapability, Prevalence: 0.7, Importance: 0.8, Sources: []string{"market_analysis"}},
		},
		"project_management": {
			{FeatureName: "task_management", FeatureType: FeatureTypeCapability, Prevalence: 0.98, Importance: 0.95, Sources: []string{"market_analysis"}},
			{FeatureName: "kanban_boards", FeatureType: FeatureTypeUI, Prevalence: 0.85, Importance: 0.85, Sources: []string{"market_analysis"}},
			{FeatureName: "time_tracking", FeatureType: FeatureTypeCapability, Prevalence: 0.8, Importance: 0.8, Sources: []string{"market_analysis"}},
			{FeatureName: "team_collaboration", FeatureType: FeatureTypeCapability, Prevalence: 0.9, Importance: 0.85, Sources: []string{"market_analysis"}},
			{FeatureName: "reporting_analytics", FeatureType: FeatureTypeReporting, Prevalence: 0.8, Importance: 0.75, Sources: []string{"market_analysis"}},
			{FeatureName: "custom_workflows", FeatureType: FeatureTypeCapability, Prevalence: 0.7, Importance: 0.8, Sources: []string{"market_analysis"}},
		},
		"dev_tools": {
			{FeatureName: "code_repository", FeatureType: FeatureTypeCapability, Prevalence: 0.98, Importance: 0.95, Sources: []string{"market_analysis"}},
			{FeatureName: "pull_requests", FeatureType: FeatureTypeCapability, Prevalence: 0.95, Importance: 0.9, Sources: []string{"market_analysis"}},
			{FeatureName: "code_review", FeatureType: FeatureTypeCapability, Prevalence: 0.9, Importance: 0.85, Sources: []string{"market_analysis"}},
			{FeatureName: "ci_cd", FeatureType: FeatureTypeCapability, Prevalence: 0.85, Importance: 0.9, Sources: []string{"market_analysis"}},
			{FeatureName: "issue_tracking", FeatureType: FeatureTypeCapability, Prevalence: 0.9, Importance: 0.85, Sources: []string{"market_analysis"}},
			{FeatureName: "ai_code_suggestions", FeatureType: FeatureTypeCapability, Prevalence: 0.5, Importance: 0.85, Sources: []string{"market_analysis"}},
		},
	}

	if features, ok := knowledge[domain]; ok {
		return features
	}

	return []MarketFeature{
		{FeatureName: "user_authentication", FeatureType: FeatureTypeAuth, Prevalence: 0.95, Importance: 0.9, Sources: []string{"market_analysis"}},
		{FeatureName: "dashboard", FeatureType: FeatureTypeUI, Prevalence: 0.85, Importance: 0.8, Sources: []string{"market_analysis"}},
		{FeatureName: "search", FeatureType: FeatureTypeCapability, Prevalence: 0.8, Importance: 0.75, Sources: []string{"market_analysis"}},
		{FeatureName: "notifications", FeatureType: FeatureTypeCapability, Prevalence: 0.75, Importance: 0.7, Sources: []string{"market_analysis"}},
		{FeatureName: "reporting", FeatureType: FeatureTypeReporting, Prevalence: 0.8, Importance: 0.8, Sources: []string{"market_analysis"}},
		{FeatureName: "api_access", FeatureType: FeatureTypeAPI, Prevalence: 0.7, Importance: 0.75, Sources: []string{"market_analysis"}},
		{FeatureName: "integrations", FeatureType: FeatureTypeIntegration, Prevalence: 0.65, Importance: 0.8, Sources: []string{"market_analysis"}},
	}
}

func generateMarketFeatureID(domain, featureName string) string {
	return domain + "_" + featureName
}
