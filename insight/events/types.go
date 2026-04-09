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
)

func (e EventType) Category() string {
	switch {
	case strings.HasPrefix(string(e), "workflow"):
		return "workflow"
	case strings.HasPrefix(string(e), "agent"):
		return "agent"
	case strings.HasPrefix(string(e), "review"):
		return "review"
	default:
		return "unknown"
	}
}
