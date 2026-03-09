package proposals

import (
	"fmt"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/patterns"
)

type ProposalGenerator interface {
	Name() string
	CanGenerate(patternType patterns.PatternType) bool
	Generate(pattern patterns.Pattern) (*Proposal, error)
}

func getIntFromEvidence(e map[string]any, key string, def int) int {
	switch v := e[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	default:
		return def
	}
}

func getFloatFromEvidence(e map[string]any, key string, def float64) float64 {
	switch v := e[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return def
	}
}

func getStringFromEvidence(e map[string]any, key, def string) string {
	if v, ok := e[key].(string); ok && v != "" {
		return v
	}
	return def
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
	dropRate := getFloatFromEvidence(pattern.Evidence, "performance_drop", 0)
	if dropRate == 0 {
		dropRate = getFloatFromEvidence(pattern.Evidence, "drop_rate", 0)
	}
	if dropRate == 0 {
		return nil, fmt.Errorf("missing or invalid performance_drop in evidence")
	}

	agentID := getStringFromEvidence(pattern.Evidence, "agent_id", "")
	if agentID == "" {
		agentID = getStringFromEvidence(pattern.Evidence, "worst_performing_agent", "unknown")
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
	durationMultiplier := getFloatFromEvidence(pattern.Evidence, "duration_multiplier", 0)
	if durationMultiplier == 0 {
		durationMultiplier = getFloatFromEvidence(pattern.Evidence, "multiplier", 0)
	}
	if durationMultiplier == 0 {
		return nil, fmt.Errorf("missing or invalid duration_multiplier in evidence")
	}

	affectedWorkflow := getStringFromEvidence(pattern.Evidence, "affected_workflow", "")
	if affectedWorkflow == "" {
		affectedWorkflow = getStringFromEvidence(pattern.Evidence, "slowest_workflow_type", "")
	}
	if affectedWorkflow == "" {
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

type WorkflowLoopGenerator struct{}

func (g *WorkflowLoopGenerator) Name() string {
	return "workflow_loop"
}

func (g *WorkflowLoopGenerator) CanGenerate(patternType patterns.PatternType) bool {
	return patternType == patterns.PatternWorkflowLoop
}

func (g *WorkflowLoopGenerator) Generate(pattern patterns.Pattern) (*Proposal, error) {
	avgLoops := getFloatFromEvidence(pattern.Evidence, "avg_loops", 0)
	if avgLoops == 0 {
		return nil, fmt.Errorf("missing or invalid avg_loops in evidence")
	}

	affectedWorkflows, _ := pattern.Evidence["affected_workflows"].([]string)
	affectedCount := getIntFromEvidence(pattern.Evidence, "affected_count", 0)

	var proposalType ProposalType
	var title, description string
	var recommendation map[string]any

	if avgLoops >= 4 {
		proposalType = ProposalTypeWorkflowDebugger
		title = "Insert debugger agent stage in looping workflows"
		description = fmt.Sprintf("High loop count (avg %.1f) detected across %d workflows; insert debugger stage before review", avgLoops, affectedCount)
		recommendation = map[string]any{
			"suggested_action":     "insert_debugger_stage",
			"stage_name":           "debug",
			"insert_before":        "review",
			"agent_type":           "debugger-agent",
			"reason":               fmt.Sprintf("avg %.1f phase loops detected", avgLoops),
			"affected_workflows":   affectedWorkflows,
			"expected_improvement": "reduce loops by catching issues early",
			"priority":             "high",
		}
	} else {
		proposalType = ProposalTypeWorkflowInvestigation
		title = "Investigate workflow loop patterns"
		description = fmt.Sprintf("Loop patterns detected (avg %.1f) across %d workflows", avgLoops, affectedCount)
		recommendation = map[string]any{
			"suggested_action":   "investigate_loop_causes",
			"affected_workflows": affectedWorkflows,
			"reason":             fmt.Sprintf("avg %.1f phase loops detected", avgLoops),
		}
	}

	proposal := NewProposal(proposalType, title, description, pattern, recommendation)
	return &proposal, nil
}

type WorkflowReviewFailureGenerator struct{}

func (g *WorkflowReviewFailureGenerator) Name() string {
	return "workflow_review_failure_cluster"
}

func (g *WorkflowReviewFailureGenerator) CanGenerate(patternType patterns.PatternType) bool {
	return patternType == patterns.PatternWorkflowReviewFailure
}

func (g *WorkflowReviewFailureGenerator) Generate(pattern patterns.Pattern) (*Proposal, error) {
	avgFailRate := getFloatFromEvidence(pattern.Evidence, "avg_fail_rate", 0)
	if avgFailRate == 0 {
		return nil, fmt.Errorf("missing or invalid avg_fail_rate in evidence")
	}

	affectedWorkflows, _ := pattern.Evidence["affected_workflows"].([]string)
	affectedCount := getIntFromEvidence(pattern.Evidence, "affected_count", 0)

	var proposalType ProposalType
	var title, description string
	var recommendation map[string]any

	if avgFailRate >= 0.60 {
		proposalType = ProposalTypeWorkflowAutoReview
		title = "Add auto-review/validation stage"
		description = fmt.Sprintf("High review failure rate (%.1f%%) across %d workflows; add pre-validation stage", avgFailRate*100, affectedCount)
		recommendation = map[string]any{
			"suggested_action":     "add_auto_review_stage",
			"stage_name":           "auto_review",
			"insert_before":        "review",
			"validation_checks":    []string{"lint", "type_check", "test"},
			"reason":               fmt.Sprintf("%.1f%% review failure rate", avgFailRate*100),
			"affected_workflows":   affectedWorkflows,
			"expected_improvement": "catch issues before human review",
			"priority":             "high",
		}
	} else {
		proposalType = ProposalTypeReviewGateAddition
		title = "Strengthen review gates in affected workflows"
		description = fmt.Sprintf("Elevated review failure rate (%.1f%%) across %d workflows", avgFailRate*100, affectedCount)
		recommendation = map[string]any{
			"suggested_action":   "strengthen_review_gates",
			"affected_workflows": affectedWorkflows,
			"reason":             fmt.Sprintf("%.1f%% review failure rate", avgFailRate*100),
		}
	}

	proposal := NewProposal(proposalType, title, description, pattern, recommendation)
	return &proposal, nil
}

type WorkflowSlowExecutionGenerator struct{}

func (g *WorkflowSlowExecutionGenerator) Name() string {
	return "workflow_slow_execution"
}

func (g *WorkflowSlowExecutionGenerator) CanGenerate(patternType patterns.PatternType) bool {
	return patternType == patterns.PatternWorkflowSlowExecution
}

func (g *WorkflowSlowExecutionGenerator) Generate(pattern patterns.Pattern) (*Proposal, error) {
	avgMultiplier := getFloatFromEvidence(pattern.Evidence, "avg_multiplier", 0)
	if avgMultiplier == 0 {
		return nil, fmt.Errorf("missing or invalid avg_multiplier in evidence")
	}

	slowWorkflows, _ := pattern.Evidence["slow_workflows"].([]string)
	affectedCount := getIntFromEvidence(pattern.Evidence, "affected_count", 0)

	var proposalType ProposalType
	var title, description string
	var recommendation map[string]any

	if avgMultiplier >= 2.5 {
		proposalType = ProposalTypeWorkflowSplit
		title = "Split slow workflows into sub-workflows"
		description = fmt.Sprintf("Severe slowdown (%.1fx) across %d workflows; consider splitting into parallel sub-workflows", avgMultiplier, affectedCount)
		recommendation = map[string]any{
			"suggested_action":     "split_workflow",
			"split_strategy":       "parallel_execution",
			"reason":               fmt.Sprintf("%.1fx slower than baseline", avgMultiplier),
			"slow_workflows":       slowWorkflows,
			"expected_improvement": "reduce cycle time by 30-50%",
			"priority":             "high",
		}
	} else {
		proposalType = ProposalTypeWorkflowInvestigation
		title = "Investigate slow workflow bottlenecks"
		description = fmt.Sprintf("Workflow slowdown (%.1fx) detected across %d workflows", avgMultiplier, affectedCount)
		recommendation = map[string]any{
			"suggested_action": "investigate_bottlenecks",
			"slow_workflows":   slowWorkflows,
			"reason":           fmt.Sprintf("%.1fx slower than baseline", avgMultiplier),
		}
	}

	proposal := NewProposal(proposalType, title, description, pattern, recommendation)
	return &proposal, nil
}
