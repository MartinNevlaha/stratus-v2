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
	ToolName      string         `json:"tool_name,omitempty"`
	ToolInput     map[string]any `json:"tool_input,omitempty"`
	Extra         map[string]any `json:"-"`
}

// Decision is the output written back to Claude Code.
type Decision struct {
	Continue bool   `json:"continue"`
	Reason   string `json:"reason,omitempty"`
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
	if decision.Continue {
		Allow()
	} else {
		Block(decision.Reason)
	}
}
