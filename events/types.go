package events

import "strings"

type EventType string

const (
	EventWorkflowStarted   EventType = "workflow.started"
	EventWorkflowCompleted EventType = "workflow.completed"
	EventWorkflowFailed    EventType = "workflow.failed"
	EventWorkflowAborted   EventType = "workflow.aborted"
	EventPhaseTransition   EventType = "workflow.phase_transition"

	EventAgentSpawned   EventType = "agent.spawned"
	EventAgentCompleted EventType = "agent.completed"
	EventAgentFailed    EventType = "agent.failed"

	EventReviewStarted EventType = "review.started"
	EventReviewPassed  EventType = "review.passed"
	EventReviewFailed  EventType = "review.failed"

	// EventAlertEmitted is published by Guardian after an alert has been saved
	// and broadcast. Subscribers (Insight) use this to correlate alerts with
	// their pattern/proposal streams. Payload: id, type, severity, message,
	// metadata, lang.
	EventAlertEmitted EventType = "alert.emitted"

	// EventGovernanceViolation is published by Guardian's governance check
	// when a changed file is flagged as (potentially) violating governance
	// rules. Subscribers (Insight proposal engine) use this to attach a
	// paired remediation proposal. Payload: file, rule_titles, reason,
	// alert_id (if already saved).
	EventGovernanceViolation EventType = "governance.violation"

	// EventCoverageDrift is published when Guardian's coverage-drift check
	// detects a material drop. Subscribers (Insight scorecards) consume the
	// numeric delta as an input signal. Payload: baseline, current, delta.
	EventCoverageDrift EventType = "coverage.drift"
)

func (e EventType) Category() string {
	switch {
	case strings.HasPrefix(string(e), "workflow"):
		return "workflow"
	case strings.HasPrefix(string(e), "agent"):
		return "agent"
	case strings.HasPrefix(string(e), "review"):
		return "review"
	case strings.HasPrefix(string(e), "alert"):
		return "alert"
	case strings.HasPrefix(string(e), "governance"):
		return "governance"
	case strings.HasPrefix(string(e), "coverage"):
		return "coverage"
	default:
		return "unknown"
	}
}
