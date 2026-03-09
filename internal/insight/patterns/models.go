package patterns

import (
	"time"
)

type PatternType string

const (
	PatternWorkflowFailureCluster PatternType = "workflow.failure_cluster"
	PatternWorkflowDurationSpike  PatternType = "workflow.duration_spike"
	PatternAgentPerformanceDrop   PatternType = "agent.performance_drop"
	PatternReviewRejectionSpike   PatternType = "review.rejection_spike"
	PatternProposalRejectionSpike PatternType = "proposal.rejection_spike"
	PatternWorkflowLoop           PatternType = "workflow.loop"
	PatternWorkflowReviewFailure  PatternType = "workflow.review_failure_cluster"
	PatternWorkflowSlowExecution  PatternType = "workflow.slow_execution"
)

type SeverityLevel string

const (
	SeverityLow      SeverityLevel = "low"
	SeverityMedium   SeverityLevel = "medium"
	SeverityHigh     SeverityLevel = "high"
	SeverityCritical SeverityLevel = "critical"
)

type Pattern struct {
	ID          string         `json:"id"`
	Type        PatternType    `json:"type"`
	Timestamp   time.Time      `json:"timestamp"`
	Severity    SeverityLevel  `json:"severity"`
	Description string         `json:"description"`
	Evidence    map[string]any `json:"evidence"`
	Frequency   int            `json:"frequency"`
	Confidence  float64        `json:"confidence"`
	FirstSeen   time.Time      `json:"first_seen"`
	LastSeen    time.Time      `json:"last_seen"`
}

type DetectionConfig struct {
	EventWindowHours         int              `json:"event_window_hours"`
	MinEventsForDetection    int              `json:"min_events_for_detection"`
	FailureRateThreshold     float64          `json:"failure_rate_threshold"`
	PerformanceDropThreshold float64          `json:"performance_drop_threshold"`
	RejectionRateThreshold   float64          `json:"rejection_rate_threshold"`
	DurationSpikeMultiplier  float64          `json:"duration_spike_multiplier"`
	LoopThreshold            int              `json:"loop_threshold"`
	ReviewFailThreshold      float64          `json:"review_fail_threshold"`
	BaselineCycleTimesMs     map[string]int64 `json:"baseline_cycle_times_ms"`
}

func DefaultDetectionConfig() DetectionConfig {
	return DetectionConfig{
		EventWindowHours:         24,
		MinEventsForDetection:    5,
		FailureRateThreshold:     0.30,
		PerformanceDropThreshold: 0.20,
		RejectionRateThreshold:   0.40,
		DurationSpikeMultiplier:  2.0,
		LoopThreshold:            3,
		ReviewFailThreshold:      0.50,
		BaselineCycleTimesMs: map[string]int64{
			"spec": 10 * 60 * 1000,
			"bug":  5 * 60 * 1000,
			"e2e":  15 * 60 * 1000,
		},
	}
}

func SeverityFromFailureRate(rate float64) SeverityLevel {
	switch {
	case rate >= 0.70:
		return SeverityCritical
	case rate >= 0.50:
		return SeverityHigh
	case rate >= 0.30:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func SeverityFromDropRate(drop float64) SeverityLevel {
	switch {
	case drop >= 0.50:
		return SeverityCritical
	case drop >= 0.35:
		return SeverityHigh
	case drop >= 0.20:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func SeverityFromRejectionRate(rate float64) SeverityLevel {
	switch {
	case rate >= 0.60:
		return SeverityHigh
	case rate >= 0.40:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func SeverityFromDurationMultiplier(multiplier float64) SeverityLevel {
	switch {
	case multiplier >= 4.0:
		return SeverityCritical
	case multiplier >= 3.0:
		return SeverityHigh
	case multiplier >= 2.0:
		return SeverityMedium
	default:
		return SeverityLow
	}
}
