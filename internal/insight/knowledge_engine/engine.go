package knowledge_engine

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Engine struct {
	artifactQuery  ArtifactQuery
	knowledgeStore KnowledgeStore
	config         EngineConfig
	mu             sync.Mutex
}

type EngineConfig struct {
	MinOccurrencesForPattern   int     `json:"min_occurrences_for_pattern"`
	MinSuccessRateForPattern   float64 `json:"min_success_rate_for_pattern"`
	MaxExampleArtifacts        int     `json:"max_example_artifacts"`
	PatternConfidenceThreshold float64 `json:"pattern_confidence_threshold"`
	AnalysisBatchSize          int     `json:"analysis_batch_size"`
}

func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		MinOccurrencesForPattern:   2,
		MinSuccessRateForPattern:   0.6,
		MaxExampleArtifacts:        5,
		PatternConfidenceThreshold: 0.7,
		AnalysisBatchSize:          500,
	}
}

func NewEngine(artifactQuery ArtifactQuery, knowledgeStore KnowledgeStore, config EngineConfig) *Engine {
	if config.MinOccurrencesForPattern <= 0 {
		config = DefaultEngineConfig()
	}

	return &Engine{
		artifactQuery:  artifactQuery,
		knowledgeStore: knowledgeStore,
		config:         config,
	}
}

func (e *Engine) RunAnalysis(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := time.Now()
	slog.Info("knowledge_engine: analysis started")

	if err := e.BuildProblemStatistics(ctx); err != nil {
		slog.Error("knowledge_engine: failed to build problem statistics", "error", err)
	}

	if err := e.MineSolutionPatterns(ctx); err != nil {
		slog.Error("knowledge_engine: failed to mine solution patterns", "error", err)
	}

	if err := e.BuildAgentPerformanceByProblem(ctx); err != nil {
		slog.Error("knowledge_engine: failed to build agent performance", "error", err)
	}

	duration := time.Since(start)
	slog.Info("knowledge_engine: analysis complete", "duration_ms", duration.Milliseconds())

	return nil
}

func (e *Engine) BuildProblemStatistics(ctx context.Context) error {
	slog.Debug("knowledge_engine: building problem statistics")

	artifacts, err := e.artifactQuery.ListArtifacts(ctx, ArtifactFilterOptions{Limit: e.config.AnalysisBatchSize})
	if err != nil {
		return err
	}

	if len(artifacts) == 0 {
		slog.Debug("knowledge_engine: no artifacts to analyze")
		return nil
	}

	type agentTracker struct {
		success int
		total   int
	}

	statsByProblemAndRepo := make(map[string]*ProblemStats)
	agentSuccessTracker := make(map[string]*agentTracker)

	for _, artifact := range artifacts {
		if artifact.ProblemClass == "" || artifact.ProblemClass == "unknown" {
			continue
		}

		key := artifact.ProblemClass + "|" + artifact.RepoType
		if statsByProblemAndRepo[key] == nil {
			statsByProblemAndRepo[key] = &ProblemStats{
				ID:            uuid.NewString(),
				ProblemClass:  artifact.ProblemClass,
				RepoType:      artifact.RepoType,
				AgentsSuccess: make(map[string]float64),
			}
		}

		stats := statsByProblemAndRepo[key]
		stats.OccurrenceCount++

		var prevTotal float64 = float64(stats.OccurrenceCount - 1)
		var newTotal float64 = float64(stats.OccurrenceCount)
		if artifact.Success {
			stats.SuccessRate = (stats.SuccessRate*prevTotal + 1.0) / newTotal
		} else {
			stats.SuccessRate = (stats.SuccessRate * prevTotal) / newTotal
		}

		if artifact.CycleTimeMin > 0 {
			stats.AvgCycleTime = (stats.AvgCycleTime*(stats.OccurrenceCount-1) + artifact.CycleTimeMin) / stats.OccurrenceCount
		}

		for _, agent := range artifact.AgentsUsed {
			agentKey := key + "|" + agent
			if agentSuccessTracker[agentKey] == nil {
				agentSuccessTracker[agentKey] = &agentTracker{}
			}
			agentSuccessTracker[agentKey].total++
			if artifact.Success {
				agentSuccessTracker[agentKey].success++
			}
		}

		if artifact.WorkflowType != "" {
			stats.BestWorkflow = artifact.WorkflowType
		}
	}

	for agentKey, tracker := range agentSuccessTracker {
		if tracker.total > 0 {
			parts := strings.Split(agentKey, "|")
			if len(parts) >= 3 {
				problemRepoKey := parts[0] + "|" + parts[1]
				agent := parts[2]
				if stats, exists := statsByProblemAndRepo[problemRepoKey]; exists {
					stats.AgentsSuccess[agent] = float64(tracker.success) / float64(tracker.total)
				}
			}
		}
	}

	for _, stats := range statsByProblemAndRepo {
		var bestAgent string
		var bestRate float64
		for agent, rate := range stats.AgentsSuccess {
			if rate > bestRate {
				bestRate = rate
				bestAgent = agent
			}
		}
		stats.BestAgent = bestAgent

		if err := e.knowledgeStore.SaveProblemStats(ctx, *stats); err != nil {
			slog.Warn("failed to save problem stats", "problem_class", stats.ProblemClass, "error", err)
		}
	}

	slog.Info("knowledge_engine: problem statistics built", "count", len(statsByProblemAndRepo))
	return nil
}

func (e *Engine) MineSolutionPatterns(ctx context.Context) error {
	slog.Debug("knowledge_engine: mining solution patterns")

	artifacts, err := e.artifactQuery.GetSuccessfulArtifactsWithSolution(ctx, e.config.AnalysisBatchSize)
	if err != nil {
		return err
	}

	if len(artifacts) == 0 {
		slog.Debug("knowledge_engine: no successful artifacts with solutions")
		return nil
	}

	patternMap := make(map[string]*patternAggregator)

	for _, artifact := range artifacts {
		if artifact.ProblemClass == "" || artifact.SolutionPattern == "" {
			continue
		}

		key := artifact.ProblemClass + "|" + artifact.SolutionPattern + "|" + artifact.RepoType
		if patternMap[key] == nil {
			patternMap[key] = &patternAggregator{
				problemClass:    artifact.ProblemClass,
				solutionPattern: artifact.SolutionPattern,
				repoType:        artifact.RepoType,
				exampleIDs:      []string{},
			}
		}

		p := patternMap[key]
		p.totalCount++
		if artifact.Success {
			p.successCount++
		}
		if len(p.exampleIDs) < e.config.MaxExampleArtifacts {
			p.exampleIDs = append(p.exampleIDs, artifact.ID)
		}
	}

	var savedPatterns int
	for _, agg := range patternMap {
		if agg.totalCount < e.config.MinOccurrencesForPattern {
			continue
		}

		successRate := float64(agg.successCount) / float64(agg.totalCount)
		if successRate < e.config.MinSuccessRateForPattern {
			continue
		}

		confidence := calculatePatternConfidence(agg.totalCount, successRate)
		if confidence < e.config.PatternConfidenceThreshold {
			continue
		}

		pattern := SolutionPattern{
			ID:               uuid.NewString(),
			ProblemClass:     agg.problemClass,
			SolutionPattern:  agg.solutionPattern,
			RepoType:         agg.repoType,
			SuccessRate:      successRate,
			OccurrenceCount:  agg.totalCount,
			ExampleArtifacts: agg.exampleIDs,
			Confidence:       confidence,
			FirstSeen:        time.Now().UTC(),
			LastSeen:         time.Now().UTC(),
		}

		if err := e.knowledgeStore.SaveSolutionPattern(ctx, pattern); err != nil {
			slog.Warn("failed to save solution pattern", "pattern", agg.solutionPattern, "error", err)
			continue
		}

		savedPatterns++
	}

	slog.Info("knowledge_engine: solution patterns mined", "count", savedPatterns)
	return nil
}

func (e *Engine) BuildAgentPerformanceByProblem(ctx context.Context) error {
	slog.Debug("knowledge_engine: building agent performance by problem")

	agentSuccess, err := e.artifactQuery.GetAgentSuccessByProblem(ctx)
	if err != nil {
		return err
	}

	if len(agentSuccess) == 0 {
		slog.Debug("knowledge_engine: no agent performance data")
		return nil
	}

	slog.Info("knowledge_engine: agent performance data collected", "problems", len(agentSuccess))
	return nil
}

type patternAggregator struct {
	problemClass    string
	solutionPattern string
	repoType        string
	totalCount      int
	successCount    int
	exampleIDs      []string
}

func calculatePatternConfidence(occurrences int, successRate float64) float64 {
	occurrenceFactor := 1.0
	switch {
	case occurrences >= 10:
		occurrenceFactor = 1.0
	case occurrences >= 5:
		occurrenceFactor = 0.85
	case occurrences >= 3:
		occurrenceFactor = 0.7
	default:
		occurrenceFactor = 0.5
	}

	successFactor := successRate

	confidence := 0.6*occurrenceFactor + 0.4*successFactor
	if confidence > 0.95 {
		confidence = 0.95
	}

	return confidence
}

func (e *Engine) GetRecommendation(ctx context.Context, problemClass, repoType string) (*KnowledgeRecommendation, error) {
	bestSolution, err := e.knowledgeStore.GetBestSolutionForProblem(ctx, problemClass, repoType)
	if err != nil {
		return nil, err
	}

	bestAgent, agentSuccess, err := e.knowledgeStore.GetBestAgentForProblem(ctx, problemClass, repoType)
	if err != nil {
		return nil, err
	}

	stats, err := e.knowledgeStore.GetProblemStatsByClassAndRepo(ctx, problemClass, repoType)
	if err != nil {
		return nil, err
	}

	if bestSolution == nil && bestAgent == "" && stats == nil {
		return nil, nil
	}

	rec := &KnowledgeRecommendation{
		ProblemClass: problemClass,
		RepoType:     repoType,
	}

	if bestSolution != nil {
		rec.SolutionPattern = bestSolution.SolutionPattern
		rec.SolutionConfidence = bestSolution.Confidence
		rec.SolutionSuccessRate = bestSolution.SuccessRate
	}

	if bestAgent != "" {
		rec.BestAgent = bestAgent
		rec.AgentSuccessRate = agentSuccess
	}

	if stats != nil {
		rec.OverallSuccessRate = stats.SuccessRate
		rec.AvgCycleTime = stats.AvgCycleTime
		rec.OccurrenceCount = stats.OccurrenceCount
	}

	return rec, nil
}

type KnowledgeRecommendation struct {
	ProblemClass         string   `json:"problem_class"`
	RepoType             string   `json:"repo_type"`
	BestAgent            string   `json:"best_agent,omitempty"`
	AgentSuccessRate     float64  `json:"agent_success_rate"`
	SolutionPattern      string   `json:"solution_pattern,omitempty"`
	SolutionConfidence   float64  `json:"solution_confidence"`
	SolutionSuccessRate  float64  `json:"solution_success_rate"`
	OverallSuccessRate   float64  `json:"overall_success_rate"`
	AvgCycleTime         int      `json:"avg_cycle_time"`
	OccurrenceCount      int      `json:"occurrence_count"`
	OptimalAgentSequence []string `json:"optimal_agent_sequence,omitempty"`
	SequenceConfidence   float64  `json:"sequence_confidence"`
	TypicalSteps         int      `json:"typical_steps"`
}

func (e *Engine) SetConfig(config EngineConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

func (e *Engine) Config() EngineConfig {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.config
}

func (e *Engine) GetStats(ctx context.Context) (*KnowledgeStats, error) {
	patternCount, err := e.knowledgeStore.CountSolutionPatterns(ctx)
	if err != nil {
		return nil, err
	}

	statsCount, err := e.knowledgeStore.CountProblemStats(ctx)
	if err != nil {
		return nil, err
	}

	artifactCount, err := e.artifactQuery.CountArtifacts(ctx)
	if err != nil {
		return nil, err
	}

	return &KnowledgeStats{
		TotalArtifacts:      artifactCount,
		SolutionPatterns:    patternCount,
		ProblemStatsEntries: statsCount,
	}, nil
}

type KnowledgeStats struct {
	TotalArtifacts      int `json:"total_artifacts"`
	SolutionPatterns    int `json:"solution_patterns"`
	ProblemStatsEntries int `json:"problem_stats_entries"`
}
