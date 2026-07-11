package hooks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const noActiveWorkflowReason = "No active workflow registered. Use mcp__stratus__register_workflow first."

const unresolvedWorkflowReason = "No workflow could be resolved for this delegation. Register a workflow with mcp__stratus__register_workflow first. " +
	"If several workflows are active in parallel, include the exact workflow ID in the Task prompt so the correct flow is selected."

// workflowIDRe matches an explicit workflow ID (spec-/bug-/e2e- prefixed) embedded in a
// Task prompt. Mirrors the OpenCode plugin regex so both runtimes resolve identically.
var workflowIDRe = regexp.MustCompile(`\b(?:bug|spec|e2e)-[a-z0-9][a-z0-9-]{0,120}\b`)

// phaseAgentAllowlist defines which delivery agents are allowed in each phase per workflow type.
var phaseAgentAllowlist = map[string]map[string][]string{
	"bug": {
		"analyze": {"delivery-debugger", "delivery-strategic-architect", "delivery-system-architect", "Plan", "Explore"},
		"fix": {
			"delivery-backend-engineer", "delivery-frontend-engineer", "delivery-database-engineer",
			"delivery-devops-engineer", "delivery-mobile-engineer", "delivery-implementation-expert",
			"delivery-ux-designer",
		},
		"review": {"delivery-code-reviewer"},
	},
	"spec": {
		"plan":       {"delivery-strategic-architect", "delivery-system-architect", "Plan", "Explore"},
		"discovery":  {"delivery-debugger", "delivery-strategic-architect", "Explore"},
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
		"plan":     {"delivery-strategic-architect", "Plan"},
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

	wf, err := fetchWorkflowForTaskStrict(event.ToolInput, event.SessionID)
	if err != nil {
		return Decision{
			Continue: false,
			Reason:   "Cannot verify workflow: " + err.Error() + ". Ensure Stratus server is running (stratus serve).",
		}
	}
	if wf == nil {
		return Decision{
			Continue: false,
			Reason:   unresolvedWorkflowReason,
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

	wf, err := fetchWorkflowForTaskStrict(event.ToolInput, event.SessionID)
	if err != nil {
		return Decision{
			Continue: false,
			Reason:   "Cannot verify workflow: " + err.Error() + ". Ensure Stratus server is running (stratus serve).",
		}
	}
	if wf == nil {
		return Decision{
			Continue: false,
			Reason:   unresolvedWorkflowReason,
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

// fetchWorkflowForTaskStrict resolves the workflow a Task delegation belongs to. It mirrors
// the OpenCode plugin: an explicit workflow ID in the task prompt is the most reliable signal
// and the only one that disambiguates parallel workflows sharing a session. FAIL-CLOSED:
// returns an error if the Stratus API is unreachable.
//
// Resolution order:
//  1. Workflow ID present in the task text (unique match, or the session-owned one).
//  2. Prefix-form ID (spec-/bug-/e2e-) not yet in dashboard state, fetched by ID.
//  3. Session ownership — only when it resolves to a single workflow.
//
// When several workflows match without a disambiguating ID, it returns nil rather than
// picking by list order, so parallel flows never mix.
func fetchWorkflowForTaskStrict(toolInput map[string]any, sessionID string) (map[string]any, error) {
	state, err := fetchDashboardStateStrict()
	if err != nil {
		return nil, err
	}

	var workflows []map[string]any
	for _, wf := range state.Workflows {
		if wf != nil {
			workflows = append(workflows, wf)
		}
	}

	taskText := getTaskText(toolInput)

	// 1. Explicit workflow ID embedded in the task text.
	var textMatches []map[string]any
	for _, wf := range workflows {
		if id, _ := wf["id"].(string); id != "" && strings.Contains(taskText, id) {
			textMatches = append(textMatches, wf)
		}
	}
	if len(textMatches) == 1 {
		return textMatches[0], nil
	}
	if len(textMatches) > 1 {
		// Multiple IDs in the prompt: prefer one owned by this session; else ambiguous.
		for _, wf := range textMatches {
			if s, _ := wf["session_id"].(string); sessionID != "" && s == sessionID {
				return wf, nil
			}
		}
		return nil, nil
	}

	// 2. Prefix-form ID not present in dashboard state.
	if id := workflowIDRe.FindString(taskText); id != "" {
		wf, err := fetchWorkflowByID(id)
		if err != nil {
			return nil, err
		}
		if wf != nil {
			return wf, nil
		}
	}

	// 3. Session ownership — only when it resolves to a single workflow. Picking the first
	//    of several session-mates is exactly what let parallel flows mix.
	if sessionID != "" {
		var sessionMatches []map[string]any
		for _, wf := range workflows {
			if s, _ := wf["session_id"].(string); s == sessionID {
				sessionMatches = append(sessionMatches, wf)
			}
		}
		if len(sessionMatches) == 1 {
			return sessionMatches[0], nil
		}
	}

	return nil, nil
}

// getTaskText concatenates the free-text fields of a Task tool call for workflow-ID matching.
func getTaskText(toolInput map[string]any) string {
	var parts []string
	for _, key := range []string{"prompt", "command", "description"} {
		if v, ok := toolInput[key].(string); ok && v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, "\n")
}

// fetchWorkflowByID looks up a single workflow by ID. Returns (nil, nil) on 404.
func fetchWorkflowByID(id string) (map[string]any, error) {
	port := getPort()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:" + port + "/api/workflows/" + url.PathEscape(id))
	if err != nil {
		return nil, fmt.Errorf("stratus API unreachable at localhost:%s: %w", port, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stratus API returned status %d", resp.StatusCode)
	}

	var wf map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&wf); err != nil {
		return nil, fmt.Errorf("failed to decode stratus response: %w", err)
	}
	return wf, nil
}

// fetchActiveWorkflow queries the local Stratus API for the active workflow state.
//
// Matching priority:
//  1. Exact session_id match — preferred and unambiguous (multiple concurrent windows).
//  2. Single active workflow — last-resort fallback for resumed sessions whose session_id
//     changed, or when CLAUDE_SESSION_ID was unavailable; safe only because there is
//     exactly one flow to bind to.
//
// When several workflows run in parallel and none is owned by this session, we do NOT
// guess. Binding the session to whichever flow happens to be first would gate its writes
// against the wrong phase (the flows "mix"). Returning nil keeps PhaseGuard best-effort
// (it simply does not block) rather than blocking legitimate work under a foreign phase.
func fetchActiveWorkflow(sessionID string) map[string]any {
	state := fetchDashboardState()
	if state == nil {
		return nil
	}

	var workflows []map[string]any
	for _, wf := range state.Workflows {
		if wf != nil {
			workflows = append(workflows, wf)
		}
	}

	// 1. Exact session match is unambiguous — always prefer it.
	if sessionID != "" {
		for _, wf := range workflows {
			if s, _ := wf["session_id"].(string); s == sessionID {
				return wf
			}
		}
	}

	// 2. No session match: fall back only when a single workflow is active.
	if len(workflows) == 1 {
		return workflows[0]
	}

	// 3. Ambiguous (multiple parallel workflows, none owned by this session) → don't guess.
	return nil
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
	// Env var takes highest priority.
	if p := os.Getenv("STRATUS_PORT"); p != "" {
		return p
	}
	// Walk up from cwd to find .stratus.json (matches config.Load behavior).
	dir := mustGetwd()
	for {
		data, err := os.ReadFile(filepath.Join(dir, ".stratus.json"))
		if err == nil {
			var cfg struct {
				Port int `json:"port"`
			}
			if json.Unmarshal(data, &cfg) == nil && cfg.Port > 0 {
				return fmt.Sprintf("%d", cfg.Port)
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "41777"
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
