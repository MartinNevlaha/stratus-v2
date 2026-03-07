package patterns

import (
	"context"
	"testing"
	"time"
)

type mockEventQuery struct {
	events []EventForDetection
}

func (m *mockEventQuery) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]EventForDetection, error) {
	return m.events, nil
}

type mockPatternStore struct {
	patterns    []Pattern
	updates     int
	lastPattern *Pattern
}

func (m *mockPatternStore) SavePattern(ctx context.Context, pattern Pattern) error {
	m.patterns = append(m.patterns, pattern)
	m.lastPattern = &pattern
	return nil
}

func (m *mockPatternStore) GetRecentPatterns(ctx context.Context, limit int) ([]Pattern, error) {
	if limit > len(m.patterns) {
		return m.patterns, nil
	}
	return m.patterns[:limit], nil
}

func (m *mockPatternStore) GetPatternsByType(ctx context.Context, patternType PatternType, limit int) ([]Pattern, error) {
	var result []Pattern
	for _, p := range m.patterns {
		if p.Type == patternType {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *mockPatternStore) FindPatternByName(ctx context.Context, patternName string) (*Pattern, error) {
	for _, p := range m.patterns {
		if string(p.Type) == patternName {
			return &p, nil
		}
	}
	return nil, nil
}

func (m *mockPatternStore) UpdatePattern(ctx context.Context, pattern Pattern) error {
	m.updates++
	m.lastPattern = &pattern
	for i, p := range m.patterns {
		if p.ID == pattern.ID {
			m.patterns[i] = pattern
			return nil
		}
	}
	m.patterns = append(m.patterns, pattern)
	return nil
}

func TestWorkflowFailureClusterDetector(t *testing.T) {
	detector := &WorkflowFailureClusterDetector{}
	config := DefaultDetectionConfig()

	tests := []struct {
		name          string
		events        []EventForDetection
		expectPattern bool
		expectedSev   SeverityLevel
	}{
		{
			name: "no pattern - low failure rate",
			events: []EventForDetection{
				{Type: "workflow.completed", Timestamp: time.Now()},
				{Type: "workflow.completed", Timestamp: time.Now()},
				{Type: "workflow.completed", Timestamp: time.Now()},
				{Type: "workflow.completed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
			},
			expectPattern: false,
		},
		{
			name: "pattern - 50% failure rate",
			events: []EventForDetection{
				{Type: "workflow.completed", Timestamp: time.Now()},
				{Type: "workflow.completed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
			},
			expectPattern: true,
			expectedSev:   SeverityHigh,
		},
		{
			name: "pattern - 80% failure rate",
			events: []EventForDetection{
				{Type: "workflow.completed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
				{Type: "workflow.failed", Timestamp: time.Now()},
			},
			expectPattern: true,
			expectedSev:   SeverityCritical,
		},
		{
			name:          "no pattern - insufficient events",
			events:        []EventForDetection{},
			expectPattern: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := detector.Detect(context.Background(), tt.events, config)
			if tt.expectPattern {
				if pattern == nil {
					t.Error("expected pattern, got nil")
					return
				}
				if pattern.Type != PatternWorkflowFailureCluster {
					t.Errorf("expected type %s, got %s", PatternWorkflowFailureCluster, pattern.Type)
				}
				if pattern.Severity != tt.expectedSev {
					t.Errorf("expected severity %s, got %s", tt.expectedSev, pattern.Severity)
				}
				if pattern.Evidence == nil {
					t.Error("expected evidence to be set")
				}
			} else {
				if pattern != nil {
					t.Errorf("expected no pattern, got %+v", pattern)
				}
			}
		})
	}
}

func TestReviewRejectionSpikeDetector(t *testing.T) {
	detector := &ReviewRejectionSpikeDetector{}
	config := DefaultDetectionConfig()

	tests := []struct {
		name          string
		events        []EventForDetection
		expectPattern bool
	}{
		{
			name: "no pattern - low rejection rate",
			events: []EventForDetection{
				{Type: "review.passed", Timestamp: time.Now()},
				{Type: "review.passed", Timestamp: time.Now()},
				{Type: "review.passed", Timestamp: time.Now()},
				{Type: "review.passed", Timestamp: time.Now()},
				{Type: "review.passed", Timestamp: time.Now()},
				{Type: "review.failed", Timestamp: time.Now()},
			},
			expectPattern: false,
		},
		{
			name: "pattern - 50% rejection rate",
			events: []EventForDetection{
				{Type: "review.passed", Timestamp: time.Now()},
				{Type: "review.passed", Timestamp: time.Now()},
				{Type: "review.passed", Timestamp: time.Now()},
				{Type: "review.failed", Timestamp: time.Now()},
				{Type: "review.failed", Timestamp: time.Now()},
				{Type: "review.failed", Timestamp: time.Now()},
			},
			expectPattern: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := detector.Detect(context.Background(), tt.events, config)
			if tt.expectPattern && pattern == nil {
				t.Error("expected pattern, got nil")
			}
			if !tt.expectPattern && pattern != nil {
				t.Errorf("expected no pattern, got %+v", pattern)
			}
		})
	}
}

func TestEngineRunDetection(t *testing.T) {
	events := []EventForDetection{
		{Type: "workflow.completed", Timestamp: time.Now(), Payload: map[string]any{"duration_ms": 1000}},
		{Type: "workflow.completed", Timestamp: time.Now(), Payload: map[string]any{"duration_ms": 1200}},
		{Type: "workflow.failed", Timestamp: time.Now()},
		{Type: "workflow.failed", Timestamp: time.Now()},
		{Type: "workflow.failed", Timestamp: time.Now()},
		{Type: "workflow.failed", Timestamp: time.Now()},
		{Type: "workflow.failed", Timestamp: time.Now()},
		{Type: "workflow.failed", Timestamp: time.Now()},
		{Type: "review.passed", Timestamp: time.Now()},
		{Type: "review.failed", Timestamp: time.Now()},
		{Type: "review.failed", Timestamp: time.Now()},
		{Type: "review.failed", Timestamp: time.Now()},
		{Type: "review.failed", Timestamp: time.Now()},
		{Type: "review.failed", Timestamp: time.Now()},
	}

	mockQuery := &mockEventQuery{events: events}
	mockStore := &mockPatternStore{}
	config := DefaultDetectionConfig()
	config.MinEventsForDetection = 3

	engine := NewEngine(mockQuery, mockStore, config)

	err := engine.RunDetection(context.Background())
	if err != nil {
		t.Fatalf("RunDetection failed: %v", err)
	}

	if len(mockStore.patterns) == 0 {
		t.Error("expected patterns to be saved")
	}
}

func TestEngineDeduplication(t *testing.T) {
	events := []EventForDetection{
		{Type: "workflow.failed", Timestamp: time.Now()},
		{Type: "workflow.failed", Timestamp: time.Now()},
		{Type: "workflow.failed", Timestamp: time.Now()},
		{Type: "workflow.failed", Timestamp: time.Now()},
		{Type: "workflow.failed", Timestamp: time.Now()},
		{Type: "workflow.completed", Timestamp: time.Now()},
	}

	mockQuery := &mockEventQuery{events: events}
	mockStore := &mockPatternStore{}
	config := DefaultDetectionConfig()
	config.MinEventsForDetection = 3

	engine := NewEngine(mockQuery, mockStore, config)

	err := engine.RunDetection(context.Background())
	if err != nil {
		t.Fatalf("first RunDetection failed: %v", err)
	}

	if len(mockStore.patterns) == 0 {
		t.Fatal("expected pattern to be saved on first run")
	}
	firstPatternCount := len(mockStore.patterns)

	err = engine.RunDetection(context.Background())
	if err != nil {
		t.Fatalf("second RunDetection failed: %v", err)
	}

	if mockStore.updates == 0 {
		t.Error("expected pattern to be updated on second run, not created new")
	}
	if len(mockStore.patterns) != firstPatternCount {
		t.Errorf("expected same number of patterns after dedup, got %d vs %d", len(mockStore.patterns), firstPatternCount)
	}
}

func TestSeverityFromFailureRate(t *testing.T) {
	tests := []struct {
		rate     float64
		expected SeverityLevel
	}{
		{0.20, SeverityLow},
		{0.35, SeverityMedium},
		{0.55, SeverityHigh},
		{0.80, SeverityCritical},
	}

	for _, tt := range tests {
		result := SeverityFromFailureRate(tt.rate)
		if result != tt.expected {
			t.Errorf("SeverityFromFailureRate(%f) = %s, want %s", tt.rate, result, tt.expected)
		}
	}
}

func TestDefaultDetectionConfig(t *testing.T) {
	config := DefaultDetectionConfig()
	if config.EventWindowHours != 24 {
		t.Errorf("expected EventWindowHours=24, got %d", config.EventWindowHours)
	}
	if config.MinEventsForDetection != 5 {
		t.Errorf("expected MinEventsForDetection=5, got %d", config.MinEventsForDetection)
	}
	if config.FailureRateThreshold != 0.30 {
		t.Errorf("expected FailureRateThreshold=0.30, got %f", config.FailureRateThreshold)
	}
}

func TestAgentPerformanceDropDetector(t *testing.T) {
	detector := &AgentPerformanceDropDetector{}
	config := DefaultDetectionConfig()

	now := time.Now()
	windowStart := now.Add(-time.Duration(config.EventWindowHours) * time.Hour)
	prevWindowStart := windowStart.Add(-time.Duration(config.EventWindowHours) * time.Hour)

	tests := []struct {
		name          string
		events        []EventForDetection
		expectPattern bool
	}{
		{
			name: "no pattern - stable performance",
			events: []EventForDetection{
				{Type: "agent.completed", Timestamp: windowStart.Add(time.Hour)},
				{Type: "agent.completed", Timestamp: windowStart.Add(2 * time.Hour)},
				{Type: "agent.completed", Timestamp: windowStart.Add(3 * time.Hour)},
				{Type: "agent.completed", Timestamp: prevWindowStart.Add(time.Hour)},
				{Type: "agent.completed", Timestamp: prevWindowStart.Add(2 * time.Hour)},
				{Type: "agent.failed", Timestamp: prevWindowStart.Add(3 * time.Hour)},
			},
			expectPattern: false,
		},
		{
			name: "pattern - 30% drop",
			events: []EventForDetection{
				{Type: "agent.completed", Timestamp: windowStart.Add(time.Hour)},
				{Type: "agent.failed", Timestamp: windowStart.Add(2 * time.Hour)},
				{Type: "agent.failed", Timestamp: windowStart.Add(3 * time.Hour)},
				{Type: "agent.failed", Timestamp: windowStart.Add(4 * time.Hour)},
				{Type: "agent.failed", Timestamp: windowStart.Add(5 * time.Hour)},
				{Type: "agent.completed", Timestamp: prevWindowStart.Add(time.Hour)},
				{Type: "agent.completed", Timestamp: prevWindowStart.Add(2 * time.Hour)},
				{Type: "agent.completed", Timestamp: prevWindowStart.Add(3 * time.Hour)},
				{Type: "agent.completed", Timestamp: prevWindowStart.Add(4 * time.Hour)},
				{Type: "agent.completed", Timestamp: prevWindowStart.Add(5 * time.Hour)},
			},
			expectPattern: true,
		},
		{
			name:          "no pattern - insufficient events",
			events:        []EventForDetection{},
			expectPattern: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := detector.Detect(context.Background(), tt.events, config)
			if tt.expectPattern && pattern == nil {
				t.Error("expected pattern, got nil")
			}
			if !tt.expectPattern && pattern != nil {
				t.Errorf("expected no pattern, got %+v", pattern)
			}
		})
	}
}

func TestWorkflowDurationSpikeDetector(t *testing.T) {
	detector := &WorkflowDurationSpikeDetector{}
	config := DefaultDetectionConfig()
	config.MinEventsForDetection = 3

	now := time.Now()
	windowStart := now.Add(-time.Duration(config.EventWindowHours) * time.Hour)

	tests := []struct {
		name          string
		events        []EventForDetection
		expectPattern bool
	}{
		{
			name: "no pattern - stable duration",
			events: []EventForDetection{
				{Type: "workflow.completed", Timestamp: now.Add(-1 * time.Hour), Payload: map[string]any{"duration_ms": float64(1000)}},
				{Type: "workflow.completed", Timestamp: now.Add(-2 * time.Hour), Payload: map[string]any{"duration_ms": float64(1100)}},
				{Type: "workflow.completed", Timestamp: now.Add(-3 * time.Hour), Payload: map[string]any{"duration_ms": float64(1050)}},
				{Type: "workflow.completed", Timestamp: windowStart.Add(-1 * time.Hour), Payload: map[string]any{"duration_ms": float64(1000)}},
				{Type: "workflow.completed", Timestamp: windowStart.Add(-2 * time.Hour), Payload: map[string]any{"duration_ms": float64(1100)}},
				{Type: "workflow.completed", Timestamp: windowStart.Add(-3 * time.Hour), Payload: map[string]any{"duration_ms": float64(1050)}},
			},
			expectPattern: false,
		},
		{
			name: "pattern - 3x duration spike",
			events: []EventForDetection{
				{Type: "workflow.completed", Timestamp: now.Add(-1 * time.Hour), Payload: map[string]any{"duration_ms": float64(3000)}},
				{Type: "workflow.completed", Timestamp: now.Add(-2 * time.Hour), Payload: map[string]any{"duration_ms": float64(3200)}},
				{Type: "workflow.completed", Timestamp: now.Add(-3 * time.Hour), Payload: map[string]any{"duration_ms": float64(3100)}},
				{Type: "workflow.completed", Timestamp: windowStart.Add(-1 * time.Hour), Payload: map[string]any{"duration_ms": float64(1000)}},
				{Type: "workflow.completed", Timestamp: windowStart.Add(-2 * time.Hour), Payload: map[string]any{"duration_ms": float64(1100)}},
				{Type: "workflow.completed", Timestamp: windowStart.Add(-3 * time.Hour), Payload: map[string]any{"duration_ms": float64(1050)}},
			},
			expectPattern: true,
		},
		{
			name: "no pattern - no previous window data",
			events: []EventForDetection{
				{Type: "workflow.completed", Timestamp: now.Add(-1 * time.Hour), Payload: map[string]any{"duration_ms": float64(3000)}},
				{Type: "workflow.completed", Timestamp: now.Add(-2 * time.Hour), Payload: map[string]any{"duration_ms": float64(3200)}},
				{Type: "workflow.completed", Timestamp: now.Add(-3 * time.Hour), Payload: map[string]any{"duration_ms": float64(3100)}},
			},
			expectPattern: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := detector.Detect(context.Background(), tt.events, config)
			if tt.expectPattern && pattern == nil {
				t.Errorf("expected pattern, got nil (currentDurations may be empty or multiplier below threshold)")
			}
			if !tt.expectPattern && pattern != nil {
				t.Errorf("expected no pattern, got type=%s", pattern.Type)
			}
		})
	}
}

func TestEngineInsufficientEvents(t *testing.T) {
	events := []EventForDetection{
		{Type: "workflow.completed", Timestamp: time.Now()},
		{Type: "workflow.failed", Timestamp: time.Now()},
	}

	mockQuery := &mockEventQuery{events: events}
	mockStore := &mockPatternStore{}
	config := DefaultDetectionConfig()
	config.MinEventsForDetection = 10

	engine := NewEngine(mockQuery, mockStore, config)

	err := engine.RunDetection(context.Background())
	if err != nil {
		t.Fatalf("RunDetection failed: %v", err)
	}

	if len(mockStore.patterns) > 0 {
		t.Errorf("expected no patterns with insufficient events, got %d", len(mockStore.patterns))
	}
}
