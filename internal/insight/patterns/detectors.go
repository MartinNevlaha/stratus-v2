package patterns

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type Detector interface {
	Name() string
	Detect(ctx context.Context, events []EventForDetection, config DetectionConfig) *Pattern
}

type WorkflowFailureClusterDetector struct{}

func (d *WorkflowFailureClusterDetector) Name() string {
	return "workflow_failure_cluster"
}

func (d *WorkflowFailureClusterDetector) Detect(ctx context.Context, events []EventForDetection, config DetectionConfig) *Pattern {
	var completed, failed int
	var workflowTypes = make(map[string]int)

	for _, e := range events {
		switch e.Type {
		case "workflow.completed":
			completed++
			if wt, ok := e.Payload["workflow_type"].(string); ok {
				workflowTypes[wt]++
			}
		case "workflow.failed":
			failed++
			if wt, ok := e.Payload["workflow_type"].(string); ok {
				workflowTypes[wt]++
			}
		}
	}

	total := completed + failed
	if total < config.MinEventsForDetection || total == 0 {
		slog.Debug("workflow_failure_cluster: insufficient events", "total", total, "min", config.MinEventsForDetection)
		return nil
	}

	failureRate := float64(failed) / float64(total)
	slog.Debug("workflow_failure_cluster: calculated rate", "failure_rate", failureRate, "threshold", config.FailureRateThreshold)

	if failureRate < config.FailureRateThreshold {
		return nil
	}

	mostAffectedType := "unknown"
	maxCount := 0
	for wt, count := range workflowTypes {
		if count > maxCount {
			maxCount = count
			mostAffectedType = wt
		}
	}

	severity := SeverityFromFailureRate(failureRate)
	confidence := 0.7 + (failureRate * 0.25)
	if confidence > 0.95 {
		confidence = 0.95
	}

	return &Pattern{
		Type:        PatternWorkflowFailureCluster,
		Timestamp:   time.Now().UTC(),
		Severity:    severity,
		Description: fmt.Sprintf("Workflow failure cluster detected: %.1f%% failure rate (%d/%d runs)", failureRate*100, failed, total),
		Evidence: map[string]any{
			"failure_rate":       failureRate,
			"failed_count":       failed,
			"completed_count":    completed,
			"total_count":        total,
			"affected_workflow":  mostAffectedType,
			"detection_window_h": config.EventWindowHours,
		},
		Frequency:  1,
		Confidence: confidence,
		FirstSeen:  time.Now().UTC(),
		LastSeen:   time.Now().UTC(),
	}
}

type AgentPerformanceDropDetector struct{}

func (d *AgentPerformanceDropDetector) Name() string {
	return "agent_performance_drop"
}

func (d *AgentPerformanceDropDetector) Detect(ctx context.Context, events []EventForDetection, config DetectionConfig) *Pattern {
	windowStart := time.Now().Add(-time.Duration(config.EventWindowHours) * time.Hour)
	prevWindowStart := windowStart.Add(-time.Duration(config.EventWindowHours) * time.Hour)

	var currentCompleted, currentFailed int
	var prevCompleted, prevFailed int
	agentStats := make(map[string]struct{ completed, failed int })

	for _, e := range events {
		switch e.Type {
		case "agent.completed":
			if e.Timestamp.After(windowStart) {
				currentCompleted++
				if agent, ok := e.Payload["agent_type"].(string); ok {
					stats := agentStats[agent]
					stats.completed++
					agentStats[agent] = stats
				}
			} else if e.Timestamp.After(prevWindowStart) {
				prevCompleted++
			}
		case "agent.failed":
			if e.Timestamp.After(windowStart) {
				currentFailed++
				if agent, ok := e.Payload["agent_type"].(string); ok {
					stats := agentStats[agent]
					stats.failed++
					agentStats[agent] = stats
				}
			} else if e.Timestamp.After(prevWindowStart) {
				prevFailed++
			}
		}
	}

	currentTotal := currentCompleted + currentFailed
	prevTotal := prevCompleted + prevFailed

	if currentTotal < config.MinEventsForDetection || prevTotal < config.MinEventsForDetection {
		slog.Debug("agent_performance_drop: insufficient events", "current", currentTotal, "prev", prevTotal)
		return nil
	}
	if currentTotal == 0 || prevTotal == 0 {
		return nil
	}

	currentRate := float64(currentCompleted) / float64(currentTotal)
	prevRate := float64(prevCompleted) / float64(prevTotal)

	dropRate := prevRate - currentRate
	slog.Debug("agent_performance_drop: calculated drop", "current_rate", currentRate, "prev_rate", prevRate, "drop", dropRate)

	if dropRate < config.PerformanceDropThreshold {
		return nil
	}

	worstAgent := ""
	worstDrop := 0.0
	for agent, stats := range agentStats {
		total := stats.completed + stats.failed
		if total >= 3 {
			rate := float64(stats.completed) / float64(total)
			if prevRate-rate > worstDrop {
				worstDrop = prevRate - rate
				worstAgent = agent
			}
		}
	}

	severity := SeverityFromDropRate(dropRate)
	confidence := 0.65 + (dropRate * 0.30)
	if confidence > 0.90 {
		confidence = 0.90
	}

	return &Pattern{
		Type:        PatternAgentPerformanceDrop,
		Timestamp:   time.Now().UTC(),
		Severity:    severity,
		Description: fmt.Sprintf("Agent performance dropped: %.1f%% decrease in success rate (%.1f%% -> %.1f%%)", dropRate*100, prevRate*100, currentRate*100),
		Evidence: map[string]any{
			"current_success_rate":   currentRate,
			"previous_success_rate":  prevRate,
			"performance_drop":       dropRate,
			"agent_id":               worstAgent,
			"drop_rate":              dropRate,
			"current_total":          currentTotal,
			"previous_total":         prevTotal,
			"worst_performing_agent": worstAgent,
			"detection_window_h":     config.EventWindowHours,
		},
		Frequency:  1,
		Confidence: confidence,
		FirstSeen:  time.Now().UTC(),
		LastSeen:   time.Now().UTC(),
	}
}

type ReviewRejectionSpikeDetector struct{}

func (d *ReviewRejectionSpikeDetector) Name() string {
	return "review_rejection_spike"
}

func (d *ReviewRejectionSpikeDetector) Detect(ctx context.Context, events []EventForDetection, config DetectionConfig) *Pattern {
	var passed, failed int
	reviewSources := make(map[string]int)

	for _, e := range events {
		switch e.Type {
		case "review.passed":
			passed++
			if src, ok := e.Payload["source"].(string); ok {
				reviewSources[src]++
			}
		case "review.failed":
			failed++
			if src, ok := e.Payload["source"].(string); ok {
				reviewSources[src]++
			}
		}
	}

	total := passed + failed
	if total < config.MinEventsForDetection || total == 0 {
		slog.Debug("review_rejection_spike: insufficient events", "total", total)
		return nil
	}

	rejectionRate := float64(failed) / float64(total)
	slog.Debug("review_rejection_spike: calculated rate", "rejection_rate", rejectionRate, "threshold", config.RejectionRateThreshold)

	if rejectionRate < config.RejectionRateThreshold {
		return nil
	}

	topSource := "unknown"
	maxCount := 0
	for src, count := range reviewSources {
		if count > maxCount {
			maxCount = count
			topSource = src
		}
	}

	severity := SeverityFromRejectionRate(rejectionRate)
	confidence := 0.7 + (rejectionRate * 0.25)
	if confidence > 0.95 {
		confidence = 0.95
	}

	return &Pattern{
		Type:        PatternReviewRejectionSpike,
		Timestamp:   time.Now().UTC(),
		Severity:    severity,
		Description: fmt.Sprintf("Review rejection spike detected: %.1f%% rejection rate (%d/%d reviews)", rejectionRate*100, failed, total),
		Evidence: map[string]any{
			"rejection_rate":     rejectionRate,
			"failed_count":       failed,
			"passed_count":       passed,
			"total_count":        total,
			"top_source":         topSource,
			"detection_window_h": config.EventWindowHours,
		},
		Frequency:  1,
		Confidence: confidence,
		FirstSeen:  time.Now().UTC(),
		LastSeen:   time.Now().UTC(),
	}
}

type WorkflowDurationSpikeDetector struct{}

func (d *WorkflowDurationSpikeDetector) Name() string {
	return "workflow_duration_spike"
}

func (d *WorkflowDurationSpikeDetector) Detect(ctx context.Context, events []EventForDetection, config DetectionConfig) *Pattern {
	windowStart := time.Now().Add(-time.Duration(config.EventWindowHours) * time.Hour)
	prevWindowStart := windowStart.Add(-time.Duration(config.EventWindowHours) * time.Hour)

	var currentDurations []float64
	var prevDurations []float64
	workflowDurations := make(map[string][]float64)

	for _, e := range events {
		if e.Type != "workflow.completed" {
			continue
		}

		durationMs, ok := e.Payload["duration_ms"].(float64)
		if !ok {
			if durInt, ok := e.Payload["duration_ms"].(int); ok {
				durationMs = float64(durInt)
			} else {
				continue
			}
		}

		if e.Timestamp.After(windowStart) {
			currentDurations = append(currentDurations, durationMs)
			if wt, ok := e.Payload["workflow_type"].(string); ok {
				workflowDurations[wt] = append(workflowDurations[wt], durationMs)
			}
		} else if e.Timestamp.After(prevWindowStart) {
			prevDurations = append(prevDurations, durationMs)
		}
	}

	if len(currentDurations) < config.MinEventsForDetection {
		slog.Debug("workflow_duration_spike: insufficient current events", "count", len(currentDurations))
		return nil
	}

	currentAvg := average(currentDurations)
	var multiplier float64
	if len(prevDurations) >= config.MinEventsForDetection {
		prevAvg := average(prevDurations)
		if prevAvg > 0 {
			multiplier = currentAvg / prevAvg
		}
	}

	slog.Debug("workflow_duration_spike: calculated multiplier", "current_avg_ms", currentAvg, "multiplier", multiplier, "threshold", config.DurationSpikeMultiplier)

	if multiplier < config.DurationSpikeMultiplier {
		return nil
	}

	slowestType := "unknown"
	slowestAvg := 0.0
	for wt, durations := range workflowDurations {
		avg := average(durations)
		if avg > slowestAvg {
			slowestAvg = avg
			slowestType = wt
		}
	}

	severity := SeverityFromDurationMultiplier(multiplier)
	confidence := 0.7 + ((multiplier - 2) * 0.1)
	if confidence > 0.90 {
		confidence = 0.90
	}

	return &Pattern{
		Type:        PatternWorkflowDurationSpike,
		Timestamp:   time.Now().UTC(),
		Severity:    severity,
		Description: fmt.Sprintf("Workflow duration spike detected: %.1fx increase in average duration (%.0fms)", multiplier, currentAvg),
		Evidence: map[string]any{
			"current_avg_duration_ms": currentAvg,
			"duration_multiplier":     multiplier,
			"affected_workflow":       slowestType,
			"multiplier":              multiplier,
			"current_sample_count":    len(currentDurations),
			"prev_sample_count":       len(prevDurations),
			"slowest_workflow_type":   slowestType,
			"detection_window_h":      config.EventWindowHours,
		},
		Frequency:  1,
		Confidence: confidence,
		FirstSeen:  time.Now().UTC(),
		LastSeen:   time.Now().UTC(),
	}
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

type WorkflowLoopDetector struct{}

func (d *WorkflowLoopDetector) Name() string {
	return "workflow_loop"
}

func (d *WorkflowLoopDetector) Detect(ctx context.Context, events []EventForDetection, config DetectionConfig) *Pattern {
	loopThreshold := config.LoopThreshold
	if loopThreshold <= 0 {
		loopThreshold = 3
	}
	workflowLoops := make(map[string]int)
	workflowTransitions := make(map[string][]string)

	for _, e := range events {
		if e.Type != "workflow.phase_transition" {
			continue
		}

		wfID, _ := e.Payload["workflow_id"].(string)
		if wfID == "" {
			continue
		}

		from, _ := e.Payload["from_phase"].(string)
		to, _ := e.Payload["to_phase"].(string)
		if from == "" || to == "" {
			continue
		}

		transition := from + "->" + to
		workflowTransitions[wfID] = append(workflowTransitions[wfID], transition)
	}

	for wfID, transitions := range workflowTransitions {
		transitionCount := make(map[string]int)
		for _, t := range transitions {
			transitionCount[t]++
			if transitionCount[t] > 1 {
				workflowLoops[wfID]++
			}
		}
	}

	if len(workflowLoops) < config.MinEventsForDetection {
		slog.Debug("workflow_loop: insufficient loop patterns", "count", len(workflowLoops))
		return nil
	}

	var totalLoops int
	var maxLoops int
	var affectedWorkflows []string
	for wfID, loopCount := range workflowLoops {
		totalLoops += loopCount
		if loopCount > maxLoops {
			maxLoops = loopCount
		}
		if loopCount >= loopThreshold && len(affectedWorkflows) < 5 {
			affectedWorkflows = append(affectedWorkflows, wfID)
		}
	}

	if len(affectedWorkflows) == 0 {
		return nil
	}

	avgLoops := float64(totalLoops) / float64(len(workflowLoops))
	severity := SeverityMedium
	if avgLoops >= 4 {
		severity = SeverityHigh
	}
	if avgLoops >= 5 {
		severity = SeverityCritical
	}

	confidence := 0.6 + (avgLoops * 0.08)
	if confidence > 0.95 {
		confidence = 0.95
	}

	return &Pattern{
		Type:        PatternWorkflowLoop,
		Timestamp:   time.Now().UTC(),
		Severity:    severity,
		Description: fmt.Sprintf("Workflow loop patterns detected: avg %.1f loops, %d workflows affected", avgLoops, len(affectedWorkflows)),
		Evidence: map[string]any{
			"avg_loops":          avgLoops,
			"max_loops":          maxLoops,
			"affected_count":     len(affectedWorkflows),
			"affected_workflows": affectedWorkflows,
			"detection_window_h": config.EventWindowHours,
		},
		Frequency:  len(affectedWorkflows),
		Confidence: confidence,
		FirstSeen:  time.Now().UTC(),
		LastSeen:   time.Now().UTC(),
	}
}

type WorkflowReviewFailureDetector struct{}

func (d *WorkflowReviewFailureDetector) Name() string {
	return "workflow_review_failure_cluster"
}

func (d *WorkflowReviewFailureDetector) Detect(ctx context.Context, events []EventForDetection, config DetectionConfig) *Pattern {
	workflowReviews := make(map[string]struct{ passed, failed int })

	for _, e := range events {
		wfID, _ := e.Payload["workflow_id"].(string)
		if wfID == "" {
			continue
		}

		stats := workflowReviews[wfID]
		switch e.Type {
		case "review.passed":
			stats.passed++
		case "review.failed":
			stats.failed++
		}
		workflowReviews[wfID] = stats
	}

	if len(workflowReviews) < config.MinEventsForDetection {
		slog.Debug("workflow_review_failure_cluster: insufficient review data", "count", len(workflowReviews))
		return nil
	}

	var totalFailRate float64
	var affectedWorkflows []string
	var highFailureCount int
	threshold := config.ReviewFailThreshold
	if threshold <= 0 {
		threshold = 0.50
	}

	for wfID, stats := range workflowReviews {
		total := stats.passed + stats.failed
		if total == 0 {
			continue
		}

		failRate := float64(stats.failed) / float64(total)
		totalFailRate += failRate

		if failRate >= threshold {
			highFailureCount++
			if len(affectedWorkflows) < 5 {
				affectedWorkflows = append(affectedWorkflows, fmt.Sprintf("%s:%.0f%%", wfID, failRate*100))
			}
		}
	}

	if highFailureCount < config.MinEventsForDetection {
		return nil
	}

	avgFailRate := totalFailRate / float64(len(workflowReviews))
	severity := SeverityMedium
	if avgFailRate >= 0.6 {
		severity = SeverityHigh
	}
	if avgFailRate >= 0.7 {
		severity = SeverityCritical
	}

	confidence := 0.55 + (avgFailRate * 0.35)
	if confidence > 0.95 {
		confidence = 0.95
	}

	return &Pattern{
		Type:        PatternWorkflowReviewFailure,
		Timestamp:   time.Now().UTC(),
		Severity:    severity,
		Description: fmt.Sprintf("High review failure cluster: %.1f%% avg failure rate, %d workflows affected", avgFailRate*100, highFailureCount),
		Evidence: map[string]any{
			"avg_fail_rate":      avgFailRate,
			"affected_count":     highFailureCount,
			"total_workflows":    len(workflowReviews),
			"affected_workflows": affectedWorkflows,
			"threshold":          threshold,
			"detection_window_h": config.EventWindowHours,
		},
		Frequency:  highFailureCount,
		Confidence: confidence,
		FirstSeen:  time.Now().UTC(),
		LastSeen:   time.Now().UTC(),
	}
}

type WorkflowSlowExecutionDetector struct{}

func (d *WorkflowSlowExecutionDetector) Name() string {
	return "workflow_slow_execution"
}

func (d *WorkflowSlowExecutionDetector) Detect(ctx context.Context, events []EventForDetection, config DetectionConfig) *Pattern {
	workflowDurations := make(map[string]float64)
	workflowTypes := make(map[string]string)
	baselineByType := config.BaselineCycleTimesMs
	if baselineByType == nil || len(baselineByType) == 0 {
		baselineByType = map[string]int64{
			"spec": 10 * 60 * 1000,
			"bug":  5 * 60 * 1000,
			"e2e":  15 * 60 * 1000,
		}
	}

	startTimes := make(map[string]time.Time)
	for _, e := range events {
		wfID, _ := e.Payload["workflow_id"].(string)
		if wfID == "" {
			continue
		}

		switch e.Type {
		case "workflow.started":
			startTimes[wfID] = e.Timestamp
			if wt, ok := e.Payload["workflow_type"].(string); ok {
				workflowTypes[wfID] = wt
			}
		case "workflow.completed", "workflow.failed":
			if start, exists := startTimes[wfID]; exists {
				duration := e.Timestamp.Sub(start).Milliseconds()
				workflowDurations[wfID] = float64(duration)
			}
		}
	}

	if len(workflowDurations) < config.MinEventsForDetection {
		slog.Debug("workflow_slow_execution: insufficient completed workflows", "count", len(workflowDurations))
		return nil
	}

	slowThreshold := config.DurationSpikeMultiplier
	var slowWorkflows []string
	var totalMultiplier float64
	var slowCount int

	for wfID, duration := range workflowDurations {
		wfType := workflowTypes[wfID]
		baseline := float64(baselineByType[wfType])
		if baseline == 0 {
			baseline = float64(baselineByType["spec"])
		}

		multiplier := duration / baseline
		if multiplier >= slowThreshold {
			slowCount++
			totalMultiplier += multiplier
			if len(slowWorkflows) < 5 {
				slowWorkflows = append(slowWorkflows, fmt.Sprintf("%s:%.1fx", wfID, multiplier))
			}
		}
	}

	if slowCount < config.MinEventsForDetection {
		return nil
	}

	avgMultiplier := totalMultiplier / float64(slowCount)
	severity := SeverityMedium
	if avgMultiplier >= 2.5 {
		severity = SeverityHigh
	}
	if avgMultiplier >= 3.0 {
		severity = SeverityCritical
	}

	confidence := 0.5 + ((avgMultiplier - 1.5) * 0.2)
	if confidence > 0.90 {
		confidence = 0.90
	}
	if confidence < 0.25 {
		confidence = 0.25
	}

	return &Pattern{
		Type:        PatternWorkflowSlowExecution,
		Timestamp:   time.Now().UTC(),
		Severity:    severity,
		Description: fmt.Sprintf("Slow workflow execution detected: %.1fx avg slowdown, %d workflows affected", avgMultiplier, slowCount),
		Evidence: map[string]any{
			"avg_multiplier":     avgMultiplier,
			"affected_count":     slowCount,
			"total_workflows":    len(workflowDurations),
			"slow_workflows":     slowWorkflows,
			"slow_threshold":     slowThreshold,
			"detection_window_h": config.EventWindowHours,
		},
		Frequency:  slowCount,
		Confidence: confidence,
		FirstSeen:  time.Now().UTC(),
		LastSeen:   time.Now().UTC(),
	}
}
