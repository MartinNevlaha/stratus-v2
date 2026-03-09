package agent_evolution

import (
	"context"
	"sort"
)

type AgentEvolutionPlanner struct {
	config Config
}

func NewAgentEvolutionPlanner(config Config) *AgentEvolutionPlanner {
	return &AgentEvolutionPlanner{config: config}
}

func (p *AgentEvolutionPlanner) DetectOpportunities(ctx context.Context, profiles []AgentPerformanceProfile) []EvolutionOpportunity {
	opportunities := make([]EvolutionOpportunity, 0)

	for _, profile := range profiles {
		if profile.TotalRuns < p.config.MinRunsForAnalysis {
			continue
		}

		opportunities = append(opportunities, p.detectSpecialization(profile)...)
		opportunities = append(opportunities, p.detectPromptImprovement(profile)...)
		opportunities = append(opportunities, p.detectDeprecation(profile)...)
	}

	sort.Slice(opportunities, func(i, j int) bool {
		return opportunities[i].Confidence > opportunities[j].Confidence
	})

	if len(opportunities) > p.config.MaxCandidatesPerRun {
		opportunities = opportunities[:p.config.MaxCandidatesPerRun]
	}

	return opportunities
}

func (p *AgentEvolutionPlanner) detectSpecialization(profile AgentPerformanceProfile) []EvolutionOpportunity {
	opportunities := make([]EvolutionOpportunity, 0)

	totalInRepo := 0
	for _, f := range profile.RepoTypeFrequency {
		totalInRepo += f
	}

	for repoType, freq := range profile.RepoTypeFrequency {
		if totalInRepo == 0 {
			continue
		}

		ratio := float64(freq) / float64(totalInRepo)
		if ratio >= p.config.SpecializationThreshold && freq >= 10 {
			specialization := repoType
			candidateName := p.generateSpecialistName(profile.AgentName, specialization)

			confidence := p.calculateSpecializationConfidence(profile, ratio, freq)

			opp := NewEvolutionOpportunity(
				profile.AgentName,
				OpportunitySpecialization,
				specialization,
				p.buildSpecializationReason(profile.AgentName, specialization, ratio, freq),
				confidence,
				freq,
			)

			opp.Evidence["repo_type"] = repoType
			opp.Evidence["frequency"] = freq
			opp.Evidence["ratio"] = ratio
			opp.Evidence["success_rate"] = profile.SuccessRate
			opp.Evidence["candidate_name"] = candidateName

			opportunities = append(opportunities, opp)
		}
	}

	totalProblems := 0
	for _, f := range profile.ProblemFrequency {
		totalProblems += f
	}

	for problemClass, freq := range profile.ProblemFrequency {
		if totalProblems == 0 {
			continue
		}

		ratio := float64(freq) / float64(totalProblems)
		if ratio >= 0.50 && freq >= 10 {
			confidence := p.calculateSpecializationConfidence(profile, ratio, freq)

			opp := NewEvolutionOpportunity(
				profile.AgentName,
				OpportunitySpecialization,
				problemClass,
				p.buildProblemSpecializationReason(profile.AgentName, problemClass, ratio, freq),
				confidence,
				freq,
			)

			opp.Evidence["problem_class"] = problemClass
			opp.Evidence["frequency"] = freq
			opp.Evidence["ratio"] = ratio
			opp.Evidence["success_rate"] = profile.SuccessRate

			opportunities = append(opportunities, opp)
		}
	}

	return opportunities
}

func (p *AgentEvolutionPlanner) detectPromptImprovement(profile AgentPerformanceProfile) []EvolutionOpportunity {
	opportunities := make([]EvolutionOpportunity, 0)

	if profile.ReworkRate >= 0.30 {
		confidence := 0.6 + (profile.ReworkRate-0.30)*0.5
		if confidence > 0.95 {
			confidence = 0.95
		}

		opp := NewEvolutionOpportunity(
			profile.AgentName,
			OpportunityPromptImprove,
			"rework_reduction",
			p.buildReworkImprovementReason(profile.AgentName, profile.ReworkRate),
			confidence,
			int(float64(profile.TotalRuns)*profile.ReworkRate),
		)

		opp.Evidence["rework_rate"] = profile.ReworkRate
		opp.Evidence["total_runs"] = profile.TotalRuns
		opp.Evidence["trend"] = profile.Trend

		opportunities = append(opportunities, opp)
	}

	if profile.ReviewPassRate < 0.60 && profile.TotalRuns >= 20 {
		confidence := 0.55 + (0.60-profile.ReviewPassRate)*0.5
		if confidence > 0.95 {
			confidence = 0.95
		}

		opp := NewEvolutionOpportunity(
			profile.AgentName,
			OpportunityPromptImprove,
			"review_pass_improvement",
			p.buildReviewImprovementReason(profile.AgentName, profile.ReviewPassRate),
			confidence,
			profile.TotalRuns,
		)

		opp.Evidence["review_pass_rate"] = profile.ReviewPassRate
		opp.Evidence["total_runs"] = profile.TotalRuns

		opportunities = append(opportunities, opp)
	}

	failureModes := make(map[string]int)
	for _, mode := range profile.CommonFailureModes {
		failureModes[mode]++
	}

	for mode, count := range failureModes {
		if count >= 3 {
			confidence := 0.5 + float64(count)*0.05
			if confidence > 0.85 {
				confidence = 0.85
			}

			opp := NewEvolutionOpportunity(
				profile.AgentName,
				OpportunityPromptImprove,
				mode,
				p.buildFailureModeReason(profile.AgentName, mode, count),
				confidence,
				count,
			)

			opp.Evidence["failure_mode"] = mode
			opp.Evidence["occurrences"] = count
			opp.Evidence["total_runs"] = profile.TotalRuns

			opportunities = append(opportunities, opp)
		}
	}

	return opportunities
}

func (p *AgentEvolutionPlanner) detectDeprecation(profile AgentPerformanceProfile) []EvolutionOpportunity {
	opportunities := make([]EvolutionOpportunity, 0)

	if profile.SuccessRate < p.config.DeprecationThreshold && profile.TotalRuns >= 20 {
		confidence := 0.7 + (p.config.DeprecationThreshold-profile.SuccessRate)*0.5
		if confidence > 0.95 {
			confidence = 0.95
		}

		if profile.Trend == "degrading" {
			confidence += 0.1
		}

		opp := NewEvolutionOpportunity(
			profile.AgentName,
			OpportunityDeprecation,
			"",
			p.buildDeprecationReason(profile.AgentName, profile.SuccessRate, profile.TotalRuns),
			confidence,
			profile.TotalRuns,
		)

		opp.Evidence["success_rate"] = profile.SuccessRate
		opp.Evidence["total_runs"] = profile.TotalRuns
		opp.Evidence["trend"] = profile.Trend
		opp.Evidence["threshold"] = p.config.DeprecationThreshold

		opportunities = append(opportunities, opp)
	}

	return opportunities
}

func (p *AgentEvolutionPlanner) calculateSpecializationConfidence(profile AgentPerformanceProfile, ratio float64, frequency int) float64 {
	confidence := 0.5

	confidence += ratio * 0.2

	if frequency >= 50 {
		confidence += 0.15
	} else if frequency >= 20 {
		confidence += 0.10
	} else if frequency >= 10 {
		confidence += 0.05
	}

	if profile.SuccessRate >= 0.70 {
		confidence += 0.15
	} else if profile.SuccessRate >= 0.50 {
		confidence += 0.05
	}

	if confidence > 0.95 {
		confidence = 0.95
	}

	return confidence
}

func (p *AgentEvolutionPlanner) generateSpecialistName(baseAgent, specialization string) string {
	shortName := specialization
	if len(shortName) > 15 {
		shortName = shortName[:15]
	}
	return baseAgent + "-" + shortName
}

func (p *AgentEvolutionPlanner) generateProblemSpecialistName(baseAgent, problemClass string) string {
	return baseAgent + "-" + problemClass + "-specialist"
}

func (p *AgentEvolutionPlanner) buildSpecializationReason(agent, specialization string, ratio float64, freq int) string {
	return agent + " handles " + specialization + " tasks " + formatPercent(ratio) + " of the time (" + itoa(freq) + " runs)"
}

func (p *AgentEvolutionPlanner) buildProblemSpecializationReason(agent, problemClass string, ratio float64, freq int) string {
	return agent + " frequently solves " + problemClass + " problems (" + formatPercent(ratio) + ", " + itoa(freq) + " occurrences)"
}

func (p *AgentEvolutionPlanner) buildReworkImprovementReason(agent string, reworkRate float64) string {
	return agent + " has high rework rate of " + formatPercent(reworkRate) + " - prompt improvements may reduce iterations"
}

func (p *AgentEvolutionPlanner) buildReviewImprovementReason(agent string, reviewPassRate float64) string {
	return agent + " has low review pass rate of " + formatPercent(reviewPassRate) + " - prompt improvements may improve quality"
}

func (p *AgentEvolutionPlanner) buildFailureModeReason(agent, mode string, count int) string {
	return agent + " has repeated failure mode '" + mode + "' (" + itoa(count) + " occurrences) - prompt adjustment recommended"
}

func (p *AgentEvolutionPlanner) buildDeprecationReason(agent string, successRate float64, totalRuns int) string {
	return agent + " has success rate of " + formatPercent(successRate) + " over " + itoa(totalRuns) + " runs - deprecation candidate"
}

func formatPercent(v float64) string {
	return itoa(int(v*100)) + "%"
}

func itoa(i int) string {
	if i < 0 {
		return "-" + itoa(-i)
	}
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}
