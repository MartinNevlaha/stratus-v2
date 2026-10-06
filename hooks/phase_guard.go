package hooks

import (
	"bytes"
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
	if !isWriteTool(event.ToolName) || !isDeliveryAgent(event) {
		return Decision{Continue: true}
	}
	if reason := architectWriteDenial(event); reason != "" {
		return blockAudited(event, "phase_guard", reason, nil)
	}

	// A session can own several workflows at once, so "the" active workflow may not
	// exist. Block only when EVERY candidate is in a write-restricted phase -- then the
	// denial is right whichever one the agent belongs to. If any candidate is in another
	// phase we cannot tell them apart, and a wrong denial costs more than a missed guard.
	blocking := blockingWorkflow(phaseGuardCandidates(event.SessionID))
	if blocking == nil {
		return Decision{Continue: true}
	}

	phase, _ := blocking["phase"].(string)
	wfID, _ := blocking["id"].(string)
	origin := " (workflow " + wfID + ", phase " + phase + ")"

	// Bash is judged by its COMMAND, not by its name. A reviewer that cannot run
	// `git diff` or the test suite cannot verify what it reviews -- it can only assert
	// that the code reads correctly, which is the failure mode this whole phase exists
	// to prevent. isWriteBashCommand is the same split BashWriteGuard already applies.
	var decision Decision
	if event.ToolName == "Bash" {
		command, _ := event.ToolInput["command"].(string)
		// An absent command is unknown, not read-only -- stay fail-closed.
		if command != "" && !isWriteBashCommand(command) {
			return Decision{Continue: true}
		}
		decision = Decision{
			Continue: false,
			Reason: "This bash command writes, and writes are not allowed during " + phase +
				" phase" + origin + ": " + command + ". Read-only commands (git diff/log/status, tests, grep) are allowed.",
		}
	} else {
		decision = Decision{
			Continue: false,
			Reason:   "Write tools are not allowed during " + phase + " phase" + origin + ". Use Read/Grep/Glob or read-only Bash.",
		}
	}

	auditDenial(event, "phase_guard", decision.Reason, map[string]any{
		"workflow_id": wfID,
		"phase":       phase,
	})
	return decision
}

// docsOnlyAgents design rather than build: they write design docs and ADRs, never source.
var docsOnlyAgents = map[string]bool{
	"delivery-system-architect":    true,
	"delivery-strategic-architect": true,
}

// architectWriteDenial returns why an architect may not write, or "" when it may: Write
// and Edit to a Markdown file under the docs/ directory of the project or of a swarm
// worktree (.stratus/worktrees/<name>/docs/). Bash may read but not write.
func architectWriteDenial(event HookEvent) string {
	if !docsOnlyAgents[event.AgentType] {
		return ""
	}
	if event.ToolName == "Bash" {
		command, _ := event.ToolInput["command"].(string)
		if command != "" && !isWriteBashCommand(command) {
			return ""
		}
		return fmt.Sprintf("%s may not write through Bash; write design docs and ADRs with Write or Edit "+
			"under docs/ (for example docs/plans/ or docs/decisions/).", event.AgentType)
	}
	path, _ := event.ToolInput["file_path"].(string)
	if path == "" {
		path, _ = event.ToolInput["notebook_path"].(string)
	}
	root := os.Getenv("CLAUDE_PROJECT_DIR")
	if root == "" {
		root = event.Cwd
	}
	if isProjectDoc(path, root) {
		return ""
	}
	return fmt.Sprintf("%s writes only Markdown design docs and ADRs under a docs/ directory "+
		"(for example docs/plans/ or docs/decisions/); %q is not one. Hand source changes to an engineering agent.",
		event.AgentType, path)
}

func isProjectDoc(path, root string) bool {
	if path == "" || root == "" || !strings.EqualFold(filepath.Ext(path), ".md") {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	// docs/ at the top of the project or of a swarm worktree, never one nested elsewhere
	// (.claude/rules/docs/ would be read as instructions, node_modules/*/docs/ is not ours).
	dirs := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")
	if len(dirs) >= 3 && dirs[0] == ".stratus" && dirs[1] == "worktrees" {
		dirs = dirs[3:]
	}
	return len(dirs) > 0 && strings.EqualFold(dirs[0], "docs")
}

// isBlockingPhase reports whether a workflow is in a phase where delivery agents must
// not write: a spec under verification or a bug under review cannot be edited by the
// same agent that is judging it.
func isBlockingPhase(wf map[string]any) bool {
	phase, _ := wf["phase"].(string)
	wtype, _ := wf["type"].(string)
	return (phase == "verify" && wtype == "spec") || (phase == "review" && wtype == "bug")
}

// blockingWorkflow returns a workflow to deny against only when every candidate is in a
// blocking phase; otherwise nil (ambiguous -- do not guess).
func blockingWorkflow(candidates []map[string]any) map[string]any {
	if len(candidates) == 0 {
		return nil
	}
	for _, wf := range candidates {
		if !isBlockingPhase(wf) {
			return nil
		}
	}
	return candidates[0]
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
			return blockAudited(event, "workflow_existence_guard", err.Error(), nil)
		}
		if isStratusSelfRepo(event) {
			return Decision{Continue: true}
		}
		return blockAudited(event, "workflow_existence_guard",
			"Cannot verify workflow: "+err.Error()+". Ensure Stratus server is running (stratus serve).", nil)
	}
	if wf == nil {
		return blockAudited(event, "workflow_existence_guard", unresolvedWorkflowReason, nil)
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
			return blockAudited(event, "delegation_guard", err.Error(), nil)
		}
		if isStratusSelfRepo(event) {
			return Decision{Continue: true}
		}
		return blockAudited(event, "delegation_guard",
			"Cannot verify workflow: "+err.Error()+". Ensure Stratus server is running (stratus serve).", nil)
	}
	if wf == nil {
		return blockAudited(event, "delegation_guard", unresolvedWorkflowReason, nil)
	}

	recordDelegation(wf, subagentType)
	return Decision{Continue: true}
}

// recordDelegation notes the delivery agent on the workflow's current phase, so the record
// no longer depends on the coordinator remembering delegate_agent. Best-effort: the API
// ignores a repeat, and a failure here never blocks the delegation. No session_id: the guard
// already resolved the workflow, which may be one this session took over from another.
func recordDelegation(wf map[string]any, agentType string) {
	id, _ := wf["id"].(string)
	if id == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"agent_id": agentType, "workflow_id": id})
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Post("http://localhost:"+getPort()+"/api/workflows/"+url.PathEscape(id)+"/delegate", "application/json", bytes.NewReader(body))
	if err == nil {
		resp.Body.Close()
	}
}

// blockAudited returns a denial and records it, so a coordinator can find out later which
// tool call was refused and why -- the Reason string alone only ever reaches the agent.
func blockAudited(event HookEvent, hookName, reason string, refs map[string]any) Decision {
	auditDenial(event, hookName, reason, refs)
	return Decision{Continue: false, Reason: reason}
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
		return blockAudited(event, "bash_write_guard",
			"Cannot verify workflow: "+err.Error()+". Ensure Stratus server is running (stratus serve).", nil)
	}
	if wf == nil {
		return blockAudited(event, "bash_write_guard",
			noActiveWorkflowReason+" Delivery agents must have an active workflow to execute write commands.", nil)
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

// redirectWritePatterns are shell syntax fragments that always write, matched literally.
var redirectWritePatterns = []string{
	" > ", " >> ", ">|",
	" 1>", " 2>", " &>",
	"sed -i", "awk -i",
}

// writeCommandRe matches file-modifying commands on word boundaries. The git alternative
// skips leading global flags and their values, so it matches `git -C /repo commit` but not
// `git worktree add` -- a scratch tree is not a repository write, and a reviewer needs one
// to compare against a baseline.
var writeCommandRe = regexp.MustCompile(
	`\b(?:tee|install|rmdir|rm|mkdir|mv|cp|dd|touch|chmod|chown|truncate)\b` +
		`|\bgit\s+(?:-\S+\s+(?:\S+\s+)?)*(?:add|commit|push|merge|rebase|cherry-pick|reset)\b`)

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

	// Check write patterns FIRST - explicit redirects, file modifications, git write ops.
	for _, p := range redirectWritePatterns {
		if strings.Contains(lowerCmd, p) {
			return true
		}
	}
	// Command names are matched on word boundaries, not as substrings: " dd " lived
	// inside "git worktree add --detach" and denied a reviewer the baseline tree it needs
	// to tell a regression from a pre-existing failure (measured 2026-09-02).
	if writeCommandRe.MatchString(lowerCmd) {
		return true
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
		"NotebookEdit": true,
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

// isDeliveryAgent checks whether the hook fired inside a delivery agent.
func isDeliveryAgent(event HookEvent) bool {
	return isDeliverySubagent(event.AgentType)
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

// phaseGuardCandidates returns every workflow the calling session might be acting for.
//
// Matching priority:
//  1. All workflows whose session_id matches — a session can have several registered at
//     once, so this is a SET, not a single flow. Returning whichever came first bound the
//     agent to a foreign phase and denied legitimate writes (measured 2026-09-02).
//  2. A single active workflow — last-resort fallback for resumed sessions whose
//     session_id changed, or when CLAUDE_SESSION_ID was unavailable; safe only because
//     there is exactly one flow to bind to.
//
// When several workflows run in parallel and none is owned by this session, we do NOT
// guess: nil keeps PhaseGuard best-effort (it simply does not block) rather than gating
// writes against the wrong phase.
func phaseGuardCandidates(sessionID string) []map[string]any {
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

	if sessionID != "" {
		if matches := workflowsForSession(workflows, sessionID); len(matches) > 0 {
			return matches
		}
	}

	if len(workflows) == 1 {
		return workflows
	}
	return nil
}

// fetchActiveWorkflow returns the single workflow this session is unambiguously acting
// for, or nil when the session owns several (or none).
func fetchActiveWorkflow(sessionID string) map[string]any {
	candidates := phaseGuardCandidates(sessionID)
	if len(candidates) == 1 {
		return candidates[0]
	}
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
