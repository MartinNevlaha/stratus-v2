package orchestration

import (
	"path/filepath"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func newBugCoord(t *testing.T) *Coordinator {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewCoordinator(database)
}

func TestBugWorkflow_FixPhaseCreatesTaskAndReviewLoopAddsIteration(t *testing.T) {
	coord := newBugCoord(t)
	const id = "bug-tasks"
	if _, err := coord.Start(id, WorkflowBug, ComplexitySimple, "Crash on save"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	st, err := coord.Transition(id, PhaseFix)
	if err != nil {
		t.Fatalf("analyze->fix: %v", err)
	}
	if len(st.Tasks) != 1 || st.Tasks[0].Title != "Fix: Crash on save" || st.Tasks[0].Status != "in_progress" {
		t.Fatalf("tasks after fix = %+v", st.Tasks)
	}
	if st.CurrentTask == nil || *st.CurrentTask != 0 || st.TotalTasks != 1 {
		t.Fatalf("current=%v total=%d", st.CurrentTask, st.TotalTasks)
	}

	st, err = coord.Transition(id, PhaseReview)
	if err != nil {
		t.Fatalf("fix->review: %v", err)
	}
	if st.Tasks[0].Status != "done" || st.CurrentTask != nil {
		t.Fatalf("after review task = %+v current=%v", st.Tasks[0], st.CurrentTask)
	}

	st, err = coord.Transition(id, PhaseFix)
	if err != nil {
		t.Fatalf("review->fix: %v", err)
	}
	if len(st.Tasks) != 2 || st.Tasks[1].Title != "Fix iteration 2" || st.Tasks[1].Status != "in_progress" {
		t.Fatalf("tasks after loop = %+v", st.Tasks)
	}
}

func TestBugWorkflow_FixPhaseKeepsExistingTasks(t *testing.T) {
	coord := newBugCoord(t)
	const id = "bug-existing"
	_, _ = coord.Start(id, WorkflowBug, ComplexitySimple, "X")
	_, _ = coord.SetTasks(id, []string{"custom a", "custom b"})
	st, err := coord.Transition(id, PhaseFix)
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if len(st.Tasks) != 2 || st.Tasks[0].Title != "custom a" {
		t.Fatalf("tasks = %+v", st.Tasks)
	}
}

func TestSpecWorkflow_TransitionsDoNotCreateTasks(t *testing.T) {
	coord := newBugCoord(t)
	const id = "spec-none"
	_, _ = coord.Start(id, WorkflowSpec, ComplexitySimple, "S")
	st, err := coord.Transition(id, PhaseImplement)
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if len(st.Tasks) != 0 {
		t.Fatalf("tasks = %+v", st.Tasks)
	}
}
