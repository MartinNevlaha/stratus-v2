package agent_evolution

import (
	"context"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type ScorecardQuery interface {
	ListAgentScorecards(window string, sortBy string, sortDir string, limit int) ([]db.AgentScorecard, error)
	GetAgentScorecardByName(agentName string, window string) (*db.AgentScorecard, error)
}

type ArtifactQuery interface {
	ListArtifacts(filters db.ArtifactFilters) ([]db.Artifact, error)
}

type AgentAnalyzer struct {
	scorecards ScorecardQuery
	artifacts  ArtifactQuery
	config     Config
}

func NewAgentAnalyzer(scorecards ScorecardQuery, artifacts ArtifactQuery, config Config) *AgentAnalyzer {
	return &AgentAnalyzer{
		scorecards: scorecards,
		artifacts:  artifacts,
		config:     config,
	}
}

func (a *AgentAnalyzer) Analyze(ctx context.Context) ([]AgentPerformanceProfile, error) {
	scorecards, err := a.scorecards.ListAgentScorecards("30d", "total_runs", "desc", 100)
	if err != nil {
		return nil, err
	}

	profiles := make([]AgentPerformanceProfile, 0, len(scorecards))
	for _, card := range scorecards {
		if card.TotalRuns < a.config.MinRunsForAnalysis {
			continue
		}

		profile := a.buildProfile(card)
		profiles = append(profiles, profile)
	}

	return profiles, nil
}

func (a *AgentAnalyzer) AnalyzeAgent(ctx context.Context, agentName string) (*AgentPerformanceProfile, error) {
	card, err := a.scorecards.GetAgentScorecardByName(agentName, "30d")
	if err != nil {
		return nil, err
	}
	if card == nil {
		return nil, nil
	}

	profile := a.buildProfile(*card)

	artifacts, err := a.artifacts.ListArtifacts(db.ArtifactFilters{Limit: 500})
	if err == nil {
		a.enrichProfileWithArtifacts(&profile, artifacts)
	}

	return &profile, nil
}

func (a *AgentAnalyzer) buildProfile(card db.AgentScorecard) AgentPerformanceProfile {
	windowStart, _ := time.Parse(time.RFC3339Nano, card.WindowStart)
	windowEnd, _ := time.Parse(time.RFC3339Nano, card.WindowEnd)

	confidence := a.calculateConfidence(card)

	return AgentPerformanceProfile{
		AgentName:          card.AgentName,
		TotalRuns:          card.TotalRuns,
		SuccessRate:        card.SuccessRate,
		FailureRate:        card.FailureRate,
		ReviewPassRate:     card.ReviewPassRate,
		ReworkRate:         card.ReworkRate,
		AvgCycleTimeMs:     card.AvgCycleTimeMs,
		Trend:              card.Trend,
		TaskTypeFrequency:  make(map[string]int),
		RepoTypeFrequency:  make(map[string]int),
		ProblemFrequency:   make(map[string]int),
		CommonFailureModes: []string{},
		ConfidenceScore:    confidence,
		WindowStart:        windowStart,
		WindowEnd:          windowEnd,
	}
}

func (a *AgentAnalyzer) enrichProfileWithArtifacts(profile *AgentPerformanceProfile, artifacts []db.Artifact) {
	for _, artifact := range artifacts {
		found := false
		for _, agent := range artifact.AgentsUsed {
			if agent == profile.AgentName {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		if artifact.TaskType != "" {
			profile.TaskTypeFrequency[artifact.TaskType]++
		}
		if artifact.RepoType != "" {
			profile.RepoTypeFrequency[artifact.RepoType]++
		}
		if artifact.ProblemClass != "" {
			profile.ProblemFrequency[artifact.ProblemClass]++
		}

		if !artifact.Success && artifact.RootCause != "" {
			profile.CommonFailureModes = append(profile.CommonFailureModes, artifact.RootCause)
		}
	}
}

func (a *AgentAnalyzer) calculateConfidence(card db.AgentScorecard) float64 {
	confidence := 0.5

	if card.TotalRuns >= 100 {
		confidence += 0.3
	} else if card.TotalRuns >= 50 {
		confidence += 0.2
	} else if card.TotalRuns >= 20 {
		confidence += 0.1
	}

	if card.Trend == "improving" {
		confidence += 0.1
	} else if card.Trend == "degrading" {
		confidence -= 0.1
	}

	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.1 {
		confidence = 0.1
	}

	return confidence
}

func (a *AgentAnalyzer) GetTopPerformers(ctx context.Context, limit int) ([]AgentPerformanceProfile, error) {
	scorecards, err := a.scorecards.ListAgentScorecards("30d", "success_rate", "desc", limit)
	if err != nil {
		return nil, err
	}

	profiles := make([]AgentPerformanceProfile, 0, len(scorecards))
	for _, card := range scorecards {
		if card.TotalRuns >= a.config.MinRunsForAnalysis {
			profiles = append(profiles, a.buildProfile(card))
		}
	}

	return profiles, nil
}

func (a *AgentAnalyzer) GetUnderperformers(ctx context.Context) ([]AgentPerformanceProfile, error) {
	scorecards, err := a.scorecards.ListAgentScorecards("30d", "success_rate", "asc", 50)
	if err != nil {
		return nil, err
	}

	profiles := make([]AgentPerformanceProfile, 0)
	for _, card := range scorecards {
		if card.TotalRuns >= a.config.MinRunsForAnalysis && card.SuccessRate < a.config.DeprecationThreshold {
			profiles = append(profiles, a.buildProfile(card))
		}
	}

	return profiles, nil
}
