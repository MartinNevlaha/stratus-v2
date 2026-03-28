package hooks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Streamer fires on every PreToolUse event, posts a lightweight log entry to
// the Stratus API, and broadcasts it to dashboard clients via WebSocket.
// It always allows the tool call — this is a best-effort side effect.
func Streamer(event HookEvent) Decision {
	summary := streamerSummary(event)
	port := getPort()
	body, _ := json.Marshal(map[string]any{
		"session_id": event.SessionID,
		"tool_name":  event.ToolName,
		"summary":    summary,
	})
	client := &http.Client{Timeout: 500 * time.Millisecond}
	req, err := http.NewRequest("POST",
		"http://localhost:"+port+"/api/workflow_logs",
		bytes.NewReader(body))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
	}
	return Decision{Continue: true}
}

// streamerSummary extracts a concise human-readable description of the tool call.
func streamerSummary(event HookEvent) string {
	in := event.ToolInput
	switch event.ToolName {
	case "Bash":
		cmd, _ := in["command"].(string)
		cmd = strings.TrimSpace(cmd)
		if len(cmd) > 120 {
			cmd = cmd[:120] + "…"
		}
		return cmd
	case "Write", "Edit", "Read":
		if p, _ := in["file_path"].(string); p != "" {
			return p
		}
	case "NotebookEdit":
		if p, _ := in["notebook_path"].(string); p != "" {
			return p
		}
	case "MultiEdit":
		if edits, _ := in["edits"].([]any); len(edits) > 0 {
			if first, ok := edits[0].(map[string]any); ok {
				if p, _ := first["file_path"].(string); p != "" {
					return p
				}
			}
		}
	case "Task":
		if t, _ := in["subagent_type"].(string); t != "" {
			return t
		}
	case "WebSearch":
		if q, _ := in["query"].(string); q != "" {
			if len(q) > 120 {
				q = q[:120] + "…"
			}
			return q
		}
	case "WebFetch":
		if u, _ := in["url"].(string); u != "" {
			return u
		}
	case "Glob":
		if p, _ := in["pattern"].(string); p != "" {
			return p
		}
	case "Grep":
		if p, _ := in["pattern"].(string); p != "" {
			return p
		}
	}
	return event.ToolName
}
