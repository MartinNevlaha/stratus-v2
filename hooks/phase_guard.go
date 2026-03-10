package hooks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const noActiveWorkflowReason = "No active workflow registered. Run POST /api/workflows first."

// PhaseGuard blocks disallowed tools during certain workflow phases.
func PhaseGuard(event HookEvent) Decision {
	if event.ToolName == "" {
		return Decision{Continue: true}
	}

	state := fetchActiveWorkflow(event.SessionID)
	if state == nil {
		return Decision{Continue: true} // no active workflow
	}

	phase, _ := state["phase"].(string)
	wtype, _ := state["type"].(string)

	// During verify/review phase: block write tools for delivery agents
	if (phase == "verify" && wtype == "spec") || (phase == "review" && wtype == "bug") {
		writeTool := isWriteTool(event.ToolName)
		if writeTool && isDeliveryAgent() {
			return Decision{
				Continue: false,
				Reason:   "Write tools are not allowed during " + phase + " phase. Use Read/Grep/Glob only.",
			}
		}
	}

	return Decision{Continue: true}
}

// WorkflowExistenceGuard blocks Task delegation when the current session has no active workflow.
func WorkflowExistenceGuard(event HookEvent) Decision {
	if event.ToolName != "Task" {
		return Decision{Continue: true}
	}

	if fetchWorkflowForSession(event.SessionID) == nil {
		return Decision{
			Continue: false,
			Reason:   noActiveWorkflowReason,
		}
	}

	return Decision{Continue: true}
}

// DelegationGuard prevents spawning write-capable Task agents without an active workflow.
func DelegationGuard(event HookEvent) Decision {
	if event.ToolName != "Task" {
		return Decision{Continue: true}
	}

	subagentType, _ := event.ToolInput["subagent_type"].(string)
	if !isDeliverySubagent(subagentType) {
		return Decision{Continue: true} // system or read-only agent, always OK
	}

	state := fetchWorkflowForSession(event.SessionID)
	if state == nil {
		return Decision{
			Continue: false,
			Reason:   noActiveWorkflowReason,
		}
	}

	return Decision{Continue: true}
}

// WorkflowEnforcer nudges the coordinator when idle between phases.
func WorkflowEnforcer(event HookEvent) Decision {
	// Best-effort: always allow, just emit nudge to coordinator
	return Decision{Continue: true}
}

// isWriteTool returns true for tools that modify files or run commands.
func isWriteTool(name string) bool {
	writeTools := map[string]bool{
		"Write": true, "Edit": true, "Bash": true,
		"NotebookEdit": true, "MultiEdit": true,
	}
	return writeTools[name]
}

// isDeliverySubagent returns true for subagent types that perform write operations.
func isDeliverySubagent(subagentType string) bool {
	return strings.HasPrefix(subagentType, "delivery-")
}

// isDeliveryAgent checks if the current process is running as a delivery agent.
func isDeliveryAgent() bool {
	// Heuristic: check if CLAUDE_AGENT_ID env var is set and starts with "delivery-"
	agentID := os.Getenv("CLAUDE_AGENT_ID")
	return isDeliverySubagent(agentID)
}

type dashboardState struct {
	Workflows []map[string]any `json:"workflows"`
}

// fetchWorkflowForSession returns the active workflow for the exact Claude session.
func fetchWorkflowForSession(sessionID string) map[string]any {
	if sessionID == "" {
		return nil
	}

	state := fetchDashboardState()
	if state == nil {
		return nil
	}

	for _, wf := range state.Workflows {
		if wf == nil {
			continue
		}
		wfSession, _ := wf["session_id"].(string)
		if wfSession == sessionID {
			return wf
		}
	}
	return nil
}

// fetchActiveWorkflow queries the local Stratus API for the active workflow state.
//
// Matching priority:
//  1. Exact session_id match          — preferred (multiple concurrent windows)
//  2. Workflow with no session_id set — created before session tracking / CLAUDE_SESSION_ID
//     was not available in the Bash environment; treated as a wildcard
//  3. First active workflow           — last-resort fallback for resumed sessions whose
//     session_id changed; PhaseGuard uses this to keep phase checks best-effort
func fetchActiveWorkflow(sessionID string) map[string]any {
	state := fetchDashboardState()
	if state == nil {
		return nil
	}

	var untracked, first map[string]any
	for _, wf := range state.Workflows {
		if wf == nil {
			continue
		}
		// Track first workflow for last-resort fallback.
		if first == nil {
			first = wf
		}
		if sessionID == "" {
			return wf // no session filter → return first
		}
		wfSession, _ := wf["session_id"].(string)
		if wfSession == sessionID {
			return wf // exact match — best case
		}
		if wfSession == "" && untracked == nil {
			untracked = wf // workflow without session tracking
		}
	}
	// Fall back: prefer an untracked workflow, then any workflow.
	// This handles CLAUDE_SESSION_ID being unset during /spec and resumed sessions.
	if untracked != nil {
		return untracked
	}
	return first
}

func fetchDashboardState() *dashboardState {
	port := getPort()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:" + port + "/api/dashboard/state")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var state dashboardState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return nil
	}
	return &state
}

func getPort() string {
	if p := os.Getenv("STRATUS_PORT"); p != "" {
		return p
	}
	// Try to read from .stratus.json
	data, err := os.ReadFile(filepath.Join(mustGetwd(), ".stratus.json"))
	if err != nil {
		return "41777"
	}
	var cfg struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil || cfg.Port == 0 {
		return "41777"
	}
	return fmt.Sprintf("%d", cfg.Port)
}

func mustGetwd() string {
	wd, _ := os.Getwd()
	return wd
}

// TeammateIdle is called when a CC Agent Teams teammate goes idle.
// Trial version: always allow. Future: check assigned Stratus tasks.
func TeammateIdle(_ HookEvent) Decision {
	return Decision{Continue: true}
}

// TaskCompleted is called when a CC native task is marked complete.
// Trial version: always allow. Future: verify deliverables.
func TaskCompleted(_ HookEvent) Decision {
	return Decision{Continue: true}
}
