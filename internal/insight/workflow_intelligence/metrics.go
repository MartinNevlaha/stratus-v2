package workflow_intelligence

import (
	"slices"
	"time"
)

type WorkflowMetrics struct {
	WorkflowID       string            `json:"workflow_id"`
	WorkflowType     string            `json:"workflow_type"`
	TaskType         string            `json:"task_type"`
	AgentsUsed       []string          `json:"agents_used"`
	RetryCount       int               `json:"retry_count"`
	CycleTimeMs      int64             `json:"cycle_time_ms"`
	SuccessRate      float64           `json:"success_rate"`
	ReviewFailRate   float64           `json:"review_fail_rate"`
	PhaseTransitions []PhaseTransition `json:"phase_transitions"`
	StartedAt        time.Time         `json:"started_at"`
	CompletedAt      *time.Time        `json:"completed_at"`
	Status           string            `json:"status"`
}

type PhaseTransition struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	At        time.Time `json:"at"`
	AgentName string    `json:"agent_name,omitempty"`
}

type EventForMetrics struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Source    string         `json:"source"`
	Payload   map[string]any `json:"payload"`
}

func ComputeWorkflowMetrics(events []EventForMetrics) []WorkflowMetrics {
	workflowEvents := groupEventsByWorkflow(events)
	var metrics []WorkflowMetrics

	for workflowID, wfEvents := range workflowEvents {
		metric := computeSingleWorkflowMetrics(workflowID, wfEvents)
		if metric != nil {
			metrics = append(metrics, *metric)
		}
	}

	return metrics
}

func groupEventsByWorkflow(events []EventForMetrics) map[string][]EventForMetrics {
	grouped := make(map[string][]EventForMetrics)

	for _, e := range events {
		workflowID := extractWorkflowID(e)
		if workflowID != "" {
			grouped[workflowID] = append(grouped[workflowID], e)
		}
	}

	return grouped
}

func extractWorkflowID(e EventForMetrics) string {
	if wfID, ok := e.Payload["workflow_id"].(string); ok && wfID != "" {
		return wfID
	}
	return ""
}

func computeSingleWorkflowMetrics(workflowID string, events []EventForMetrics) *WorkflowMetrics {
	if len(events) == 0 {
		return nil
	}

	metric := &WorkflowMetrics{
		WorkflowID:       workflowID,
		AgentsUsed:       []string{},
		PhaseTransitions: []PhaseTransition{},
	}

	sortEventsByTime(events)

	var startedAt *time.Time
	var completedAt *time.Time
	var workflowCompleted, workflowFailed bool
	var reviewPassed, reviewFailed int

	agentSet := make(map[string]bool)
	phaseHistory := []string{}

	for _, e := range events {
		switch e.Type {
		case "workflow.started":
			startedAt = &e.Timestamp
			metric.StartedAt = e.Timestamp
			metric.Status = "running"
			if wt, ok := e.Payload["workflow_type"].(string); ok {
				metric.WorkflowType = wt
			}

		case "workflow.completed":
			completedAt = &e.Timestamp
			metric.CompletedAt = &e.Timestamp
			metric.Status = "completed"
			workflowCompleted = true

		case "workflow.failed":
			completedAt = &e.Timestamp
			metric.CompletedAt = &e.Timestamp
			metric.Status = "failed"
			workflowFailed = true

		case "workflow.phase_transition":
			from, _ := e.Payload["from_phase"].(string)
			to, _ := e.Payload["to_phase"].(string)
			agentName, _ := e.Payload["agent_name"].(string)

			if from != "" && to != "" {
				metric.PhaseTransitions = append(metric.PhaseTransitions, PhaseTransition{
					From:      from,
					To:        to,
					At:        e.Timestamp,
					AgentName: agentName,
				})

				phaseHistory = append(phaseHistory, from, to)
			}

		case "agent.spawned", "agent.completed":
			agentName, _ := e.Payload["agent_name"].(string)
			if agentName == "" {
				agentName, _ = e.Payload["agent_type"].(string)
			}
			if agentName != "" {
				agentSet[agentName] = true
			}

		case "review.passed":
			reviewPassed++

		case "review.failed":
			reviewFailed++
		}
	}

	for agent := range agentSet {
		metric.AgentsUsed = append(metric.AgentsUsed, agent)
	}
	slices.Sort(metric.AgentsUsed)

	metric.RetryCount = DetectPhaseLoops(metric.PhaseTransitions)

	if startedAt != nil && completedAt != nil {
		metric.CycleTimeMs = completedAt.Sub(*startedAt).Milliseconds()
	}

	if workflowCompleted || workflowFailed {
		if workflowCompleted {
			metric.SuccessRate = 1.0
		} else {
			metric.SuccessRate = 0.0
		}
	}

	totalReviews := reviewPassed + reviewFailed
	if totalReviews > 0 {
		metric.ReviewFailRate = float64(reviewFailed) / float64(totalReviews)
	}

	return metric
}

func DetectPhaseLoops(transitions []PhaseTransition) int {
	if len(transitions) < 2 {
		return 0
	}

	loopPatterns := map[string]int{
		"fix-review":       0,
		"review-fix":       0,
		"implement-verify": 0,
		"verify-implement": 0,
		"generate-heal":    0,
		"heal-generate":    0,
	}

	phaseSequence := make([]string, 0, len(transitions)+1)
	for i, t := range transitions {
		if i == 0 {
			phaseSequence = append(phaseSequence, t.From)
		}
		phaseSequence = append(phaseSequence, t.To)
	}

	for i := 0; i < len(phaseSequence)-1; i++ {
		current := phaseSequence[i]
		next := phaseSequence[i+1]

		pattern := current + "-" + next
		if _, exists := loopPatterns[pattern]; exists {
			loopPatterns[pattern]++
		}
	}

	totalLoops := 0
	for _, count := range loopPatterns {
		if count > 1 {
			totalLoops += count - 1
		}
	}

	return totalLoops
}

func sortEventsByTime(events []EventForMetrics) {
	slices.SortFunc(events, func(a, b EventForMetrics) int {
		return a.Timestamp.Compare(b.Timestamp)
	})
}

type MetricsConfig struct {
	MinEventsForWorkflow int `json:"min_events_for_workflow"`
}

func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		MinEventsForWorkflow: 2,
	}
}

func ComputeAggregatedMetrics(metrics []WorkflowMetrics) map[string]AggregatedWorkflowMetrics {
	byType := make(map[string][]WorkflowMetrics)

	for _, m := range metrics {
		if m.WorkflowType != "" {
			byType[m.WorkflowType] = append(byType[m.WorkflowType], m)
		}
	}

	result := make(map[string]AggregatedWorkflowMetrics)
	for wfType, wfMetrics := range byType {
		result[wfType] = aggregateMetrics(wfType, wfMetrics)
	}

	return result
}

type AggregatedWorkflowMetrics struct {
	WorkflowType        string  `json:"workflow_type"`
	TotalRuns           int     `json:"total_runs"`
	AvgRetryCount       float64 `json:"avg_retry_count"`
	AvgCycleTimeMs      int64   `json:"avg_cycle_time_ms"`
	SuccessRate         float64 `json:"success_rate"`
	AvgReviewFailRate   float64 `json:"avg_review_fail_rate"`
	HighLoopCount       int     `json:"high_loop_count"`
	HighReviewFailCount int     `json:"high_review_fail_count"`
}

func aggregateMetrics(wfType string, metrics []WorkflowMetrics) AggregatedWorkflowMetrics {
	agg := AggregatedWorkflowMetrics{
		WorkflowType: wfType,
		TotalRuns:    len(metrics),
	}

	if len(metrics) == 0 {
		return agg
	}

	var totalRetry, totalCycleTime int64
	var totalSuccess, totalReviewFail float64
	var completed int

	highLoopThreshold := 3
	highReviewFailThreshold := 0.5

	for _, m := range metrics {
		totalRetry += int64(m.RetryCount)

		if m.CycleTimeMs > 0 {
			totalCycleTime += m.CycleTimeMs
		}

		if m.Status == "completed" {
			totalSuccess++
			completed++
		}

		totalReviewFail += m.ReviewFailRate

		if m.RetryCount >= highLoopThreshold {
			agg.HighLoopCount++
		}

		if m.ReviewFailRate >= highReviewFailThreshold {
			agg.HighReviewFailCount++
		}
	}

	n := len(metrics)
	agg.AvgRetryCount = float64(totalRetry) / float64(n)
	agg.AvgCycleTimeMs = totalCycleTime / int64(n)
	agg.SuccessRate = totalSuccess / float64(n)
	agg.AvgReviewFailRate = totalReviewFail / float64(n)

	return agg
}
