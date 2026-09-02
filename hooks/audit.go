package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// denialLogName is the JSONL file every guard denial is appended to.
const denialLogName = "hook_denials.jsonl"

// auditDenial records a blocked tool call. Without it a denial reached only the agent:
// when the agent answered by ending its turn, the coordinator never learned which command
// was refused or why, which made two live incident reports impossible to confirm or refute.
//
// Both sinks are best-effort and must never change the decision or stall the hook:
//   - a JSONL line on disk, which still works when the API is down (that is exactly when
//     the fail-closed guards deny),
//   - a POST to /api/events, so the denial shows in the dashboard timeline and in search.
func auditDenial(event HookEvent, hookName, reason string, refs map[string]any) {
	record := map[string]any{
		"ts":         time.Now().UTC().Format(time.RFC3339),
		"hook":       hookName,
		"session_id": event.SessionID,
		"agent_type": agentTypeOf(event),
		"tool_name":  event.ToolName,
		"reason":     reason,
	}
	if cmd, _ := event.ToolInput["command"].(string); cmd != "" {
		record["command"] = cmd
	}
	if path, _ := event.ToolInput["file_path"].(string); path != "" {
		record["file_path"] = path
	}
	for k, v := range refs {
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		record[k] = v
	}

	appendDenialLine(record)
	postDenialEvent(record)
}

func agentTypeOf(event HookEvent) string {
	if event.AgentType != "" {
		return event.AgentType
	}
	return os.Getenv("CLAUDE_AGENT_ID")
}

func appendDenialLine(record map[string]any) {
	dir := stratusDataDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	line, err := json.Marshal(record)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, denialLogName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, string(line))
}

func postDenialEvent(record map[string]any) {
	title, _ := record["tool_name"].(string)
	body, err := json.Marshal(map[string]any{
		"actor":      "stratus-hook",
		"scope":      "project",
		"type":       "hook_denial",
		"title":      "hook denied " + title,
		"text":       record["reason"],
		"tags":       []string{"hook_denial", fmt.Sprint(record["hook"])},
		"refs":       record,
		"importance": 0.6,
	})
	if err != nil {
		return
	}
	req, err := http.NewRequest("POST", "http://localhost:"+getPort()+"/api/events", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 1 * time.Second}
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
}

// stratusDataDir mirrors config.Load's resolution order: STRATUS_DATA_DIR, then
// data_dir from the nearest .stratus.json walking up from cwd, then ~/.stratus/data.
func stratusDataDir() string {
	if d := os.Getenv("STRATUS_DATA_DIR"); d != "" {
		return d
	}
	dir := mustGetwd()
	for dir != "" {
		data, err := os.ReadFile(filepath.Join(dir, ".stratus.json"))
		if err == nil {
			var cfg struct {
				DataDir string `json:"data_dir"`
			}
			if json.Unmarshal(data, &cfg) == nil && cfg.DataDir != "" {
				return cfg.DataDir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".stratus", "data")
}
