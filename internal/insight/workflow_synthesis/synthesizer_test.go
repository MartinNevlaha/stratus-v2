package workflow_synthesis

import (
	"context"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func openWorkflowSynthTestDB(t *testing.T) *db.DB {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})

	return database
}

func TestSelectWorkflowUsesPromotedCandidate(t *testing.T) {
	database := openWorkflowSynthTestDB(t)
	store := NewDBStore(database)
	synthesizer := NewSynthesizer(store, DefaultConfig())

	candidate := &db.WorkflowCandidate{
		WorkflowName: "promoted-workflow",
		TaskType:     "bug_fix",
		RepoType:     "go",
		BaseWorkflow: "bug",
		Steps: []db.WorkflowStep{
			{Phase: "analysis", NextPhases: []string{"implement"}},
		},
		PhaseTransitions: map[string]string{"analysis": "implement"},
		Confidence:       0.9,
		Status:           "promoted",
	}
	if err := store.SaveCandidate(context.Background(), candidate); err != nil {
		t.Fatalf("save candidate: %v", err)
	}

	selected, useCandidate, err := synthesizer.SelectWorkflow(context.Background(), "bug_fix", "go")
	if err != nil {
		t.Fatalf("select workflow: %v", err)
	}
	if selected == nil {
		t.Fatal("expected promoted candidate to be returned")
	}
	if !useCandidate {
		t.Fatal("expected useCandidate=true for promoted candidate")
	}
}
