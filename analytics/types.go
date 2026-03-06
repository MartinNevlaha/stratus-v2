package analytics

import "time"

type DailyMetric struct {
	Date                  string  `json:"date"`
	TotalWorkflows        int     `json:"total_workflows"`
	CompletedWorkflows    int     `json:"completed_workflows"`
	AvgWorkflowDurationMs int     `json:"avg_workflow_duration_ms"`
	TotalTasks            int     `json:"total_tasks"`
	CompletedTasks        int     `json:"completed_tasks"`
	SuccessRate           float64 `json:"success_rate"`
}

type TrendDirection string

const (
	TrendUp     TrendDirection = "up"
	TrendDown   TrendDirection = "down"
	TrendStable TrendDirection = "stable"
)

type TrendAnalysis struct {
	MetricName     string         `json:"metric_name"`
	Direction      TrendDirection `json:"direction"`
	Slope          float64        `json:"slope"`
	Confidence     float64        `json:"confidence"`
	PredictedValue float64        `json:"predicted_value"`
	CurrentValue   float64        `json:"current_value"`
	Period         string         `json:"period"`
}

type AnomalyType string

const (
	AnomalySpike   AnomalyType = "spike"
	AnomalyDrop    AnomalyType = "drop"
	AnomalyOutlier AnomalyType = "outlier"
)

type Anomaly struct {
	ID            string      `json:"id"`
	Type          AnomalyType `json:"type"`
	MetricName    string      `json:"metric_name"`
	ActualValue   float64     `json:"actual_value"`
	ExpectedValue float64     `json:"expected_value"`
	Deviation     float64     `json:"deviation"`
	Severity      string      `json:"severity"`
	DetectedAt    time.Time   `json:"detected_at"`
	Description   string      `json:"description"`
}

type Prediction struct {
	MetricName     string    `json:"metric_name"`
	CurrentValue   float64   `json:"current_value"`
	PredictedValue float64   `json:"predicted_value"`
	Confidence     float64   `json:"confidence"`
	PredictionDate time.Time `json:"prediction_date"`
	Insight        string    `json:"insight"`
	Recommendation string    `json:"recommendation"`
}

type ReportSummary struct {
	Text               string  `json:"text"`
	TotalWorkflows     int     `json:"total_workflows"`
	CompletedWorkflows int     `json:"completed_workflows"`
	SuccessRate        float64 `json:"success_rate"`
	AvgDuration        int     `json:"avg_duration_ms"`
	Improvement        float64 `json:"improvement"`
}

type Report struct {
	ID                  string           `json:"id"`
	Type                string           `json:"type"`
	GeneratedAt         time.Time        `json:"generated_at"`
	Period              string           `json:"period"`
	PeriodStart         time.Time        `json:"period_start"`
	PeriodEnd           time.Time        `json:"period_end"`
	Summary             ReportSummary    `json:"summary"`
	Trends              []TrendAnalysis  `json:"trends"`
	Anomalies           []Anomaly        `json:"anomalies"`
	Predictions         []Prediction     `json:"predictions"`
	TopPerformers       []map[string]any `json:"top_performers"`
	TotalWorkflows      int              `json:"total_workflows"`
	CompletedWorkflows  int              `json:"completed_workflows"`
	TotalTasks          int              `json:"total_tasks"`
	CompletedTasks      int              `json:"completed_tasks"`
	CompletionRate      float64          `json:"completion_rate"`
	AvgDuration         int              `json:"avg_duration_ms"`
	SuccessRate         float64          `json:"success_rate"`
	ImprovementFromLast float64          `json:"improvement_from_last"`
}
