package analytics

import (
	"fmt"
	"time"
)

type Predictor struct {
	analyzer *TrendAnalyzer
}

func NewPredictor() *Predictor {
	return &Predictor{
		analyzer: NewTrendAnalyzer(),
	}
}

func (p *Predictor) GeneratePredictions(dailyMetrics []DailyMetric) []Prediction {
	if len(dailyMetrics) < 2 {
		return nil
	}

	predictions := []Prediction{}

	// Workflow completion predictions
	completionTrend := p.analyzer.AnalyzeTrend("completed_workflows", dailyMetrics)
	if completionTrend.Confidence > 0.3 {
		predicted := p.predictNext7Days(dailyMetrics, completionTrend)
		predictions = append(predictions, Prediction{
			MetricName:     "workflow_completion_7d",
			CurrentValue:   completionTrend.CurrentValue,
			PredictedValue: predicted,
			Confidence:     completionTrend.Confidence,
			PredictionDate: time.Now().AddDate(0, 0, 7),
			Insight:        fmt.Sprintf("Expected to complete %.0f workflows in next 7 days", predicted),
			Recommendation: generateWorkflowRecommendation(completionTrend),
		})
	}

	// Success rate predictions
	successTrend := p.analyzer.AnalyzeTrend("success_rate", dailyMetrics)
	if successTrend.Confidence > 0.3 {
		predictions = append(predictions, Prediction{
			MetricName:     "success_rate_7d",
			CurrentValue:   successTrend.CurrentValue,
			PredictedValue: successTrend.PredictedValue,
			Confidence:     successTrend.Confidence,
			PredictionDate: time.Now().AddDate(0, 0, 7),
			Insight:        generateSuccessInsight(successTrend),
			Recommendation: generateSuccessRecommendation(successTrend),
		})
	}

	return predictions
}

func (p *Predictor) predictNext7Days(metrics []DailyMetric, trend TrendAnalysis) float64 {
	if len(metrics) == 0 {
		return 0
	}

	// Use linear extrapolation
	lastValue := float64(metrics[len(metrics)-1].CompletedWorkflows)
	predicted := lastValue + (trend.Slope * 7)

	if predicted < 0 {
		predicted = 0
	}

	return predicted
}

func (p *Predictor) GenerateBottleneckPrediction(metrics []DailyMetric) *Prediction {
	if len(metrics) < 2 {
		return nil
	}

	// Calculate average duration per phase
	phaseCounts := make(map[string]int)

	for range metrics {
		for _, phase := range []string{"plan", "implement", "verify", "learn"} {
			phaseCounts[phase]++
		}
	}

	if len(phaseCounts) == 0 {
		return nil
	}

	// Find bottleneck
	maxPhase := ""
	maxCount := 0
	for phase, count := range phaseCounts {
		if count > maxCount {
			maxCount = count
			maxPhase = phase
		}
	}

	avgDuration := float64(maxCount) / float64(len(metrics))

	return &Prediction{
		MetricName:     "bottleneck_phase",
		CurrentValue:   float64(len(metrics)),
		PredictedValue: avgDuration,
		Confidence:     0.65,
		PredictionDate: time.Now().AddDate(0, 0, 7),
		Insight:        fmt.Sprintf("%s phase is the bottleneck (%.1fx more work items)", maxPhase, avgDuration),
		Recommendation: fmt.Sprintf("Consider optimizing %s phase or adding more resources", maxPhase),
	}
}

func generateSuccessInsight(trend TrendAnalysis) string {
	if trend.Direction == TrendUp {
		return fmt.Sprintf("Success rate improving (+%.1f%%). Great progress!", trend.Slope)
	} else if trend.Direction == TrendDown {
		return fmt.Sprintf("Success rate declining (%.1f%%). Investigate root causes", trend.Slope)
	}
	return "Success rate stable. Continue monitoring."
}

func generateSuccessRecommendation(trend TrendAnalysis) string {
	if trend.Direction == TrendDown {
		return "Review recent failures, check for common patterns, consider adding validation steps"
	}
	return "Maintain current practices and quality standards"
}

func generateWorkflowRecommendation(trend TrendAnalysis) string {
	if trend.Direction == TrendUp {
		return "Team velocity increasing - consider taking on more complex tasks"
	} else if trend.Direction == TrendDown {
		return "Velocity decreasing - check for blockers and provide support"
	}
	return "Maintain current workflow pace"
}
