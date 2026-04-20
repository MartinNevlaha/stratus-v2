package proposals

import (
	"fmt"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/patterns"
	"github.com/google/uuid"
)

type ProposalType string

const (
	ProposalTypeRoutingChange         ProposalType = "routing.change"
	ProposalTypeReviewGateAddition    ProposalType = "review_gate.add"
	ProposalTypeWorkflowInvestigation ProposalType = "workflow.investigate"
	ProposalTypeAgentDeprioritize     ProposalType = "agent.deprioritize"
	ProposalTypeRetryPolicyAdjust     ProposalType = "retry_policy.adjust"
	ProposalTypeWorkflowStageAdd      ProposalType = "workflow.stage_add"
	ProposalTypeWorkflowSplit         ProposalType = "workflow.split"
	ProposalTypeWorkflowDebugger      ProposalType = "workflow.add_debugger"
	ProposalTypeWorkflowAutoReview    ProposalType = "workflow.add_auto_review"
	ProposalTypeAgentSpecialize       ProposalType = "agent.specialize"
	ProposalTypeAgentPromptUpdate     ProposalType = "agent.improve_prompt"
	ProposalTypeAgentDeprecate        ProposalType = "agent.deprecate"
	ProposalTypeAgentPromote          ProposalType = "agent.promote"

	// ProposalTypeGovernanceRemediation is attached to a Guardian
	// governance_violation alert and proposes a human-reviewable fix for
	// the flagged file. Proposals of this type are event-driven (not
	// pattern-derived) — SourcePatternID uses the synthetic form
	// "guardian-alert:<alert_id>" to link back to the originating alert.
	ProposalTypeGovernanceRemediation ProposalType = "governance.remediation"
)

type ProposalStatus string

const (
	ProposalStatusDetected ProposalStatus = "detected"
	ProposalStatusDrafted  ProposalStatus = "drafted"
	ProposalStatusApproved ProposalStatus = "approved"
	ProposalStatusRejected ProposalStatus = "rejected"
	ProposalStatusArchived ProposalStatus = "archived"
)

type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

type Proposal struct {
	ID              string         `json:"id"`
	Type            ProposalType   `json:"type"`
	Status          ProposalStatus `json:"status"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	Confidence      float64        `json:"confidence"`
	RiskLevel       RiskLevel      `json:"risk_level"`
	SourcePatternID string         `json:"source_pattern_id"`
	Evidence        map[string]any `json:"evidence"`
	Recommendation  map[string]any `json:"recommendation"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func generateProposalID() string {
	return uuid.New().String()
}

func determineRiskLevel(severity patterns.SeverityLevel, confidence float64) RiskLevel {
	if severity == patterns.SeverityCritical && confidence >= 0.70 {
		return RiskHigh
	}

	if severity == patterns.SeverityHigh {
		return RiskMedium
	}

	return RiskLow
}

func calculateConfidence(pattern patterns.Pattern) float64 {
	base := pattern.Confidence

	freqBoost := 0.0
	if pattern.Frequency >= 5 {
		freqBoost = 0.10
	} else if pattern.Frequency >= 3 {
		freqBoost = 0.05
	}

	severityBoost := 0.0
	switch pattern.Severity {
	case patterns.SeverityCritical:
		severityBoost = 0.10
	case patterns.SeverityHigh:
		severityBoost = 0.05
	}

	evidenceBoost := 0.0
	if totalCount, ok := pattern.Evidence["total_count"].(float64); ok {
		totalCountInt := int(totalCount)
		if totalCountInt >= 20 {
			evidenceBoost = 0.10
		} else if totalCountInt >= 10 {
			evidenceBoost = 0.05
		}
	}

	timeSinceLastSeen := time.Since(pattern.LastSeen)
	recencyBoost := 0.0
	if timeSinceLastSeen < 6*time.Hour {
		recencyBoost = 0.05
	}

	confidence := base + freqBoost + severityBoost + evidenceBoost + recencyBoost

	if confidence > 0.95 {
		confidence = 0.95
	}

	return confidence
}

func NewProposal(proposalType ProposalType, title, description string, pattern patterns.Pattern, recommendation map[string]any) Proposal {
	confidence := calculateConfidence(pattern)
	riskLevel := determineRiskLevel(pattern.Severity, confidence)

	now := time.Now().UTC()

	return Proposal{
		ID:              generateProposalID(),
		Type:            proposalType,
		Status:          ProposalStatusDetected,
		Title:           title,
		Description:     description,
		Confidence:      confidence,
		RiskLevel:       riskLevel,
		SourcePatternID: pattern.ID,
		Evidence:        pattern.Evidence,
		Recommendation:  recommendation,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// NewGovernanceRemediationProposal builds a proposal paired to a Guardian
// governance_violation alert. Unlike NewProposal, it is not pattern-derived
// — confidence and risk come from the alert's own metadata rather than a
// pattern's frequency signal.
//
// alertID is the DB row ID of the Guardian alert; file is the file path that
// was flagged; rules is a human-readable description of the matching rules
// (either rule titles or an LLM-produced one-line reason).
func NewGovernanceRemediationProposal(alertID int64, file, rules, severity string) Proposal {
	now := time.Now().UTC()

	// Severity comes from the Guardian alert (info|warning|critical). The
	// mapping to risk is conservative: only a "critical" alert produces a
	// high-risk proposal; the default is medium because governance hits
	// warrant at least a human review.
	risk := RiskMedium
	confidence := 0.60
	switch severity {
	case "critical":
		risk = RiskHigh
		confidence = 0.75
	case "info":
		risk = RiskLow
		confidence = 0.50
	}

	title := fmt.Sprintf("Review governance violation in %s", file)
	if file == "" {
		title = "Review governance violation"
	}

	return Proposal{
		ID:              generateProposalID(),
		Type:            ProposalTypeGovernanceRemediation,
		Status:          ProposalStatusDetected,
		Title:           title,
		Description:     rules,
		Confidence:      confidence,
		RiskLevel:       risk,
		SourcePatternID: fmt.Sprintf("guardian-alert:%d", alertID),
		Evidence: map[string]any{
			"alert_id": alertID,
			"file":     file,
			"rules":    rules,
		},
		Recommendation: map[string]any{
			"action": "review_and_fix",
			"file":   file,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}
