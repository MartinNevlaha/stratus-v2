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

const unresolvedWorkflowReason = "WORKFLOW_NOT_RESOLVED: No workflow could be resolved for this delegation. " +
	"If several workflows are active in parallel, pass the exact workflow_id as structured tool input."

const (
	workflowNotFoundErr      = "WORKFLOW_NOT_FOUND"
	workflowNotResolvedErr   = "WORKFLOW_NOT_RESOLVED"
	ambiguousWorkflowErr     = "AMBIGUOUS_WORKFLOW"
	workflowIDMismatchErr    = "WORKFLOW_ID_MISMATCH"
	resolutionExplicitTool   = "explicit_tool"
	resolutionExplicitPrompt = "explicit_prompt"
	resolutionSession        = "session_fallback"
	resolutionGlobal         = "global_fallback"
)

// workflowIDRe matches an explicit workflow ID (spec-/bug-/e2e- prefixed) embedded in a
// Task prompt. Mirrors the OpenCode plugin regex so both runtimes resolve identically.
var workflowIDRe = regexp.MustCompile(`\b(?:bug|spec|e2e)-[a-z0-9][a-z0-9-]{0,120}\b`)

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

	// During verify/review phase: block write tools for delivery agents.
	//
	// Bash is judged by its COMMAND, not by its name. A reviewer that cannot run
	// `git diff` or the test suite cannot verify what it reviews -- it can only assert
	// that the code reads correctly, which is the failure mode this whole phase exists
	// to prevent. isWriteBashCommand is the same split BashWriteGuard already applies.
	if (phase == "verify" && wtype == "spec") || (phase == "review" && wtype == "bug") {
		if isWriteTool(event.ToolName) && isDeliveryAgent(event) {
			if event.ToolName == "Bash" {
				command, _ := event.ToolInput["command"].(string)
				// An absent command is unknown, not read-only -- stay fail-closed.
				if command != "" && !isWriteBashCommand(command) {
					return Decision{Continue: true}
				}
				return Decision{
					Continue: false,
					Reason: "This bash command writes, and writes are not allowed during " + phase +
						" phase: " + command + ". Read-only commands (git diff/log/status, tests, grep) are allowed.",
				}
			}
			return Decision{
				Continue: false,
				Reason:   "Write tools are not allowed during " + phase + " phase. Use Read/Grep/Glob or read-only Bash.",
			}
		}
	}

	return Decision{Continue: true}
}

// WorkflowExistenceGuard blocks delivery-agent delegation when the current session has no active workflow.
// FAIL-CLOSED: blocks if Stratus API is unreachable.
func WorkflowExistenceGuard(event HookEvent) Decision {
	if !isDelegationTool(event.ToolName) {
		return Decision{Continue: true}
	}

	agentType := delegatedAgentType(event)
	if !isDeliverySubagent(agentType) {
		return Decision{Continue: true}
	}

	wf, err := fetchWorkflowForTaskStrict(event.ToolInput, event.SessionID)
	if err != nil {
		if isWorkflowResolutionError(err) {
			return Decision{Continue: false, Reason: err.Error()}
		}
		if isStratusSelfRepo(event) {
			return Decision{Continue: true}
		}
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

// DelegationGuard prevents spawning write-capable delivery agents without an active workflow.
// FAIL-CLOSED: blocks if Stratus API is unreachable.
//
// It deliberately does NOT restrict which agent may run in which phase. The allowlist it
// used to carry blocked coordinators from picking an agent they legitimately needed, and
// a wrong denial mid-phase costs more than the ordering it enforced.
func DelegationGuard(event HookEvent) Decision {
	if !isDelegationTool(event.ToolName) {
		return Decision{Continue: true}
	}

	subagentType := delegatedAgentType(event)
	if !isDeliverySubagent(subagentType) {
		return Decision{Continue: true}
	}

	wf, err := fetchWorkflowForTaskStrict(event.ToolInput, event.SessionID)
	if err != nil {
		if isWorkflowResolutionError(err) {
			return Decision{Continue: false, Reason: err.Error()}
		}
		if isStratusSelfRepo(event) {
			return Decision{Continue: true}
		}
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
	if !isDeliveryAgent(event) {
		return Decision{Continue: true}
	}

	command, _ := event.ToolInput["command"].(string)
	if !isWriteBashCommand(command) {
		return Decision{Continue: true}
	}

	// Check for active workflow
	wf, err := fetchWorkflowForSessionStrict(event.SessionID)
	if err != nil {
		if isStratusSelfRepo(event) {
			return Decision{Continue: true}
		}
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

// fdDupRe matches descriptor duplication (`2>&1`, `1>&2`, `>&2`). It redirects one
// stream into another -- nothing reaches disk -- so it must not read as a redirect.
var fdDupRe = regexp.MustCompile(`\d?>&\d`)

// devNullRe matches a redirect into /dev/null (`2>/dev/null`, `>/dev/null`, `&> /dev/null`,
// `>> /dev/null`). The stream is discarded, so nothing reaches disk. Without this, the write
// pattern " 2>" below matched `grep ... 2>/dev/null` and -- because write patterns are tested
// before the read-only ones -- denied a reviewer a plain grep. Measured live 2026-09-01.
//
// The trailing group keeps the boundary: `>> /dev/null/notes.md` writes a real file and must
// still read as a write, so /dev/null only counts when nothing path-like follows it.
var devNullRe = regexp.MustCompile(`\d?&?>>?\s*/dev/null(\s|;|\||&|$)`)

// isWriteBashCommand detects write operations in bash commands.
func isWriteBashCommand(cmd string) bool {
	// Normalize whitespace: replace tabs with spaces for consistent pattern matching
	normalizedCmd := strings.ReplaceAll(cmd, "\t", " ")
	// Drop descriptor duplication first. `pytest ... 2>&1 | tail` is how this repo's
	// suites are run, and reading its `2>&1` as a file redirect denied a reviewer every
	// command it needed. A real redirect into a FILE still matches below.
	normalizedCmd = fdDupRe.ReplaceAllString(normalizedCmd, " ")
	// Same reasoning for /dev/null: discarding a stream is not a write.
	normalizedCmd = devNullRe.ReplaceAllString(normalizedCmd, " $1")
	lowerCmd := strings.ToLower(normalizedCmd)

	// Check write patterns FIRST - explicit redirects, file modifications, git write ops
	writePatterns := []string{
		" > ", " >> ", ">|",
		" 1>", " 2>", " &>",
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

func isDelegationTool(toolName string) bool {
	return toolName == "Agent" || toolName == "Task"
}

func delegatedAgentType(event HookEvent) string {
	for _, key := range []string{"agent_type", "subagent_type", "type"} {
		if v, ok := event.ToolInput[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// isDeliverySubagent returns true for subagent types that perform write operations.
func isDeliverySubagent(subagentType string) bool {
	return strings.HasPrefix(subagentType, "delivery-")
}

// isDeliveryAgent checks if the current process is running as a delivery agent.
func isDeliveryAgent(event HookEvent) bool {
	if event.AgentType != "" {
		return isDeliverySubagent(event.AgentType)
	}
	// Fallback for older Claude Code builds that exposed only environment state.
	return isDeliverySubagent(os.Getenv("CLAUDE_AGENT_ID"))
}

func isStratusSelfRepo(event HookEvent) bool {
	for _, dir := range candidateProjectDirs(event) {
		if dir == "" {
			continue
		}
		if dirHasStratusModule(dir) {
			return true
		}
	}
	return false
}

func candidateProjectDirs(event HookEvent) []string {
	var dirs []string
	dirs = append(dirs, event.Cwd, os.Getenv("CLAUDE_PROJECT_DIR"))
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	return dirs
}

func dirHasStratusModule(dir string) bool {
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(data), "module github.com/MartinNevlaha/stratus-v2") {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

type dashboardState struct {
	Workflows []map[string]any `json:"workflows"`
}

type workflowResolutionError struct {
	Code        string
	RequestedID string
	PromptID    string
	Source      string
	SessionID   string
	Candidates  []string
}

func (e *workflowResolutionError) Error() string {
	parts := []string{e.Code}
	if e.RequestedID != "" {
		parts = append(parts, "requested: "+e.RequestedID)
	}
	if e.PromptID != "" {
		parts = append(parts, "prompt: "+e.PromptID)
	}
	if e.Source != "" {
		parts = append(parts, "source: "+e.Source)
	}
	if e.SessionID != "" {
		parts = append(parts, "session_id: "+e.SessionID)
	}
	if len(e.Candidates) > 0 {
		parts = append(parts, "candidates: "+strings.Join(e.Candidates, ", "))
	}
	return strings.Join(parts, "\n")
}

func isWorkflowResolutionError(err error) bool {
	_, ok := err.(*workflowResolutionError)
	return ok
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

// fetchWorkflowForTaskStrict resolves the workflow a Task delegation belongs to.
// Explicit structured workflow_id is authoritative; prompt text is only a compatibility
// fallback. It never falls through from a bad explicit ID to some active workflow.
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

	toolID := workflowIDFromToolInput(toolInput)
	promptIDs := workflowIDsFromPrompt(toolInput, workflows)
	promptID := singleWorkflowID(promptIDs)
	if toolID != "" && promptHasDifferentWorkflowID(promptIDs, toolID) {
		return nil, &workflowResolutionError{
			Code:        workflowIDMismatchErr,
			RequestedID: toolID,
			PromptID:    strings.Join(promptIDs, ", "),
			Source:      resolutionExplicitTool,
			SessionID:   sessionID,
			Candidates:  workflowIDs(workflows),
		}
	}

	if toolID != "" {
		return requireExactWorkflow(toolID, resolutionExplicitTool, sessionID, workflows)
	}
	if promptID != "" {
		return requireExactWorkflow(promptID, resolutionExplicitPrompt, sessionID, workflows)
	}

	if sessionID != "" {
		sessionMatches := workflowsForSession(workflows, sessionID)
		if len(sessionMatches) == 1 {
			return sessionMatches[0], nil
		}
		if len(sessionMatches) > 1 {
			return nil, &workflowResolutionError{
				Code:       ambiguousWorkflowErr,
				Source:     resolutionSession,
				SessionID:  sessionID,
				Candidates: workflowIDs(sessionMatches),
			}
		}
	}

	if len(workflows) == 1 {
		return workflows[0], nil
	}
	if len(workflows) > 1 {
		return nil, &workflowResolutionError{
			Code:       ambiguousWorkflowErr,
			Source:     resolutionGlobal,
			SessionID:  sessionID,
			Candidates: workflowIDs(workflows),
		}
	}

	return nil, &workflowResolutionError{Code: workflowNotResolvedErr, SessionID: sessionID}
}

func requireExactWorkflow(id, source, sessionID string, candidates []map[string]any) (map[string]any, error) {
	if wf := workflowByID(candidates, id); wf != nil {
		return wf, nil
	}
	wf, err := fetchWorkflowByID(id)
	if err != nil {
		return nil, err
	}
	if wf == nil {
		return nil, &workflowResolutionError{
			Code:        workflowNotFoundErr,
			RequestedID: id,
			Source:      source,
			SessionID:   sessionID,
			Candidates:  workflowIDs(candidates),
		}
	}
	return wf, nil
}

func workflowIDFromToolInput(toolInput map[string]any) string {
	if toolInput == nil {
		return ""
	}
	if id, ok := toolInput["workflow_id"].(string); ok {
		return strings.TrimSpace(id)
	}
	return ""
}

func workflowIDsFromPrompt(toolInput map[string]any, workflows []map[string]any) []string {
	taskText := getTaskText(toolInput)
	if taskText == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var ids []string
	for _, wf := range workflows {
		if id, _ := wf["id"].(string); id != "" && strings.Contains(taskText, id) {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	for _, id := range workflowIDRe.FindAllString(taskText, -1) {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func singleWorkflowID(ids []string) string {
	if len(ids) == 1 {
		return ids[0]
	}
	return ""
}

func promptHasDifferentWorkflowID(ids []string, toolID string) bool {
	for _, id := range ids {
		if id != toolID {
			return true
		}
	}
	return false
}

func workflowByID(workflows []map[string]any, id string) map[string]any {
	for _, wf := range workflows {
		if got, _ := wf["id"].(string); got == id {
			return wf
		}
	}
	return nil
}

func workflowsForSession(workflows []map[string]any, sessionID string) []map[string]any {
	var matches []map[string]any
	for _, wf := range workflows {
		if s, _ := wf["session_id"].(string); s == sessionID {
			matches = append(matches, wf)
		}
	}
	return matches
}

func workflowIDs(workflows []map[string]any) []string {
	ids := make([]string, 0, len(workflows))
	for _, wf := range workflows {
		if id, _ := wf["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// getTaskText concatenates the free-text fields of an Agent/Task tool call for workflow-ID matching.
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

// TeammateIdle lives in teammate_idle.go.

// TaskCompleted is called when a CC native task is marked complete.
// Trial version: always allow. Future: verify deliverables.
func TaskCompleted(_ HookEvent) Decision {
	return Decision{Continue: true}
}
