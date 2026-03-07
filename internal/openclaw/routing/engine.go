package routing

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/openclaw/scorecards"
)

type ScorecardProvider interface {
	GetAgentScorecards(ctx context.Context, window scorecards.Window, sortBy string, sortDir string, limit int) ([]scorecards.AgentScorecard, error)
	GetWorkflowScorecards(ctx context.Context, window scorecards.Window, sortBy string, sortDir string, limit int) ([]scorecards.WorkflowScorecard, error)
}

type Engine struct {
	scorecardStore ScorecardProvider
	routingStore   RoutingStore
	analyzers      []RoutingAnalyzer
	config         RoutingConfig
	mu             sync.Mutex
}

func NewEngine(scorecardStore ScorecardProvider, routingStore RoutingStore, config RoutingConfig) *Engine {
	if config.MinObservations <= 0 {
		config = DefaultRoutingConfig()
	}

	return &Engine{
		scorecardStore: scorecardStore,
		routingStore:   routingStore,
		config:         config,
		analyzers:      GetAllAnalyzers(),
	}
}

func (e *Engine) RunAnalysis(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := time.Now()
	slog.Info("openclaw routing: analysis started")

	agentMetrics, workflowMetrics, err := e.loadMetrics(ctx)
	if err != nil {
		return fmt.Errorf("load metrics: %w", err)
	}

	slog.Debug("openclaw routing: loaded metrics",
		"agents", len(agentMetrics),
		"workflows", len(workflowMetrics))

	if len(agentMetrics) == 0 && len(workflowMetrics) == 0 {
		slog.Info("openclaw routing: no metrics available for analysis")
		return nil
	}

	var allRecommendations []RoutingRecommendation
	for _, analyzer := range e.analyzers {
		recommendations := analyzer.Analyze(agentMetrics, workflowMetrics, e.config)
		allRecommendations = append(allRecommendations, recommendations...)

		slog.Debug("openclaw routing: analyzer completed",
			"analyzer", analyzer.Name(),
			"recommendations", len(recommendations))
	}

	slog.Debug("openclaw routing: total recommendations generated", "count", len(allRecommendations))

	var saved, skipped int
	dedupWindow := time.Duration(e.config.DedupWindowHours) * time.Hour

	for _, rec := range allRecommendations {
		isDup, err := e.isDuplicate(ctx, rec, dedupWindow)
		if err != nil {
			slog.Error("deduplication check failed", "error", err, "recommendation_id", rec.ID)
			continue
		}

		if isDup {
			skipped++
			continue
		}

		if err := e.routingStore.SaveRecommendation(ctx, rec); err != nil {
			slog.Error("save recommendation failed", "error", err, "recommendation_id", rec.ID)
			continue
		}

		saved++

		slog.Info("routing recommendation generated",
			"id", rec.ID,
			"workflow", rec.WorkflowType,
			"type", rec.RecommendationType,
			"agent", rec.RecommendedAgent,
			"confidence", rec.Confidence,
			"risk", rec.RiskLevel)
	}

	duration := time.Since(start)
	slog.Info("openclaw routing: analysis complete",
		"generated", len(allRecommendations),
		"saved", saved,
		"duplicated", skipped,
		"duration_ms", duration.Milliseconds())

	return nil
}

func (e *Engine) loadMetrics(ctx context.Context) ([]AgentMetrics, []WorkflowMetrics, error) {
	agentCards, err := e.scorecardStore.GetAgentScorecards(ctx, scorecards.Window7d, "total_runs", "DESC", 200)
	if err != nil {
		return nil, nil, fmt.Errorf("get agent scorecards: %w", err)
	}

	workflowCards, err := e.scorecardStore.GetWorkflowScorecards(ctx, scorecards.Window7d, "total_runs", "DESC", 200)
	if err != nil {
		return nil, nil, fmt.Errorf("get workflow scorecards: %w", err)
	}

	agentMetrics := make([]AgentMetrics, len(agentCards))
	for i, card := range agentCards {
		agentMetrics[i] = AgentMetrics{
			AgentName:      card.AgentName,
			TotalRuns:      card.TotalRuns,
			SuccessRate:    card.SuccessRate,
			FailureRate:    card.FailureRate,
			ReviewPassRate: card.ReviewPassRate,
			ReworkRate:     card.ReworkRate,
			Trend:          string(card.Trend),
		}
	}

	workflowMetrics := make([]WorkflowMetrics, len(workflowCards))
	for i, card := range workflowCards {
		workflowMetrics[i] = WorkflowMetrics{
			WorkflowType:        card.WorkflowType,
			TotalRuns:           card.TotalRuns,
			CompletionRate:      card.CompletionRate,
			FailureRate:         card.FailureRate,
			ReviewRejectionRate: card.ReviewRejectionRate,
			ReworkRate:          card.ReworkRate,
			Trend:               string(card.Trend),
			AgentCount:          1,
		}
	}

	return agentMetrics, workflowMetrics, nil
}

func (e *Engine) isDuplicate(ctx context.Context, rec RoutingRecommendation, within time.Duration) (bool, error) {
	similar, err := e.routingStore.FindSimilarRecommendation(ctx, rec, within)
	if err != nil {
		return false, err
	}

	if similar != nil {
		slog.Debug("routing recommendation deduplicated",
			"type", rec.RecommendationType,
			"workflow", rec.WorkflowType,
			"similar_id", similar.ID)
		return true, nil
	}

	return false, nil
}

func (e *Engine) AddAnalyzer(analyzer RoutingAnalyzer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.analyzers = append(e.analyzers, analyzer)
}

func (e *Engine) SetConfig(config RoutingConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

func (e *Engine) Config() RoutingConfig {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.config
}

func (e *Engine) GetRecentRecommendations(ctx context.Context, limit int, filters RecommendationFilters) ([]RoutingRecommendation, error) {
	return e.routingStore.GetRecentRecommendations(ctx, limit, filters)
}

func (e *Engine) GetRecommendationByID(ctx context.Context, id string) (*RoutingRecommendation, error) {
	return e.routingStore.GetRecommendationByID(ctx, id)
}

func (e *Engine) CleanupOldRecommendations(ctx context.Context, olderThan time.Duration) (int64, error) {
	return e.routingStore.DeleteOldRecommendations(ctx, olderThan)
}
