package analytics

import (
	"math"
	"sort"
)

type TrendAnalyzer struct{}

func NewTrendAnalyzer() *TrendAnalyzer {
	return &TrendAnalyzer{}
}

func (ta *TrendAnalyzer) AnalyzeTrend(metricName string, metrics []DailyMetric) TrendAnalysis {
	if len(metrics) < 2 {
		return TrendAnalysis{
			MetricName: metricName,
			Direction:  TrendStable,
			Confidence: 0,
		}
	}

	values := ta.extractValues(metricName, metrics)
	if len(values) == 0 {
		return TrendAnalysis{
			MetricName: metricName,
			Direction:  TrendStable,
			Confidence: 0,
		}
	}

	slope := ta.calculateSlope(values)
	direction := ta.determineDirection(slope)
	confidence := ta.calculateConfidence(values, slope)
	predicted := ta.predictNext(values, slope)

	return TrendAnalysis{
		MetricName:     metricName,
		Direction:      direction,
		Slope:          slope,
		Confidence:     confidence,
		PredictedValue: predicted,
		CurrentValue:   values[len(values)-1],
		Period:         "7d",
	}
}

func (ta *TrendAnalyzer) extractValues(metricName string, metrics []DailyMetric) []float64 {
	var values []float64
	switch metricName {
	case "completed_workflows":
		for _, m := range metrics {
			values = append(values, float64(m.CompletedWorkflows))
		}
	case "total_workflows":
		for _, m := range metrics {
			values = append(values, float64(m.TotalWorkflows))
		}
	case "success_rate":
		for _, m := range metrics {
			values = append(values, m.SuccessRate)
		}
	case "total_tasks":
		for _, m := range metrics {
			values = append(values, float64(m.TotalTasks))
		}
	case "completed_tasks":
		for _, m := range metrics {
			values = append(values, float64(m.CompletedTasks))
		}
	case "avg_workflow_duration_ms":
		for _, m := range metrics {
			values = append(values, float64(m.AvgWorkflowDurationMs))
		}
	default:
		for _, m := range metrics {
			values = append(values, float64(m.CompletedWorkflows))
		}
	}
	return values
}

func (ta *TrendAnalyzer) calculateSlope(values []float64) float64 {
	n := float64(len(values))
	if n < 2 {
		return 0
	}

	sumX := 0.0
	sumY := 0.0
	sumXY := 0.0
	sumX2 := 0.0

	for i, y := range values {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denominator := n*sumX2 - sumX*sumX
	if denominator == 0 {
		return 0
	}

	slope := (n*sumXY - sumX*sumY) / denominator
	return slope
}

func (ta *TrendAnalyzer) determineDirection(slope float64) TrendDirection {
	threshold := 0.01
	if slope > threshold {
		return TrendUp
	} else if slope < -threshold {
		return TrendDown
	}
	return TrendStable
}

func (ta *TrendAnalyzer) calculateConfidence(values []float64, slope float64) float64 {
	if len(values) < 3 {
		return 0.3
	}

	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))

	if mean == 0 {
		return 0.5
	}

	ssTotal := 0.0
	ssRes := 0.0

	for i, y := range values {
		predicted := values[0] + slope*float64(i)
		ssRes += (y - predicted) * (y - predicted)
		ssTotal += (y - mean) * (y - mean)
	}

	if ssTotal == 0 {
		return 0.5
	}

	rSquared := 1 - (ssRes / ssTotal)
	confidence := math.Max(0.1, math.Min(0.95, rSquared))

	variance := ta.calculateVariance(values)
	normalizedVariance := variance / (mean * mean)
	if normalizedVariance > 0.5 {
		confidence *= 0.7
	}

	return confidence
}

func (ta *TrendAnalyzer) calculateVariance(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}

	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))

	variance := 0.0
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(values) - 1)

	return variance
}

func (ta *TrendAnalyzer) predictNext(values []float64, slope float64) float64 {
	if len(values) == 0 {
		return 0
	}

	lastValue := values[len(values)-1]
	predicted := lastValue + slope*7

	return math.Max(0, predicted)
}

func (ta *TrendAnalyzer) AnalyzeMultipleTrends(metrics []DailyMetric) []TrendAnalysis {
	metricNames := []string{
		"completed_workflows",
		"success_rate",
		"total_tasks",
		"avg_workflow_duration_ms",
	}

	var trends []TrendAnalysis
	for _, name := range metricNames {
		trend := ta.AnalyzeTrend(name, metrics)
		if trend.Confidence > 0.1 {
			trends = append(trends, trend)
		}
	}

	return trends
}

func (ta *TrendAnalyzer) GetTrendScore(trends []TrendAnalysis) float64 {
	if len(trends) == 0 {
		return 0.5
	}

	var totalScore float64
	var totalWeight float64

	for _, t := range trends {
		weight := t.Confidence
		var score float64

		switch t.MetricName {
		case "success_rate", "completed_workflows":
			if t.Direction == TrendUp {
				score = 0.8 + (0.2 * t.Confidence)
			} else if t.Direction == TrendDown {
				score = 0.2 + (0.2 * (1 - t.Confidence))
			} else {
				score = 0.5
			}
		case "avg_workflow_duration_ms":
			if t.Direction == TrendDown {
				score = 0.8 + (0.2 * t.Confidence)
			} else if t.Direction == TrendUp {
				score = 0.2 + (0.2 * (1 - t.Confidence))
			} else {
				score = 0.5
			}
		default:
			score = 0.5
		}

		totalScore += score * weight
		totalWeight += weight
	}

	if totalWeight == 0 {
		return 0.5
	}

	return totalScore / totalWeight
}

func (ta *TrendAnalyzer) GetTopPerformers(agentMetrics []map[string]any, limit int) []map[string]any {
	if len(agentMetrics) == 0 {
		return nil
	}

	sorted := make([]map[string]any, len(agentMetrics))
	copy(sorted, agentMetrics)

	sort.Slice(sorted, func(i, j int) bool {
		scoreI := ta.calculateAgentScore(sorted[i])
		scoreJ := ta.calculateAgentScore(sorted[j])
		return scoreI > scoreJ
	})

	if limit > len(sorted) {
		limit = len(sorted)
	}

	return sorted[:limit]
}

func (ta *TrendAnalyzer) calculateAgentScore(agent map[string]any) float64 {
	tasks, _ := agent["tasks_completed"].(int)
	successRate, _ := agent["success_rate"].(float64)

	return float64(tasks)*successRate + successRate*100
}
