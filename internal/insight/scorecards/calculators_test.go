package scorecards

import (
	"testing"
	"time"
)

func TestCalculateSuccessRate(t *testing.T) {
	tests := []struct {
		completed int
		failed    int
		expected  float64
	}{
		{10, 0, 1.0},
		{8, 2, 0.8},
		{0, 10, 0.0},
		{0, 0, 0.0},
		{5, 5, 0.5},
	}

	for _, tt := range tests {
		result := CalculateSuccessRate(tt.completed, tt.failed)
		if result != tt.expected {
			t.Errorf("CalculateSuccessRate(%d, %d) = %f, want %f", tt.completed, tt.failed, result, tt.expected)
		}
	}
}

func TestCalculateFailureRate(t *testing.T) {
	tests := []struct {
		failed   int
		total    int
		expected float64
	}{
		{0, 10, 0.0},
		{5, 10, 0.5},
		{10, 10, 1.0},
		{0, 0, 0.0},
	}

	for _, tt := range tests {
		result := CalculateFailureRate(tt.failed, tt.total)
		if result != tt.expected {
			t.Errorf("CalculateFailureRate(%d, %d) = %f, want %f", tt.failed, tt.total, result, tt.expected)
		}
	}
}

func TestCalculateReviewPassRate(t *testing.T) {
	tests := []struct {
		passed   int
		failed   int
		expected float64
	}{
		{10, 0, 1.0},
		{7, 3, 0.7},
		{0, 10, 0.0},
		{0, 0, 0.0},
	}

	for _, tt := range tests {
		result := CalculateReviewPassRate(tt.passed, tt.failed)
		if result != tt.expected {
			t.Errorf("CalculateReviewPassRate(%d, %d) = %f, want %f", tt.passed, tt.failed, result, tt.expected)
		}
	}
}

func TestCalculateReworkRate(t *testing.T) {
	tests := []struct {
		retryCycles int
		totalRuns   int
		expected    float64
	}{
		{0, 10, 0.0},
		{3, 10, 0.3},
		{5, 5, 1.0},
		{0, 0, 0.0},
	}

	for _, tt := range tests {
		result := CalculateReworkRate(tt.retryCycles, tt.totalRuns)
		if result != tt.expected {
			t.Errorf("CalculateReworkRate(%d, %d) = %f, want %f", tt.retryCycles, tt.totalRuns, result, tt.expected)
		}
	}
}

func TestCalculateRegressionRate(t *testing.T) {
	tests := []struct {
		regressions int
		successes   int
		expected    float64
	}{
		{0, 10, 0.0},
		{2, 10, 0.2},
		{5, 5, 1.0},
		{0, 0, 0.0},
	}

	for _, tt := range tests {
		result := CalculateRegressionRate(tt.regressions, tt.successes)
		if result != tt.expected {
			t.Errorf("CalculateRegressionRate(%d, %d) = %f, want %f", tt.regressions, tt.successes, result, tt.expected)
		}
	}
}

func TestCalculateConfidenceScore(t *testing.T) {
	config := DefaultScorecardConfig()

	tests := []struct {
		totalRuns   int
		minExpected float64
		maxExpected float64
	}{
		{1, 0.2, 0.4},
		{5, 0.4, 0.7},
		{20, 0.7, 0.95},
		{50, 0.85, 0.95},
	}

	for _, tt := range tests {
		result := CalculateConfidenceScore(tt.totalRuns, config)
		if result < tt.minExpected || result > tt.maxExpected {
			t.Errorf("CalculateConfidenceScore(%d) = %f, want between %f and %f", tt.totalRuns, result, tt.minExpected, tt.maxExpected)
		}
	}
}

func TestCalculateTrend(t *testing.T) {
	threshold := 0.05

	tests := []struct {
		name     string
		current  *AgentScorecard
		previous *AgentScorecard
		expected Trend
	}{
		{
			name:     "no previous data",
			current:  &AgentScorecard{SuccessRate: 0.9, TotalRuns: 10},
			previous: nil,
			expected: TrendStable,
		},
		{
			name:     "previous has no runs",
			current:  &AgentScorecard{SuccessRate: 0.9, TotalRuns: 10},
			previous: &AgentScorecard{SuccessRate: 0.5, TotalRuns: 0},
			expected: TrendStable,
		},
		{
			name:     "improving success rate",
			current:  &AgentScorecard{SuccessRate: 0.95, FailureRate: 0.05, ReviewPassRate: 0.9, TotalRuns: 20},
			previous: &AgentScorecard{SuccessRate: 0.80, FailureRate: 0.20, ReviewPassRate: 0.8, TotalRuns: 15},
			expected: TrendImproving,
		},
		{
			name:     "degrading success rate",
			current:  &AgentScorecard{SuccessRate: 0.70, FailureRate: 0.30, ReviewPassRate: 0.6, TotalRuns: 20},
			previous: &AgentScorecard{SuccessRate: 0.90, FailureRate: 0.10, ReviewPassRate: 0.9, TotalRuns: 15},
			expected: TrendDegrading,
		},
		{
			name:     "stable metrics",
			current:  &AgentScorecard{SuccessRate: 0.85, FailureRate: 0.15, ReviewPassRate: 0.85, TotalRuns: 20},
			previous: &AgentScorecard{SuccessRate: 0.83, FailureRate: 0.17, ReviewPassRate: 0.84, TotalRuns: 15},
			expected: TrendStable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateTrend(tt.current, tt.previous, threshold)
			if result != tt.expected {
				t.Errorf("CalculateTrend() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestCalculateWorkflowTrend(t *testing.T) {
	threshold := 0.05

	tests := []struct {
		name     string
		current  *WorkflowScorecard
		previous *WorkflowScorecard
		expected Trend
	}{
		{
			name:     "no previous data",
			current:  &WorkflowScorecard{CompletionRate: 0.9, TotalRuns: 10},
			previous: nil,
			expected: TrendStable,
		},
		{
			name:     "improving completion rate",
			current:  &WorkflowScorecard{CompletionRate: 0.95, FailureRate: 0.05, ReworkRate: 0.1, TotalRuns: 20},
			previous: &WorkflowScorecard{CompletionRate: 0.80, FailureRate: 0.20, ReworkRate: 0.3, TotalRuns: 15},
			expected: TrendImproving,
		},
		{
			name:     "degrading completion rate",
			current:  &WorkflowScorecard{CompletionRate: 0.60, FailureRate: 0.40, ReworkRate: 0.5, TotalRuns: 20},
			previous: &WorkflowScorecard{CompletionRate: 0.90, FailureRate: 0.10, ReworkRate: 0.1, TotalRuns: 15},
			expected: TrendDegrading,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateWorkflowTrend(tt.current, tt.previous, threshold)
			if result != tt.expected {
				t.Errorf("CalculateWorkflowTrend() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestAggregateAgentEvents(t *testing.T) {
	now := time.Now().UTC()

	events := []EventForScorecard{
		{ID: "1", Type: "agent.spawned", AgentName: "agent-a", Timestamp: now.Add(-1 * time.Hour), WorkflowID: "wf-1"},
		{ID: "1", Type: "agent.completed", AgentName: "agent-a", Timestamp: now.Add(-30 * time.Minute), WorkflowID: "wf-1"},
		{ID: "2", Type: "agent.spawned", AgentName: "agent-b", Timestamp: now.Add(-2 * time.Hour), WorkflowID: "wf-2"},
		{ID: "2", Type: "agent.failed", AgentName: "agent-b", Timestamp: now.Add(-90 * time.Minute), WorkflowID: "wf-2"},
		{ID: "3", Type: "review.passed", AgentName: "agent-a", Timestamp: now.Add(-20 * time.Minute)},
		{ID: "4", Type: "review.failed", AgentName: "agent-b", Timestamp: now.Add(-60 * time.Minute)},
	}

	stats := AggregateAgentEvents(events)

	if len(stats) < 2 {
		t.Errorf("Expected at least 2 agent stats, got %d", len(stats))
	}

	if stats["agent-a"] == nil {
		t.Error("Expected stats for agent-a")
	} else {
		if stats["agent-a"].Completed != 1 {
			t.Errorf("Expected agent-a Completed = 1, got %d", stats["agent-a"].Completed)
		}
		if stats["agent-a"].ReviewPassed != 1 {
			t.Errorf("Expected agent-a ReviewPassed = 1, got %d", stats["agent-a"].ReviewPassed)
		}
	}

	if stats["agent-b"] == nil {
		t.Error("Expected stats for agent-b")
	} else {
		if stats["agent-b"].Failed != 1 {
			t.Errorf("Expected agent-b Failed = 1, got %d", stats["agent-b"].Failed)
		}
		if stats["agent-b"].ReviewFailed != 1 {
			t.Errorf("Expected agent-b ReviewFailed = 1, got %d", stats["agent-b"].ReviewFailed)
		}
	}
}

func TestComputeAgentScorecard(t *testing.T) {
	now := time.Now().UTC()
	windowStart := now.Add(-7 * 24 * time.Hour)
	windowEnd := now
	config := DefaultScorecardConfig()

	events := []EventForScorecard{
		{ID: "1", Type: "agent.spawned", AgentName: "test-agent", Timestamp: now.Add(-1 * time.Hour)},
		{ID: "1", Type: "agent.completed", AgentName: "test-agent", Timestamp: now.Add(-30 * time.Minute)},
		{ID: "2", Type: "agent.spawned", AgentName: "test-agent", Timestamp: now.Add(-2 * time.Hour)},
		{ID: "2", Type: "agent.completed", AgentName: "test-agent", Timestamp: now.Add(-90 * time.Minute)},
		{ID: "3", Type: "agent.spawned", AgentName: "test-agent", Timestamp: now.Add(-3 * time.Hour)},
		{ID: "3", Type: "agent.failed", AgentName: "test-agent", Timestamp: now.Add(-150 * time.Minute)},
		{ID: "4", Type: "review.passed", AgentName: "test-agent", Timestamp: now.Add(-20 * time.Minute)},
		{ID: "5", Type: "review.failed", AgentName: "test-agent", Timestamp: now.Add(-60 * time.Minute)},
	}

	card := ComputeAgentScorecard("test-agent", Window7d, windowStart, windowEnd, events, config)

	if card.AgentName != "test-agent" {
		t.Errorf("Expected AgentName = test-agent, got %s", card.AgentName)
	}

	if card.TotalRuns < 3 {
		t.Errorf("Expected TotalRuns >= 3, got %d", card.TotalRuns)
	}

	if card.SuccessRate < 0.5 || card.SuccessRate > 0.8 {
		t.Errorf("Expected SuccessRate between 0.5 and 0.8, got %f", card.SuccessRate)
	}

	if card.ReviewPassRate < 0.4 || card.ReviewPassRate > 0.6 {
		t.Errorf("Expected ReviewPassRate around 0.5, got %f", card.ReviewPassRate)
	}
}

func TestComputeWorkflowScorecard(t *testing.T) {
	now := time.Now().UTC()
	windowStart := now.Add(-7 * 24 * time.Hour)
	windowEnd := now
	config := DefaultScorecardConfig()

	events := []EventForScorecard{
		{Type: "workflow.started", WorkflowID: "spec", Timestamp: now.Add(-1 * time.Hour)},
		{Type: "workflow.completed", WorkflowID: "spec", Timestamp: now.Add(-30 * time.Minute)},
		{Type: "workflow.started", WorkflowID: "spec", Timestamp: now.Add(-2 * time.Hour)},
		{Type: "workflow.completed", WorkflowID: "spec", Timestamp: now.Add(-90 * time.Minute)},
		{Type: "workflow.started", WorkflowID: "spec", Timestamp: now.Add(-3 * time.Hour)},
		{Type: "workflow.failed", WorkflowID: "spec", Timestamp: now.Add(-150 * time.Minute)},
	}

	card := ComputeWorkflowScorecard("spec", Window7d, windowStart, windowEnd, events, config)

	if card.WorkflowType != "spec" {
		t.Errorf("Expected WorkflowType = spec, got %s", card.WorkflowType)
	}

	if card.TotalRuns < 3 {
		t.Errorf("Expected TotalRuns >= 3, got %d", card.TotalRuns)
	}

	if card.CompletionRate < 0.5 || card.CompletionRate > 0.8 {
		t.Errorf("Expected CompletionRate between 0.5 and 0.8, got %f", card.CompletionRate)
	}
}
