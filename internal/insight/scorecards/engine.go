package scorecards

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type EventQuery interface {
	GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]EventForScorecard, error)
}

type Engine struct {
	eventQuery     EventQuery
	scorecardStore ScorecardStore
	config         ScorecardConfig
	mu             sync.Mutex
}

func NewEngine(eventQuery EventQuery, scorecardStore ScorecardStore, config ScorecardConfig) *Engine {
	if config.MinSampleSize <= 0 {
		config = DefaultScorecardConfig()
	}

	return &Engine{
		eventQuery:     eventQuery,
		scorecardStore: scorecardStore,
		config:         config,
	}
}

func (e *Engine) RunComputation(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := time.Now()
	slog.Info("insight scorecards: computation started")

	windows := []Window{Window7d, Window30d}
	totalAgentCards := 0
	totalWorkflowCards := 0

	for _, window := range windows {
		agentCards, workflowCards, err := e.computeForWindow(ctx, window)
		if err != nil {
			slog.Error("insight scorecards: window computation failed", "window", window, "error", err)
			continue
		}
		totalAgentCards += len(agentCards)
		totalWorkflowCards += len(workflowCards)
	}

	duration := time.Since(start)
	slog.Info("insight scorecards: computation complete",
		"agent_count", totalAgentCards,
		"workflow_count", totalWorkflowCards,
		"duration_ms", duration.Milliseconds())

	return nil
}

func (e *Engine) computeForWindow(ctx context.Context, window Window) ([]AgentScorecard, []WorkflowScorecard, error) {
	now := time.Now().UTC()
	windowEnd := now
	windowStart := now.Add(-e.config.WindowDuration(window))

	prevWindowEnd := windowStart
	prevWindowStart := prevWindowEnd.Add(-e.config.WindowDuration(window))

	eventTypes := []string{
		"workflow.started", "workflow.completed", "workflow.failed", "workflow.phase_transition",
		"agent.spawned", "agent.completed", "agent.failed",
		"review.passed", "review.failed",
	}

	events, err := e.eventQuery.GetEventsByTypesInTimeRange(ctx, eventTypes, windowStart, windowEnd, 10000)
	if err != nil {
		return nil, nil, fmt.Errorf("load events for window %s: %w", window, err)
	}

	slog.Debug("insight scorecards: loaded events", "window", window, "count", len(events))

	agentCards := e.computeAgentScorecards(ctx, events, window, windowStart, windowEnd, prevWindowStart, prevWindowEnd)
	workflowCards := e.computeWorkflowScorecards(ctx, events, window, windowStart, windowEnd, prevWindowStart, prevWindowEnd)

	return agentCards, workflowCards, nil
}

func (e *Engine) computeAgentScorecards(
	ctx context.Context,
	events []EventForScorecard,
	window Window,
	windowStart, windowEnd time.Time,
	prevWindowStart, prevWindowEnd time.Time,
) []AgentScorecard {
	agentNames := extractAgentNames(events)
	if len(agentNames) == 0 {
		slog.Debug("insight scorecards: no agents found in events")
		return nil
	}

	var cards []AgentScorecard
	for _, agentName := range agentNames {
		card := ComputeAgentScorecard(agentName, window, windowStart, windowEnd, events, e.config)

		if card.TotalRuns > 0 {
			prevCard, _ := e.scorecardStore.GetAgentScorecardByName(ctx, agentName, window)
			if prevCard != nil {
				card.ID = prevCard.ID
			}
			card.Trend = CalculateTrend(&card, prevCard, e.config.TrendThreshold)

			if err := e.scorecardStore.SaveAgentScorecard(ctx, card); err != nil {
				slog.Error("insight scorecards: failed to save agent scorecard",
					"agent", agentName, "error", err)
				continue
			}
			cards = append(cards, card)
		}
	}

	slog.Debug("insight scorecards: computed agent scorecards", "count", len(cards))
	return cards
}

func (e *Engine) computeWorkflowScorecards(
	ctx context.Context,
	events []EventForScorecard,
	window Window,
	windowStart, windowEnd time.Time,
	prevWindowStart, prevWindowEnd time.Time,
) []WorkflowScorecard {
	workflowTypes := extractWorkflowTypes(events)
	if len(workflowTypes) == 0 {
		slog.Debug("insight scorecards: no workflow types found in events")
		return nil
	}

	var cards []WorkflowScorecard
	for _, wfType := range workflowTypes {
		card := ComputeWorkflowScorecard(wfType, window, windowStart, windowEnd, events, e.config)

		if card.TotalRuns > 0 {
			prevCard, _ := e.scorecardStore.GetWorkflowScorecardByType(ctx, wfType, window)
			if prevCard != nil {
				card.ID = prevCard.ID
			}
			card.Trend = CalculateWorkflowTrend(&card, prevCard, e.config.TrendThreshold)

			if err := e.scorecardStore.SaveWorkflowScorecard(ctx, card); err != nil {
				slog.Error("insight scorecards: failed to save workflow scorecard",
					"workflow", wfType, "error", err)
				continue
			}
			cards = append(cards, card)
		}
	}

	slog.Debug("insight scorecards: computed workflow scorecards", "count", len(cards))
	return cards
}

func extractAgentNames(events []EventForScorecard) []string {
	seen := make(map[string]bool)
	var names []string

	for _, e := range events {
		agentName := e.AgentName
		if agentName == "" {
			if a, ok := e.Payload["agent_name"].(string); ok {
				agentName = a
			}
		}
		if agentName != "" && !seen[agentName] {
			seen[agentName] = true
			names = append(names, agentName)
		}
	}

	return names
}

func extractWorkflowTypes(events []EventForScorecard) []string {
	seen := make(map[string]bool)
	var types []string

	for _, e := range events {
		wfType := e.WorkflowID
		if wt, ok := e.Payload["workflow_type"].(string); ok && wt != "" {
			wfType = wt
		}
		if wfType != "" && !seen[wfType] {
			seen[wfType] = true
			types = append(types, wfType)
		}
	}

	return types
}

type DBEventQuery struct {
	queryFunc func(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]EventForScorecard, error)
}

func NewDBEventQuery(queryFunc func(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]EventForScorecard, error)) *DBEventQuery {
	return &DBEventQuery{queryFunc: queryFunc}
}

func (q *DBEventQuery) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]EventForScorecard, error) {
	if q.queryFunc == nil {
		return nil, fmt.Errorf("query function not set")
	}
	return q.queryFunc(ctx, eventTypes, start, end, limit)
}

func (e *Engine) SetConfig(config ScorecardConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

func (e *Engine) Config() ScorecardConfig {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.config
}

func (e *Engine) GetAgentScorecards(ctx context.Context, window Window, sortBy string, sortDir string, limit int) ([]AgentScorecard, error) {
	return e.scorecardStore.GetAgentScorecards(ctx, window, sortBy, sortDir, limit)
}

func (e *Engine) GetWorkflowScorecards(ctx context.Context, window Window, sortBy string, sortDir string, limit int) ([]WorkflowScorecard, error) {
	return e.scorecardStore.GetWorkflowScorecards(ctx, window, sortBy, sortDir, limit)
}

func (e *Engine) GetAgentScorecardByName(ctx context.Context, agentName string, window Window) (*AgentScorecard, error) {
	return e.scorecardStore.GetAgentScorecardByName(ctx, agentName, window)
}

func (e *Engine) GetWorkflowScorecardByType(ctx context.Context, workflowType string, window Window) (*WorkflowScorecard, error) {
	return e.scorecardStore.GetWorkflowScorecardByType(ctx, workflowType, window)
}

func (e *Engine) GetScorecardHighlights(ctx context.Context, window Window) (map[string]interface{}, error) {
	return e.scorecardStore.GetScorecardHighlights(ctx, window)
}
