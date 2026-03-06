package analytics

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ReportGenerator struct {
	analyzer *TrendAnalyzer
	detector *AnomalyDetector
}

func NewReportGenerator() *ReportGenerator {
	return &ReportGenerator{
		analyzer: NewTrendAnalyzer(),
		detector: NewAnomalyDetector(),
	}
}

func (rg *ReportGenerator) GenerateWeeklyReport(metrics []DailyMetric, agentMetrics []map[string]any) *Report {
	if len(metrics) == 0 {
		return nil
	}

	now := time.Now()
	periodStart := now.AddDate(0, 0, -7)

	report := &Report{
		ID:          uuid.New().String()[:8],
		Type:        "weekly",
		GeneratedAt: now,
		Period:      "7d",
		PeriodStart: periodStart,
		PeriodEnd:   now,
	}

	report.TotalWorkflows = rg.sumTotalWorkflows(metrics)
	report.CompletedWorkflows = rg.sumCompletedWorkflows(metrics)
	report.TotalTasks = rg.sumTotalTasks(metrics)
	report.CompletedTasks = rg.sumCompletedTasks(metrics)

	if report.TotalWorkflows > 0 {
		report.CompletionRate = float64(report.CompletedWorkflows) / float64(report.TotalWorkflows)
	}
	if report.TotalTasks > 0 {
		report.SuccessRate = float64(report.CompletedTasks) / float64(report.TotalTasks)
	}
	report.AvgDuration = rg.calculateAvgDuration(metrics)

	report.Trends = rg.analyzer.AnalyzeMultipleTrends(metrics)
	report.Anomalies = rg.detector.DetectAnomalies(metrics)
	report.Predictions = NewPredictor().GeneratePredictions(metrics)
	report.TopPerformers = rg.analyzer.GetTopPerformers(agentMetrics, 5)

	report.Summary = rg.generateSummary(report)
	report.ImprovementFromLast = rg.calculateImprovement(metrics)

	return report
}

func (rg *ReportGenerator) GenerateMonthlyReport(metrics []DailyMetric, agentMetrics []map[string]any) *Report {
	if len(metrics) == 0 {
		return nil
	}

	now := time.Now()
	periodStart := now.AddDate(0, -1, 0)

	report := &Report{
		ID:          uuid.New().String()[:8],
		Type:        "monthly",
		GeneratedAt: now,
		Period:      "30d",
		PeriodStart: periodStart,
		PeriodEnd:   now,
	}

	report.TotalWorkflows = rg.sumTotalWorkflows(metrics)
	report.CompletedWorkflows = rg.sumCompletedWorkflows(metrics)
	report.TotalTasks = rg.sumTotalTasks(metrics)
	report.CompletedTasks = rg.sumCompletedTasks(metrics)

	if report.TotalWorkflows > 0 {
		report.CompletionRate = float64(report.CompletedWorkflows) / float64(report.TotalWorkflows)
	}
	if report.TotalTasks > 0 {
		report.SuccessRate = float64(report.CompletedTasks) / float64(report.TotalTasks)
	}
	report.AvgDuration = rg.calculateAvgDuration(metrics)

	report.Trends = rg.analyzer.AnalyzeMultipleTrends(metrics)
	report.Anomalies = rg.detector.DetectAnomalies(metrics)
	report.Predictions = NewPredictor().GeneratePredictions(metrics)
	report.TopPerformers = rg.analyzer.GetTopPerformers(agentMetrics, 10)

	report.Summary = rg.generateSummary(report)
	report.ImprovementFromLast = rg.calculateImprovement(metrics)

	return report
}

func (rg *ReportGenerator) GenerateDailyReport(metrics []DailyMetric) *Report {
	if len(metrics) == 0 {
		return nil
	}

	today := metrics[len(metrics)-1]

	report := &Report{
		ID:          uuid.New().String()[:8],
		Type:        "daily",
		GeneratedAt: time.Now(),
		Period:      "1d",
		PeriodStart: time.Now().AddDate(0, 0, -1),
		PeriodEnd:   time.Now(),
	}

	report.TotalWorkflows = today.TotalWorkflows
	report.CompletedWorkflows = today.CompletedWorkflows
	report.TotalTasks = today.TotalTasks
	report.CompletedTasks = today.CompletedTasks
	report.SuccessRate = today.SuccessRate
	report.AvgDuration = today.AvgWorkflowDurationMs

	if report.TotalWorkflows > 0 {
		report.CompletionRate = float64(report.CompletedWorkflows) / float64(report.TotalWorkflows)
	}

	report.Summary = ReportSummary{
		Text:               rg.generateDailySummaryText(today),
		TotalWorkflows:     today.TotalWorkflows,
		CompletedWorkflows: today.CompletedWorkflows,
		SuccessRate:        today.SuccessRate,
		AvgDuration:        today.AvgWorkflowDurationMs,
		Improvement:        0,
	}

	return report
}

func (rg *ReportGenerator) generateSummary(report *Report) ReportSummary {
	text := fmt.Sprintf(
		"Completed %d/%d workflows (%.1f%%) with %.1f%% success rate. Average duration: %dms.",
		report.CompletedWorkflows, report.TotalWorkflows, report.CompletionRate*100,
		report.SuccessRate*100, report.AvgDuration,
	)

	improvement := 0.0
	if report.ImprovementFromLast > 0 {
		improvement = report.ImprovementFromLast * 100
		text += fmt.Sprintf(" Improved by %.1f%% from last period.", improvement)
	} else if report.ImprovementFromLast < 0 {
		text += fmt.Sprintf(" Decreased by %.1f%% from last period.", -report.ImprovementFromLast*100)
	}

	if len(report.Anomalies) > 0 {
		text += fmt.Sprintf(" %d anomalies detected.", len(report.Anomalies))
	}

	return ReportSummary{
		Text:               text,
		TotalWorkflows:     report.TotalWorkflows,
		CompletedWorkflows: report.CompletedWorkflows,
		SuccessRate:        report.SuccessRate,
		AvgDuration:        report.AvgDuration,
		Improvement:        improvement,
	}
}

func (rg *ReportGenerator) generateDailySummaryText(m DailyMetric) string {
	return fmt.Sprintf(
		"Today: %d/%d workflows completed, %d/%d tasks done (%.1f%% success).",
		m.CompletedWorkflows, m.TotalWorkflows,
		m.CompletedTasks, m.TotalTasks,
		m.SuccessRate*100,
	)
}

func (rg *ReportGenerator) sumTotalWorkflows(metrics []DailyMetric) int {
	sum := 0
	for _, m := range metrics {
		sum += m.TotalWorkflows
	}
	return sum
}

func (rg *ReportGenerator) sumCompletedWorkflows(metrics []DailyMetric) int {
	sum := 0
	for _, m := range metrics {
		sum += m.CompletedWorkflows
	}
	return sum
}

func (rg *ReportGenerator) sumTotalTasks(metrics []DailyMetric) int {
	sum := 0
	for _, m := range metrics {
		sum += m.TotalTasks
	}
	return sum
}

func (rg *ReportGenerator) sumCompletedTasks(metrics []DailyMetric) int {
	sum := 0
	for _, m := range metrics {
		sum += m.CompletedTasks
	}
	return sum
}

func (rg *ReportGenerator) calculateAvgDuration(metrics []DailyMetric) int {
	if len(metrics) == 0 {
		return 0
	}

	sum := 0
	count := 0
	for _, m := range metrics {
		if m.AvgWorkflowDurationMs > 0 {
			sum += m.AvgWorkflowDurationMs
			count++
		}
	}

	if count == 0 {
		return 0
	}
	return sum / count
}

func (rg *ReportGenerator) calculateImprovement(metrics []DailyMetric) float64 {
	if len(metrics) < 7 {
		return 0
	}

	recent := metrics[len(metrics)-3:]
	previous := metrics[len(metrics)-7 : len(metrics)-4]

	recentSuccess := rg.calculateAvgSuccess(recent)
	previousSuccess := rg.calculateAvgSuccess(previous)

	if previousSuccess == 0 {
		return 0
	}

	return (recentSuccess - previousSuccess) / previousSuccess
}

func (rg *ReportGenerator) calculateAvgSuccess(metrics []DailyMetric) float64 {
	if len(metrics) == 0 {
		return 0
	}

	sum := 0.0
	for _, m := range metrics {
		sum += m.SuccessRate
	}
	return sum / float64(len(metrics))
}

func (rg *ReportGenerator) GetReportInsights(report *Report) []string {
	insights := []string{}

	if report.CompletionRate > 0.9 {
		insights = append(insights, "Excellent workflow completion rate (>90%)")
	} else if report.CompletionRate < 0.5 {
		insights = append(insights, "Low workflow completion rate - investigate bottlenecks")
	}

	if report.SuccessRate > 0.95 {
		insights = append(insights, "High task success rate - quality is excellent")
	} else if report.SuccessRate < 0.7 {
		insights = append(insights, "Low task success rate - review quality processes")
	}

	if report.ImprovementFromLast > 0.1 {
		insights = append(insights, "Significant improvement from last period")
	} else if report.ImprovementFromLast < -0.1 {
		insights = append(insights, "Performance declined - take corrective action")
	}

	if len(report.Anomalies) > 3 {
		insights = append(insights, "Multiple anomalies detected - review system stability")
	}

	for _, trend := range report.Trends {
		if trend.Direction == TrendUp && trend.MetricName == "success_rate" {
			insights = append(insights, "Success rate trending upward")
		}
		if trend.Direction == TrendDown && trend.MetricName == "success_rate" {
			insights = append(insights, "Warning: Success rate trending downward")
		}
	}

	return insights
}
