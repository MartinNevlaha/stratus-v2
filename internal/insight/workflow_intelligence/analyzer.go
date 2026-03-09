package workflow_intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type EventQuery interface {
	GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]EventForMetrics, error)
}

type WorkflowFinding struct {
	Type          string         `json:"type"`
	WorkflowType  string         `json:"workflow_type"`
	Severity      string         `json:"severity"`
	Description   string         `json:"description"`
	Evidence      map[string]any `json:"evidence"`
	Confidence    float64        `json:"confidence"`
	AffectedCount int            `json:"affected_count"`
	DetectedAt    time.Time      `json:"detected_at"`
}

type LoopPattern struct {
	WorkflowType  string   `json:"workflow_type"`
	AvgRetries    float64  `json:"avg_retries"`
	MaxRetries    int      `json:"max_retries"`
	AffectedCount int      `json:"affected_count"`
	Examples      []string `json:"examples"`
	Severity      string   `json:"severity"`
}

type SlowWorkflowPattern struct {
	WorkflowType  string   `json:"workflow_type"`
	AvgCycleMs    int64    `json:"avg_cycle_ms"`
	BaselineMs    int64    `json:"baseline_ms"`
	Multiplier    float64  `json:"multiplier"`
	AffectedCount int      `json:"affected_count"`
	Examples      []string `json:"examples"`
	Severity      string   `json:"severity"`
}

type ReviewFailurePattern struct {
	WorkflowType   string   `json:"workflow_type"`
	AvgFailRate    float64  `json:"avg_fail_rate"`
	AffectedCount  int      `json:"affected_count"`
	TotalWorkflows int      `json:"total_workflows"`
	Examples       []string `json:"examples"`
	Severity       string   `json:"severity"`
}

type AnalyzerConfig struct {
	LoopThreshold            int              `json:"loop_threshold"`
	ReviewFailThreshold      float64          `json:"review_fail_threshold"`
	SlowMultiplierThreshold  float64          `json:"slow_multiplier_threshold"`
	AnalysisWindowHours      int              `json:"analysis_window_hours"`
	MinWorkflowsForDetection int              `json:"min_workflows_for_detection"`
	BaselineCycleTimes       map[string]int64 `json:"baseline_cycle_times"`
}

func DefaultAnalyzerConfig() AnalyzerConfig {
	return AnalyzerConfig{
		LoopThreshold:            3,
		ReviewFailThreshold:      0.50,
		SlowMultiplierThreshold:  1.5,
		AnalysisWindowHours:      24,
		MinWorkflowsForDetection: 3,
		BaselineCycleTimes: map[string]int64{
			"spec": 10 * 60 * 1000,
			"bug":  5 * 60 * 1000,
			"e2e":  15 * 60 * 1000,
		},
	}
}

type WorkflowAnalyzer struct {
	eventQuery EventQuery
	config     AnalyzerConfig
	mu         sync.Mutex
}

func NewWorkflowAnalyzer(eventQuery EventQuery, config AnalyzerConfig) *WorkflowAnalyzer {
	if config.LoopThreshold <= 0 {
		config = DefaultAnalyzerConfig()
	}

	return &WorkflowAnalyzer{
		eventQuery: eventQuery,
		config:     config,
	}
}

func (a *WorkflowAnalyzer) AnalyzeWorkflowPerformance(ctx context.Context) ([]WorkflowFinding, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	start := time.Now()
	slog.Info("workflow_intelligence: analysis started")

	events, err := a.loadEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("load events: %w", err)
	}

	metrics := ComputeWorkflowMetrics(events)
	slog.Debug("workflow_intelligence: computed metrics", "workflow_count", len(metrics))

	if len(metrics) < a.config.MinWorkflowsForDetection {
		slog.Info("workflow_intelligence: insufficient workflows", "count", len(metrics))
		return nil, nil
	}

	var findings []WorkflowFinding

	loopFindings := a.DetectWorkflowLoops(metrics)
	for _, p := range loopFindings {
		findings = append(findings, WorkflowFinding{
			Type:          "workflow_loop",
			WorkflowType:  p.WorkflowType,
			Severity:      p.Severity,
			Description:   fmt.Sprintf("Workflow loops detected: avg %.1f retries across %d workflows", p.AvgRetries, p.AffectedCount),
			Evidence:      map[string]any{"avg_retries": p.AvgRetries, "max_retries": p.MaxRetries, "examples": p.Examples},
			Confidence:    calculateLoopConfidence(p),
			AffectedCount: p.AffectedCount,
			DetectedAt:    time.Now().UTC(),
		})
	}

	slowFindings := a.DetectSlowWorkflows(metrics)
	for _, p := range slowFindings {
		findings = append(findings, WorkflowFinding{
			Type:          "slow_workflow",
			WorkflowType:  p.WorkflowType,
			Severity:      p.Severity,
			Description:   fmt.Sprintf("Slow workflow detected: %.1fx slower than baseline (%dms vs %dms)", p.Multiplier, p.AvgCycleMs, p.BaselineMs),
			Evidence:      map[string]any{"avg_cycle_ms": p.AvgCycleMs, "baseline_ms": p.BaselineMs, "multiplier": p.Multiplier, "examples": p.Examples},
			Confidence:    calculateSlowConfidence(p),
			AffectedCount: p.AffectedCount,
			DetectedAt:    time.Now().UTC(),
		})
	}

	reviewFindings := a.DetectHighReviewFailure(metrics)
	for _, p := range reviewFindings {
		findings = append(findings, WorkflowFinding{
			Type:          "review_failure_cluster",
			WorkflowType:  p.WorkflowType,
			Severity:      p.Severity,
			Description:   fmt.Sprintf("High review failure rate: %.1f%% across %d workflows", p.AvgFailRate*100, p.AffectedCount),
			Evidence:      map[string]any{"avg_fail_rate": p.AvgFailRate, "affected_count": p.AffectedCount, "total_workflows": p.TotalWorkflows, "examples": p.Examples},
			Confidence:    calculateReviewFailConfidence(p),
			AffectedCount: p.AffectedCount,
			DetectedAt:    time.Now().UTC(),
		})
	}

	duration := time.Since(start)
	slog.Info("workflow_intelligence: analysis complete",
		"findings", len(findings),
		"duration_ms", duration.Milliseconds())

	return findings, nil
}

func (a *WorkflowAnalyzer) loadEvents(ctx context.Context) ([]EventForMetrics, error) {
	end := time.Now()
	start := end.Add(-time.Duration(a.config.AnalysisWindowHours) * time.Hour)

	eventTypes := []string{
		"workflow.started", "workflow.completed", "workflow.failed",
		"workflow.phase_transition",
		"agent.spawned", "agent.completed", "agent.failed",
		"review.passed", "review.failed",
	}

	events, err := a.eventQuery.GetEventsByTypesInTimeRange(ctx, eventTypes, start, end, 10000)
	if err != nil {
		return nil, err
	}

	return events, nil
}

func (a *WorkflowAnalyzer) DetectWorkflowLoops(metrics []WorkflowMetrics) []LoopPattern {
	byType := make(map[string][]WorkflowMetrics)
	for _, m := range metrics {
		if m.WorkflowType != "" {
			byType[m.WorkflowType] = append(byType[m.WorkflowType], m)
		}
	}

	var patterns []LoopPattern

	for wfType, wfMetrics := range byType {
		if len(wfMetrics) < a.config.MinWorkflowsForDetection {
			continue
		}

		var totalRetries float64
		var affectedCount int
		var maxRetries int
		var examples []string

		for _, m := range wfMetrics {
			totalRetries += float64(m.RetryCount)
			if m.RetryCount > maxRetries {
				maxRetries = m.RetryCount
			}
			if m.RetryCount >= a.config.LoopThreshold {
				affectedCount++
				if len(examples) < 3 {
					examples = append(examples, fmt.Sprintf("%s: %d retries", m.WorkflowID, m.RetryCount))
				}
			}
		}

		avgRetries := totalRetries / float64(len(wfMetrics))

		if affectedCount >= a.config.MinWorkflowsForDetection || avgRetries >= float64(a.config.LoopThreshold) {
			severity := determineLoopSeverity(avgRetries, affectedCount, len(wfMetrics))
			patterns = append(patterns, LoopPattern{
				WorkflowType:  wfType,
				AvgRetries:    avgRetries,
				MaxRetries:    maxRetries,
				AffectedCount: affectedCount,
				Examples:      examples,
				Severity:      severity,
			})
		}
	}

	return patterns
}

func (a *WorkflowAnalyzer) DetectSlowWorkflows(metrics []WorkflowMetrics) []SlowWorkflowPattern {
	byType := make(map[string][]WorkflowMetrics)
	for _, m := range metrics {
		if m.WorkflowType != "" && m.CycleTimeMs > 0 {
			byType[m.WorkflowType] = append(byType[m.WorkflowType], m)
		}
	}

	var patterns []SlowWorkflowPattern

	for wfType, wfMetrics := range byType {
		if len(wfMetrics) < a.config.MinWorkflowsForDetection {
			continue
		}

		var totalCycleMs int64
		for _, m := range wfMetrics {
			totalCycleMs += m.CycleTimeMs
		}
		avgCycleMs := totalCycleMs / int64(len(wfMetrics))

		baselineMs := a.config.BaselineCycleTimes[wfType]
		if baselineMs <= 0 {
			baselineMs = 10 * 60 * 1000
		}

		multiplier := float64(avgCycleMs) / float64(baselineMs)

		if multiplier >= a.config.SlowMultiplierThreshold {
			var examples []string
			var affectedCount int
			thresholdMs := int64(float64(baselineMs) * a.config.SlowMultiplierThreshold)
			for _, m := range wfMetrics {
				if m.CycleTimeMs >= thresholdMs {
					affectedCount++
					if len(examples) < 3 {
						examples = append(examples, fmt.Sprintf("%s: %dms", m.WorkflowID, m.CycleTimeMs))
					}
				}
			}

			severity := determineSlowSeverity(multiplier)
			patterns = append(patterns, SlowWorkflowPattern{
				WorkflowType:  wfType,
				AvgCycleMs:    avgCycleMs,
				BaselineMs:    baselineMs,
				Multiplier:    multiplier,
				AffectedCount: affectedCount,
				Examples:      examples,
				Severity:      severity,
			})
		}
	}

	return patterns
}

func (a *WorkflowAnalyzer) DetectHighReviewFailure(metrics []WorkflowMetrics) []ReviewFailurePattern {
	byType := make(map[string][]WorkflowMetrics)
	for _, m := range metrics {
		if m.WorkflowType != "" {
			byType[m.WorkflowType] = append(byType[m.WorkflowType], m)
		}
	}

	var patterns []ReviewFailurePattern

	for wfType, wfMetrics := range byType {
		if len(wfMetrics) < a.config.MinWorkflowsForDetection {
			continue
		}

		var totalFailRate float64
		var affectedCount int
		var examples []string
		var withReview int

		for _, m := range wfMetrics {
			if m.ReviewFailRate > 0 || m.Status != "" {
				withReview++
				totalFailRate += m.ReviewFailRate
				if m.ReviewFailRate >= a.config.ReviewFailThreshold {
					affectedCount++
					if len(examples) < 3 {
						examples = append(examples, fmt.Sprintf("%s: %.1f%% review fail rate", m.WorkflowID, m.ReviewFailRate*100))
					}
				}
			}
		}

		if withReview < a.config.MinWorkflowsForDetection {
			continue
		}

		avgFailRate := totalFailRate / float64(withReview)

		if avgFailRate >= a.config.ReviewFailThreshold || affectedCount >= a.config.MinWorkflowsForDetection {
			severity := determineReviewFailSeverity(avgFailRate, affectedCount, withReview)
			patterns = append(patterns, ReviewFailurePattern{
				WorkflowType:   wfType,
				AvgFailRate:    avgFailRate,
				AffectedCount:  affectedCount,
				TotalWorkflows: withReview,
				Examples:       examples,
				Severity:       severity,
			})
		}
	}

	return patterns
}

func (a *WorkflowAnalyzer) SetConfig(config AnalyzerConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
}

func (a *WorkflowAnalyzer) Config() AnalyzerConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.config
}

func determineLoopSeverity(avgRetries float64, affectedCount, total int) string {
	ratio := float64(affectedCount) / float64(total)
	if avgRetries >= 5 || ratio >= 0.5 {
		return "critical"
	}
	if avgRetries >= 4 || ratio >= 0.3 {
		return "high"
	}
	if avgRetries >= 3 || ratio >= 0.2 {
		return "medium"
	}
	return "low"
}

func determineSlowSeverity(multiplier float64) string {
	if multiplier >= 3.0 {
		return "critical"
	}
	if multiplier >= 2.5 {
		return "high"
	}
	if multiplier >= 2.0 {
		return "medium"
	}
	return "low"
}

func determineReviewFailSeverity(avgFailRate float64, affectedCount, total int) string {
	ratio := float64(affectedCount) / float64(total)
	if avgFailRate >= 0.7 || ratio >= 0.5 {
		return "critical"
	}
	if avgFailRate >= 0.6 || ratio >= 0.3 {
		return "high"
	}
	if avgFailRate >= 0.5 || ratio >= 0.2 {
		return "medium"
	}
	return "low"
}

func calculateLoopConfidence(p LoopPattern) float64 {
	base := 0.5
	if p.AvgRetries >= 4 {
		base += 0.2
	}
	if p.AffectedCount >= 5 {
		base += 0.15
	}
	if len(p.Examples) >= 2 {
		base += 0.1
	}
	if base > 0.95 {
		base = 0.95
	}
	return base
}

func calculateSlowConfidence(p SlowWorkflowPattern) float64 {
	base := 0.5
	if p.Multiplier >= 2.5 {
		base += 0.2
	}
	if p.AffectedCount >= 5 {
		base += 0.15
	}
	if len(p.Examples) >= 2 {
		base += 0.1
	}
	if base > 0.95 {
		base = 0.95
	}
	return base
}

func calculateReviewFailConfidence(p ReviewFailurePattern) float64 {
	base := 0.5
	if p.AvgFailRate >= 0.6 {
		base += 0.2
	}
	if p.AffectedCount >= 5 {
		base += 0.15
	}
	if len(p.Examples) >= 2 {
		base += 0.1
	}
	if base > 0.95 {
		base = 0.95
	}
	return base
}
