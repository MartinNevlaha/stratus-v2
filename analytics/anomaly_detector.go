package analytics

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

type AnomalyDetector struct {
	threshold float64
}

func NewAnomalyDetector() *AnomalyDetector {
	return &AnomalyDetector{
		threshold: 2.0,
	}
}

func (ad *AnomalyDetector) DetectAnomalies(metrics []DailyMetric) []Anomaly {
	if len(metrics) < 3 {
		return nil
	}

	anomalies := []Anomaly{}

	anomalies = append(anomalies, ad.detectWorkflowAnomalies(metrics)...)
	anomalies = append(anomalies, ad.detectSuccessRateAnomalies(metrics)...)
	anomalies = append(anomalies, ad.detectTaskAnomalies(metrics)...)

	return anomalies
}

func (ad *AnomalyDetector) detectWorkflowAnomalies(metrics []DailyMetric) []Anomaly {
	values := make([]float64, len(metrics))
	for i, m := range metrics {
		values[i] = float64(m.TotalWorkflows)
	}

	return ad.detectMetricAnomalies(values, "total_workflows", metrics)
}

func (ad *AnomalyDetector) detectSuccessRateAnomalies(metrics []DailyMetric) []Anomaly {
	values := make([]float64, len(metrics))
	for i, m := range metrics {
		values[i] = m.SuccessRate
	}

	return ad.detectMetricAnomalies(values, "success_rate", metrics)
}

func (ad *AnomalyDetector) detectTaskAnomalies(metrics []DailyMetric) []Anomaly {
	values := make([]float64, len(metrics))
	for i, m := range metrics {
		values[i] = float64(m.TotalTasks)
	}

	return ad.detectMetricAnomalies(values, "total_tasks", metrics)
}

func (ad *AnomalyDetector) detectMetricAnomalies(values []float64, metricName string, metrics []DailyMetric) []Anomaly {
	if len(values) < 3 {
		return nil
	}

	anomalies := []Anomaly{}

	mean := ad.calculateMean(values)
	stdDev := ad.calculateStdDev(values, mean)

	if stdDev == 0 {
		return nil
	}

	for i, value := range values {
		zScore := math.Abs(value-mean) / stdDev

		if zScore > ad.threshold {
			anomalyType := AnomalySpike
			if value < mean {
				anomalyType = AnomalyDrop
			}

			severity := "medium"
			if zScore > 3.0 {
				severity = "high"
			} else if zScore > 4.0 {
				severity = "critical"
			}

			anomaly := Anomaly{
				ID:            uuid.New().String()[:8],
				Type:          anomalyType,
				MetricName:    metricName,
				ActualValue:   value,
				ExpectedValue: mean,
				Deviation:     zScore,
				Severity:      severity,
				DetectedAt:    time.Now(),
				Description:   fmt.Sprintf("%s: %.2f (expected: %.2f, z-score: %.2f)", metricName, value, mean, zScore),
			}

			if i < len(metrics) {
				anomaly.Description = fmt.Sprintf("%s on %s", anomaly.Description, metrics[i].Date)
			}

			anomalies = append(anomalies, anomaly)
		}
	}

	return anomalies
}

func (ad *AnomalyDetector) calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (ad *AnomalyDetector) calculateStdDev(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}

	variance := 0.0
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(values) - 1)

	return math.Sqrt(variance)
}

func (ad *AnomalyDetector) DetectAgentAnomalies(agentMetrics []map[string]any) []Anomaly {
	if len(agentMetrics) < 2 {
		return nil
	}

	anomalies := []Anomaly{}

	successRates := make([]float64, len(agentMetrics))
	for i, agent := range agentMetrics {
		if sr, ok := agent["success_rate"].(float64); ok {
			successRates[i] = sr
		}
	}

	mean := ad.calculateMean(successRates)
	stdDev := ad.calculateStdDev(successRates, mean)

	for i, agent := range agentMetrics {
		agentID, _ := agent["agent_id"].(string)
		successRate := successRates[i]

		if stdDev > 0 {
			zScore := math.Abs(successRate-mean) / stdDev

			if zScore > ad.threshold {
				severity := "medium"
				if successRate < mean {
					severity = "high"
				}

				anomalies = append(anomalies, Anomaly{
					ID:            uuid.New().String()[:8],
					Type:          AnomalyOutlier,
					MetricName:    "agent_success_rate",
					ActualValue:   successRate,
					ExpectedValue: mean,
					Deviation:     zScore,
					Severity:      severity,
					DetectedAt:    time.Now(),
					Description:   fmt.Sprintf("Agent %s has unusual success rate: %.2f%% (expected: %.2f%%)", agentID, successRate*100, mean*100),
				})
			}
		}
	}

	return anomalies
}

func (ad *AnomalyDetector) GetAnomalySummary(anomalies []Anomaly) map[string]any {
	if len(anomalies) == 0 {
		return map[string]any{
			"total":       0,
			"by_type":     map[string]int{},
			"by_severity": map[string]int{},
		}
	}

	byType := make(map[string]int)
	bySeverity := make(map[string]int)

	for _, a := range anomalies {
		byType[string(a.Type)]++
		bySeverity[a.Severity]++
	}

	return map[string]any{
		"total":        len(anomalies),
		"by_type":      byType,
		"by_severity":  bySeverity,
		"has_critical": bySeverity["critical"] > 0,
		"has_high":     bySeverity["high"] > 0,
	}
}

func (ad *AnomalyDetector) ShouldAlert(anomalies []Anomaly) bool {
	for _, a := range anomalies {
		if a.Severity == "critical" || a.Severity == "high" {
			return true
		}
	}
	return false
}

func (ad *AnomalyDetector) GenerateAlertMessage(anomalies []Anomaly) string {
	if len(anomalies) == 0 {
		return ""
	}

	critical := 0
	high := 0
	for _, a := range anomalies {
		if a.Severity == "critical" {
			critical++
		} else if a.Severity == "high" {
			high++
		}
	}

	if critical > 0 {
		return fmt.Sprintf("CRITICAL: %d critical anomalies detected. Immediate attention required.", critical)
	}
	if high > 0 {
		return fmt.Sprintf("WARNING: %d high-severity anomalies detected. Review recommended.", high)
	}
	return fmt.Sprintf("INFO: %d minor anomalies detected.", len(anomalies))
}
