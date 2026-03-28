package scorecards

import (
	"time"

	"github.com/google/uuid"
)

type Window string

const (
	Window7d  Window = "7d"
	Window30d Window = "30d"
)

type Trend string

const (
	TrendImproving Trend = "improving"
	TrendDegrading Trend = "degrading"
	TrendStable    Trend = "stable"
)

type AgentScorecard struct {
	ID              string    `json:"id"`
	AgentName       string    `json:"agent_name"`
	Window          Window    `json:"window"`
	WindowStart     time.Time `json:"window_start"`
	WindowEnd       time.Time `json:"window_end"`
	TotalRuns       int       `json:"total_runs"`
	SuccessRate     float64   `json:"success_rate"`
	FailureRate     float64   `json:"failure_rate"`
	ReviewPassRate  float64   `json:"review_pass_rate"`
	ReworkRate      float64   `json:"rework_rate"`
	AvgCycleTimeMs  int64     `json:"avg_cycle_time_ms"`
	RegressionRate  float64   `json:"regression_rate"`
	ConfidenceScore float64   `json:"confidence_score"`
	Trend           Trend     `json:"trend"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type WorkflowScorecard struct {
	ID                  string    `json:"id"`
	WorkflowType        string    `json:"workflow_type"`
	Window              Window    `json:"window"`
	WindowStart         time.Time `json:"window_start"`
	WindowEnd           time.Time `json:"window_end"`
	TotalRuns           int       `json:"total_runs"`
	CompletionRate      float64   `json:"completion_rate"`
	FailureRate         float64   `json:"failure_rate"`
	ReviewRejectionRate float64   `json:"review_rejection_rate"`
	ReworkRate          float64   `json:"rework_rate"`
	AvgDurationMs       int64     `json:"avg_duration_ms"`
	ConfidenceScore     float64   `json:"confidence_score"`
	Trend               Trend     `json:"trend"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type ScorecardConfig struct {
	MinSampleSize      int     `json:"min_sample_size"`
	LowConfidenceScore float64 `json:"low_confidence_score"`
	TrendThreshold     float64 `json:"trend_threshold"`
}

func DefaultScorecardConfig() ScorecardConfig {
	return ScorecardConfig{
		MinSampleSize:      5,
		LowConfidenceScore: 0.3,
		TrendThreshold:     0.05,
	}
}

func (c *ScorecardConfig) WindowDuration(window Window) time.Duration {
	switch window {
	case Window7d:
		return 7 * 24 * time.Hour
	case Window30d:
		return 30 * 24 * time.Hour
	default:
		return 7 * 24 * time.Hour
	}
}

func generateID() string {
	return uuid.New().String()
}

func NewAgentScorecard(agentName string, window Window, windowStart, windowEnd time.Time) AgentScorecard {
	return AgentScorecard{
		ID:          generateID(),
		AgentName:   agentName,
		Window:      window,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		Trend:       TrendStable,
		UpdatedAt:   time.Now().UTC(),
	}
}

func NewWorkflowScorecard(workflowType string, window Window, windowStart, windowEnd time.Time) WorkflowScorecard {
	return WorkflowScorecard{
		ID:           generateID(),
		WorkflowType: workflowType,
		Window:       window,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		Trend:        TrendStable,
		UpdatedAt:    time.Now().UTC(),
	}
}

type EventForScorecard struct {
	ID         string
	Type       string
	Timestamp  time.Time
	Source     string
	WorkflowID string
	AgentName  string
	Phase      string
	DurationMs int64
	Payload    map[string]any
}
