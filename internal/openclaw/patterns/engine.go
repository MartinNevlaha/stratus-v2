package patterns

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Engine struct {
	eventQuery   EventQuery
	patternStore PatternStore
	config       DetectionConfig
	detectors    []Detector
	mu           sync.Mutex
}

func NewEngine(eventQuery EventQuery, patternStore PatternStore, config DetectionConfig) *Engine {
	if config.EventWindowHours <= 0 {
		config = DefaultDetectionConfig()
	}

	return &Engine{
		eventQuery:   eventQuery,
		patternStore: patternStore,
		config:       config,
		detectors: []Detector{
			&WorkflowFailureClusterDetector{},
			&AgentPerformanceDropDetector{},
			&ReviewRejectionSpikeDetector{},
			&WorkflowDurationSpikeDetector{},
		},
	}
}

func (e *Engine) RunDetection(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := time.Now()
	slog.Info("openclaw patterns: detection started")

	end := time.Now()
	startTime := end.Add(-time.Duration(e.config.EventWindowHours) * time.Hour)

	allEventTypes := []string{
		"workflow.completed", "workflow.failed",
		"agent.completed", "agent.failed",
		"review.passed", "review.failed",
	}

	events, err := e.eventQuery.GetEventsByTypesInTimeRange(ctx, allEventTypes, startTime, end, 5000)
	if err != nil {
		return fmt.Errorf("load events: %w", err)
	}

	slog.Debug("openclaw patterns: loaded events", "count", len(events), "window_h", e.config.EventWindowHours)

	if len(events) < e.config.MinEventsForDetection {
		slog.Info("openclaw patterns: insufficient events for detection", "count", len(events), "min", e.config.MinEventsForDetection)
		return nil
	}

	var detectedPatterns []*Pattern
	for _, detector := range e.detectors {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("openclaw patterns: detector panic recovered", "detector", detector.Name(), "panic", r)
				}
			}()

			pattern := detector.Detect(ctx, events, e.config)
			if pattern != nil {
				detectedPatterns = append(detectedPatterns, pattern)
				slog.Info("openclaw patterns: pattern detected",
					"type", pattern.Type,
					"severity", pattern.Severity,
					"description", pattern.Description)
			}
		}()
	}

	saved := 0
	updated := 0
	for _, pattern := range detectedPatterns {
		err := e.savePatternWithDedup(ctx, pattern)
		if err != nil {
			slog.Error("openclaw patterns: failed to save pattern", "type", pattern.Type, "error", err)
			continue
		}
		if pattern.Frequency > 1 {
			updated++
		} else {
			saved++
		}
	}

	duration := time.Since(start)
	slog.Info("openclaw patterns: detection complete",
		"detected", len(detectedPatterns),
		"saved_new", saved,
		"updated", updated,
		"duration_ms", duration.Milliseconds())

	return nil
}

func (e *Engine) savePatternWithDedup(ctx context.Context, pattern *Pattern) error {
	existing, err := e.patternStore.FindPatternByName(ctx, string(pattern.Type))
	if err != nil {
		return fmt.Errorf("check existing: %w", err)
	}

	if existing != nil {
		existing.Frequency++
		existing.LastSeen = time.Now().UTC()
		existing.Severity = pattern.Severity
		existing.Description = pattern.Description
		existing.Evidence = pattern.Evidence
		existing.Confidence = max(existing.Confidence, pattern.Confidence)
		pattern.Frequency = existing.Frequency
		pattern.ID = existing.ID
		return e.patternStore.UpdatePattern(ctx, *existing)
	}

	return e.patternStore.SavePattern(ctx, *pattern)
}

func (e *Engine) GetRecentPatterns(ctx context.Context, limit int) ([]Pattern, error) {
	return e.patternStore.GetRecentPatterns(ctx, limit)
}

func (e *Engine) GetPatternsByType(ctx context.Context, patternType PatternType, limit int) ([]Pattern, error) {
	return e.patternStore.GetPatternsByType(ctx, patternType, limit)
}

func (e *Engine) AddDetector(detector Detector) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.detectors = append(e.detectors, detector)
}

func (e *Engine) SetConfig(config DetectionConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

func (e *Engine) Config() DetectionConfig {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.config
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
