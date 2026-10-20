package api

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

// staleAgentAfter mirrors the Guardian threshold for an idle running agent.
const staleAgentAfter = 10 * time.Minute

// handleWorkflowDenials lists guard denials recorded for the workflow's session, so the
// user can see why a delegated agent was blocked (the reason otherwise reaches only the agent).
func (s *Server) handleWorkflowDenials(w http.ResponseWriter, r *http.Request) {
	state, err := s.coordinator.Get(r.PathValue("id"))
	if err != nil {
		jsonErr(w, http.StatusNotFound, "workflow not found")
		return
	}
	denials := []map[string]any{}
	if state.SessionID != "" {
		events, err := s.db.SearchEvents(db.SearchEventsInput{Type: "hook_denial", Limit: 200})
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "search denials failed")
			return
		}
		for _, e := range events {
			if sess, _ := e.Refs["session_id"].(string); sess != state.SessionID {
				continue
			}
			denials = append(denials, map[string]any{
				"ts":         e.Ts,
				"hook":       e.Refs["hook"],
				"tool_name":  e.Refs["tool_name"],
				"agent_type": e.Refs["agent_type"],
				"reason":     e.Refs["reason"],
			})
		}
	}
	json200(w, map[string]any{"denials": denials})
}

type attentionItem struct {
	WorkflowID    string   `json:"workflow_id"`
	Title         string   `json:"title"`
	Phase         string   `json:"phase"`
	RunningAgents int      `json:"running_agents"`
	StaleAgents   int      `json:"stale_agents"`
	ReviewFailed  bool     `json:"review_failed"`
	Conflicts     []string `json:"conflicts"`
}

// handleAttention summarises, across active workflows, what needs a human: idle agents,
// a failing latest review, files touched by several agents. Quiet workflows are omitted.
func (s *Server) handleAttention(w http.ResponseWriter, r *http.Request) {
	active, err := s.coordinator.ListActive()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list workflows failed")
		return
	}
	items := []attentionItem{}
	for _, wf := range active {
		agents, err := s.db.ListWorkflowAgents(wf.ID)
		if err != nil {
			continue
		}
		it := attentionItem{WorkflowID: wf.ID, Title: wf.Title, Phase: string(wf.Phase), Conflicts: fileConflicts(agents)}
		var lastVerdict string
		for _, a := range agents {
			if a.Status == "running" {
				it.RunningAgents++
				if t, err := time.Parse("2006-01-02T15:04:05.000Z", a.LastActivity); err == nil && time.Since(t) > staleAgentAfter {
					it.StaleAgents++
				}
			}
			if a.Verdict != "" {
				lastVerdict = a.Verdict
			}
		}
		it.ReviewFailed = lastVerdict == "FAIL"
		if wf.Runtime == "codex" {
			it.ReviewFailed = db.CodexReview(agents, string(wf.Phase), wf.CurrentTask, wf.ReviewGeneration).Status == "FAIL"
		}
		if it.StaleAgents > 0 || it.ReviewFailed || len(it.Conflicts) > 0 {
			items = append(items, it)
		}
	}
	json200(w, map[string]any{"items": items})
}

// handleRunSummary renders a markdown report of one run from its tasks, agents and reviews.
func (s *Server) handleRunSummary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, err := s.coordinator.Get(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "workflow not found")
		return
	}
	if state.RunSummary != "" {
		json200(w, map[string]any{"markdown": state.RunSummary, "stored": true})
		return
	}
	agents, err := s.db.ListWorkflowAgents(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list agents failed")
		return
	}
	json200(w, map[string]any{"markdown": renderRunSummary(state, agents), "stored": false})
}

// storeRunSummary writes the run report into the workflow when it completes, so it is
// available later without recomputing from agents that may have been pruned.
func (s *Server) storeRunSummary(state *orchestration.WorkflowState) {
	agents, err := s.db.ListWorkflowAgents(state.ID)
	if err != nil {
		return
	}
	if _, err := s.coordinator.SetRunSummary(state.ID, renderRunSummary(state, agents)); err != nil {
		log.Printf("warning: store run summary for %s: %v", state.ID, err)
	}
}

func renderRunSummary(state *orchestration.WorkflowState, agents []db.WorkflowAgent) string {
	var b strings.Builder
	title := state.Title
	if title == "" {
		title = state.ID
	}
	fmt.Fprintf(&b, "## %s\n\n", title)
	fmt.Fprintf(&b, "- Workflow: `%s` (%s%s), phase `%s`\n", state.ID, state.Type, complexitySuffix(state), state.Phase)
	if d := runDuration(state); d != "" {
		fmt.Fprintf(&b, "- Duration: %s\n", d)
	}
	tin, tout := 0, 0
	for _, a := range agents {
		tin += a.TokensIn
		tout += a.TokensOut
	}
	if tin+tout > 0 {
		fmt.Fprintf(&b, "- Tokens: %d in / %d out\n", tin, tout)
	}

	if len(state.Tasks) > 0 {
		b.WriteString("\n### Tasks\n\n")
		for _, t := range state.Tasks {
			mark := " "
			if t.Status == "done" {
				mark = "x"
			}
			dur := ""
			if t.StartedAt != "" && t.CompletedAt != "" {
				if d := spanBetween(t.StartedAt, t.CompletedAt, time.RFC3339); d != "" {
					dur = " (" + d + ")"
				}
			}
			fmt.Fprintf(&b, "- [%s] %s%s\n", mark, t.Title, dur)
		}
	}

	if len(agents) > 0 {
		b.WriteString("\n### Agents\n\n")
		files := map[string]bool{}
		fails := 0
		for _, a := range agents {
			line := fmt.Sprintf("- %s: %s, %d tool calls", strings.TrimPrefix(a.AgentType, "delivery-"), a.Status, a.ToolCalls)
			if a.Verdict != "" {
				line += fmt.Sprintf(", review **%s** (%d must_fix, %d should_fix)", a.Verdict, a.MustFix, a.ShouldFix)
				if a.Verdict == "FAIL" {
					fails++
				}
			}
			b.WriteString(line + "\n")
			for _, f := range a.Files {
				files[f] = true
			}
		}
		if fails > 0 {
			fmt.Fprintf(&b, "\nReview loops: %d\n", fails)
		}
		if len(files) > 0 {
			names := make([]string, 0, len(files))
			for f := range files {
				names = append(names, f)
			}
			sort.Strings(names)
			b.WriteString("\n### Files touched\n\n")
			for _, f := range names {
				fmt.Fprintf(&b, "- `%s`\n", f)
			}
		}
	}
	return b.String()
}

func complexitySuffix(state *orchestration.WorkflowState) string {
	if state.Complexity == orchestration.ComplexityComplex {
		return ", complex"
	}
	return ""
}

func runDuration(state *orchestration.WorkflowState) string {
	return spanBetween(state.CreatedAt, state.UpdatedAt, time.RFC3339Nano)
}

func spanBetween(from, to, layout string) string {
	a, err1 := time.Parse(layout, from)
	b, err2 := time.Parse(layout, to)
	if err1 != nil || err2 != nil || b.Before(a) {
		return ""
	}
	d := b.Sub(a).Round(time.Second)
	if d < time.Minute {
		return d.String()
	}
	return d.Round(time.Minute).String()
}

type runMetrics struct {
	ID          string `json:"id"`
	DurationSec int    `json:"duration_sec"`
	Tasks       int    `json:"tasks"`
	Agents      int    `json:"agents"`
	ReviewLoops int    `json:"review_loops"`
	Tokens      int    `json:"tokens"`
}

func (s *Server) metricsFor(state *orchestration.WorkflowState) runMetrics {
	m := runMetrics{ID: state.ID, Tasks: len(state.Tasks)}
	a, err1 := time.Parse(time.RFC3339Nano, state.CreatedAt)
	b, err2 := time.Parse(time.RFC3339Nano, state.UpdatedAt)
	if err1 == nil && err2 == nil && !b.Before(a) {
		m.DurationSec = int(b.Sub(a).Seconds())
	}
	agents, _ := s.db.ListWorkflowAgents(state.ID)
	m.Agents = len(agents)
	for _, ag := range agents {
		m.Tokens += ag.TokensIn + ag.TokensOut
		if ag.Verdict == "FAIL" {
			m.ReviewLoops++
		}
	}
	return m
}

// handleRunComparison compares a run with the most recent earlier finished run of the same
// type and title, so a re-run shows whether it got faster or needed fewer review loops.
func (s *Server) handleRunComparison(w http.ResponseWriter, r *http.Request) {
	state, err := s.coordinator.Get(r.PathValue("id"))
	if err != nil {
		jsonErr(w, http.StatusNotFound, "workflow not found")
		return
	}
	all, err := s.coordinator.ListAll()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list workflows failed")
		return
	}
	var prev *orchestration.WorkflowState
	for _, w := range all {
		if w.ID == state.ID || w.Type != state.Type || w.Phase != orchestration.PhaseComplete || w.Aborted {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(w.Title), strings.TrimSpace(state.Title)) || state.Title == "" {
			continue
		}
		if w.CreatedAt >= state.CreatedAt {
			continue
		}
		if prev == nil || w.CreatedAt > prev.CreatedAt {
			prev = w
		}
	}
	resp := map[string]any{"current": s.metricsFor(state), "previous": nil}
	if prev != nil {
		resp["previous"] = s.metricsFor(prev)
	}
	json200(w, resp)
}
