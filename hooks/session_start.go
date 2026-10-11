package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SessionStart registers the session with Stratus and tells it which workflows are
// active in this project, so a new, resumed, cleared or compacted session knows where
// the work stands. Fail-open: an unreachable API adds nothing and blocks nothing.
func SessionStart(event HookEvent) Decision {
	registerSession(event)

	// Independent of the API: a disabled MCP server removes every mcp__stratus__* tool.
	notice := mcpDisabledNotice(candidateProjectDirs(event))

	state, err := fetchDashboardStateStrict()
	if err != nil {
		return Decision{Continue: true, Context: notice}
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
		return Decision{Continue: true, Context: notice}
	}
	context := "Active Stratus workflows in this project:\n" + strings.Join(lines, "\n") +
		"\nContinue one with /resume <workflow-id>. Register a workflow before delegating to delivery agents."
	if notice != "" {
		context = notice + "\n\n" + context
	}
	return Decision{Continue: true, Context: context}
}

// mcpDisabledNotice returns a warning when the project's Claude Code settings list the
// stratus MCP server in disabledMcpjsonServers (typically after declining the one-time
// approval prompt). Without the server every mcp__stratus__* tool is missing for the main
// agent and for subagents, workflow registration and swarm ticket updates silently fail,
// and the guards deny delegation, so the session needs to be told why.
func mcpDisabledNotice(dirs []string) string {
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		for _, name := range []string{"settings.local.json", "settings.json"} {
			data, err := os.ReadFile(filepath.Join(dir, ".claude", name))
			if err != nil {
				continue
			}
			var cfg struct {
				Disabled []string `json:"disabledMcpjsonServers"`
			}
			if json.Unmarshal(data, &cfg) != nil {
				continue
			}
			for _, s := range cfg.Disabled {
				if s == "stratus" {
					return "The Stratus MCP server is DISABLED for this project (\"stratus\" is in disabledMcpjsonServers in " +
						filepath.Join(dir, ".claude", name) + "). The mcp__stratus__* tools (register_workflow, transition_phase, " +
						"swarm_ticket_update, ...) are unavailable to you and to every subagent, so workflows cannot be tracked and " +
						"swarm workers cannot report tickets. Tell the user to enable it with /mcp (or remove \"stratus\" from " +
						"disabledMcpjsonServers) and restart the session."
				}
			}
		}
	}
	return ""
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
