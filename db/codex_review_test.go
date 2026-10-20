package db

import "testing"

func TestCodexReview_CurrentWork(t *testing.T) {
	n := func(v int) *int { return &v }
	pass := WorkflowAgent{ID: "old", Phase: "implement", TaskIndex: n(0), Status: "done", AgentType: "delivery-code-reviewer", Verdict: "PASS", StartedAt: "2026-10-20T10:00:00Z", EndedAt: "2026-10-20T10:01:00Z"}
	for _, tc := range []struct {
		name   string
		task   *int
		agents []WorkflowAgent
		want   string
	}{
		{"new task", n(1), []WorkflowAgent{pass}, "awaiting"},
		{"new reviewer without verdict", n(0), []WorkflowAgent{pass, {ID: "new", Phase: "implement", TaskIndex: n(0), Status: "done", AgentType: "delivery-code-reviewer", StartedAt: "2026-10-20T10:02:00Z"}}, "unverified"},
		{"running reviewer", n(0), []WorkflowAgent{{ID: "new", Phase: "implement", TaskIndex: n(0), Status: "running", AgentType: "delivery-code-reviewer", StartedAt: "2026-10-20T10:02:00Z"}, pass}, "running"},
		{"subsequent work", n(0), []WorkflowAgent{{ID: "fix", Phase: "implement", TaskIndex: n(0), Status: "running", AgentType: "default", StartedAt: "2026-10-20T10:02:00Z"}, pass}, "outdated"},
		{"current pass", n(0), []WorkflowAgent{pass}, "PASS"},
		{"current fail unsorted", n(0), []WorkflowAgent{{ID: "new", Phase: "implement", TaskIndex: n(0), Status: "done", Verdict: "FAIL", MustFix: 1, StartedAt: "2026-10-20T10:02:00Z"}, pass}, "FAIL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CodexReview(tc.agents, "implement", tc.task)
			if got.Status != tc.want {
				t.Fatalf("got %+v want %s", got, tc.want)
			}
		})
	}
}

func TestCodexReview_WorkContinuesAfterPASS(t *testing.T) {
	agents := []WorkflowAgent{
		{ID: "worker", Phase: "plan", Status: "running", StartedAt: "2026-10-20T10:00:00.000Z", LastActivity: "2026-10-20T10:05:00.000Z"},
		{ID: "review", Phase: "plan", Status: "done", AgentType: "delivery-code-reviewer", Verdict: "PASS", StartedAt: "2026-10-20T10:01:00.000Z", EndedAt: "2026-10-20T10:02:00.000Z"},
	}
	if got := CodexReview(agents, "plan", nil); got.Status != "outdated" {
		t.Fatal(got)
	}
}
