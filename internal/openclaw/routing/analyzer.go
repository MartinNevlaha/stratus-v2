package routing

import (
	"fmt"
	"math"
)

type RoutingAnalyzer interface {
	Analyze(agentMetrics []AgentMetrics, workflowMetrics []WorkflowMetrics, config RoutingConfig) []RoutingRecommendation
	Name() string
}

type BestAgentAnalyzer struct{}

func (a *BestAgentAnalyzer) Name() string {
	return "best_agent"
}

func (a *BestAgentAnalyzer) Analyze(agentMetrics []AgentMetrics, workflowMetrics []WorkflowMetrics, config RoutingConfig) []RoutingRecommendation {
	var recommendations []RoutingRecommendation

	if len(agentMetrics) < 2 {
		return recommendations
	}

	var totalSuccessRate float64
	var validAgents int
	var bestAgent *AgentMetrics

	for i := range agentMetrics {
		agent := &agentMetrics[i]
		if agent.TotalRuns < config.MinObservations {
			continue
		}
		totalSuccessRate += agent.SuccessRate
		validAgents++

		if bestAgent == nil || agent.SuccessRate > bestAgent.SuccessRate {
			bestAgent = agent
		}
	}

	if bestAgent == nil || validAgents < 2 {
		return recommendations
	}

	avgSuccessRate := totalSuccessRate / float64(validAgents)
	improvementThreshold := 0.15

	if bestAgent.SuccessRate > avgSuccessRate+improvementThreshold && bestAgent.SuccessRate > 0.7 {
		for _, wf := range workflowMetrics {
			if wf.TotalRuns < config.MinObservations {
				continue
			}

			rec := NewRoutingRecommendation(
				wf.WorkflowType,
				RecommendationBestAgent,
				bestAgent.AgentName,
				"",
			)

			rec.Observations = bestAgent.TotalRuns
			rec.Evidence = map[string]any{
				"agent_success_rate":  bestAgent.SuccessRate,
				"avg_success_rate":    avgSuccessRate,
				"improvement":         bestAgent.SuccessRate - avgSuccessRate,
				"observations":        bestAgent.TotalRuns,
				"trend":               bestAgent.Trend,
				"workflow_runs":       wf.TotalRuns,
				"workflow_completion": wf.CompletionRate,
			}
			rec.Reason = fmt.Sprintf(
				"Agent %s has %.1f%% success rate overall (%.1f%% above average); consider for workflow %s",
				bestAgent.AgentName,
				bestAgent.SuccessRate*100,
				(bestAgent.SuccessRate-avgSuccessRate)*100,
				wf.WorkflowType,
			)

			rec.CalculateConfidence(config)
			rec.DetermineRiskLevel()
			recommendations = append(recommendations, rec)
		}
	}

	return recommendations
}

type DeprioritizationAnalyzer struct{}

func (a *DeprioritizationAnalyzer) Name() string {
	return "deprioritize"
}

func (a *DeprioritizationAnalyzer) Analyze(agentMetrics []AgentMetrics, workflowMetrics []WorkflowMetrics, config RoutingConfig) []RoutingRecommendation {
	var recommendations []RoutingRecommendation

	for _, agent := range agentMetrics {
		if agent.TotalRuns < config.MinObservations {
			continue
		}

		if agent.SuccessRate < config.DeprioritizeThreshold && agent.Trend == "degrading" {
			for _, wf := range workflowMetrics {
				if wf.TotalRuns < config.MinObservations {
					continue
				}

				rec := NewRoutingRecommendation(
					wf.WorkflowType,
					RecommendationDeprioritize,
					"",
					agent.AgentName,
				)

				rec.Observations = agent.TotalRuns
				rec.Evidence = map[string]any{
					"agent_success_rate":  agent.SuccessRate,
					"failure_rate":        agent.FailureRate,
					"rework_rate":         agent.ReworkRate,
					"review_pass_rate":    agent.ReviewPassRate,
					"trend":               agent.Trend,
					"observations":        agent.TotalRuns,
					"workflow_runs":       wf.TotalRuns,
					"workflow_completion": wf.CompletionRate,
				}
				rec.Reason = fmt.Sprintf(
					"Agent %s should be deprioritized: %.1f%% success rate with degrading trend",
					agent.AgentName,
					agent.SuccessRate*100,
				)

				rec.CalculateConfidence(config)
				rec.DetermineRiskLevel()
				recommendations = append(recommendations, rec)
			}
		}
	}

	return recommendations
}

type FallbackAnalyzer struct{}

func (a *FallbackAnalyzer) Name() string {
	return "fallback_needed"
}

func (a *FallbackAnalyzer) Analyze(agentMetrics []AgentMetrics, workflowMetrics []WorkflowMetrics, config RoutingConfig) []RoutingRecommendation {
	var recommendations []RoutingRecommendation

	for _, wf := range workflowMetrics {
		if wf.TotalRuns < config.MinObservations {
			continue
		}

		if wf.FailureRate >= config.FallbackFailureThresh && wf.AgentCount <= 1 {
			rec := NewRoutingRecommendation(
				wf.WorkflowType,
				RecommendationFallback,
				"",
				"",
			)

			rec.Observations = wf.TotalRuns
			rec.Evidence = map[string]any{
				"workflow_failure_rate":    wf.FailureRate,
				"workflow_completion_rate": wf.CompletionRate,
				"agent_count":              wf.AgentCount,
				"rework_rate":              wf.ReworkRate,
				"trend":                    wf.Trend,
				"observations":             wf.TotalRuns,
				"available_agents":         len(agentMetrics),
			}
			rec.Reason = fmt.Sprintf(
				"Workflow %s has %.1f%% failure rate with limited routing (%d agent(s)); consider adding fallback options",
				wf.WorkflowType,
				wf.FailureRate*100,
				wf.AgentCount,
			)

			rec.CalculateConfidence(config)
			rec.DetermineRiskLevel()
			recommendations = append(recommendations, rec)
		}
	}

	return recommendations
}

type InstabilityAnalyzer struct{}

func (a *InstabilityAnalyzer) Name() string {
	return "instability"
}

func (a *InstabilityAnalyzer) Analyze(agentMetrics []AgentMetrics, workflowMetrics []WorkflowMetrics, config RoutingConfig) []RoutingRecommendation {
	var recommendations []RoutingRecommendation

	for _, wf := range workflowMetrics {
		if wf.TotalRuns < config.MinObservations {
			continue
		}

		isInstable := wf.ReworkRate >= config.InstabilityReworkThresh ||
			(wf.ReworkRate >= 0.25 && wf.ReviewRejectionRate >= 0.30)

		if isInstable {
			rec := NewRoutingRecommendation(
				wf.WorkflowType,
				RecommendationInstability,
				"",
				"",
			)

			rec.Observations = wf.TotalRuns
			rec.Evidence = map[string]any{
				"rework_rate":           wf.ReworkRate,
				"review_rejection_rate": wf.ReviewRejectionRate,
				"completion_rate":       wf.CompletionRate,
				"failure_rate":          wf.FailureRate,
				"trend":                 wf.Trend,
				"observations":          wf.TotalRuns,
				"metric_consistency":    calculateConsistency(wf),
			}
			rec.Reason = fmt.Sprintf(
				"Workflow %s shows instability: %.1f%% rework rate, %.1f%% review rejection; recommend investigation",
				wf.WorkflowType,
				wf.ReworkRate*100,
				wf.ReviewRejectionRate*100,
			)

			rec.CalculateConfidence(config)
			rec.DetermineRiskLevel()
			recommendations = append(recommendations, rec)
		}
	}

	return recommendations
}

func calculateConsistency(wf WorkflowMetrics) float64 {
	rates := []float64{wf.CompletionRate, wf.FailureRate, wf.ReworkRate}
	if len(rates) == 0 {
		return 0.5
	}

	var sum float64
	for _, r := range rates {
		sum += r
	}
	mean := sum / float64(len(rates))

	var variance float64
	for _, r := range rates {
		variance += math.Pow(r-mean, 2)
	}
	variance /= float64(len(rates))

	stdDev := math.Sqrt(variance)

	consistency := 1.0 - (stdDev * 2)
	if consistency < 0 {
		consistency = 0
	}
	if consistency > 1 {
		consistency = 1
	}

	return consistency
}

func GetAllAnalyzers() []RoutingAnalyzer {
	return []RoutingAnalyzer{
		&BestAgentAnalyzer{},
		&DeprioritizationAnalyzer{},
		&FallbackAnalyzer{},
		&InstabilityAnalyzer{},
	}
}
