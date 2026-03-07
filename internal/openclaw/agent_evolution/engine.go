package agent_evolution

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type Engine struct {
	store        Store
	analyzer     *AgentAnalyzer
	planner      *AgentEvolutionPlanner
	generator    *AgentCandidateGenerator
	runner       *AgentExperimentRunner
	config       Config
	logger       *slog.Logger
	mu           sync.Mutex
	lastAnalysis time.Time
}

func NewEngine(database *db.DB, config Config, claudeDir, opencodeDir string, logger *slog.Logger) *Engine {
	store := NewDBStore(database)

	return &Engine{
		store:     store,
		analyzer:  NewAgentAnalyzer(database, database, config),
		planner:   NewAgentEvolutionPlanner(config),
		generator: NewAgentCandidateGenerator(store, config, claudeDir, opencodeDir),
		runner:    NewAgentExperimentRunner(store, config),
		config:    config,
		logger:    logger,
	}
}

func NewEngineWithStore(store Store, config Config, claudeDir, opencodeDir string, logger *slog.Logger) *Engine {
	return &Engine{
		store:     store,
		analyzer:  NewAgentAnalyzer(nil, nil, config),
		planner:   NewAgentEvolutionPlanner(config),
		generator: NewAgentCandidateGenerator(store, config, claudeDir, opencodeDir),
		runner:    NewAgentExperimentRunner(store, config),
		config:    config,
		logger:    logger,
	}
}

func (e *Engine) Run(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.logger.Info("Running agent evolution analysis")

	profiles, err := e.analyzer.Analyze(ctx)
	if err != nil {
		e.logger.Error("Failed to analyze agent performance", "error", err)
		return err
	}

	e.logger.Info("Analyzed agent profiles", "count", len(profiles))

	opportunities := e.planner.DetectOpportunities(ctx, profiles)
	e.logger.Info("Detected evolution opportunities", "count", len(opportunities))

	candidates, err := e.generator.GenerateCandidates(ctx, opportunities)
	if err != nil {
		e.logger.Error("Failed to generate candidates", "error", err)
		return err
	}

	e.logger.Info("Generated agent candidates", "count", len(candidates))

	e.lastAnalysis = time.Now().UTC()

	return nil
}

func (e *Engine) RunExperiments(ctx context.Context) error {
	experiments, err := e.store.ListRunningExperiments(ctx)
	if err != nil {
		return err
	}

	for _, exp := range experiments {
		metrics, err := e.EvaluateExperiment(ctx, exp.ID)
		if err != nil {
			e.logger.Error("Failed to evaluate experiment", "experiment_id", exp.ID, "error", err)
			continue
		}

		totalRuns := metrics.RunsCandidate + metrics.RunsBaseline
		if totalRuns >= exp.SampleSize {
			winner := metrics.DetermineWinner()
			e.logger.Info("Experiment completed",
				"experiment_id", exp.ID,
				"winner", winner,
				"candidate_success_rate", metrics.CandidateSuccessRate,
				"baseline_success_rate", metrics.BaselineSuccessRate,
			)
		}
	}

	return nil
}

func (e *Engine) GetStore() Store {
	return e.store
}

func (e *AgentAnalyzer) SetScorecards(scorecards ScorecardQuery) {
	e.scorecards = scorecards
}

func (e *AgentAnalyzer) SetArtifacts(artifacts ArtifactQuery) {
	e.artifacts = artifacts
}

func (e *Engine) SetScorecardQuery(scorecards ScorecardQuery) {
	e.analyzer.SetScorecards(scorecards)
}

func (e *Engine) SetArtifactQuery(artifacts ArtifactQuery) {
	e.analyzer.SetArtifacts(artifacts)
}

func (e *Engine) GetProfiles(ctx context.Context) ([]AgentPerformanceProfile, error) {
	return e.analyzer.Analyze(ctx)
}

func (e *Engine) GetOpportunities(ctx context.Context) ([]EvolutionOpportunity, error) {
	profiles, err := e.analyzer.Analyze(ctx)
	if err != nil {
		return nil, err
	}
	return e.planner.DetectOpportunities(ctx, profiles), nil
}

func (e *Engine) GetCandidates(ctx context.Context, status string, limit int) ([]CandidateAgent, error) {
	return e.store.ListCandidates(ctx, status, limit)
}

func (e *Engine) GetCandidate(ctx context.Context, id string) (*CandidateAgent, error) {
	return e.store.GetCandidateByID(ctx, id)
}

func (e *Engine) ApproveCandidate(ctx context.Context, id string) error {
	candidate, err := e.store.GetCandidateByID(ctx, id)
	if err != nil {
		return err
	}
	if candidate == nil {
		return nil
	}

	if err := e.generator.WriteAgentFiles(*candidate); err != nil {
		return err
	}

	return e.store.UpdateCandidateStatus(ctx, id, CandidatePromoted)
}

func (e *Engine) RejectCandidate(ctx context.Context, id string) error {
	return e.store.UpdateCandidateStatus(ctx, id, CandidateRejected)
}

func (e *Engine) StartExperiment(ctx context.Context, candidateID string) (*AgentExperiment, error) {
	return e.runner.StartExperiment(ctx, candidateID)
}

func (e *Engine) GetExperiment(ctx context.Context, id string) (*AgentExperiment, error) {
	return e.store.GetExperimentByID(ctx, id)
}

func (e *Engine) GetExperiments(ctx context.Context, status string, limit int) ([]AgentExperiment, error) {
	return e.store.ListExperiments(ctx, status, limit)
}

func (e *Engine) EvaluateExperiment(ctx context.Context, experimentID string) (*ExperimentMetrics, error) {
	return e.runner.EvaluateExperiment(ctx, experimentID)
}

func (e *Engine) RecordExperimentResult(ctx context.Context, experimentID string, result ExperimentResult) error {
	return e.runner.RecordResult(ctx, experimentID, result)
}

func (e *Engine) CancelExperiment(ctx context.Context, experimentID string) error {
	return e.runner.CancelExperiment(ctx, experimentID)
}

func (e *Engine) GetActiveExperimentForAgent(ctx context.Context, baselineAgent string) (*AgentExperiment, error) {
	return e.runner.GetActiveExperimentForAgent(ctx, baselineAgent)
}

func (e *Engine) ShouldUseCandidate(ctx context.Context, experimentID string) (bool, error) {
	return e.runner.ShouldUseCandidate(ctx, experimentID)
}

func (e *Engine) GetLastAnalysis() time.Time {
	return e.lastAnalysis
}

type EvolutionSummary struct {
	TotalCandidates      int                    `json:"total_candidates"`
	PendingCandidates    int                    `json:"pending_candidates"`
	RunningExperiments   int                    `json:"running_experiments"`
	CompletedExperiments int                    `json:"completed_experiments"`
	PromotedAgents       int                    `json:"promoted_agents"`
	RejectedAgents       int                    `json:"rejected_agents"`
	LastAnalysis         time.Time              `json:"last_analysis"`
	TopOpportunities     []EvolutionOpportunity `json:"top_opportunities,omitempty"`
}

func (e *Engine) GetSummary(ctx context.Context) (*EvolutionSummary, error) {
	summary := &EvolutionSummary{
		LastAnalysis: e.lastAnalysis,
	}

	pending, _ := e.store.ListCandidates(ctx, string(CandidatePending), 100)
	summary.PendingCandidates = len(pending)
	summary.TotalCandidates = summary.PendingCandidates

	promoted, _ := e.store.ListCandidates(ctx, string(CandidatePromoted), 100)
	summary.PromotedAgents = len(promoted)
	summary.TotalCandidates += summary.PromotedAgents

	rejected, _ := e.store.ListCandidates(ctx, string(CandidateRejected), 100)
	summary.RejectedAgents = len(rejected)
	summary.TotalCandidates += summary.RejectedAgents

	running, _ := e.store.ListRunningExperiments(ctx)
	summary.RunningExperiments = len(running)

	completed, _ := e.store.ListExperiments(ctx, string(ExperimentCompleted), 100)
	summary.CompletedExperiments = len(completed)

	return summary, nil
}
