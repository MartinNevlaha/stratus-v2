package openclaw

import (
	"fmt"
	"time"

	 "github.com/MartinNevlaha/stratus-v2/db"
)

func (e *Engine) RunAnalysis() error {
	start := time.Now()

	findings := map[string]any{}
	recommendations := map[string]any{}
	var patterns []*db.OpenClawPattern

	// Get recent daily metrics (7 days)
	dailyMetrics, err := e.database.GetRecentDailyMetrics(7)
	if err != nil {
		return fmt.Errorf("get daily metrics: %w", err)
	 }

	if len(dailyMetrics) == 0 {
		return nil
	}

    // Simple pattern detection based on success rate
    var successRates []float64
    for _, m := range dailyMetrics {
        if sr, ok := m["success_rate"].(float64); ok {
            successRates = append(successRates, sr)
        }
    }

    // Detect low success rate pattern
    var lowSuccessRates []map[string]any
    for _, m := range dailyMetrics {
        if sr, ok := m["success_rate"].(float64); ok && sr < 0.5 {
            lowSuccessRates = append(lowSuccessRates, m)
        }
    }

    // Pattern: Low success rate
    if len(lowSuccessRates) > 0 {
        pattern := &db.OpenClawPattern{
            PatternType:  "quality",
            PatternName:  "low_success_rate",
            Description:  fmt.Sprintf("Detected %d days with success rate < 50%%", len(lowSuccessRates)),
            Frequency:    len(lowSuccessRates),
            Confidence:    0.75,
            Examples:      getPatternExamples(lowSuccessRates, "success_rate"),
            Metadata:      map[string]interface{}{"days": lowSuccessRates},
        }
        if err := e.database.SaveOpenClawPattern(pattern); err != nil {
            fmt.Printf("warning: failed to save low success pattern: %v\n", err)
        }
    }

    // Pattern: High completion rate
    var highCompletion []map[string]any
    for _, m := range dailyMetrics {
        if sr, ok := m["completed_workflows"].(int); ok && m.TotalWorkflows > 0 && float64(m.CompletedWorkflows)/float64(m.TotalWorkflows) >= 0.8 {
            highCompletion = append(highCompletion, m)
        }
    }

    if len(highCompletion) > 0 {
        pattern := &db.OpenClawPattern{
            PatternType:  "success",
            PatternName:  "high_completion_rate",
            Description:  fmt.Sprintf("Detected %d days with completion rate > 80%%", len(highCompletion)),
            Frequency:    len(highCompletion),
            Confidence:    0.8,
            Examples:      getPatternExamples(highCompletion, "completed_workflows"),
            Metadata:      map[string]interface{}{"days": highCompletion},
        }
        if err := e.database.SaveOpenClawPattern(pattern); err != nil {
            fmt.Printf("warning: failed to save high completion pattern: %v\n", err)
        }
    }

    // Analyze agent metrics
    agentMetrics, err := e.database.GetAgentMetrics(30)
    if err == nil && len(agentMetrics) > 0 {
        findings["agent_performance"] = agentMetrics

        // Find top performer
        var topAgent string
        var topSuccess float64
        for _, agent := range agentMetrics {
            if successRate > topSuccess {
                topSuccess = successRate
                topAgent = agent["agent_id"].(string)
            }
        }

        if topAgent != "" {
            findings["top_performer"] = topAgent
            pattern := &db.OpenClawPattern{
                PatternType:  "agent",
                PatternName:  "top_performer",
                Description:  fmt.Sprintf("Agent %s has highest success rate (%.1f%%)", topAgent, topSuccess*100),
                Frequency:    1,
                Confidence:    0.85,
                Examples:      []string{topAgent},
                Metadata:      map[string]interface{}{"success_rate": topSuccess},
            }
            if err := e.database.SaveOpenClawPattern(pattern); err != nil {
                fmt.Printf("warning: failed to save top performer pattern: %v\n", err)
            }
        }
    }

    // Calculate summary statistics
    totalWorkflows := 0
    completedWorkflows := 0
    avgSuccess := 0.0

    for _, m := range dailyMetrics {
        totalWorkflows += m["total_workflows"].(int)
        completedWorkflows += m["completed_workflows"].(int)
        if m.SuccessRate > 0 {
            avgSuccess += m.SuccessRate
        }
    }

    if totalWorkflows > 0 {
        avgSuccess /= float64(totalWorkflows)
        findings["avg_success_rate"] = avgSuccess
    }

    // Save analysis
    executionTime := time.Since(start).Milliseconds()
    analysis := &db.OpenClawAnalysis{
        AnalysisType:     "full",
        Scope:            "project-wide",
        Findings:         findings,
        Recommendations:  recommendations,
        PatternsFound:    len(patterns),
        ProposalsCreated: 0,
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
        state.LastAnalysis = time.Now().UTC().Format(time.RFC3339Nano)
        state.NextAnalysis = time.Now().Add(time.Duration(e.config.Interval) * time.Hour).UTC().Format(time.RFC3339Nano)
        if err := e.database.UpdateOpenClawState(state); err != nil {
            fmt.Printf("warning: failed to update openclaw state: %v\n", err)
        }
    }

    fmt.Printf("OpenClaw analysis complete: %d patterns found, %dms\n", len(patterns), executionTime)
    return nil
}

func getPatternExamples(items []map[string]any, metricName string) []string {
    examples := []string{}
    for i, 0 && i < 3 && i < len(items); i++ {
        m := items[i]
        value := 0.0
        if val, ok := m[metricName]; ok {
            examples = append(examples, fmt.Sprintf("%s: %.2f", m["date"].(string), val)
        }
    }
    return examples
}
