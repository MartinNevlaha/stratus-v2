package hooks

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// AgentActivity reports subagent lifecycle (SubagentStart/SubagentStop) and tool calls
// (PostToolUse inside a subagent) to the Stratus API, so the dashboard can show agents
// of normal workflows. Best-effort side effect: it always allows.
func AgentActivity(event HookEvent) Decision {
	payload, ok := buildAgentEvent(event)
	if !ok {
		return Decision{Continue: true}
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Post("http://localhost:"+getPort()+"/api/workflow-agents/events", "application/json", bytes.NewReader(body))
	if err == nil {
		resp.Body.Close()
	}
	return Decision{Continue: true}
}

// buildAgentEvent maps a hook event to the API payload; ok is false for events that
// carry nothing to record (main-agent tool calls, other hook kinds).
func buildAgentEvent(event HookEvent) (map[string]any, bool) {
	if event.SessionID == "" {
		return nil, false
	}
	if p, ok := waitingEvent(event); ok {
		return p, true
	}
	if event.AgentID == "" {
		return nil, false
	}
	p := map[string]any{
		"session_id": event.SessionID,
		"agent_id":   event.AgentID,
		"agent_type": event.AgentType,
	}
	switch event.HookEventName {
	case "SubagentStart":
		p["event"] = "start"
		p["transcript_path"] = event.TranscriptPath
	case "SubagentStop":
		p["event"] = "stop"
		msg := event.LastAssistantMessage
		if msg == "" {
			msg = finalAgentMessage(event.AgentTranscriptPath)
		}
		p["last_assistant_message"] = msg
		p["agent_transcript_path"] = event.AgentTranscriptPath
	case "PostToolUse":
		if event.ToolName == handbackTool {
			return nil, false // the report delivery itself is not work worth counting
		}
		p["event"] = "tool"
		p["tool_name"] = event.ToolName
		p["files"] = touchedPaths(event)
		p["summary"] = toolSummary(event)
	default:
		return nil, false
	}
	return p, true
}

const maxToolSummary = 160

// waitingEvent maps the moments Claude waits for the human to a "waiting" API event: a
// Notification (permission or idle prompt) or a PreToolUse of AskUserQuestion.
func waitingEvent(event HookEvent) (map[string]any, bool) {
	switch {
	case event.HookEventName == "Notification" && event.Message != "" && notificationMeansWaiting(event.NotificationType):
		return map[string]any{
			"event": "waiting", "session_id": event.SessionID,
			"message": event.Message, "notification_type": event.NotificationType,
		}, true
	case event.HookEventName == "PreToolUse" && event.ToolName == "AskUserQuestion":
		msg := "Claude is asking for your decision"
		if qs, ok := event.ToolInput["questions"].([]any); ok && len(qs) > 0 {
			if q, ok := qs[0].(map[string]any); ok {
				if text, _ := q["question"].(string); text != "" {
					msg = text
				}
			}
		}
		return map[string]any{
			"event": "waiting", "session_id": event.SessionID,
			"message": msg, "notification_type": "ask_user_question",
		}, true
	}
	return nil, false
}

// notificationMeansWaiting reports whether a Notification type asks the human for something.
// An empty type (older Claude Code) is treated as waiting; completion, auth and quota
// notices are not.
func notificationMeansWaiting(t string) bool {
	switch t {
	case "", "permission_prompt", "idle_prompt", "elicitation_dialog", "elicitation_url_dialog", "agent_needs_input":
		return true
	}
	return false
}

// toolSummary is a short human-readable description of what a tool call touched.
func toolSummary(event HookEvent) string {
	for _, key := range []string{"file_path", "notebook_path", "command", "pattern", "path", "query", "url"} {
		if v, _ := event.ToolInput[key].(string); v != "" {
			if len(v) > maxToolSummary {
				return v[:maxToolSummary]
			}
			return v
		}
	}
	return ""
}

// handbackTool is the pseudo tool a subagent uses to deliver its final report.
const handbackTool = "SubagentHandback"

// maxTranscriptScan bounds how much of a subagent transcript is read for its report.
const maxTranscriptScan = 4 << 20

// finalAgentMessage returns a subagent's final report from its JSONL transcript: the
// message of its last SubagentHandback call, else the text of its last assistant turn.
// Best-effort: any read or parse problem yields "".
func finalAgentMessage(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > maxTranscriptScan {
		if _, err := f.Seek(st.Size()-maxTranscriptScan, 0); err != nil {
			return ""
		}
	}

	var handback, lastText string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxTranscriptScan)
	for sc.Scan() {
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type  string         `json:"type"`
					Text  string         `json:"text"`
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "assistant" {
			continue // also skips the partial first line after a tail seek
		}
		var text []string
		for _, c := range line.Message.Content {
			switch {
			case c.Type == "tool_use" && c.Name == handbackTool:
				if m, _ := c.Input["message"].(string); m != "" {
					handback = m
				}
			case c.Type == "text" && c.Text != "":
				text = append(text, c.Text)
			}
		}
		if len(text) > 0 {
			lastText = strings.Join(text, "\n")
		}
	}
	if handback != "" {
		return handback
	}
	return lastText
}

// touchedPaths lists the files a tool call wrote: Write/Edit/NotebookEdit inputs, or the
// redirect and tee targets of a Bash command.
func touchedPaths(event HookEvent) []string {
	if event.ToolName == "Bash" {
		cmd, _ := event.ToolInput["command"].(string)
		return bashWrittenPaths(cmd)
	}
	return watcherExtractPaths(event)
}

var (
	// redirectRe matches `>` and `>>` (optionally with a leading fd) and captures the target.
	// A target cannot start with `&`, so descriptor duplication such as 2>&1 never matches.
	redirectRe = regexp.MustCompile(`(?:^|\s)\d?>>?\s*("[^"]+"|'[^']+'|[^\s;&|<>()]+)`)
	teeRe      = regexp.MustCompile(`\btee\s+(?:-a\s+)?([^\s;&|<>()]+)`)
)

// bashWrittenPaths extracts files a shell command writes via redirection or tee. It is a
// heuristic for the dashboard's per-task file list, not a security boundary (that is
// bash_write_guard's job). Targets with spaces in quotes are skipped.
func bashWrittenPaths(command string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || p == "/dev/null" || strings.ContainsAny(p, "\"' ") || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, m := range redirectRe.FindAllStringSubmatch(command, -1) {
		add(m[1])
	}
	for _, m := range teeRe.FindAllStringSubmatch(command, -1) {
		add(m[1])
	}
	return out
}
