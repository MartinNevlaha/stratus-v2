package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

const (
	briefMaxRunes      = 3000
	briefMaxRole       = 64
	briefMaxDecisions  = 5
	briefDecisionPool  = 50 // newest decisions considered before ranking
	briefMaxPointers   = 6
	briefMaxGovernance = 3
	briefSnippetRunes  = 200
	briefLineRunes     = 200
	briefMaxFiles      = 3
	briefFileRunes     = 80
	briefAgentRunes    = 64

	// workflowAgentTimeLayout is how workflow_agents stores its timestamps.
	workflowAgentTimeLayout = "2006-01-02T15:04:05.000Z"
)

// buildBrief composes a compact, capped context brief for a delegated agent:
// the task line, the latest review verdict, recent decisions of this workflow
// and pointers (no excerpts) from its latest context prefetch. It returns ""
// when there is no review, no decisions and no pointers, so callers never
// inject boilerplate.
//
// The review line comes from workflow_agents (hook-observed reviewers and the
// projected team reviews alike); only counts are stored, so only counts are
// shown. User-denied team approvals arrive as rejected decisions (see
// recordApprovalDenial), so neither depends on agents saving anything.
//
// Decision convention (see the spec/swarm skills): save_memory with type
// "decision", tags ["decision", "approved"|"rejected"] and refs
// {status, workflow_id, files}. Rejected decisions are shown first, marked
// "do not revisit". Events without a status render as plain decisions.
// role only biases ordering: reviewer roles see governance rules before code
// pointers.
//
// A non-zero since is the start of the recipient's previous turn (lastTurnStart):
// only decisions created after it are listed, a review is shown only if it
// finished after it, and context pointers and governance rules are omitted
// (the recipient already saw them), so the result is "" when nothing is new. A zero since yields the full brief.
func (s *Server) buildBrief(workflowID, role string, since time.Time) string {
	state, err := s.coordinator.Get(workflowID)
	if err != nil {
		return ""
	}
	phase := string(state.Phase)

	review := s.briefReviewLine(state, since)
	decisions := s.briefDecisionLines(workflowID, since)
	var pointers, rules []string
	if since.IsZero() {
		pointers, rules = s.briefPointerLines(workflowID, phase)
	}
	if review == "" && len(decisions) == 0 && len(pointers) == 0 && len(rules) == 0 {
		return ""
	}

	task := fmt.Sprintf("Task: %s [%s, phase %s]", clipRunes(oneLine(state.Title), briefLineRunes), state.Type, phase)
	if state.CurrentTask != nil && *state.CurrentTask >= 0 && *state.CurrentTask < len(state.Tasks) {
		task += "; current task: " + clipRunes(oneLine(state.Tasks[*state.CurrentTask].Title), briefLineRunes)
	}

	sections := []string{task}
	if review != "" {
		sections = append(sections, review)
	}
	if len(decisions) > 0 {
		sections = append(sections, "Decisions:\n"+strings.Join(decisions, "\n"))
	}
	pointerSection, ruleSection := "", ""
	if len(pointers) > 0 {
		pointerSection = "Context pointers:\n" + strings.Join(pointers, "\n")
	}
	if len(rules) > 0 {
		ruleSection = "Governance rules:\n" + strings.Join(rules, "\n")
	}
	if teams.CanonicalRole(role) == "review" {
		sections = append(sections, ruleSection, pointerSection)
	} else {
		sections = append(sections, pointerSection, ruleSection)
	}

	var kept []string
	for _, sec := range sections {
		if sec != "" {
			kept = append(kept, sec)
		}
	}
	return clipRunes(strings.Join(kept, "\n\n"), briefMaxRunes)
}

// briefReviewLine renders the latest finished review of the workflow as one
// line, or "" when there is none (or, with a non-zero since, none that finished
// after it). Codex workflows use the same scoped projection as the attention
// items (db.CodexReview: current phase, task and generation). Other runtimes
// take the newest PASS/FAIL reviewer run across phases, so a FAIL stays
// visible while the fix is made, and say when later work or a running
// re-review makes it stale.
func (s *Server) briefReviewLine(state *orchestration.WorkflowState, since time.Time) string {
	agents, err := s.db.ListWorkflowAgents(state.ID)
	if err != nil {
		return ""
	}
	var latest *db.WorkflowAgent
	mustFix, shouldFix := 0, 0
	suffix := ""
	if state.Runtime == "codex" {
		rs := db.CodexReview(agents, string(state.Phase), state.CurrentTask, state.ReviewGeneration)
		if rs.Status != "PASS" && rs.Status != "FAIL" && rs.Status != "outdated" {
			return ""
		}
		for i := range agents {
			if agents[i].ID == rs.AgentID {
				latest = &agents[i]
			}
		}
		mustFix, shouldFix = rs.MustFix, rs.ShouldFix
		if rs.Status == "outdated" {
			suffix = " - outdated, work continued after it"
		}
	} else {
		for i := range agents {
			if v := agents[i].Verdict; v == "PASS" || v == "FAIL" {
				latest = &agents[i]
			}
		}
		if latest != nil {
			mustFix, shouldFix = latest.MustFix, latest.ShouldFix
			suffix = reviewFollowUp(agents, latest)
		}
	}
	if latest == nil || (latest.Verdict != "PASS" && latest.Verdict != "FAIL") {
		return ""
	}
	if !since.IsZero() {
		if t, err := time.Parse(workflowAgentTimeLayout, reviewEnd(*latest)); err != nil || !t.After(since) {
			return ""
		}
	}
	who := strings.TrimPrefix(clipRunes(oneLine(latest.AgentType), briefAgentRunes), "delivery-")
	var where []string
	if who != "" {
		where = append(where, who)
	}
	if phase := clipRunes(oneLine(latest.Phase), briefAgentRunes); phase != "" {
		where = append(where, phase)
	}
	line := "Latest review"
	if len(where) > 0 {
		line += " (" + strings.Join(where, ", ") + ")"
	}
	return line + fmt.Sprintf(": %s (%d must_fix, %d should_fix)", latest.Verdict, mustFix, shouldFix) + suffix
}

// reviewEnd is when a review run finished, as stored in workflow_agents.
func reviewEnd(a db.WorkflowAgent) string {
	for _, ts := range []string{a.EndedAt, a.LastActivity, a.StartedAt} {
		if ts != "" {
			return ts
		}
	}
	return ""
}

// isReviewAgent mirrors db.CodexReview's notion of a review run.
func isReviewAgent(a db.WorkflowAgent) bool {
	return a.Verdict != "" || strings.Contains(a.AgentType, "reviewer")
}

// reviewFollowUp says what happened after a review without a scope to compare
// against: any non-review agent started after it finished makes it outdated,
// and a reviewer that is running and newer means a re-review is under way.
func reviewFollowUp(agents []db.WorkflowAgent, review *db.WorkflowAgent) string {
	end := reviewEnd(*review)
	outdated, rerun := false, false
	for _, a := range agents {
		if a.ID == review.ID || a.StartedAt <= end {
			continue
		}
		switch {
		case !isReviewAgent(a):
			outdated = true
		case a.Status == "running":
			rerun = true
		}
	}
	suffix := ""
	if outdated {
		suffix += " - outdated, work continued after it"
	}
	if rerun {
		suffix += " - re-review running"
	}
	return suffix
}

func (s *Server) briefDecisionLines(workflowID string, since time.Time) []string {
	events, err := s.db.RecentWorkflowDecisions(workflowID, briefDecisionPool, since)
	if err != nil {
		return nil
	}
	var rejected, other []string
	for _, e := range events {
		status := briefStatus(e.Refs["status"])
		for _, tag := range e.Tags {
			if status == "" {
				status = briefStatus(tag)
			}
		}
		body := clipRunes(oneLine(e.Title), briefLineRunes)
		if snippet := clipRunes(oneLine(e.Text), briefSnippetRunes); snippet != "" {
			if body != "" {
				body += " - "
			}
			body += snippet
		}
		body += briefFiles(e.Refs["files"])
		switch status {
		case "rejected":
			rejected = append(rejected, "- Rejected - do not revisit: "+body)
		case "approved":
			other = append(other, "- Approved: "+body)
		default:
			other = append(other, "- Decision: "+body)
		}
	}
	lines := append(rejected, other...)
	if len(lines) > briefMaxDecisions {
		lines = lines[:briefMaxDecisions]
	}
	return lines
}

// briefPointerLines reads the latest context_prefetch for the workflow's
// current phase, falling back to its most recent one.
func (s *Server) briefPointerLines(workflowID, phase string) (pointers, rules []string) {
	ev, err := s.db.LatestWorkflowEvent("context_prefetch", workflowID, phase)
	if err == nil && ev == nil {
		ev, err = s.db.LatestWorkflowEvent("context_prefetch", workflowID, "")
	}
	if err != nil || ev == nil {
		return nil, nil
	}
	raw, err := json.Marshal(ev.Refs["results"])
	if err != nil {
		return nil, nil
	}
	var results []retrieveResult
	if json.Unmarshal(raw, &results) != nil {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, r := range results {
		name := r.FilePath
		if name == "" {
			name = r.Title
		}
		if name == "" || seen[r.Source+"|"+name] {
			continue
		}
		seen[r.Source+"|"+name] = true
		if r.Source == "governance" {
			if r.Title != "" {
				name = r.Title
			}
			if len(rules) < briefMaxGovernance {
				rules = append(rules, "- "+clipRunes(oneLine(name), briefLineRunes))
			}
		} else if len(pointers) < briefMaxPointers {
			pointers = append(pointers, "- "+clipRunes(oneLine(name), briefLineRunes)+" ("+r.Source+")")
		}
	}
	return pointers, rules
}

// briefStatus normalizes a decision status; only approved/rejected are known.
func briefStatus(v any) string {
	str, _ := v.(string)
	str = strings.ToLower(strings.TrimSpace(str))
	if str == "approved" || str == "rejected" {
		return str
	}
	return ""
}

// briefFiles renders up to briefMaxFiles paths from a decision's refs.files
// as " [files: a, b]". Anything that is not an array of strings is ignored.
func briefFiles(v any) string {
	list, _ := v.([]any)
	var files []string
	for _, item := range list {
		f, _ := item.(string)
		if f = clipRunes(oneLine(f), briefFileRunes); f != "" {
			files = append(files, f)
		}
		if len(files) == briefMaxFiles {
			break
		}
	}
	if len(files) == 0 {
		return ""
	}
	return " [files: " + strings.Join(files, ", ") + "]"
}

// handleBrief serves GET /api/workflows/{id}/brief?role=.
func (s *Server) handleBrief(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	role := strings.TrimSpace(r.URL.Query().Get("role"))
	if utf8.RuneCountInString(role) > briefMaxRole {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("role must be at most %d characters", briefMaxRole))
		return
	}
	if _, err := s.coordinator.Get(id); err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	json200(w, map[string]any{"workflow_id": id, "brief": s.buildBrief(id, role, time.Time{})})
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// oneLine turns arbitrary stored text into a single safe line: ANSI escapes,
// control and zero-width/format runes are dropped, whitespace runs collapse
// to one space.
func oneLine(s string) string {
	s = ansiEscape.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// clipRunes truncates s to at most n runes, marking the cut with an ellipsis.
// It never splits a multi-byte rune.
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
