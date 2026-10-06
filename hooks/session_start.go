package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// SessionStart registers the session with Stratus and tells it which workflows are
// active in this project, so a new, resumed, cleared or compacted session knows where
// the work stands. Fail-open: an unreachable API adds nothing and blocks nothing.
func SessionStart(event HookEvent) Decision {
	registerSession(event)

	state, err := fetchDashboardStateStrict()
	if err != nil {
		return Decision{Continue: true}
	}
	var lines []string
	for _, wf := range state.Workflows {
		phase, _ := wf["phase"].(string)
		if aborted, _ := wf["aborted"].(bool); aborted || phase == "" || phase == "complete" {
			continue
		}
		id, _ := wf["id"].(string)
		wfType, _ := wf["type"].(string)
		line := fmt.Sprintf("- %s (%s, phase %s", id, wfType, phase)
		if done, total := taskProgress(wf); total > 0 {
			line += fmt.Sprintf(", %d/%d tasks", done, total)
		}
		line += ")"
		if title, _ := wf["title"].(string); title != "" {
			line += " " + title
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return Decision{Continue: true}
	}
	return Decision{Continue: true, Context: "Active Stratus workflows in this project:\n" + strings.Join(lines, "\n") +
		"\nContinue one with /resume <workflow-id>. Register a workflow before delegating to delivery agents."}
}

func taskProgress(wf map[string]any) (done, total int) {
	tasks, _ := wf["tasks"].([]any)
	for _, t := range tasks {
		if task, _ := t.(map[string]any); task["status"] == "done" {
			done++
		}
	}
	total = len(tasks)
	if n, ok := wf["total_tasks"].(float64); ok && int(n) > total {
		total = int(n)
	}
	return done, total
}

// registerSession records the session in Stratus; the API keeps the first record of a
// session, so a resume or compaction re-registering it changes nothing. Best-effort.
func registerSession(event HookEvent) {
	if event.SessionID == "" {
		return
	}
	project := ""
	if event.Cwd != "" {
		project = filepath.Base(event.Cwd)
	}
	body, _ := json.Marshal(map[string]string{"content_session_id": event.SessionID, "project": project})
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Post("http://localhost:"+getPort()+"/api/sessions", "application/json", bytes.NewReader(body))
	if err == nil {
		resp.Body.Close()
	}
}
