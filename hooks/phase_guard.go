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

const noActiveWorkflowReason = "No active workflow registered. Use mcp__stratus__register_workflow first."

// phaseAgentAllowlist defines which delivery agents are allowed in each phase per workflow type.
var phaseAgentAllowlist = map[string]map[string][]string{
	"bug": {
		"analyze": {"delivery-debugger", "delivery-strategic-architect", "delivery-system-architect", "plan", "explore"},
		"fix": {
			"delivery-backend-engineer", "delivery-frontend-engineer", "delivery-database-engineer",
			"delivery-devops-engineer", "delivery-mobile-engineer", "delivery-implementation-expert",
			"delivery-ux-designer",
		},
		"review": {"delivery-code-reviewer"},
	},
	"spec": {
		"plan":       {"delivery-strategic-architect", "delivery-system-architect", "plan", "explore"},
		"discovery":  {"delivery-debugger", "delivery-strategic-architect", "explore"},
		"design":     {"delivery-strategic-architect", "delivery-system-architect", "delivery-ux-designer"},
		"governance": {"delivery-code-reviewer", "delivery-governance-checker"},
		"accept":     {},
		"implement": {
			"delivery-backend-engineer", "delivery-frontend-engineer", "delivery-database-engineer",
			"delivery-devops-engineer", "delivery-mobile-engineer", "delivery-implementation-expert",
			"delivery-ux-designer",
		},
		"verify":   {"delivery-code-reviewer"},
		"learn":    {},
		"complete": {},
	},
	"e2e": {
		"setup":    {"delivery-qa-engineer"},
		"plan":     {"delivery-strategic-architect", "plan"},
		"generate": {"delivery-qa-engineer", "delivery-frontend-engineer"},
		"heal":     {"delivery-debugger", "delivery-qa-engineer"},
		"complete": {},
	},
}

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
// FAIL-CLOSED: blocks if Stratus API is unreachable.
func WorkflowExistenceGuard(event HookEvent) Decision {
	if event.ToolName != "Task" {
		return Decision{Continue: true}
	}

	wf, err := fetchWorkflowForSessionStrict(event.SessionID)
	if err != nil {
		return Decision{
			Continue: false,
			Reason:   "Cannot verify workflow: " + err.Error() + ". Ensure Stratus server is running (stratus serve).",
		}
	}
	if wf == nil {
		return Decision{
			Continue: false,
			Reason:   noActiveWorkflowReason,
		}
	}

	return Decision{Continue: true}
}

// DelegationGuard prevents spawning write-capable Task agents without an active workflow.
// FAIL-CLOSED: blocks if Stratus API is unreachable.
// Also enforces phase-agent matching: delivery agents can only be delegated in allowed phases.
func DelegationGuard(event HookEvent) Decision {
	if event.ToolName != "Task" {
		return Decision{Continue: true}
	}

	subagentType, _ := event.ToolInput["subagent_type"].(string)
	if !isDeliverySubagent(subagentType) {
		return Decision{Continue: true}
	}

	wf, err := fetchWorkflowForSessionStrict(event.SessionID)
	if err != nil {
		return Decision{
			Continue: false,
			Reason:   "Cannot verify workflow: " + err.Error() + ". Ensure Stratus server is running (stratus serve).",
		}
	}
	if wf == nil {
		return Decision{
			Continue: false,
			Reason:   noActiveWorkflowReason,
		}
	}

	phase, _ := wf["phase"].(string)
	wtype, _ := wf["type"].(string)

	if !isAgentAllowedInPhase(subagentType, wtype, phase) {
		allowed := getAllowedAgentsForPhase(wtype, phase)
		return Decision{
			Continue: false,
			Reason: fmt.Sprintf("Agent %q is not allowed in phase %q (workflow type: %s). Allowed agents: %v",
				subagentType, phase, wtype, allowed),
		}
	}

	return Decision{Continue: true}
}

// isAgentAllowedInPhase checks if the agent is allowed in the current phase.
func isAgentAllowedInPhase(agentID, wtype, phase string) bool {
	workflowAgents, ok := phaseAgentAllowlist[wtype]
	if !ok {
		return true
	}
	allowedAgents, ok := workflowAgents[phase]
	if !ok {
		return true
	}
	for _, a := range allowedAgents {
		if a == agentID {
			return true
		}
	}
	return false
}

// getAllowedAgentsForPhase returns the list of allowed agents for a phase.
func getAllowedAgentsForPhase(wtype, phase string) []string {
	if workflowAgents, ok := phaseAgentAllowlist[wtype]; ok {
		if agents, ok := workflowAgents[phase]; ok {
			return agents
		}
	}
	return []string{"(any)"}
}

// WorkflowEnforcer nudges the coordinator when idle between phases.
func WorkflowEnforcer(event HookEvent) Decision {
	// Best-effort: always allow, just emit nudge to coordinator
	return Decision{Continue: true}
}

// BashWriteGuard blocks file-modifying bash commands when running as a delivery agent without a workflow.
// This prevents delivery agents from bypassing workflow tracking via bash commands.
func BashWriteGuard(event HookEvent) Decision {
	if event.ToolName != "Bash" {
		return Decision{Continue: true}
	}

	// Only applies to delivery agents
	if !isDeliveryAgent() {
		return Decision{Continue: true}
	}

	command, _ := event.ToolInput["command"].(string)
	if !isWriteBashCommand(command) {
		return Decision{Continue: true}
	}

	// Check for active workflow
	wf, err := fetchWorkflowForSessionStrict(event.SessionID)
	if err != nil {
		return Decision{
			Continue: false,
			Reason:   "Cannot verify workflow: " + err.Error() + ". Ensure Stratus server is running (stratus serve).",
		}
	}
	if wf == nil {
		return Decision{
			Continue: false,
			Reason:   noActiveWorkflowReason + " Delivery agents must have an active workflow to execute write commands.",
		}
	}

	return Decision{Continue: true}
}

// isWriteBashCommand detects write operations in bash commands.
func isWriteBashCommand(cmd string) bool {
	// Normalize whitespace: replace tabs with spaces for consistent pattern matching
	normalizedCmd := strings.ReplaceAll(cmd, "\t", " ")
	lowerCmd := strings.ToLower(normalizedCmd)

	// Check write patterns FIRST - explicit redirects, file modifications, git write ops
	writePatterns := []string{
		" > ", " >> ", ">|",
		" 1>", " 2>", " &>", "2>&1",
		"sed -i", "awk -i",
		"tee ",
		"install ",
		"git add", "git commit", "git push", "git merge", "git rebase", "git cherry-pick", "git reset",
		"rm ", "rmdir ", "mv ", "mkdir ", "touch ",
		"chmod ", "chown ",
		"cp ",
		"dd ",
		"truncate ",
	}
	for _, p := range writePatterns {
		if strings.Contains(lowerCmd, p) {
			return true
		}
	}

	// Check read-only patterns BEFORE generic redirect check
	// This handles URLs and other cases where > appears but isn't a redirect
	readOnlyPatterns := []string{
		"git status", "git log", "git diff", "git show", "git branch", "git remote",
		"cat ", "head ", "tail ", "less ", "more ",
		"ls ", "find ", "which ", "whereis ",
		"grep ", "rg ", "ag ", "ack ",
		"go test", "npm test", "npm run test", "pytest", "jest", "cargo test",
		"curl ", "wget ",
	}
	for _, p := range readOnlyPatterns {
		if strings.Contains(lowerCmd, p) {
			return false
		}
	}

	// Check for redirects without spaces: `cmd>file`
	// Only if > is not part of a URL (preceded by / or :) or query param (preceded by =)
	if idx := strings.Index(lowerCmd, ">"); idx >= 0 {
		precededByURLContext := false
		if idx > 0 {
			prev := lowerCmd[idx-1]
			// /, :, = indicate URL or query param context
			if prev == '/' || prev == ':' || prev == '=' {
				precededByURLContext = true
			}
		}
		// If not URL context, it's likely a redirect
		if !precededByURLContext {
			return true
		}
	}

	return false
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
	wf, _ := fetchWorkflowForSessionStrict(sessionID)
	return wf
}

func fetchWorkflowForSessionStrict(sessionID string) (map[string]any, error) {
	if sessionID == "" {
		return nil, nil
	}

	state, err := fetchDashboardStateStrict()
	if err != nil {
		return nil, err
	}

	for _, wf := range state.Workflows {
		if wf == nil {
			continue
		}
		wfSession, _ := wf["session_id"].(string)
		if wfSession == sessionID {
			return wf, nil
		}
	}
	return nil, nil
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
	state, _ := fetchDashboardStateStrict()
	return state
}

func fetchDashboardStateStrict() (*dashboardState, error) {
	port := getPort()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:" + port + "/api/dashboard/state")
	if err != nil {
		return nil, fmt.Errorf("stratus API unreachable at localhost:%s: %w", port, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stratus API returned status %d", resp.StatusCode)
	}

	var state dashboardState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return nil, fmt.Errorf("failed to decode stratus response: %w", err)
	}
	return &state, nil
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
