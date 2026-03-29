package insight

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
	"github.com/MartinNevlaha/stratus-v2/internal/insight/artifacts"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/knowledge_engine"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/patterns"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/product_intelligence"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/proposals"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/routing"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/scorecards"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/trajectory_engine"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/workflow_intelligence"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/workflow_synthesis"
	"github.com/MartinNevlaha/stratus-v2/insight/events"
)

type Engine struct {
	database            *db.DB
	config              config.InsightConfig
	scheduler           *Scheduler
	eventBus            events.EventBus
	eventStore          events.Store
	subscriptionID      events.SubscriptionID
	llmClient           llm.Client
	patternEngine       *patterns.Engine
	proposalEngine      *proposals.Engine
	scorecardEngine     *scorecards.Engine
	routingEngine       *routing.Engine
	workflowAnalyzer    *workflow_intelligence.WorkflowAnalyzer
	artifactBuilder     *artifacts.ArtifactBuilder
	knowledgeEngine     *knowledge_engine.Engine
	trajectoryRecorder  *trajectory_engine.TrajectoryRecorder
	trajectoryAnalyzer  *trajectory_engine.TrajectoryAnalyzer
	workflowSynthesizer *workflow_synthesis.Synthesizer
	productIntelligence *product_intelligence.Engine

	ctx     context.Context
	cancel  context.CancelFunc
	running bool
	mu      sync.Mutex

	analysisMu sync.Mutex
}

func NewEngine(database *db.DB, cfg config.InsightConfig) *Engine {
	e := &Engine{
		database: database,
		config:   cfg,
	}
	e.initLLMClient()
	e.scheduler = newScheduler(e)
	e.initPatternEngine()
	e.initProposalEngine()
	e.initScorecardEngine()
	e.initRoutingEngine()
	e.initWorkflowAnalyzer()
	e.initArtifactBuilder()
	e.initKnowledgeEngine()
	e.initTrajectoryEngine()
	e.initWorkflowSynthesizer()
	e.initProductIntelligence()
	return e
}

func NewEngineWithEvents(database *db.DB, cfg config.InsightConfig, eventBus events.EventBus) *Engine {
	e := &Engine{
		database:   database,
		config:     cfg,
		eventBus:   eventBus,
		eventStore: events.NewDBStore(database.SQL()),
	}
	e.initLLMClient()
	e.scheduler = newScheduler(e)
	e.initPatternEngine()
	e.initProposalEngine()
	e.initScorecardEngine()
	e.initRoutingEngine()
	e.initWorkflowAnalyzer()
	e.initArtifactBuilder()
	e.initKnowledgeEngine()
	e.initTrajectoryEngine()
	e.initWorkflowSynthesizer()
	e.initProductIntelligence()
	return e
}

func (e *Engine) initLLMClient() {
	llmCfg := llm.Config{
		Provider:    e.config.LLM.Provider,
		Model:       e.config.LLM.Model,
		APIKey:      e.config.LLM.APIKey,
		BaseURL:     e.config.LLM.BaseURL,
		Timeout:     e.config.LLM.Timeout,
		MaxTokens:   e.config.LLM.MaxTokens,
		Temperature: e.config.LLM.Temperature,
	}
	if llmCfg.Provider == "" {
		llmCfg = llm.DefaultConfig()
	}
	llmCfg = llmCfg.WithEnv()
	client, err := llm.NewClient(llmCfg)
	if err != nil {
		slog.Warn("insight: failed to initialize LLM client", "error", err)
		return
	}
	e.llmClient = client
	slog.Info("insight: LLM client initialized", "provider", llmCfg.Provider, "model", llmCfg.Model)
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

func (e *Engine) initWorkflowAnalyzer() {
	eventQuery := newWorkflowEventQuery(e.database.SQL())
	config := workflow_intelligence.DefaultAnalyzerConfig()
	e.workflowAnalyzer = workflow_intelligence.NewWorkflowAnalyzer(eventQuery, config)
}

func (e *Engine) initArtifactBuilder() {
	eventQuery := newArtifactEventQuery(e.database.SQL())
	artifactStore := artifacts.NewDBArtifactStore(e.database)
	config := artifacts.DefaultArtifactConfig()
	e.artifactBuilder = artifacts.NewArtifactBuilder(eventQuery, artifactStore, config)
}

func (e *Engine) initKnowledgeEngine() {
	artifactQuery := newKnowledgeArtifactQuery(e.database)
	knowledgeStore := knowledge_engine.NewDBKnowledgeStore(e.database)
	config := knowledge_engine.DefaultEngineConfig()
	e.knowledgeEngine = knowledge_engine.NewEngine(artifactQuery, knowledgeStore, config)
}

func (e *Engine) initTrajectoryEngine() {
	config := trajectory_engine.DefaultConfig()
	e.trajectoryRecorder = trajectory_engine.NewTrajectoryRecorder(e.database, e.eventBus, config)
	e.trajectoryAnalyzer = trajectory_engine.NewTrajectoryAnalyzer(e.database, config)
}

func (e *Engine) initWorkflowSynthesizer() {
	store := workflow_synthesis.NewDBStore(e.database)
	config := workflow_synthesis.DefaultConfig()
	e.workflowSynthesizer = workflow_synthesis.NewSynthesizer(store, config)
}

func (e *Engine) initProductIntelligence() {
	store := product_intelligence.NewDBStore(e.database)
	cfg := product_intelligence.DefaultEngineConfig()
	e.productIntelligence = product_intelligence.NewEngine(store, cfg, e.llmClient)
}

func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return errors.New("insight: engine already running")
	}

	state, err := e.database.GetInsightState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}

	if state == nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		state = &db.InsightState{
			LastAnalysis:       now,
			NextAnalysis:       now,
			PatternsDetected:   0,
			ProposalsGenerated: 0,
			ProposalsAccepted:  0,
			AcceptanceRate:     0,
			ModelVersion:       "v1",
			ConfigJSON:         "{}",
		}
		if err := e.database.SaveInsightState(state); err != nil {
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
			slog.Error("insight: scheduler stopped with error", "error", err)
		}
	}()

	if e.eventBus != nil {
		e.subscriptionID = e.eventBus.Subscribe(e.HandleEvent)
	}

	if e.trajectoryRecorder != nil {
		if err := e.trajectoryRecorder.Start(e.ctx); err != nil {
			slog.Warn("insight: failed to start trajectory recorder", "error", err)
		}
	}

	slog.Info("insight: engine started")
	return nil
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return
	}

	if e.trajectoryRecorder != nil {
		e.trajectoryRecorder.Stop()
	}

	if e.eventBus != nil && e.subscriptionID != 0 {
		e.eventBus.Unsubscribe(e.subscriptionID)
		e.subscriptionID = 0
	}

	if e.cancel != nil {
		e.cancel()
	}
	e.running = false

	slog.Info("insight: engine stopped")
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
	slog.Info("insight event received",
		"type", event.Type,
		"source", event.Source,
		"id", event.ID)

	if e.eventStore != nil {
		if err := e.eventStore.SaveEvent(ctx, event); err != nil {
			slog.Error("insight: failed to persist event", "error", err, "event_id", event.ID)
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
		slog.Warn("insight: cannot set event bus while engine is running")
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
		return errors.New("insight: pattern engine not initialized")
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
		return errors.New("insight: proposal engine not initialized")
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
		return errors.New("insight: scorecard engine not initialized")
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
		return errors.New("insight: routing engine not initialized")
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
		FROM insight_events
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
		if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			slog.Warn("failed to parse timestamp", "event_id", e.ID, "timestamp", timestamp, "error", err)
		} else {
			e.Timestamp = parsed
		}
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

func (e *Engine) WorkflowAnalyzer() *workflow_intelligence.WorkflowAnalyzer {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.workflowAnalyzer
}

func (e *Engine) RunWorkflowAnalysis(ctx context.Context) error {
	if e.workflowAnalyzer == nil {
		return errors.New("insight: workflow analyzer not initialized")
	}
	_, err := e.workflowAnalyzer.AnalyzeWorkflowPerformance(ctx)
	return err
}

func (e *Engine) ArtifactBuilder() *artifacts.ArtifactBuilder {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.artifactBuilder
}

func (e *Engine) BuildArtifact(ctx context.Context, workflowID string) (*artifacts.Artifact, error) {
	if e.artifactBuilder == nil {
		return nil, errors.New("insight: artifact builder not initialized")
	}
	return e.artifactBuilder.Build(ctx, workflowID)
}

func (e *Engine) BuildRecentArtifacts(ctx context.Context, since time.Time) (int, error) {
	if e.artifactBuilder == nil {
		return 0, errors.New("insight: artifact builder not initialized")
	}
	return e.artifactBuilder.BuildRecent(ctx, since)
}

func (e *Engine) KnowledgeEngine() *knowledge_engine.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.knowledgeEngine
}

func (e *Engine) RunKnowledgeAnalysis(ctx context.Context) error {
	if e.knowledgeEngine == nil {
		return errors.New("insight: knowledge engine not initialized")
	}
	return e.knowledgeEngine.RunAnalysis(ctx)
}

func (e *Engine) GetKnowledgeRecommendation(ctx context.Context, problemClass, repoType string) (*knowledge_engine.KnowledgeRecommendation, error) {
	if e.knowledgeEngine == nil {
		return nil, errors.New("insight: knowledge engine not initialized")
	}
	return e.knowledgeEngine.GetRecommendation(ctx, problemClass, repoType)
}

func (e *Engine) TrajectoryRecorder() *trajectory_engine.TrajectoryRecorder {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.trajectoryRecorder
}

func (e *Engine) TrajectoryAnalyzer() *trajectory_engine.TrajectoryAnalyzer {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.trajectoryAnalyzer
}

func (e *Engine) RunTrajectoryAnalysis(ctx context.Context) (*trajectory_engine.AnalysisResult, error) {
	if e.trajectoryAnalyzer == nil {
		return nil, errors.New("insight: trajectory analyzer not initialized")
	}
	return e.trajectoryAnalyzer.Analyze(ctx)
}

func (e *Engine) GetOptimalAgentSequence(ctx context.Context, problemType, repoType string) ([]string, float64, error) {
	if e.trajectoryAnalyzer == nil {
		return nil, 0, errors.New("insight: trajectory analyzer not initialized")
	}
	return e.trajectoryAnalyzer.GetOptimalSequence(problemType, repoType)
}

func (e *Engine) GetEnrichedRecommendation(ctx context.Context, problemClass, repoType string) (*knowledge_engine.KnowledgeRecommendation, error) {
	rec, err := e.GetKnowledgeRecommendation(ctx, problemClass, repoType)
	if err != nil {
		return nil, err
	}

	if rec == nil {
		rec = &knowledge_engine.KnowledgeRecommendation{
			ProblemClass: problemClass,
			RepoType:     repoType,
		}
	}

	if e.trajectoryAnalyzer != nil {
		sequence, confidence, err := e.trajectoryAnalyzer.GetOptimalSequence(problemClass, repoType)
		if err == nil && len(sequence) > 0 {
			rec.OptimalAgentSequence = sequence
			rec.SequenceConfidence = confidence
			rec.TypicalSteps = len(sequence)
		}
	}

	return rec, nil
}

func (e *Engine) WorkflowSynthesizer() *workflow_synthesis.Synthesizer {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.workflowSynthesizer
}

func (e *Engine) RunWorkflowSynthesis(ctx context.Context) (*workflow_synthesis.SynthesisResult, error) {
	if e.workflowSynthesizer == nil {
		return nil, errors.New("insight: workflow synthesizer not initialized")
	}
	return e.workflowSynthesizer.Run(ctx)
}

func (e *Engine) GetSynthesizedWorkflow(ctx context.Context, taskType, repoType string) (*db.WorkflowCandidate, bool, error) {
	if e.workflowSynthesizer == nil {
		return nil, false, errors.New("insight: workflow synthesizer not initialized")
	}
	return e.workflowSynthesizer.SelectWorkflow(ctx, taskType, repoType)
}

func (e *Engine) RecordSynthesisResult(ctx context.Context, taskType, repoType, workflowID string, useCandidate bool, success bool, cycleTimeMin, retryCount, reviewPasses int) error {
	if e.workflowSynthesizer == nil {
		return errors.New("insight: workflow synthesizer not initialized")
	}
	return e.workflowSynthesizer.RecordResult(ctx, taskType, repoType, workflowID, useCandidate, success, cycleTimeMin, retryCount, reviewPasses)
}

func (e *Engine) GetSynthesisStats(ctx context.Context) (map[string]interface{}, error) {
	if e.workflowSynthesizer == nil {
		return nil, errors.New("insight: workflow synthesizer not initialized")
	}
	return e.workflowSynthesizer.GetExperimentStats(ctx)
}

func (e *Engine) ProductIntelligence() *product_intelligence.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.productIntelligence
}

func (e *Engine) LLMClient() llm.Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.llmClient
}

func (e *Engine) ProductIntelligenceEngine() *product_intelligence.Engine {
	return e.ProductIntelligence()
}

func (e *Engine) RunProductAnalysis(ctx context.Context, projectPath string, projectName string) (*product_intelligence.AnalysisResult, error) {
	if e.productIntelligence == nil {
		return nil, errors.New("insight: product intelligence not initialized")
	}

	project, err := e.productIntelligence.RegisterProject(ctx, projectPath, projectName)
	if err != nil {
		return nil, fmt.Errorf("register project: %w", err)
	}

	cfg := product_intelligence.ProjectAnalysisConfig{}
	return e.productIntelligence.AnalyzeProject(ctx, project.ID, cfg)
}

func newWorkflowEventQuery(db *sql.DB) workflow_intelligence.EventQuery {
	return &workflowEventQuery{db: db}
}

type workflowEventQuery struct {
	db *sql.DB
}

func (q *workflowEventQuery) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]workflow_intelligence.EventForMetrics, error) {
	if limit <= 0 {
		limit = 10000
	}
	if len(eventTypes) == 0 {
		return []workflow_intelligence.EventForMetrics{}, nil
	}

	placeholders := ""
	for i := range eventTypes {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}

	query := `SELECT id, type, timestamp, source, payload
		FROM insight_events
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

	var events []workflow_intelligence.EventForMetrics
	for rows.Next() {
		var e workflow_intelligence.EventForMetrics
		var timestamp, payloadStr string
		if err := rows.Scan(&e.ID, &e.Type, &timestamp, &e.Source, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			slog.Warn("failed to parse timestamp", "event_id", e.ID, "timestamp", timestamp, "error", err)
		} else {
			e.Timestamp = parsed
		}
		if payloadStr != "" {
			if err := json.Unmarshal([]byte(payloadStr), &e.Payload); err != nil {
				slog.Warn("failed to parse event payload", "event_id", e.ID, "error", err)
				e.Payload = make(map[string]any)
			}
		} else {
			e.Payload = make(map[string]any)
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

func newArtifactEventQuery(db *sql.DB) artifacts.EventQuery {
	return &artifactEventQuery{db: db}
}

type artifactEventQuery struct {
	db *sql.DB
}

func (q *artifactEventQuery) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]artifacts.EventForArtifact, error) {
	if limit <= 0 {
		limit = 10000
	}
	if len(eventTypes) == 0 {
		return []artifacts.EventForArtifact{}, nil
	}

	placeholders := ""
	for i := range eventTypes {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}

	query := `SELECT id, type, timestamp, source, payload
		FROM insight_events
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

	var events []artifacts.EventForArtifact
	for rows.Next() {
		var e artifacts.EventForArtifact
		var timestamp, payloadStr string
		if err := rows.Scan(&e.ID, &e.Type, &timestamp, &e.Source, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, timestamp)
		if payloadStr != "" {
			if err := json.Unmarshal([]byte(payloadStr), &e.Payload); err != nil {
				e.Payload = make(map[string]any)
			}
		} else {
			e.Payload = make(map[string]any)
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

func (q *artifactEventQuery) GetEventsByWorkflowID(ctx context.Context, workflowID string) ([]artifacts.EventForArtifact, error) {
	query := `SELECT id, type, timestamp, source, payload
		FROM insight_events
		WHERE json_extract(payload, '$.workflow_id') = ?
		ORDER BY timestamp ASC`

	rows, err := q.db.QueryContext(ctx, query, workflowID)
	if err != nil {
		return nil, fmt.Errorf("query events by workflow: %w", err)
	}
	defer rows.Close()

	var events []artifacts.EventForArtifact
	for rows.Next() {
		var e artifacts.EventForArtifact
		var timestamp, payloadStr string
		if err := rows.Scan(&e.ID, &e.Type, &timestamp, &e.Source, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, timestamp)
		if payloadStr != "" {
			if err := json.Unmarshal([]byte(payloadStr), &e.Payload); err != nil {
				e.Payload = make(map[string]any)
			}
		} else {
			e.Payload = make(map[string]any)
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

func (q *artifactEventQuery) GetCompletedWorkflowIDs(ctx context.Context, since time.Time, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 1000
	}

	query := `SELECT DISTINCT json_extract(payload, '$.workflow_id')
		FROM insight_events
		WHERE type = 'workflow.completed'
		  AND timestamp >= ?
		  AND json_extract(payload, '$.workflow_id') IS NOT NULL
		ORDER BY timestamp DESC
		LIMIT ?`

	rows, err := q.db.QueryContext(ctx, query, since.Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, fmt.Errorf("query completed workflows: %w", err)
	}
	defer rows.Close()

	var workflowIDs []string
	for rows.Next() {
		var id *string
		if err := rows.Scan(&id); err != nil {
			slog.Warn("failed to scan workflow id", "error", err)
			continue
		}
		if id != nil && *id != "" {
			workflowIDs = append(workflowIDs, *id)
		}
	}

	return workflowIDs, rows.Err()
}

func newKnowledgeArtifactQuery(database *db.DB) knowledge_engine.ArtifactQuery {
	return &knowledgeArtifactQuery{database: database}
}

type knowledgeArtifactQuery struct {
	database *db.DB
}

func (q *knowledgeArtifactQuery) GetSuccessfulArtifactsWithSolution(ctx context.Context, limit int) ([]knowledge_engine.ArtifactData, error) {
	if limit <= 0 {
		limit = 100
	}

	artifacts, err := q.database.GetSuccessfulArtifactsWithSolution(limit)
	if err != nil {
		return nil, fmt.Errorf("get successful artifacts: %w", err)
	}

	result := make([]knowledge_engine.ArtifactData, len(artifacts))
	for i, a := range artifacts {
		result[i] = knowledgeArtifactDataFromDB(a)
	}

	return result, nil
}

func (q *knowledgeArtifactQuery) ListArtifacts(ctx context.Context, filters knowledge_engine.ArtifactFilterOptions) ([]knowledge_engine.ArtifactData, error) {
	dbFilters := db.ArtifactFilters{
		WorkflowType: filters.WorkflowType,
		ProblemClass: filters.ProblemClass,
		RepoType:     filters.RepoType,
		Success:      filters.Success,
		Limit:        filters.Limit,
		Offset:       filters.Offset,
	}

	artifacts, err := q.database.ListArtifacts(dbFilters)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}

	result := make([]knowledge_engine.ArtifactData, len(artifacts))
	for i, a := range artifacts {
		result[i] = knowledgeArtifactDataFromDB(a)
	}

	return result, nil
}

func (q *knowledgeArtifactQuery) CountArtifacts(ctx context.Context) (int, error) {
	return q.database.CountArtifacts()
}

func (q *knowledgeArtifactQuery) GetProblemClassStats(ctx context.Context) ([]map[string]any, error) {
	return q.database.GetProblemClassStats()
}

func (q *knowledgeArtifactQuery) GetAgentSuccessByProblem(ctx context.Context) ([]map[string]any, error) {
	return q.database.GetAgentSuccessByProblem()
}

func knowledgeArtifactDataFromDB(a db.Artifact) knowledge_engine.ArtifactData {
	return knowledge_engine.ArtifactData{
		ID:              a.ID,
		WorkflowID:      a.WorkflowID,
		TaskType:        a.TaskType,
		WorkflowType:    a.WorkflowType,
		RepoType:        a.RepoType,
		ProblemClass:    a.ProblemClass,
		AgentsUsed:      a.AgentsUsed,
		RootCause:       a.RootCause,
		SolutionPattern: a.SolutionPattern,
		FilesChanged:    a.FilesChanged,
		ReviewResult:    a.ReviewResult,
		CycleTimeMin:    a.CycleTimeMin,
		Success:         a.Success,
		Metadata:        a.Metadata,
		CreatedAt:       parseTime(a.CreatedAt),
	}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}
