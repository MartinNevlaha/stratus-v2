package routing

import (
	"testing"
	"time"
)

func TestBestAgentRecommendation(t *testing.T) {
	analyzer := &BestAgentAnalyzer{}
	config := DefaultRoutingConfig()

	agentMetrics := []AgentMetrics{
		{AgentName: "mobile-dev-specialist", TotalRuns: 50, SuccessRate: 0.84, Trend: "stable"},
		{AgentName: "backend-engineer", TotalRuns: 45, SuccessRate: 0.51, Trend: "stable"},
		{AgentName: "generalist", TotalRuns: 30, SuccessRate: 0.62, Trend: "stable"},
	}

	workflowMetrics := []WorkflowMetrics{
		{WorkflowType: "mobile-implementation", TotalRuns: 100, AgentCount: 3},
	}

	recommendations := analyzer.Analyze(agentMetrics, workflowMetrics, config)

	if len(recommendations) == 0 {
		t.Fatal("Expected at least one recommendation, got none")
	}

	rec := recommendations[0]
	if rec.RecommendationType != RecommendationBestAgent {
		t.Errorf("Expected recommendation type %s, got %s", RecommendationBestAgent, rec.RecommendationType)
	}
	if rec.RecommendedAgent != "mobile-dev-specialist" {
		t.Errorf("Expected recommended agent 'mobile-dev-specialist', got '%s'", rec.RecommendedAgent)
	}
	if rec.WorkflowType != "mobile-implementation" {
		t.Errorf("Expected workflow type 'mobile-implementation', got '%s'", rec.WorkflowType)
	}
	if rec.Confidence <= 0 {
		t.Errorf("Expected positive confidence, got %f", rec.Confidence)
	}
}

func TestAgentDeprioritizationRecommendation(t *testing.T) {
	analyzer := &DeprioritizationAnalyzer{}
	config := DefaultRoutingConfig()

	agentMetrics := []AgentMetrics{
		{AgentName: "failing-agent", TotalRuns: 50, SuccessRate: 0.43, FailureRate: 0.57, Trend: "degrading"},
		{AgentName: "stable-agent", TotalRuns: 40, SuccessRate: 0.78, Trend: "stable"},
	}

	workflowMetrics := []WorkflowMetrics{
		{WorkflowType: "general", TotalRuns: 90, AgentCount: 2},
	}

	recommendations := analyzer.Analyze(agentMetrics, workflowMetrics, config)

	if len(recommendations) == 0 {
		t.Fatal("Expected at least one deprioritization recommendation, got none")
	}

	rec := recommendations[0]
	if rec.RecommendationType != RecommendationDeprioritize {
		t.Errorf("Expected recommendation type %s, got %s", RecommendationDeprioritize, rec.RecommendationType)
	}
	if rec.CurrentAgent != "failing-agent" {
		t.Errorf("Expected current agent 'failing-agent', got '%s'", rec.CurrentAgent)
	}
	if rec.RiskLevel != RiskHigh && rec.RiskLevel != RiskMedium {
		t.Errorf("Expected risk level high or medium, got %s", rec.RiskLevel)
	}
}

func TestFallbackAgentRecommendation(t *testing.T) {
	analyzer := &FallbackAnalyzer{}
	config := DefaultRoutingConfig()

	agentMetrics := []AgentMetrics{
		{AgentName: "single-agent", TotalRuns: 50, SuccessRate: 0.55},
	}

	workflowMetrics := []WorkflowMetrics{
		{WorkflowType: "unstable-workflow", TotalRuns: 50, FailureRate: 0.41, CompletionRate: 0.59, AgentCount: 1},
	}

	recommendations := analyzer.Analyze(agentMetrics, workflowMetrics, config)

	if len(recommendations) == 0 {
		t.Fatal("Expected at least one fallback recommendation, got none")
	}

	rec := recommendations[0]
	if rec.RecommendationType != RecommendationFallback {
		t.Errorf("Expected recommendation type %s, got %s", RecommendationFallback, rec.RecommendationType)
	}
	if rec.WorkflowType != "unstable-workflow" {
		t.Errorf("Expected workflow type 'unstable-workflow', got '%s'", rec.WorkflowType)
	}
	if rec.Evidence["workflow_failure_rate"] == nil {
		t.Error("Expected workflow_failure_rate in evidence")
	}
}

func TestWorkflowInstabilityRecommendation(t *testing.T) {
	analyzer := &InstabilityAnalyzer{}
	config := DefaultRoutingConfig()

	workflowMetrics := []WorkflowMetrics{
		{
			WorkflowType:        "buggy-workflow",
			TotalRuns:           50,
			ReworkRate:          0.45,
			ReviewRejectionRate: 0.35,
			CompletionRate:      0.55,
		},
	}

	recommendations := analyzer.Analyze([]AgentMetrics{}, workflowMetrics, config)

	if len(recommendations) == 0 {
		t.Fatal("Expected at least one instability recommendation, got none")
	}

	rec := recommendations[0]
	if rec.RecommendationType != RecommendationInstability {
		t.Errorf("Expected recommendation type %s, got %s", RecommendationInstability, rec.RecommendationType)
	}
	if rec.WorkflowType != "buggy-workflow" {
		t.Errorf("Expected workflow type 'buggy-workflow', got '%s'", rec.WorkflowType)
	}
	if rec.Evidence["rework_rate"] == nil {
		t.Error("Expected rework_rate in evidence")
	}
}

func TestRoutingConfidenceScoring(t *testing.T) {
	config := DefaultRoutingConfig()

	tests := []struct {
		name          string
		observations  int
		trend         string
		minConfidence float64
		maxConfidence float64
	}{
		{
			name:          "Low observations",
			observations:  3,
			trend:         "stable",
			minConfidence: 0.20,
			maxConfidence: 0.45,
		},
		{
			name:          "Medium observations",
			observations:  15,
			trend:         "improving",
			minConfidence: 0.45,
			maxConfidence: 0.75,
		},
		{
			name:          "High observations",
			observations:  50,
			trend:         "stable",
			minConfidence: 0.50,
			maxConfidence: 0.95,
		},
		{
			name:          "Degrading trend reduces confidence",
			observations:  30,
			trend:         "degrading",
			minConfidence: 0.30,
			maxConfidence: 0.70,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := NewRoutingRecommendation("test-workflow", RecommendationBestAgent, "test-agent", "")
			rec.Observations = tt.observations
			rec.Evidence = map[string]any{
				"trend": tt.trend,
			}

			rec.CalculateConfidence(config)

			if rec.Confidence < tt.minConfidence {
				t.Errorf("Confidence %f is below minimum expected %f", rec.Confidence, tt.minConfidence)
			}
			if rec.Confidence > tt.maxConfidence {
				t.Errorf("Confidence %f is above maximum expected %f", rec.Confidence, tt.maxConfidence)
			}
		})
	}
}

func TestRiskLevelDetermination(t *testing.T) {
	tests := []struct {
		name         string
		recType      RecommendationType
		evidence     map[string]any
		expectedRisk RiskLevel
	}{
		{
			name:         "Best agent high confidence = low risk",
			recType:      RecommendationBestAgent,
			evidence:     map[string]any{},
			expectedRisk: RiskLow,
		},
		{
			name:         "Deprioritize very low success = high risk",
			recType:      RecommendationDeprioritize,
			evidence:     map[string]any{"agent_success_rate": 0.25},
			expectedRisk: RiskHigh,
		},
		{
			name:         "Fallback critical failure = critical risk",
			recType:      RecommendationFallback,
			evidence:     map[string]any{"workflow_failure_rate": 0.55},
			expectedRisk: RiskCritical,
		},
		{
			name:         "Instability high rework = critical risk",
			recType:      RecommendationInstability,
			evidence:     map[string]any{"rework_rate": 0.55},
			expectedRisk: RiskCritical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := RoutingRecommendation{
				RecommendationType: tt.recType,
				Confidence:         0.8,
				Evidence:           tt.evidence,
			}
			rec.DetermineRiskLevel()

			if rec.RiskLevel != tt.expectedRisk {
				t.Errorf("Expected risk level %s, got %s", tt.expectedRisk, rec.RiskLevel)
			}
		})
	}
}

func TestDeduplicationLogic(t *testing.T) {
	now := time.Now().UTC()

	existing := &RoutingRecommendation{
		ID:                 "existing-123",
		WorkflowType:       "mobile-implementation",
		RecommendationType: RecommendationBestAgent,
		RecommendedAgent:   "mobile-dev-specialist",
		CreatedAt:          now.Add(-2 * time.Hour),
	}

	newRec := RoutingRecommendation{
		ID:                 "new-456",
		WorkflowType:       "mobile-implementation",
		RecommendationType: RecommendationBestAgent,
		RecommendedAgent:   "mobile-dev-specialist",
		CreatedAt:          now,
	}

	if newRec.WorkflowType != existing.WorkflowType {
		t.Error("Workflow types should match for deduplication")
	}
	if newRec.RecommendationType != existing.RecommendationType {
		t.Error("Recommendation types should match for deduplication")
	}
	if newRec.RecommendedAgent != existing.RecommendedAgent {
		t.Error("Recommended agents should match for deduplication")
	}

	timeDiff := newRec.CreatedAt.Sub(existing.CreatedAt)
	if timeDiff < 0 || timeDiff > 24*time.Hour {
		t.Errorf("Time difference %v should be within 24 hours for deduplication", timeDiff)
	}
}

func TestNoRecommendationsWithInsufficientData(t *testing.T) {
	analyzer := &BestAgentAnalyzer{}
	config := DefaultRoutingConfig()

	agentMetrics := []AgentMetrics{
		{AgentName: "agent1", TotalRuns: 2, SuccessRate: 0.90},
		{AgentName: "agent2", TotalRuns: 1, SuccessRate: 0.50},
	}

	workflowMetrics := []WorkflowMetrics{
		{WorkflowType: "test-workflow", TotalRuns: 3, AgentCount: 2},
	}

	recommendations := analyzer.Analyze(agentMetrics, workflowMetrics, config)

	if len(recommendations) != 0 {
		t.Errorf("Expected no recommendations with insufficient data, got %d", len(recommendations))
	}
}

func TestGetConfidenceLevel(t *testing.T) {
	tests := []struct {
		confidence float64
		expected   ConfidenceLevel
	}{
		{0.80, ConfidenceHigh},
		{0.75, ConfidenceHigh},
		{0.60, ConfidenceMedium},
		{0.45, ConfidenceMedium},
		{0.40, ConfidenceLow},
		{0.25, ConfidenceLow},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			rec := RoutingRecommendation{Confidence: tt.confidence}
			level := rec.GetConfidenceLevel()
			if level != tt.expected {
				t.Errorf("For confidence %f, expected %s, got %s", tt.confidence, tt.expected, level)
			}
		})
	}
}
