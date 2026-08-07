// Package hooks implements Claude Code lifecycle hook handlers.
// Hooks receive JSON on stdin and write JSON to stdout.
// Exit code 0 = allow, exit code 2 = block with message.
package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// HookEvent is the input received from Claude Code.
type HookEvent struct {
	HookEventName string         `json:"hook_event_name"`
	SessionID     string         `json:"session_id"`
	Cwd           string         `json:"cwd,omitempty"`
	ToolName      string         `json:"tool_name,omitempty"`
	ToolInput     map[string]any `json:"tool_input,omitempty"`
	AgentID       string         `json:"agent_id,omitempty"`
	AgentType     string         `json:"agent_type,omitempty"`
	// TranscriptPath points at the JSONL transcript of the agent the hook fired for.
	TranscriptPath string `json:"transcript_path,omitempty"`
	// TeammateName and TeamName are only set on Agent Teams events (TeammateIdle,
	// TaskCreated, TaskCompleted).
	TeammateName string         `json:"teammate_name,omitempty"`
	TeamName     string         `json:"team_name,omitempty"`
	Extra        map[string]any `json:"-"`
}

// Decision is the output written back to Claude Code.
type Decision struct {
	Continue bool   `json:"continue"`
	Reason   string `json:"reason,omitempty"`
	// Nudge redirects the agent instead of stopping it: Claude Code injects Reason
	// into the agent's conversation and lets it keep working. Only meaningful for
	// stop-like events (TeammateIdle); PreToolUse guards deny with Continue=false.
	Nudge bool `json:"-"`
}

// Handler is a function that processes a hook event.
type Handler func(event HookEvent) Decision

// ReadEvent reads and parses the hook event from stdin.
func ReadEvent() (HookEvent, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return HookEvent{}, fmt.Errorf("read stdin: %w", err)
	}
	var event HookEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return HookEvent{}, fmt.Errorf("parse event: %w", err)
	}
	return event, nil
}

// Allow writes an allow response and exits 0.
func Allow() {
	writeDecision(Decision{Continue: true})
	os.Exit(0)
}

// Block writes a block response with reason and exits 2.
func Block(reason string) {
	writeDecision(Decision{Continue: false, Reason: reason})
	os.Exit(2)
}

// Nudge writes a stop-hook "block" decision and exits 0: Claude Code feeds reason
// back into the agent's conversation as a message and the agent keeps working.
//
// This deliberately does not reuse Block. Claude Code reads `{"continue": false}`
// as preventContinuation — it would stop the agent dead, which is the opposite of
// redirecting it — and exit 2 is the plain blocking-error path.
func Nudge(reason string) {
	fmt.Println(string(nudgePayload(reason)))
	os.Exit(0)
}

func nudgePayload(reason string) []byte {
	data, _ := json.Marshal(map[string]string{"decision": "block", "reason": reason})
	return data
}

func writeDecision(d Decision) {
	data, _ := json.Marshal(d)
	fmt.Println(string(data))
}

// Run is the main hook dispatch function.
// name must match the hook name passed as the first CLI arg (e.g., "phase_guard").
func Run(name string, handlers map[string]Handler) {
	event, err := ReadEvent()
	if err != nil {
		// Best-effort: never block on error
		Allow()
		return
	}

	h, ok := handlers[name]
	if !ok {
		Allow()
		return
	}

	decision := h(event)
	switch {
	case decision.Nudge:
		Nudge(decision.Reason)
	case decision.Continue:
		Allow()
	default:
		Block(decision.Reason)
	}
}
