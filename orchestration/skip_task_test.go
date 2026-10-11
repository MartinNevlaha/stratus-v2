package orchestration

import (
	"strings"
	"testing"
)

func TestSkipTask_MarksSkippedAndDoesNotBlockVerify(t *testing.T) {
	coord := newBugCoord(t)
	const id = "spec-skip"
	_, _ = coord.Start(id, WorkflowSpec, ComplexitySimple, "Skip")
	_, _ = coord.SetTasks(id, []string{"a", "b"})
	_, _ = coord.StartTask(id, 0)
	_, _ = coord.CompleteTask(id, 0)

	st, err := coord.SkipTask(id, 1)
	if err != nil {
		t.Fatalf("SkipTask: %v", err)
	}
	if st.Tasks[1].Status != "skipped" || st.Tasks[1].CompletedAt == "" {
		t.Fatalf("task = %+v", st.Tasks[1])
	}
	if warns := validatePhaseReadiness(st, PhaseVerify); containsWarning(warns, "task 2") {
		t.Errorf("skipped task still reported incomplete: %v", warns)
	}
}

func TestSkipTask_RejectsFinishedAndOutOfRange(t *testing.T) {
	coord := newBugCoord(t)
	const id = "spec-skip2"
	_, _ = coord.Start(id, WorkflowSpec, ComplexitySimple, "Skip")
	_, _ = coord.SetTasks(id, []string{"a"})
	_, _ = coord.StartTask(id, 0)
	_, _ = coord.CompleteTask(id, 0)
	if _, err := coord.SkipTask(id, 0); err == nil {
		t.Error("skipping a done task must fail")
	}
	if _, err := coord.SkipTask(id, 5); err == nil {
		t.Error("out of range must fail")
	}
}

func containsWarning(warns []string, sub string) bool {
	for _, w := range warns {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func TestSetRunSummary_Persists(t *testing.T) {
	coord := newBugCoord(t)
	const id = "spec-sum"
	_, _ = coord.Start(id, WorkflowSpec, ComplexitySimple, "Sum")
	if _, err := coord.SetRunSummary(id, "## done"); err != nil {
		t.Fatalf("SetRunSummary: %v", err)
	}
	st, _ := coord.Get(id)
	if st.RunSummary != "## done" {
		t.Errorf("RunSummary = %q", st.RunSummary)
	}
}
