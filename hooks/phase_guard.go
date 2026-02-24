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

// PhaseGuard blocks disallowed tools during certain workflow phases.
func PhaseGuard(event HookEvent) Decision {
	if event.ToolName == "" {
		return Decision{Continue: true}
	}

	state := fetchActiveWorkflow()
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

// DelegationGuard prevents spawning write-capable Task agents without an active workflow.
func DelegationGuard(event HookEvent) Decision {
	if event.ToolName != "Task" {
		return Decision{Continue: true}
	}

	subagentType, _ := event.ToolInput["subagent_type"].(string)
	if !isDeliverySubagent(subagentType) {
		return Decision{Continue: true} // system or read-only agent, always OK
	}

	state := fetchActiveWorkflow()
	if state == nil {
		return Decision{
			Continue: false,
			Reason:   "Cannot spawn delivery agent without an active workflow. Start a /spec or /bug workflow first.",
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

// fetchActiveWorkflow queries the local Stratus API for the active workflow state.
func fetchActiveWorkflow() map[string]any {
	port := getPort()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:" + port + "/api/dashboard/state")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var state struct {
		Workflows []map[string]any `json:"workflows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return nil
	}
	if len(state.Workflows) == 0 {
		return nil
	}
	return state.Workflows[0]
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
