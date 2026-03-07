package proposals

import (
	"fmt"

	"github.com/MartinNevlaha/stratus-v2/internal/openclaw/patterns"
)

type ProposalGenerator interface {
	Name() string
	CanGenerate(patternType patterns.PatternType) bool
	Generate(pattern patterns.Pattern) (*Proposal, error)
}

type WorkflowFailureClusterGenerator struct{}

func (g *WorkflowFailureClusterGenerator) Name() string {
	return "workflow_failure_cluster"
}

func (g *WorkflowFailureClusterGenerator) CanGenerate(patternType patterns.PatternType) bool {
	return patternType == patterns.PatternWorkflowFailureCluster
}

func (g *WorkflowFailureClusterGenerator) Generate(pattern patterns.Pattern) (*Proposal, error) {
	failureRate, ok := pattern.Evidence["failure_rate"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing or invalid failure_rate in evidence")
	}

	affectedWorkflow, ok := pattern.Evidence["affected_workflow"].(string)
	if !ok {
		affectedWorkflow = "unknown"
	}

	var proposalType ProposalType
	var title, description string
	var recommendation map[string]any

	if failureRate >= 0.70 {
		proposalType = ProposalTypeRoutingChange
		title = fmt.Sprintf("Route away from failing workflow: %s", affectedWorkflow)
		description = fmt.Sprintf("Critical failure rate (%.1f%%) detected in %s workflows", failureRate*100, affectedWorkflow)
		recommendation = map[string]any{
			"workflow_type":    affectedWorkflow,
			"suggested_action": "reroute_to_alternate",
			"reason":           fmt.Sprintf("%.1f%% failure rate", failureRate*100),
			"priority":         "high",
		}
	} else if failureRate >= 0.50 {
		proposalType = ProposalTypeReviewGateAddition
		title = fmt.Sprintf("Add review gate to %s workflow", affectedWorkflow)
		description = fmt.Sprintf("High failure rate (%.1f%%) suggests quality issues", failureRate*100)
		recommendation = map[string]any{
			"workflow_type":    affectedWorkflow,
			"suggested_action": "add_review_gate",
			"reason":           "high rejection rate",
		}
	} else {
		proposalType = ProposalTypeWorkflowInvestigation
		title = fmt.Sprintf("Investigate failures in %s workflow", affectedWorkflow)
		description = fmt.Sprintf("Elevated failure rate (%.1f%%) requires investigation", failureRate*100)
		recommendation = map[string]any{
			"workflow_type":    affectedWorkflow,
			"suggested_action": "investigate_root_cause",
			"reason":           "elevated failure rate",
		}
	}

	proposal := NewProposal(proposalType, title, description, pattern, recommendation)
	return &proposal, nil
}

type AgentPerformanceDropGenerator struct{}

func (g *AgentPerformanceDropGenerator) Name() string {
	return "agent_performance_drop"
}

func (g *AgentPerformanceDropGenerator) CanGenerate(patternType patterns.PatternType) bool {
	return patternType == patterns.PatternAgentPerformanceDrop
}

func (g *AgentPerformanceDropGenerator) Generate(pattern patterns.Pattern) (*Proposal, error) {
	dropRate, ok := pattern.Evidence["performance_drop"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing or invalid performance_drop in evidence")
	}

	agentID, ok := pattern.Evidence["agent_id"].(string)
	if !ok {
		agentID = "unknown"
	}

	var proposalType ProposalType
	var title, description string
	var recommendation map[string]any

	if dropRate >= 0.30 {
		proposalType = ProposalTypeAgentDeprioritize
		title = fmt.Sprintf("Deprioritize agent: %s", agentID)
		description = fmt.Sprintf("Severe performance drop (%.1f%%) detected for agent %s", dropRate*100, agentID)
		recommendation = map[string]any{
			"agent_id":         agentID,
			"suggested_action": "deprioritize_for_all_workflows",
			"reason":           fmt.Sprintf("%.1f%% performance drop", dropRate*100),
			"priority":         "high",
		}
	} else {
		proposalType = ProposalTypeRoutingChange
		title = fmt.Sprintf("Re-route tasks away from agent: %s", agentID)
		description = fmt.Sprintf("Performance drop (%.1f%%) detected for agent %s", dropRate*100, agentID)
		recommendation = map[string]any{
			"agent_id":         agentID,
			"suggested_action": "reroute_to_alternate_agent",
			"reason":           fmt.Sprintf("%.1f%% performance drop", dropRate*100),
		}
	}

	proposal := NewProposal(proposalType, title, description, pattern, recommendation)
	return &proposal, nil
}

type ReviewRejectionSpikeGenerator struct{}

func (g *ReviewRejectionSpikeGenerator) Name() string {
	return "review_rejection_spike"
}

func (g *ReviewRejectionSpikeGenerator) CanGenerate(patternType patterns.PatternType) bool {
	return patternType == patterns.PatternReviewRejectionSpike
}

func (g *ReviewRejectionSpikeGenerator) Generate(pattern patterns.Pattern) (*Proposal, error) {
	rejectionRate, ok := pattern.Evidence["rejection_rate"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing or invalid rejection_rate in evidence")
	}

	affectedWorkflow, ok := pattern.Evidence["affected_workflow"].(string)
	if !ok {
		affectedWorkflow = "unknown"
	}

	var proposalType ProposalType
	var title, description string
	var recommendation map[string]any

	if rejectionRate >= 0.50 {
		proposalType = ProposalTypeWorkflowInvestigation
		title = fmt.Sprintf("Investigate review failures in %s workflow", affectedWorkflow)
		description = fmt.Sprintf("Critical rejection rate (%.1f%%) suggests fundamental issues", rejectionRate*100)
		recommendation = map[string]any{
			"workflow_type":    affectedWorkflow,
			"suggested_action": "investigate_review_process",
			"reason":           fmt.Sprintf("%.1f%% rejection rate", rejectionRate*100),
			"priority":         "high",
		}
	} else {
		proposalType = ProposalTypeReviewGateAddition
		title = fmt.Sprintf("Add review gate to %s workflow", affectedWorkflow)
		description = fmt.Sprintf("Elevated rejection rate (%.1f%%) suggests quality issues", rejectionRate*100)
		recommendation = map[string]any{
			"workflow_type":    affectedWorkflow,
			"suggested_action": "add_review_gate",
			"reason":           fmt.Sprintf("%.1f%% rejection rate", rejectionRate*100),
		}
	}

	proposal := NewProposal(proposalType, title, description, pattern, recommendation)
	return &proposal, nil
}

type WorkflowDurationSpikeGenerator struct{}

func (g *WorkflowDurationSpikeGenerator) Name() string {
	return "workflow_duration_spike"
}

func (g *WorkflowDurationSpikeGenerator) CanGenerate(patternType patterns.PatternType) bool {
	return patternType == patterns.PatternWorkflowDurationSpike
}

func (g *WorkflowDurationSpikeGenerator) Generate(pattern patterns.Pattern) (*Proposal, error) {
	durationMultiplier, ok := pattern.Evidence["duration_multiplier"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing or invalid duration_multiplier in evidence")
	}

	affectedWorkflow, ok := pattern.Evidence["affected_workflow"].(string)
	if !ok {
		affectedWorkflow = "unknown"
	}

	var proposalType ProposalType
	var title, description string
	var recommendation map[string]any

	if durationMultiplier >= 3.0 {
		proposalType = ProposalTypeWorkflowInvestigation
		title = fmt.Sprintf("Investigate performance issues in %s workflow", affectedWorkflow)
		description = fmt.Sprintf("Critical duration spike (%.1fx longer) detected", durationMultiplier)
		recommendation = map[string]any{
			"workflow_type":    affectedWorkflow,
			"suggested_action": "investigate_bottlenecks",
			"reason":           fmt.Sprintf("%.1fx duration increase", durationMultiplier),
			"priority":         "high",
		}
	} else {
		proposalType = ProposalTypeRetryPolicyAdjust
		title = fmt.Sprintf("Adjust retry policy for %s workflow", affectedWorkflow)
		description = fmt.Sprintf("Duration spike (%.1fx longer) may indicate transient issues", durationMultiplier)
		recommendation = map[string]any{
			"workflow_type":    affectedWorkflow,
			"suggested_action": "increase_retry_timeout",
			"reason":           fmt.Sprintf("%.1fx duration increase", durationMultiplier),
		}
	}

	proposal := NewProposal(proposalType, title, description, pattern, recommendation)
	return &proposal, nil
}
