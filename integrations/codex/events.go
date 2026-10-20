// Package codex adapts Codex lifecycle events without depending on Claude's wire format.
package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/MartinNevlaha/stratus-v2/integrations"
	"io"
	"strings"
	"unicode/utf8"
)

type HookEvent struct {
	Name                 string          `json:"hook_event_name"`
	SessionID            string          `json:"session_id"`
	Cwd                  string          `json:"cwd"`
	TurnID               string          `json:"turn_id"`
	AgentID              string          `json:"agent_id"`
	AgentType            string          `json:"agent_type"`
	ToolName             string          `json:"tool_name"`
	ToolUseID            string          `json:"tool_use_id"`
	ToolInput            map[string]any  `json:"tool_input"`
	ToolResponse         json.RawMessage `json:"tool_response"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	MainAgent            bool            `json:"main_agent"`
}

func Decode(r io.Reader) (HookEvent, error) {
	var h HookEvent
	d := json.NewDecoder(io.LimitReader(r, 2<<20))
	e := d.Decode(&h)
	if e == nil {
		var extra any
		if d.Decode(&extra) != io.EOF {
			e = fmt.Errorf("multiple hook payloads")
		}
	}
	return h, e
}
func Normalize(h HookEvent) (integrations.Event, error) {
	return normalizeForProject(h, h.Cwd)
}
func normalizeForProject(h HookEvent, root string) (integrations.Event, error) {
	e := integrations.Event{NativeSessionID: h.SessionID, TurnID: h.TurnID, NativeAgentID: h.AgentID, AgentType: h.AgentType, ToolName: h.ToolName, ToolUseID: h.ToolUseID, MainAgent: h.MainAgent, Report: clipBytes(h.LastAssistantMessage, 16<<10)}
	kinds := map[string]string{"SessionStart": "session_start", "SubagentStart": "agent_start", "SubagentStop": "agent_stop", "PostToolUse": "tool", "PermissionRequest": "waiting", "Stop": "turn_stop", "Interrupt": "interrupt", "SessionEnd": "session_end"}
	e.Kind = kinds[h.Name]
	if e.Kind == "" || strings.TrimSpace(e.NativeSessionID) == "" {
		return e, fmt.Errorf("unsupported hook or missing session identity")
	}
	if (e.Kind == "agent_start" || e.Kind == "agent_stop") && e.NativeAgentID == "" {
		return e, fmt.Errorf("agent identity required")
	}
	if e.Kind == "tool" && e.ToolName == "" {
		return e, fmt.Errorf("tool name required")
	}
	e.WorkflowID, _ = h.ToolInput["workflow_id"].(string)
	for _, k := range []string{"command", "file_path", "path", "description", "message", "prompt"} {
		if v, ok := h.ToolInput[k].(string); ok && v != "" {
			e.Summary = clipRunes(v, 400)
			break
		}
	}
	var response map[string]any
	if json.Unmarshal(h.ToolResponse, &response) == nil {
		if v, ok := response["success"].(bool); ok {
			e.Succeeded = &v
		} else if v, ok := response["exit_code"].(float64); ok {
			b := v == 0
			e.Succeeded = &b
		} else if v, ok := response["isError"].(bool); ok {
			b := !v
			e.Succeeded = &b
		}
	}
	if e.Succeeded == nil {
		var response string
		if json.Unmarshal(h.ToolResponse, &response) == nil && strings.HasPrefix(response, "Success.") {
			b := true
			e.Succeeded = &b
		}
	}
	if h.ToolName == "apply_patch" && e.Succeeded != nil && *e.Succeeded {
		command, _ := h.ToolInput["command"].(string)
		paths, err := PatchPaths(command)
		if err != nil {
			return e, err
		}
		for _, p := range paths {
			if p, ok := operationProjectPath(root, h.Cwd, p); ok {
				e.Files = append(e.Files, p)
			}
		}
	}
	return e, nil
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func AgentKey(session, agent string) string {
	if session == "" || agent == "" {
		return ""
	}
	return "codex-agent-" + digest([]string{session, agent})
}

// AgentRunKey isolates repeated native turns without reopening historical runs.
func AgentRunKey(session, agent, turn string) string {
	if turn == "" {
		return AgentKey(session, agent)
	}
	if session == "" || agent == "" {
		return ""
	}
	return "codex-run-" + digest([]string{session, agent, turn})
}
func CoordinatorKey(session, turn string) string {
	if session == "" || turn == "" {
		return ""
	}
	return "codex-coordinator-" + digest([]string{session, turn})
}
func EventKey(e integrations.Event) string {
	if e.Kind == "tool" && (e.ToolUseID == "" || e.TurnID == "") {
		return ""
	}
	return digest([]string{"codex", e.NativeSessionID, e.Kind, e.NativeAgentID, e.TurnID, e.ToolUseID})
}
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "… [truncated]"
	}
	return s
}
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n-16]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "… [truncated]"
}
