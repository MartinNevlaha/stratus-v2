package db

import "strings"

// ReviewState is the current scope's evidence, never a guess based on report prose.
type ReviewState struct {
	Status     string `json:"status"`
	AgentID    string `json:"agent_id,omitempty"`
	MustFix    int    `json:"must_fix"`
	ShouldFix  int    `json:"should_fix"`
	Suggestion int    `json:"suggestion"`
	Reviews    int    `json:"reviews"`
	Loops      int    `json:"loops"`
}

func CodexReview(agents []WorkflowAgent, phase string, task *int, generation ...int64) ReviewState {
	out := ReviewState{Status: "awaiting"}
	var latest *WorkflowAgent
	relevant := func(a WorkflowAgent) bool {
		return (len(generation) == 0 || a.ReviewGeneration == generation[0]) && a.Phase == phase && ((a.TaskIndex == nil && task == nil) || (a.TaskIndex != nil && task != nil && *a.TaskIndex == *task))
	}
	isReview := func(a WorkflowAgent) bool { return a.Verdict != "" || strings.Contains(a.AgentType, "reviewer") }
	for i := range agents {
		a := &agents[i]
		if !relevant(*a) || !isReview(*a) {
			continue
		}
		out.Reviews++
		if a.Verdict == "FAIL" {
			out.Loops++
		}
		if latest == nil || a.StartedAt > latest.StartedAt || (a.StartedAt == latest.StartedAt && a.ID > latest.ID) {
			latest = a
		}
	}
	if latest == nil {
		return out
	}
	out.AgentID = latest.ID
	out.MustFix, out.ShouldFix, out.Suggestion = latest.MustFix, latest.ShouldFix, latest.Suggestion
	if latest.Status == "running" {
		out.Status = "running"
		return out
	}
	if latest.Verdict != "PASS" && latest.Verdict != "FAIL" {
		out.Status = "unverified"
		return out
	}
	out.Status = latest.Verdict
	end := latest.EndedAt
	if end == "" {
		end = latest.LastActivity
	}
	if end == "" {
		end = latest.StartedAt
	}
	for _, a := range agents {
		if relevant(a) && a.ID != latest.ID && (a.StartedAt > end || a.LastActivity > end) {
			out.Status = "outdated"
			break
		}
	}
	return out
}
