package openclaw

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/openclaw/patterns"
	"github.com/MartinNevlaha/stratus-v2/internal/openclaw/proposals"
	"github.com/MartinNevlaha/stratus-v2/internal/openclaw/routing"
	"github.com/MartinNevlaha/stratus-v2/internal/openclaw/scorecards"
	"github.com/MartinNevlaha/stratus-v2/openclaw/events"
)

type Engine struct {
	database        *db.DB
	config          config.OpenClawConfig
	scheduler       *Scheduler
	eventBus        events.EventBus
	eventStore      events.Store
	subscriptionID  events.SubscriptionID
	patternEngine   *patterns.Engine
	proposalEngine  *proposals.Engine
	scorecardEngine *scorecards.Engine
	routingEngine   *routing.Engine

	ctx     context.Context
	cancel  context.CancelFunc
	running bool
	mu      sync.Mutex

	analysisMu sync.Mutex
}

func NewEngine(database *db.DB, cfg config.OpenClawConfig) *Engine {
	e := &Engine{
		database: database,
		config:   cfg,
	}
	e.scheduler = newScheduler(e)
	e.initPatternEngine()
	e.initProposalEngine()
	e.initScorecardEngine()
	e.initRoutingEngine()
	return e
}

func NewEngineWithEvents(database *db.DB, cfg config.OpenClawConfig, eventBus events.EventBus) *Engine {
	e := &Engine{
		database:   database,
		config:     cfg,
		eventBus:   eventBus,
		eventStore: events.NewDBStore(database.SQL()),
	}
	e.scheduler = newScheduler(e)
	e.initPatternEngine()
	e.initProposalEngine()
	e.initScorecardEngine()
	e.initRoutingEngine()
	return e
}

func (e *Engine) initPatternEngine() {
	eventQuery := patterns.NewDBEventQuery(e.database.SQL())
	patternStore := patterns.NewDBPatternStore(e.database)
	config := patterns.DefaultDetectionConfig()
	e.patternEngine = patterns.NewEngine(eventQuery, patternStore, config)
}

func (e *Engine) initProposalEngine() {
	patternStore := patterns.NewDBPatternStore(e.database)
	proposalStore := proposals.NewDBProposalStore(e.database)
	config := proposals.DefaultEngineConfig()
	e.proposalEngine = proposals.NewEngine(patternStore, proposalStore, config)
}

func (e *Engine) initScorecardEngine() {
	eventQuery := newScorecardEventQuery(e.database.SQL())
	scorecardStore := scorecards.NewDBScorecardStore(e.database)
	config := scorecards.DefaultScorecardConfig()
	e.scorecardEngine = scorecards.NewEngine(eventQuery, scorecardStore, config)
}

func (e *Engine) initRoutingEngine() {
	scorecardStore := scorecards.NewDBScorecardStore(e.database)
	routingStore := routing.NewDBRoutingStore(e.database)
	config := routing.DefaultRoutingConfig()
	e.routingEngine = routing.NewEngine(scorecardStore, routingStore, config)
}

func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return errors.New("openclaw: engine already running")
	}

	state, err := e.database.GetOpenClawState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}

	if state == nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		state = &db.OpenClawState{
			LastAnalysis:       now,
			NextAnalysis:       now,
			PatternsDetected:   0,
			ProposalsGenerated: 0,
			ProposalsAccepted:  0,
			AcceptanceRate:     0,
			ModelVersion:       "v1",
			ConfigJSON:         "{}",
		}
		if err := e.database.SaveOpenClawState(state); err != nil {
			return fmt.Errorf("init state: %w", err)
		}
	}

	e.ctx, e.cancel = context.WithCancel(ctx)
	e.running = true

	go func() {
		defer func() {
			e.mu.Lock()
			e.running = false
			e.mu.Unlock()
		}()

		if err := e.scheduler.Run(e.ctx); err != nil && err != context.Canceled {
			slog.Error("openclaw: scheduler stopped with error", "error", err)
		}
	}()

	if e.eventBus != nil {
		e.subscriptionID = e.eventBus.Subscribe(e.HandleEvent)
	}

	slog.Info("openclaw: engine started")
	return nil
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return
	}

	if e.eventBus != nil && e.subscriptionID != 0 {
		e.eventBus.Unsubscribe(e.subscriptionID)
		e.subscriptionID = 0
	}

	if e.cancel != nil {
		e.cancel()
	}
	e.running = false

	slog.Info("openclaw: engine stopped")
}

func (e *Engine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

func (e *Engine) HandleEvent(ctx context.Context, event events.Event) {
	if !e.IsRunning() {
		return
	}
	slog.Info("openclaw event received",
		"type", event.Type,
		"source", event.Source,
		"id", event.ID)

	if e.eventStore != nil {
		if err := e.eventStore.SaveEvent(ctx, event); err != nil {
			slog.Error("openclaw: failed to persist event", "error", err, "event_id", event.ID)
		}
	}
}

func (e *Engine) EventBus() events.EventBus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.eventBus
}

func (e *Engine) SetEventBus(bus events.EventBus) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		slog.Warn("openclaw: cannot set event bus while engine is running")
		return
	}
	e.eventBus = bus
	e.eventStore = events.NewDBStore(e.database.SQL())
}

func (e *Engine) PatternEngine() *patterns.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.patternEngine
}

func (e *Engine) RunPatternDetection(ctx context.Context) error {
	if e.patternEngine == nil {
		return errors.New("openclaw: pattern engine not initialized")
	}
	return e.patternEngine.RunDetection(ctx)
}

func (e *Engine) ProposalEngine() *proposals.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.proposalEngine
}

func (e *Engine) RunProposalGeneration(ctx context.Context) error {
	if e.proposalEngine == nil {
		return errors.New("openclaw: proposal engine not initialized")
	}
	return e.proposalEngine.RunGeneration(ctx)
}

func (e *Engine) ScorecardEngine() *scorecards.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.scorecardEngine
}

func (e *Engine) RunScorecardComputation(ctx context.Context) error {
	if e.scorecardEngine == nil {
		return errors.New("openclaw: scorecard engine not initialized")
	}
	return e.scorecardEngine.RunComputation(ctx)
}

func (e *Engine) RoutingEngine() *routing.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.routingEngine
}

func (e *Engine) RunRoutingAnalysis(ctx context.Context) error {
	if e.routingEngine == nil {
		return errors.New("openclaw: routing engine not initialized")
	}
	return e.routingEngine.RunAnalysis(ctx)
}

func newScorecardEventQuery(db *sql.DB) scorecards.EventQuery {
	return &scorecardEventQuery{db: db}
}

type scorecardEventQuery struct {
	db *sql.DB
}

func (q *scorecardEventQuery) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]scorecards.EventForScorecard, error) {
	if limit <= 0 {
		limit = 10000
	}
	if len(eventTypes) == 0 {
		return []scorecards.EventForScorecard{}, nil
	}

	placeholders := ""
	for i := range eventTypes {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}

	query := `SELECT id, type, timestamp, source, payload
		FROM openclaw_events
		WHERE type IN (` + placeholders + `) AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT ?`

	args := make([]any, 0, len(eventTypes)+3)
	for _, et := range eventTypes {
		args = append(args, et)
	}
	args = append(args, start.Format(time.RFC3339Nano))
	args = append(args, end.Format(time.RFC3339Nano))
	args = append(args, limit)

	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []scorecards.EventForScorecard
	for rows.Next() {
		var e scorecards.EventForScorecard
		var timestamp, payloadStr string
		if err := rows.Scan(&e.ID, &e.Type, &timestamp, &e.Source, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, timestamp)
		if err := unmarshalPayload(payloadStr, &e); err != nil {
			slog.Warn("failed to parse event payload", "event_id", e.ID, "error", err)
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

func unmarshalPayload(payloadStr string, e *scorecards.EventForScorecard) error {
	if payloadStr == "" {
		e.Payload = make(map[string]any)
		return nil
	}
	if err := json.Unmarshal([]byte(payloadStr), &e.Payload); err != nil {
		return err
	}
	if wfID, ok := e.Payload["workflow_id"].(string); ok {
		e.WorkflowID = wfID
	}
	if agentName, ok := e.Payload["agent_name"].(string); ok {
		e.AgentName = agentName
	}
	if phase, ok := e.Payload["phase"].(string); ok {
		e.Phase = phase
	}
	if durMs, ok := e.Payload["duration_ms"].(float64); ok {
		e.DurationMs = int64(durMs)
	}
	return nil
}
