// Package teams manages durable, event-driven work in owned native AI sessions.
package teams

import (
	"context"
	"errors"
	"time"
)

type Profile struct {
	Runtime string `json:"runtime"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
}
type SessionSpec struct {
	MCPBinary    string  `json:"mcp_binary,omitempty"`
	APIBase      string  `json:"api_base,omitempty"`
	Token        string  `json:"token,omitempty"`
	ProjectRoot  string  `json:"project_root"`
	TeamID       string  `json:"team_id"`
	MemberID     string  `json:"member_id"`
	NativeID     string  `json:"native_id"`
	Instructions string  `json:"instructions"`
	Profile      Profile `json:"profile"`
}
type Input struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ModelBinding is transport-verified alias resolution, not a model assertion.
type ModelBinding struct{ Requested, Resolved string }

type Result struct {
	ModelBinding *ModelBinding `json:"-"`
	Text         string        `json:"text"`
	NativeID     string        `json:"native_id"`
	Model        string        `json:"model"`
	Effort       string        `json:"effort"`
	InputTokens  *int64        `json:"input_tokens,omitempty"`
	OutputTokens *int64        `json:"output_tokens,omitempty"`
}
type Event struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Text     string `json:"text"`
	NativeID string `json:"native_id"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
}
type Availability struct {
	Runtime   string  `json:"runtime"`
	Available bool    `json:"available"`
	Reason    string  `json:"reason,omitempty"`
	Models    []Model `json:"models"`
}
type Model struct {
	ID      string   `json:"id"`
	Efforts []string `json:"efforts"`
}
type Adapter interface {
	Preflight(context.Context, string) Availability
	Open(context.Context, SessionSpec, func(Event)) (Session, error)
}
type Session interface {
	Submit(context.Context, Input) (Result, error)
	Approve(context.Context, string, bool) error
	Interrupt(context.Context) error
	Close() error
}

type Limits struct {
	MaxConcurrent int   `json:"max_concurrent"`
	MaxAutoRounds int   `json:"max_auto_rounds"`
	MaxTurns      int   `json:"max_turns"`
	MaxTokens     int64 `json:"max_tokens"`
}
type MemberConfig struct {
	ComplexProfile *Profile `json:"complex_profile,omitempty"`
	ID             string   `json:"id,omitempty"`
	Role           string   `json:"role"`
	Profile        Profile  `json:"profile"`
}
type CreateRequest struct {
	ProjectID   string         `json:"project_id"`
	ProjectRoot string         `json:"project_root"`
	Members     []MemberConfig `json:"members"`
	Limits      Limits         `json:"limits"`
}
type TaskRequest struct {
	WorkflowPhase      string   `json:"workflow_phase,omitempty"`
	WorkflowGeneration int64    `json:"workflow_generation,omitempty"`
	WorkflowTaskIndex  *int     `json:"workflow_task_index,omitempty"`
	WorkflowSessionID  string   `json:"workflow_session_id,omitempty"`
	ID                 string   `json:"id,omitempty"`
	From               string   `json:"from"`
	To                 string   `json:"to,omitempty"`
	Role               string   `json:"role,omitempty"`
	Text               string   `json:"text"`
	WorkflowID         string   `json:"workflow_id,omitempty"`
	WorkflowType       string   `json:"workflow_type,omitempty"`
	Complexity         string   `json:"complexity,omitempty"`
	Revision           string   `json:"revision,omitempty"`
	Profile            *Profile `json:"profile,omitempty"`
	Kind               string   `json:"kind,omitempty"`
	ParentTaskID       string   `json:"parent_task_id,omitempty"`
	ReturnTo           string   `json:"return_to,omitempty"`
}
type Member struct {
	ComplexProfile     *Profile `json:"complex_profile,omitempty"`
	Token              string   `json:"token,omitempty"`
	ID                 string   `json:"id"`
	Role               string   `json:"role"`
	Profile            Profile  `json:"profile"`
	NativeID           string   `json:"native_id,omitempty"`
	State              string   `json:"state"`
	ObservedModel      string   `json:"observed_model,omitempty"`
	ObservedEffort     string   `json:"observed_effort,omitempty"`
	Error              string   `json:"error,omitempty"`
	Approval           *Event   `json:"approval,omitempty"`
	PendingApprovals   []Event  `json:"pending_approvals,omitempty"`
	ApprovalResponseID string   `json:"approval_response_id,omitempty"`
	ActiveTaskID       string   `json:"active_task_id,omitempty"`
	WorkflowID         string   `json:"workflow_id,omitempty"`
	Revision           string   `json:"revision,omitempty"`
	Configured         bool     `json:"configured"`
	ProfileExplicit    bool     `json:"profile_explicit"`
	LastOutput         string   `json:"last_output,omitempty"`
}
type Task struct {
	BatchedInto        string     `json:"batched_into,omitempty"`
	WorkflowPhase      string     `json:"workflow_phase,omitempty"`
	WorkflowGeneration int64      `json:"workflow_generation,omitempty"`
	WorkflowTaskIndex  *int       `json:"workflow_task_index,omitempty"`
	WorkflowSessionID  string     `json:"workflow_session_id,omitempty"`
	ID                 string     `json:"id"`
	From               string     `json:"from"`
	To                 string     `json:"to"`
	Role               string     `json:"role"`
	Text               string     `json:"text"`
	WorkflowID         string     `json:"workflow_id,omitempty"`
	WorkflowType       string     `json:"workflow_type,omitempty"`
	Complexity         string     `json:"complexity,omitempty"`
	Revision           string     `json:"revision,omitempty"`
	State              string     `json:"state"`
	Result             string     `json:"result,omitempty"`
	Error              string     `json:"error,omitempty"`
	Profile            Profile    `json:"profile"`
	Kind               string     `json:"kind"`
	ParentTaskID       string     `json:"parent_task_id,omitempty"`
	ReturnTo           string     `json:"return_to,omitempty"`
	AutoRound          int        `json:"auto_round"`
	InputTokens        *int64     `json:"input_tokens,omitempty"`
	OutputTokens       *int64     `json:"output_tokens,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	RequestHash        string     `json:"request_hash,omitempty"`
}
type Team struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	ProjectRoot  string    `json:"project_root"`
	State        string    `json:"state"`
	Members      []Member  `json:"members"`
	Tasks        []Task    `json:"tasks"`
	Limits       Limits    `json:"limits"`
	Turns        int       `json:"turns"`
	AutoWakeups  int       `json:"auto_wakeups"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	UsageKnown   bool      `json:"usage_known"`
	Reason       string    `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

var (
	ErrNotFound  = errors.New("team or task not found")
	ErrClosed    = errors.New("team manager is closed")
	ErrConflict  = errors.New("request conflicts with existing state")
	ErrForbidden = errors.New("task is outside the member's assigned scope")
)
