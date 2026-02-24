package orchestration

import "fmt"

// Phase represents a workflow phase.
type Phase string

const (
	// Spec phases
	PhasePlan       Phase = "plan"
	PhaseImplement  Phase = "implement"
	PhaseVerify     Phase = "verify"
	PhaseLearn      Phase = "learn"
	PhaseComplete   Phase = "complete"

	// Complex spec additional phases
	PhaseDiscovery   Phase = "discovery"
	PhaseDesign      Phase = "design"
	PhaseGovernance  Phase = "governance"
	PhaseAccept      Phase = "accept"

	// Bug phases
	PhaseAnalyze Phase = "analyze"
	PhaseFix     Phase = "fix"
	PhaseReview  Phase = "review"
)

// WorkflowType distinguishes spec vs bug workflows.
type WorkflowType string

const (
	WorkflowSpec WorkflowType = "spec"
	WorkflowBug  WorkflowType = "bug"
)

// Complexity selects simple vs complex spec flow.
type Complexity string

const (
	ComplexitySimple  Complexity = "simple"
	ComplexityComplex Complexity = "complex"
)

// validTransitions defines allowed phase transitions per workflow type.
var validTransitions = map[WorkflowType]map[Phase][]Phase{
	WorkflowSpec: {
		// Simple: plan → implement → verify → learn → complete
		// Complex: plan → discovery → design → governance → accept → implement → verify → learn → complete
		PhasePlan:      {PhaseImplement, PhaseDiscovery, PhaseAccept},
		PhaseDiscovery:  {PhaseDesign},
		PhaseDesign:     {PhaseGovernance, PhasePlan},
		PhaseGovernance: {PhasePlan},
		PhaseAccept:     {PhaseImplement},
		PhaseImplement:  {PhaseVerify},
		PhaseVerify:     {PhaseImplement, PhaseLearn}, // IMPLEMENT = fix loop
		PhaseLearn:      {PhaseComplete},
		PhaseComplete:   {},
	},
	WorkflowBug: {
		PhaseAnalyze: {PhaseFix},
		PhaseFix:     {PhaseReview},
		PhaseReview:  {PhaseFix, PhaseComplete}, // FIX = another iteration
		PhaseComplete: {},
	},
}

// ValidateTransition checks if transitioning from → to is allowed.
func ValidateTransition(wtype WorkflowType, from, to Phase) error {
	allowed, ok := validTransitions[wtype][from]
	if !ok {
		return fmt.Errorf("unknown phase %q for workflow type %q", from, wtype)
	}
	for _, p := range allowed {
		if p == to {
			return nil
		}
	}
	return fmt.Errorf("invalid transition %q → %q for workflow type %q", from, to, wtype)
}

// InitialPhase returns the starting phase for a workflow type.
func InitialPhase(wtype WorkflowType) Phase {
	switch wtype {
	case WorkflowBug:
		return PhaseAnalyze
	default:
		return PhasePlan
	}
}
