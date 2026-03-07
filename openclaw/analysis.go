package openclaw

import (
	"fmt"
	"log"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func (e *Engine) RunAnalysis() error {
	e.analysisMu.Lock()
	defer e.analysisMu.Unlock()

	start := time.Now()
	log.Println("openclaw: analysis started")

	findings := map[string]any{}
	recommendations := map[string]any{}
	var patterns []*db.OpenClawPattern
	var proposals []db.Proposal

	// Get recent daily metrics (7 days)
	dailyMetrics, err := e.database.GetRecentDailyMetrics(7)
	if err != nil {
		return fmt.Errorf("get daily metrics: %w", err)
	}

	if len(dailyMetrics) == 0 {
		log.Println("openclaw: no metrics data available")
		return nil
	}

	findings["metrics_analyzed"] = len(dailyMetrics)
	findings["analysis_date"] = time.Now().Format(time.RFC3339)

	// Pattern 1: Analyze success rates
	var successRates []float64
	var dates []string
	for _, m := range dailyMetrics {
		if sr, ok := m["success_rate"].(float64); ok {
			successRates = append(successRates, sr)
			if date, ok := m["date"].(string); ok {
				dates = append(dates, date)
			}
		}
	}

	if len(successRates) > 0 {
		avgSuccessRate := calculateAverage(successRates)
		findings["avg_success_rate"] = avgSuccessRate

		// Pattern: Low success rate
		if avgSuccessRate < 0.7 {
			pattern := &db.OpenClawPattern{
				PatternType: "quality",
				PatternName: "low_success_rate",
				Description: fmt.Sprintf("Average success rate (%.1f%%) is below 70%% threshold", avgSuccessRate*100),
				Frequency:   len(successRates),
				Confidence:  0.85,
				Examples:    []string{fmt.Sprintf("7-day average: %.2f%%", avgSuccessRate*100)},
				Metadata:    map[string]interface{}{"avg_rate": avgSuccessRate, "threshold": 0.7},
			}
			patterns = append(patterns, pattern)

			// Generate rule proposal
			proposal := db.Proposal{
				CandidateID:     "",
				Type:            "rule",
				Title:           "Improve workflow success rate",
				Description:     fmt.Sprintf("Detected %.1f%% average success rate over %d days (threshold: 70%%)", avgSuccessRate*100, len(successRates)),
				ProposedContent: generateRuleContent("Improve Workflow Success Rate", avgSuccessRate, "quality"),
				Confidence:      0.80,
				Status:          "pending",
				CreatedAt:       time.Now().UTC().Format(time.RFC3339Nano),
			}
			proposals = append(proposals, proposal)

			recommendations["improve_quality"] = map[string]any{
				"priority": "high",
				"action":   "investigate recent workflow failures",
				"impact":   "increase success rate by 20%",
			}
		}

		// Pattern: High success rate (positive pattern)
		if avgSuccessRate >= 0.9 {
			pattern := &db.OpenClawPattern{
				PatternType: "success",
				PatternName: "high_success_rate",
				Description: fmt.Sprintf("Excellent success rate: %.1f%%", avgSuccessRate*100),
				Frequency:   len(successRates),
				Confidence:  0.90,
				Examples:    []string{fmt.Sprintf("7-day average: %.2f%%", avgSuccessRate*100)},
			}
			patterns = append(patterns, pattern)
		}
	}

	// Pattern 2: Analyze workflow completion
	var totalWorkflows, completedWorkflows int
	for _, m := range dailyMetrics {
		if tw, ok := m["total_workflows"].(int); ok {
			totalWorkflows += tw
		}
		if cw, ok := m["completed_workflows"].(int); ok {
			completedWorkflows += cw
		}
	}

	if totalWorkflows > 0 {
		completionRate := float64(completedWorkflows) / float64(totalWorkflows)
		findings["completion_rate"] = completionRate
		findings["total_workflows"] = totalWorkflows
		findings["completed_workflows"] = completedWorkflows

		// Pattern: Low completion rate
		if completionRate < 0.5 {
			pattern := &db.OpenClawPattern{
				PatternType: "workflow",
				PatternName: "low_completion_rate",
				Description: fmt.Sprintf("Only %.1f%% of workflows are completed (%d/%d)", completionRate*100, completedWorkflows, totalWorkflows),
				Frequency:   1,
				Confidence:  0.75,
				Examples:    []string{fmt.Sprintf("%d/%d completed", completedWorkflows, totalWorkflows)},
			}
			patterns = append(patterns, pattern)

			proposal := db.Proposal{
				CandidateID:     "",
				Type:            "rule",
				Title:           "Improve workflow completion",
				Description:     fmt.Sprintf("Low completion rate: %.1f%% (%d/%d workflows)", completionRate*100, completedWorkflows, totalWorkflows),
				ProposedContent: generateRuleContent("Improve Workflow Completion", completionRate, "workflow"),
				Confidence:      0.75,
				Status:          "pending",
				CreatedAt:       time.Now().UTC().Format(time.RFC3339Nano),
			}
			proposals = append(proposals, proposal)
		}

		// Pattern: High completion rate
		if completionRate >= 0.9 {
			pattern := &db.OpenClawPattern{
				PatternType: "success",
				PatternName: "high_completion_rate",
				Description: fmt.Sprintf("High completion rate: %.1f%%", completionRate*100),
				Frequency:   1,
				Confidence:  0.85,
				Examples:    []string{fmt.Sprintf("%d/%d completed", completedWorkflows, totalWorkflows)},
			}
			patterns = append(patterns, pattern)
		}
	}

	// Pattern 3: Analyze task performance
	var totalTasks, completedTasks int
	for _, m := range dailyMetrics {
		if tt, ok := m["total_tasks"].(int); ok {
			totalTasks += tt
		}
		if ct, ok := m["completed_tasks"].(int); ok {
			completedTasks += ct
		}
	}

	if totalTasks > 0 {
		taskSuccessRate := float64(completedTasks) / float64(totalTasks)
		findings["task_success_rate"] = taskSuccessRate
		findings["total_tasks"] = totalTasks
		findings["completed_tasks"] = completedTasks

		// Pattern: Task bottleneck
		if taskSuccessRate < 0.6 && totalTasks > 10 {
			pattern := &db.OpenClawPattern{
				PatternType: "performance",
				PatternName: "task_bottleneck",
				Description: fmt.Sprintf("Low task success rate: %.1f%% (%d/%d tasks)", taskSuccessRate*100, completedTasks, totalTasks),
				Frequency:   1,
				Confidence:  0.80,
				Examples:    []string{fmt.Sprintf("%d/%d tasks completed", completedTasks, totalTasks)},
			}
			patterns = append(patterns, pattern)

			proposal := db.Proposal{
				CandidateID:     "",
				Type:            "rule",
				Title:           "Optimize task execution",
				Description:     fmt.Sprintf("Task success rate is %.1f%%, below optimal threshold", taskSuccessRate*100),
				ProposedContent: generateRuleContent("Optimize Task Execution", taskSuccessRate, "performance"),
				Confidence:      0.70,
				Status:          "pending",
				CreatedAt:       time.Now().UTC().Format(time.RFC3339Nano),
			}
			proposals = append(proposals, proposal)
		}
	}

	// Pattern 4: Analyze workflow duration
	var durations []int
	for _, m := range dailyMetrics {
		if dur, ok := m["avg_workflow_duration_ms"].(int); ok && dur > 0 {
			durations = append(durations, dur)
		}
	}

	if len(durations) > 0 {
		avgDuration := calculateAverageInt(durations)
		findings["avg_duration_ms"] = avgDuration

		// Pattern: Slow workflows
		if avgDuration > 30000 { // > 30 seconds
			pattern := &db.OpenClawPattern{
				PatternType: "performance",
				PatternName: "slow_workflows",
				Description: fmt.Sprintf("Average workflow duration is high: %dms (>30s threshold)", avgDuration),
				Frequency:   len(durations),
				Confidence:  0.75,
				Examples:    []string{fmt.Sprintf("Avg duration: %dms", avgDuration)},
			}
			patterns = append(patterns, pattern)

			recommendations["optimize_performance"] = map[string]any{
				"priority": "medium",
				"action":   "profile and optimize slow phases",
				"impact":   "reduce average duration by 30%",
			}
		}
	}

	// Pattern 5: Analyze agent performance (if available)
	// TODO: Implement agent metrics when available
	// agentMetrics, err := e.database.GetAgentMetrics(30)
	// if err == nil && len(agentMetrics) > 0 {
	//	 findAgentPatterns(agentMetrics, findings, patterns)
	// }

	// Save patterns with idempotency check
	for _, pattern := range patterns {
		if err := e.savePatternIfNew(pattern); err != nil {
			log.Printf("openclaw: failed to save pattern: %v", err)
		}
	}

	// Save proposals (limit to MaxProposals)
	if e.config.MaxProposals > 0 && len(proposals) > e.config.MaxProposals {
		proposals = proposals[:e.config.MaxProposals]
	}

	for _, proposal := range proposals {
		if _, err := e.database.SaveProposal(proposal); err != nil {
			log.Printf("openclaw: failed to save proposal: %v", err)
		}
	}

	// Save analysis
	executionTime := time.Since(start).Milliseconds()
	analysis := &db.OpenClawAnalysis{
		AnalysisType:     "full",
		Scope:            "project-wide",
		Findings:         findings,
		Recommendations:  recommendations,
		PatternsFound:    len(patterns),
		ProposalsCreated: len(proposals),
		ExecutionTimeMs:  int(executionTime),
	}

	if err := e.database.SaveOpenClawAnalysis(analysis); err != nil {
		return fmt.Errorf("save analysis: %w", err)
	}

	// Update state
	state, err := e.database.GetOpenClawState()
	if err != nil {
		return err
	}
	if state != nil {
		state.PatternsDetected += len(patterns)
		state.ProposalsGenerated += len(proposals)
		state.LastAnalysis = time.Now().UTC().Format(time.RFC3339Nano)
		state.NextAnalysis = time.Now().Add(time.Duration(e.config.Interval) * time.Hour).UTC().Format(time.RFC3339Nano)

		// Calculate acceptance rate
		if state.ProposalsGenerated > 0 {
			state.AcceptanceRate = float64(state.ProposalsAccepted) / float64(state.ProposalsGenerated)
		}

		if err := e.database.UpdateOpenClawState(state); err != nil {
			log.Printf("openclaw: failed to update state: %v", err)
		}
	}

	log.Printf("openclaw: analysis complete: patterns=%d proposals=%d duration=%dms",
		len(patterns), len(proposals), executionTime)
	return nil
}

// Helper functions
func calculateAverage(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func calculateAverageInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	sum := 0
	for _, v := range values {
		sum += v
	}
	return sum / len(values)
}

func generateRuleContent(title string, rate float64, category string) string {
	return fmt.Sprintf(`# %s

## Status
Proposed

## Context
Analysis detected metric below optimal threshold: %.1f%%

## Pattern
Category: %s
Confidence: High
Frequency: Recurring

## Recommendation
1. Investigate root causes
2. Implement countermeasures
3. Monitor improvement
4. Document learnings

## Success Criteria
- Increase metric by 20%% within 30 days
- Maintain improvement for 7 consecutive days
- Document 3+ actionable insights

## Measurement
 Track daily and report weekly
`, title, rate*100, category)
}

func (e *Engine) savePatternIfNew(pattern *db.OpenClawPattern) error {
	existing, err := e.database.FindPatternByName(pattern.PatternName)
	if err != nil {
		return fmt.Errorf("check existing pattern: %w", err)
	}

	if existing != nil {
		existing.Frequency++
		existing.LastSeen = time.Now().UTC().Format(time.RFC3339Nano)
		if err := e.database.UpdateOpenClawPattern(existing); err != nil {
			return fmt.Errorf("update pattern: %w", err)
		}
		log.Printf("openclaw: updated existing pattern: name=%s frequency=%d", pattern.PatternName, existing.Frequency)
		return nil
	}

	if err := e.database.SaveOpenClawPattern(pattern); err != nil {
		return fmt.Errorf("save pattern: %w", err)
	}

	log.Printf("openclaw: detected new pattern: type=%s name=%s confidence=%.2f",
		pattern.PatternType, pattern.PatternName, pattern.Confidence)
	return nil
}
