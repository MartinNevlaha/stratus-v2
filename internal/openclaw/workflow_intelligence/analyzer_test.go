package workflow_intelligence

import (
	"context"
	"testing"
	"time"
)

func TestComputeWorkflowMetrics(t *testing.T) {
	now := time.Now()

	events := []EventForMetrics{
		{
			ID:        "1",
			Type:      "workflow.started",
			Timestamp: now.Add(-30 * time.Minute),
			Source:    "test",
			Payload:   map[string]any{"workflow_id": "wf-1", "workflow_type": "bug"},
		},
		{
			ID:        "2",
			Type:      "workflow.phase_transition",
			Timestamp: now.Add(-25 * time.Minute),
			Source:    "test",
			Payload:   map[string]any{"workflow_id": "wf-1", "from_phase": "analyze", "to_phase": "fix"},
		},
		{
			ID:        "3",
			Type:      "workflow.phase_transition",
			Timestamp: now.Add(-20 * time.Minute),
			Source:    "test",
			Payload:   map[string]any{"workflow_id": "wf-1", "from_phase": "fix", "to_phase": "review"},
		},
		{
			ID:        "4",
			Type:      "workflow.phase_transition",
			Timestamp: now.Add(-18 * time.Minute),
			Source:    "test",
			Payload:   map[string]any{"workflow_id": "wf-1", "from_phase": "review", "to_phase": "fix"},
		},
		{
			ID:        "5",
			Type:      "workflow.phase_transition",
			Timestamp: now.Add(-15 * time.Minute),
			Source:    "test",
			Payload:   map[string]any{"workflow_id": "wf-1", "from_phase": "fix", "to_phase": "review"},
		},
		{
			ID:        "6",
			Type:      "workflow.phase_transition",
			Timestamp: now.Add(-10 * time.Minute),
			Source:    "test",
			Payload:   map[string]any{"workflow_id": "wf-1", "from_phase": "review", "to_phase": "complete"},
		},
		{
			ID:        "7",
			Type:      "workflow.completed",
			Timestamp: now.Add(-5 * time.Minute),
			Source:    "test",
			Payload:   map[string]any{"workflow_id": "wf-1"},
		},
	}

	metrics := ComputeWorkflowMetrics(events)

	if len(metrics) == 0 {
		t.Fatal("Expected at least one workflow metric")
	}

	metric := metrics[0]

	if metric.WorkflowID != "wf-1" {
		t.Errorf("Expected workflow_id 'wf-1', got '%s'", metric.WorkflowID)
	}

	if metric.WorkflowType != "bug" {
		t.Errorf("Expected workflow_type 'bug', got '%s'", metric.WorkflowType)
	}

	if metric.RetryCount < 1 {
		t.Errorf("Expected retry_count >= 1 (fix->review->fix loop), got %d", metric.RetryCount)
	}

	if metric.Status != "completed" {
		t.Errorf("Expected status 'completed', got '%s'", metric.Status)
	}

	if metric.SuccessRate != 1.0 {
		t.Errorf("Expected success_rate 1.0, got %f", metric.SuccessRate)
	}
}

func TestDetectPhaseLoops(t *testing.T) {
	tests := []struct {
		name        string
		transitions []PhaseTransition
		expected    int
	}{
		{
			name: "no loops",
			transitions: []PhaseTransition{
				{From: "analyze", To: "fix"},
				{From: "fix", To: "review"},
				{From: "review", To: "complete"},
			},
			expected: 0,
		},
		{
			name: "one loop",
			transitions: []PhaseTransition{
				{From: "analyze", To: "fix"},
				{From: "fix", To: "review"},
				{From: "review", To: "fix"},
				{From: "fix", To: "review"},
				{From: "review", To: "complete"},
			},
			expected: 1,
		},
		{
			name: "multiple loops",
			transitions: []PhaseTransition{
				{From: "analyze", To: "fix"},
				{From: "fix", To: "review"},
				{From: "review", To: "fix"},
				{From: "fix", To: "review"},
				{From: "review", To: "fix"},
				{From: "fix", To: "review"},
				{From: "review", To: "complete"},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectPhaseLoops(tt.transitions)
			if result < tt.expected {
				t.Errorf("Expected at least %d loops, got %d", tt.expected, result)
			}
		})
	}
}

func TestComputeAggregatedMetrics(t *testing.T) {
	now := time.Now()

	metrics := []WorkflowMetrics{
		{
			WorkflowID:     "wf-1",
			WorkflowType:   "bug",
			RetryCount:     3,
			CycleTimeMs:    10000,
			SuccessRate:    1.0,
			ReviewFailRate: 0.5,
			Status:         "completed",
			StartedAt:      now.Add(-30 * time.Minute),
		},
		{
			WorkflowID:     "wf-2",
			WorkflowType:   "bug",
			RetryCount:     2,
			CycleTimeMs:    15000,
			SuccessRate:    0.0,
			ReviewFailRate: 0.3,
			Status:         "failed",
			StartedAt:      now.Add(-60 * time.Minute),
		},
		{
			WorkflowID:     "wf-3",
			WorkflowType:   "spec",
			RetryCount:     0,
			CycleTimeMs:    5000,
			SuccessRate:    1.0,
			ReviewFailRate: 0.0,
			Status:         "completed",
			StartedAt:      now.Add(-15 * time.Minute),
		},
	}

	aggregated := ComputeAggregatedMetrics(metrics)

	if len(aggregated) != 2 {
		t.Errorf("Expected 2 aggregated metrics (bug, spec), got %d", len(aggregated))
	}

	bugMetrics, ok := aggregated["bug"]
	if !ok {
		t.Fatal("Expected 'bug' workflow type in aggregated metrics")
	}

	if bugMetrics.TotalRuns != 2 {
		t.Errorf("Expected TotalRuns 2, got %d", bugMetrics.TotalRuns)
	}

	if bugMetrics.SuccessRate != 0.5 {
		t.Errorf("Expected SuccessRate 0.5, got %f", bugMetrics.SuccessRate)
	}

	if bugMetrics.HighLoopCount != 1 {
		t.Errorf("Expected HighLoopCount 1, got %d", bugMetrics.HighLoopCount)
	}
}

func TestWorkflowAnalyzer(t *testing.T) {
	now := time.Now()

	mockQuery := &mockEventQuery{
		events: []EventForMetrics{
			{
				ID:        "1",
				Type:      "workflow.started",
				Timestamp: now.Add(-1 * time.Hour),
				Source:    "test",
				Payload:   map[string]any{"workflow_id": "wf-1", "workflow_type": "bug"},
			},
			{
				ID:        "2",
				Type:      "workflow.phase_transition",
				Timestamp: now.Add(-55 * time.Minute),
				Source:    "test",
				Payload:   map[string]any{"workflow_id": "wf-1", "from_phase": "fix", "to_phase": "review"},
			},
			{
				ID:        "3",
				Type:      "workflow.phase_transition",
				Timestamp: now.Add(-50 * time.Minute),
				Source:    "test",
				Payload:   map[string]any{"workflow_id": "wf-1", "from_phase": "review", "to_phase": "fix"},
			},
			{
				ID:        "4",
				Type:      "workflow.phase_transition",
				Timestamp: now.Add(-45 * time.Minute),
				Source:    "test",
				Payload:   map[string]any{"workflow_id": "wf-1", "from_phase": "fix", "to_phase": "review"},
			},
			{
				ID:        "5",
				Type:      "workflow.phase_transition",
				Timestamp: now.Add(-40 * time.Minute),
				Source:    "test",
				Payload:   map[string]any{"workflow_id": "wf-1", "from_phase": "review", "to_phase": "fix"},
			},
			{
				ID:        "6",
				Type:      "workflow.phase_transition",
				Timestamp: now.Add(-35 * time.Minute),
				Source:    "test",
				Payload:   map[string]any{"workflow_id": "wf-1", "from_phase": "fix", "to_phase": "review"},
			},
			{
				ID:        "7",
				Type:      "review.failed",
				Timestamp: now.Add(-30 * time.Minute),
				Source:    "test",
				Payload:   map[string]any{"workflow_id": "wf-1"},
			},
			{
				ID:        "8",
				Type:      "workflow.completed",
				Timestamp: now.Add(-25 * time.Minute),
				Source:    "test",
				Payload:   map[string]any{"workflow_id": "wf-1"},
			},
		},
	}

	config := DefaultAnalyzerConfig()
	config.MinWorkflowsForDetection = 1

	analyzer := NewWorkflowAnalyzer(mockQuery, config)

	findings, err := analyzer.AnalyzeWorkflowPerformance(nil)
	if err != nil {
		t.Fatalf("AnalyzeWorkflowPerformance failed: %v", err)
	}

	if len(findings) == 0 {
		t.Error("Expected at least one finding")
	}

	foundLoop := false
	for _, f := range findings {
		if f.Type == "workflow_loop" {
			foundLoop = true
			if f.WorkflowType != "bug" {
				t.Errorf("Expected workflow_type 'bug', got '%s'", f.WorkflowType)
			}
		}
	}

	if !foundLoop {
		t.Error("Expected to find workflow_loop finding")
	}
}

type mockEventQuery struct {
	events []EventForMetrics
}

func (m *mockEventQuery) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]EventForMetrics, error) {
	return m.events, nil
}
