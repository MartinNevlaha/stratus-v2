package api

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/MartinNevlaha/stratus-v2/analytics"
)

func (s *Server) handleGetMetricsSummary(w http.ResponseWriter, r *http.Request) {
	days := queryInt(r, "days", 7)
	if days < 1 {
		days = 1
	}

	if days > 365 {
		days = 365
	}

	summary, err := s.db.GetMetricsSummary(days)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"period_days": days,
		"summary":     summary,
	})
}

func (s *Server) handleGetDailyMetrics(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	if limit < 1 {
		limit = 1
	}
	if limit > 365 {
		limit = 365
	}

	metrics, err := s.db.GetRecentDailyMetrics(limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if metrics == nil {
		metrics = []map[string]any{}
	}

	json200(w, map[string]any{
		"metrics": metrics,
	})
}

func (s *Server) handleGetAgentMetrics(w http.ResponseWriter, r *http.Request) {
	days := queryInt(r, "days", 30)
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}

	agentMetrics, err := s.db.GetAgentMetrics(days)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if agentMetrics == nil {
		agentMetrics = []map[string]any{}
	}

	json200(w, map[string]any{
		"agents": agentMetrics,
	})
}

func (s *Server) handleGetWorkflowMetrics(w http.ResponseWriter, r *http.Request) {
	workflowID := r.PathValue("id")
	if workflowID == "" {
		jsonErr(w, http.StatusBadRequest, "workflow id required")
		return
	}

	metrics, err := s.db.GetWorkflowMetrics(workflowID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"workflow_id": workflowID,
		"metrics":     metrics,
	})
}

func (s *Server) handleExportMetrics(w http.ResponseWriter, r *http.Request) {
	days := queryInt(r, "days", 30)
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}

	dailyMetrics, err := s.db.GetRecentDailyMetrics(days)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment;filename=metrics.csv")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	if err := writer.Write([]string{
		"Date",
		"Total Workflows",
		"Completed Workflows",
		"Avg Duration (ms)",
		"Total Tasks",
		"Completed Tasks",
		"Success Rate",
	}); err != nil {
		log.Printf("csv write error: %v", err)
		return
	}

	for _, m := range dailyMetrics {
		date, _ := m["date"].(string)
		totalWf, _ := m["total_workflows"].(int)
		completedWf, _ := m["completed_workflows"].(int)
		avgDuration, _ := m["avg_workflow_duration_ms"].(int)
		totalTasks, _ := m["total_tasks"].(int)
		completedTasks, _ := m["completed_tasks"].(int)
		successRate, _ := m["success_rate"].(float64)

		if err := writer.Write([]string{
			date,
			strconv.Itoa(totalWf),
			strconv.Itoa(completedWf),
			strconv.Itoa(avgDuration),
			strconv.Itoa(totalTasks),
			strconv.Itoa(completedTasks),
			fmt.Sprintf("%.2f", successRate),
		}); err != nil {
			log.Printf("csv write error: %v", err)
			return
		}
	}
}

func (s *Server) handleTriggerAggregation(w http.ResponseWriter, r *http.Request) {
	if err := s.db.AggregateYesterday(); err != nil {
		log.Printf("warning: failed to aggregate yesterday: %v", err)
	}

	if err := s.db.AggregateAllMissing(); err != nil {
		log.Printf("warning: failed to aggregate missing metrics: %v", err)
	}

	json200(w, map[string]any{
		"status": "aggregation_complete",
	})
}

func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	reportType := r.PathValue("type")
	if reportType == "" {
		reportType = "weekly"
	}

	days := queryInt(r, "days", 30)
	if days < 1 {
		days = 1
	}
	if days > 90 {
		days = 90
	}

	dailyMetrics, err := s.db.GetRecentDailyMetrics(days)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	agentMetrics, err := s.db.GetAgentMetrics(days)
	if err != nil {
		log.Printf("warning: failed to get agent metrics: %v", err)
	}

	analyticsMetrics := convertToAnalyticsMetrics(dailyMetrics)

	generator := analytics.NewReportGenerator()

	var report *analytics.Report
	switch reportType {
	case "daily":
		if len(analyticsMetrics) > 0 {
			report = generator.GenerateDailyReport(analyticsMetrics)
		}
	case "weekly":
		report = generator.GenerateWeeklyReport(analyticsMetrics, agentMetrics)
	case "monthly":
		report = generator.GenerateMonthlyReport(analyticsMetrics, agentMetrics)
	default:
		report = generator.GenerateWeeklyReport(analyticsMetrics, agentMetrics)
	}

	if report == nil {
		jsonErr(w, http.StatusNotFound, "no data available for report")
		return
	}

	insights := generator.GetReportInsights(report)

	json200(w, map[string]any{
		"report":   report,
		"insights": insights,
	})
}

func (s *Server) handleGetPredictions(w http.ResponseWriter, r *http.Request) {
	days := queryInt(r, "days", 30)
	if days < 1 {
		days = 1
	}
	if days > 90 {
		days = 90
	}

	dailyMetrics, err := s.db.GetRecentDailyMetrics(days)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	analyticsMetrics := convertToAnalyticsMetrics(dailyMetrics)

	predictor := analytics.NewPredictor()
	predictions := predictor.GeneratePredictions(analyticsMetrics)

	bottleneck := predictor.GenerateBottleneckPrediction(analyticsMetrics)
	if bottleneck != nil {
		predictions = append(predictions, *bottleneck)
	}

	json200(w, map[string]any{
		"predictions": predictions,
		"count":       len(predictions),
	})
}

func (s *Server) handleGetAnomalies(w http.ResponseWriter, r *http.Request) {
	days := queryInt(r, "days", 30)
	if days < 1 {
		days = 1
	}
	if days > 90 {
		days = 90
	}

	dailyMetrics, err := s.db.GetRecentDailyMetrics(days)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	agentMetrics, err := s.db.GetAgentMetrics(days)
	if err != nil {
		log.Printf("warning: failed to get agent metrics: %v", err)
	}

	analyticsMetrics := convertToAnalyticsMetrics(dailyMetrics)

	detector := analytics.NewAnomalyDetector()

	anomalies := detector.DetectAnomalies(analyticsMetrics)
	agentAnomalies := detector.DetectAgentAnomalies(agentMetrics)

	anomalies = append(anomalies, agentAnomalies...)

	summary := detector.GetAnomalySummary(anomalies)
	shouldAlert := detector.ShouldAlert(anomalies)
	alertMessage := ""
	if shouldAlert {
		alertMessage = detector.GenerateAlertMessage(anomalies)
	}

	json200(w, map[string]any{
		"anomalies":    anomalies,
		"count":        len(anomalies),
		"summary":      summary,
		"should_alert": shouldAlert,
		"alert":        alertMessage,
	})
}

func (s *Server) handleGetTrends(w http.ResponseWriter, r *http.Request) {
	days := queryInt(r, "days", 30)
	if days < 1 {
		days = 1
	}
	if days > 90 {
		days = 90
	}

	dailyMetrics, err := s.db.GetRecentDailyMetrics(days)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	analyticsMetrics := convertToAnalyticsMetrics(dailyMetrics)

	analyzer := analytics.NewTrendAnalyzer()
	trends := analyzer.AnalyzeMultipleTrends(analyticsMetrics)
	trendScore := analyzer.GetTrendScore(trends)

	agentMetrics, err := s.db.GetAgentMetrics(days)
	if err != nil {
		log.Printf("warning: failed to get agent metrics: %v", err)
	}

	topPerformers := analyzer.GetTopPerformers(agentMetrics, 5)

	json200(w, map[string]any{
		"trends":         trends,
		"trend_score":    trendScore,
		"top_performers": topPerformers,
	})
}

func convertToAnalyticsMetrics(dbMetrics []map[string]any) []analytics.DailyMetric {
	result := make([]analytics.DailyMetric, 0, len(dbMetrics))

	for _, m := range dbMetrics {
		dm := analytics.DailyMetric{}

		if v, ok := m["date"].(string); ok {
			dm.Date = v
		}
		if v, ok := m["total_workflows"].(int); ok {
			dm.TotalWorkflows = v
		}
		if v, ok := m["completed_workflows"].(int); ok {
			dm.CompletedWorkflows = v
		}
		if v, ok := m["avg_workflow_duration_ms"].(int); ok {
			dm.AvgWorkflowDurationMs = v
		}
		if v, ok := m["total_tasks"].(int); ok {
			dm.TotalTasks = v
		}
		if v, ok := m["completed_tasks"].(int); ok {
			dm.CompletedTasks = v
		}
		if v, ok := m["success_rate"].(float64); ok {
			dm.SuccessRate = v
		}

		result = append(result, dm)
	}

	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}
