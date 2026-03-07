package routing

import (
	"time"

	"github.com/google/uuid"
)

type RecommendationType string

const (
	RecommendationBestAgent    RecommendationType = "best_agent"
	RecommendationDeprioritize RecommendationType = "deprioritize"
	RecommendationFallback     RecommendationType = "fallback_needed"
	RecommendationInstability  RecommendationType = "instability"
)

type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

type ConfidenceLevel string

const (
	ConfidenceLow    ConfidenceLevel = "low"
	ConfidenceMedium ConfidenceLevel = "medium"
	ConfidenceHigh   ConfidenceLevel = "high"
)

type RoutingRecommendation struct {
	ID                 string             `json:"id"`
	WorkflowType       string             `json:"workflow_type"`
	RecommendationType RecommendationType `json:"recommendation_type"`
	RecommendedAgent   string             `json:"recommended_agent,omitempty"`
	CurrentAgent       string             `json:"current_agent,omitempty"`
	Confidence         float64            `json:"confidence"`
	RiskLevel          RiskLevel          `json:"risk_level"`
	Reason             string             `json:"reason"`
	Evidence           map[string]any     `json:"evidence"`
	Observations       int                `json:"observations"`
	CreatedAt          time.Time          `json:"created_at"`
}

type RoutingConfig struct {
	MinObservations         int     `json:"min_observations"`
	HighConfidenceThresh    float64 `json:"high_confidence_threshold"`
	MediumConfidenceThresh  float64 `json:"medium_confidence_threshold"`
	DeprioritizeThreshold   float64 `json:"deprioritize_threshold"`
	FallbackFailureThresh   float64 `json:"fallback_failure_threshold"`
	InstabilityReworkThresh float64 `json:"instability_rework_threshold"`
	DedupWindowHours        int     `json:"dedup_window_hours"`
}

func DefaultRoutingConfig() RoutingConfig {
	return RoutingConfig{
		MinObservations:         5,
		HighConfidenceThresh:    0.75,
		MediumConfidenceThresh:  0.45,
		DeprioritizeThreshold:   0.50,
		FallbackFailureThresh:   0.30,
		InstabilityReworkThresh: 0.35,
		DedupWindowHours:        24,
	}
}

func NewRoutingRecommendation(
	workflowType string,
	recType RecommendationType,
	recommendedAgent string,
	currentAgent string,
) RoutingRecommendation {
	return RoutingRecommendation{
		ID:                 uuid.New().String(),
		WorkflowType:       workflowType,
		RecommendationType: recType,
		RecommendedAgent:   recommendedAgent,
		CurrentAgent:       currentAgent,
		RiskLevel:          RiskMedium,
		Evidence:           make(map[string]any),
		CreatedAt:          time.Now().UTC(),
	}
}

func (r *RoutingRecommendation) CalculateConfidence(config RoutingConfig) {
	obsFactor := 1.0
	switch {
	case r.Observations < config.MinObservations:
		obsFactor = 0.3
	case r.Observations < 10:
		obsFactor = 0.5
	case r.Observations < 30:
		obsFactor = 0.7
	case r.Observations < 50:
		obsFactor = 0.8
	default:
		obsFactor = 0.9
	}

	baseConfidence := 0.6 * obsFactor

	if trend, ok := r.Evidence["trend"].(string); ok {
		switch trend {
		case "improving":
			baseConfidence += 0.1
		case "degrading":
			baseConfidence -= 0.1
		}
	}

	if consistency, ok := r.Evidence["metric_consistency"].(float64); ok {
		baseConfidence += (consistency - 0.5) * 0.2
	}

	if baseConfidence > 0.95 {
		baseConfidence = 0.95
	}
	if baseConfidence < 0.25 {
		baseConfidence = 0.25
	}

	r.Confidence = baseConfidence
}

func (r *RoutingRecommendation) DetermineRiskLevel() {
	switch r.RecommendationType {
	case RecommendationBestAgent:
		if r.Confidence >= 0.8 {
			r.RiskLevel = RiskLow
		} else {
			r.RiskLevel = RiskMedium
		}
	case RecommendationDeprioritize:
		if successRate, ok := r.Evidence["agent_success_rate"].(float64); ok {
			if successRate < 0.3 {
				r.RiskLevel = RiskHigh
			} else {
				r.RiskLevel = RiskMedium
			}
		} else {
			r.RiskLevel = RiskMedium
		}
	case RecommendationFallback:
		if failRate, ok := r.Evidence["workflow_failure_rate"].(float64); ok {
			if failRate >= 0.5 {
				r.RiskLevel = RiskCritical
			} else if failRate >= 0.35 {
				r.RiskLevel = RiskHigh
			} else {
				r.RiskLevel = RiskMedium
			}
		} else {
			r.RiskLevel = RiskMedium
		}
	case RecommendationInstability:
		if reworkRate, ok := r.Evidence["rework_rate"].(float64); ok {
			if reworkRate >= 0.5 {
				r.RiskLevel = RiskCritical
			} else if reworkRate >= 0.35 {
				r.RiskLevel = RiskHigh
			} else {
				r.RiskLevel = RiskMedium
			}
		} else {
			r.RiskLevel = RiskMedium
		}
	default:
		r.RiskLevel = RiskMedium
	}
}

func (r *RoutingRecommendation) GetConfidenceLevel() ConfidenceLevel {
	switch {
	case r.Confidence >= 0.75:
		return ConfidenceHigh
	case r.Confidence >= 0.45:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

type AgentMetrics struct {
	AgentName      string  `json:"agent_name"`
	TotalRuns      int     `json:"total_runs"`
	SuccessRate    float64 `json:"success_rate"`
	FailureRate    float64 `json:"failure_rate"`
	ReviewPassRate float64 `json:"review_pass_rate"`
	ReworkRate     float64 `json:"rework_rate"`
	Trend          string  `json:"trend"`
}

type WorkflowMetrics struct {
	WorkflowType        string  `json:"workflow_type"`
	TotalRuns           int     `json:"total_runs"`
	CompletionRate      float64 `json:"completion_rate"`
	FailureRate         float64 `json:"failure_rate"`
	ReviewRejectionRate float64 `json:"review_rejection_rate"`
	ReworkRate          float64 `json:"rework_rate"`
	Trend               string  `json:"trend"`
	AgentCount          int     `json:"agent_count"`
}
