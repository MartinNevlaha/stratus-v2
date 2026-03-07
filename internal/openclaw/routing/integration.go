package routing

import (
	"fmt"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/openclaw/proposals"
)

type RoutingChange struct {
	RecommendedAgent string         `json:"recommended_agent,omitempty"`
	CurrentAgent     string         `json:"current_agent,omitempty"`
	ChangeType       string         `json:"change_type"`
	Reason           string         `json:"reason"`
	Evidence         map[string]any `json:"evidence"`
}

type WorkflowChange struct {
	ChangeType       string         `json:"change_type"`
	StageName        string         `json:"stage_name,omitempty"`
	InsertBefore     string         `json:"insert_before,omitempty"`
	SplitStrategy    string         `json:"split_strategy,omitempty"`
	ValidationChecks []string       `json:"validation_checks,omitempty"`
	Reason           string         `json:"reason"`
	Evidence         map[string]any `json:"evidence"`
}

type CombinedRecommendation struct {
	ID                 string          `json:"id"`
	WorkflowType       string          `json:"workflow_type"`
	RoutingChange      *RoutingChange  `json:"routing_change,omitempty"`
	WorkflowChange     *WorkflowChange `json:"workflow_change,omitempty"`
	CombinedConfidence float64         `json:"combined_confidence"`
	ImpactScore        float64         `json:"impact_score"`
	Priority           string          `json:"priority"`
	CreatedAt          time.Time       `json:"created_at"`
}

func GenerateCombinedRecommendations(
	routingRecommendations []RoutingRecommendation,
	workflowProposals []proposals.Proposal,
) []CombinedRecommendation {
	var combined []CombinedRecommendation

	routingByWorkflow := make(map[string][]RoutingRecommendation)
	for _, rec := range routingRecommendations {
		routingByWorkflow[rec.WorkflowType] = append(routingByWorkflow[rec.WorkflowType], rec)
	}

	proposalsByWorkflow := make(map[string][]proposals.Proposal)
	for _, prop := range workflowProposals {
		if wfType, ok := prop.Evidence["workflow_type"].(string); ok && wfType != "" {
			proposalsByWorkflow[wfType] = append(proposalsByWorkflow[wfType], prop)
		}
		if affectedWfs, ok := prop.Evidence["affected_workflows"].([]string); ok {
			for _, wfID := range affectedWfs {
				proposalsByWorkflow[wfID] = append(proposalsByWorkflow[wfID], prop)
			}
		}
	}

	allWorkflows := make(map[string]bool)
	for wf := range routingByWorkflow {
		allWorkflows[wf] = true
	}
	for wf := range proposalsByWorkflow {
		allWorkflows[wf] = true
	}

	for wfType := range allWorkflows {
		rec := CombinedRecommendation{
			ID:           generateCombinedID(wfType),
			WorkflowType: wfType,
			CreatedAt:    time.Now().UTC(),
		}

		routingRecs := routingByWorkflow[wfType]
		workflowProps := proposalsByWorkflow[wfType]

		if len(routingRecs) > 0 {
			bestRouting := selectBestRouting(routingRecs)
			rec.RoutingChange = &RoutingChange{
				RecommendedAgent: bestRouting.RecommendedAgent,
				CurrentAgent:     bestRouting.CurrentAgent,
				ChangeType:       string(bestRouting.RecommendationType),
				Reason:           bestRouting.Reason,
				Evidence:         bestRouting.Evidence,
			}
		}

		if len(workflowProps) > 0 {
			bestProposal := selectBestProposal(workflowProps)
			rec.WorkflowChange = convertProposalToChange(bestProposal)
		}

		rec.CombinedConfidence = calculateCombinedConfidence(routingRecs, workflowProps)
		rec.ImpactScore = calculateImpactScore(routingRecs, workflowProps)
		rec.Priority = determinePriority(rec.ImpactScore, rec.CombinedConfidence)

		combined = append(combined, rec)
	}

	return combined
}

func selectBestRouting(recs []RoutingRecommendation) RoutingRecommendation {
	if len(recs) == 0 {
		return RoutingRecommendation{}
	}

	best := recs[0]
	for _, rec := range recs[1:] {
		if rec.Confidence > best.Confidence {
			best = rec
		}
	}
	return best
}

func selectBestProposal(props []proposals.Proposal) proposals.Proposal {
	if len(props) == 0 {
		return proposals.Proposal{}
	}

	best := props[0]
	for _, prop := range props[1:] {
		if prop.Confidence > best.Confidence {
			best = prop
		}
	}
	return best
}

func convertProposalToChange(prop proposals.Proposal) *WorkflowChange {
	if prop.ID == "" {
		return nil
	}

	change := &WorkflowChange{
		ChangeType: string(prop.Type),
		Reason:     prop.Description,
		Evidence:   prop.Evidence,
	}

	switch prop.Type {
	case proposals.ProposalTypeWorkflowDebugger:
		change.StageName = "debug"
		change.InsertBefore = "review"
		if stage, ok := prop.Recommendation["stage_name"].(string); ok {
			change.StageName = stage
		}
		if before, ok := prop.Recommendation["insert_before"].(string); ok {
			change.InsertBefore = before
		}

	case proposals.ProposalTypeWorkflowAutoReview:
		change.StageName = "auto_review"
		change.InsertBefore = "review"
		if checks, ok := prop.Recommendation["validation_checks"].([]string); ok {
			change.ValidationChecks = checks
		}

	case proposals.ProposalTypeWorkflowSplit:
		change.SplitStrategy = "parallel_execution"
		if strategy, ok := prop.Recommendation["split_strategy"].(string); ok {
			change.SplitStrategy = strategy
		}
	}

	return change
}

func calculateCombinedConfidence(routingRecs []RoutingRecommendation, props []proposals.Proposal) float64 {
	var totalConf float64
	var count int

	for _, rec := range routingRecs {
		totalConf += rec.Confidence
		count++
	}

	for _, prop := range props {
		totalConf += prop.Confidence
		count++
	}

	if count == 0 {
		return 0
	}

	avgConf := totalConf / float64(count)

	boost := 0.0
	if len(routingRecs) > 0 && len(props) > 0 {
		boost = 0.10
	}

	confidence := avgConf + boost
	if confidence > 0.95 {
		confidence = 0.95
	}

	return confidence
}

func calculateImpactScore(routingRecs []RoutingRecommendation, props []proposals.Proposal) float64 {
	score := 0.0

	for _, rec := range routingRecs {
		switch rec.RiskLevel {
		case RiskCritical:
			score += 0.40
		case RiskHigh:
			score += 0.25
		case RiskMedium:
			score += 0.15
		default:
			score += 0.05
		}
	}

	for _, prop := range props {
		switch prop.RiskLevel {
		case proposals.RiskHigh:
			score += 0.35
		case proposals.RiskMedium:
			score += 0.20
		default:
			score += 0.10
		}
	}

	if score > 1.0 {
		score = 1.0
	}

	return score
}

func determinePriority(impactScore, confidence float64) string {
	combined := impactScore * confidence

	if combined >= 0.60 {
		return "critical"
	}
	if combined >= 0.40 {
		return "high"
	}
	if combined >= 0.25 {
		return "medium"
	}
	return "low"
}

func generateCombinedID(workflowType string) string {
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("combined-%s-%d", workflowType, timestamp)
}

type CombinedRecommendationSummary struct {
	TotalRecommendations int            `json:"total_recommendations"`
	ByPriority           map[string]int `json:"by_priority"`
	ByWorkflowType       map[string]int `json:"by_workflow_type"`
	WithRoutingOnly      int            `json:"with_routing_only"`
	WithWorkflowOnly     int            `json:"with_workflow_only"`
	WithBoth             int            `json:"with_both"`
}

func SummarizeCombinedRecommendations(combined []CombinedRecommendation) CombinedRecommendationSummary {
	summary := CombinedRecommendationSummary{
		TotalRecommendations: len(combined),
		ByPriority:           make(map[string]int),
		ByWorkflowType:       make(map[string]int),
	}

	for _, rec := range combined {
		summary.ByPriority[rec.Priority]++
		summary.ByWorkflowType[rec.WorkflowType]++

		hasRouting := rec.RoutingChange != nil
		hasWorkflow := rec.WorkflowChange != nil

		if hasRouting && hasWorkflow {
			summary.WithBoth++
		} else if hasRouting {
			summary.WithRoutingOnly++
		} else if hasWorkflow {
			summary.WithWorkflowOnly++
		}
	}

	return summary
}
