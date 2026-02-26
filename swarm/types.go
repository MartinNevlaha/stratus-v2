package swarm

// Worker status lifecycle: pending → active → stale → done | failed | killed
const (
	WorkerPending = "pending"
	WorkerActive  = "active"
	WorkerStale   = "stale"
	WorkerDone    = "done"
	WorkerFailed  = "failed"
	WorkerKilled  = "killed"
)

// Ticket status lifecycle: pending → assigned → in_progress → done | failed | blocked
const (
	TicketPending    = "pending"
	TicketAssigned   = "assigned"
	TicketInProgress = "in_progress"
	TicketDone       = "done"
	TicketFailed     = "failed"
	TicketBlocked    = "blocked"
)

// Mission status lifecycle: planning → active → merging → verifying → complete | failed | aborted
const (
	MissionPlanning  = "planning"
	MissionActive    = "active"
	MissionMerging   = "merging"
	MissionVerifying = "verifying"
	MissionComplete  = "complete"
	MissionFailed    = "failed"
	MissionAborted   = "aborted"
)

// Signal types for inter-agent communication.
const (
	SignalTicketAssigned = "TICKET_ASSIGNED"
	SignalTicketStarted  = "TICKET_STARTED"
	SignalTicketDone     = "TICKET_DONE"
	SignalTicketFailed   = "TICKET_FAILED"
	SignalMergeReady     = "MERGE_READY"
	SignalMerged         = "MERGED"
	SignalConflict       = "CONFLICT"
	SignalHelp           = "HELP"
	SignalAbort          = "ABORT"
	SignalMissionDone    = "MISSION_DONE"
)

// Forge entry status lifecycle: pending → merging → merged | conflict | failed
const (
	ForgePending  = "pending"
	ForgeMerging  = "merging"
	ForgeMerged   = "merged"
	ForgeConflict = "conflict"
	ForgeFailed   = "failed"
)

// Assignment represents a ticket-to-worker dispatch result.
type Assignment struct {
	TicketID string `json:"ticket_id"`
	WorkerID string `json:"worker_id"`
}
