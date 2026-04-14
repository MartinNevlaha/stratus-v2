package orchestration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// errorAutodocStore always fails on upsert.
type errorAutodocStore struct{}

func (e *errorAutodocStore) UpsertWikiPageByWorkflow(ctx context.Context, workflowID, featureSlug string, page *db.WikiPage) (*db.WikiPage, error) {
	return nil, context.DeadlineExceeded
}

// TestCoordinator_LearnToComplete_WikiStoreError_TransitionSucceeds verifies
// that a learn→complete transition is NOT blocked when the wiki store fails.
func TestCoordinator_LearnToComplete_WikiStoreError_TransitionSucceeds(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	coord := NewCoordinator(database)
	coord.SetWikiStore(&errorAutodocStore{})

	// Create a spec workflow and advance to learn phase.
	state, err := coord.Start("spec-wiki-fail-test", WorkflowSpec, ComplexitySimple, "Wiki Fail Test")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if state.Phase != PhasePlan {
		t.Fatalf("expected plan phase, got %s", state.Phase)
	}

	for _, transition := range []Phase{PhaseImplement, PhaseVerify, PhaseLearn} {
		if _, err := coord.Transition("spec-wiki-fail-test", transition); err != nil {
			t.Fatalf("Transition to %s: %v", transition, err)
		}
	}

	// Now transition learn → complete; wiki store will error but must not block.
	final, err := coord.Transition("spec-wiki-fail-test", PhaseComplete)
	if err != nil {
		t.Fatalf("Transition learn→complete must succeed even when wiki store fails: %v", err)
	}
	if final.Phase != PhaseComplete {
		t.Errorf("Phase: got %s want complete", final.Phase)
	}
}

// TestCoordinator_LearnToComplete_NilWikiStore_TransitionSucceeds verifies that
// the coordinator silently skips autodoc when no wiki store is configured.
func TestCoordinator_LearnToComplete_NilWikiStore_TransitionSucceeds(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	coord := NewCoordinator(database)
	// No SetWikiStore call — wikiStore remains nil.

	if _, err := coord.Start("spec-no-wiki", WorkflowSpec, ComplexitySimple, "No Wiki Store"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for _, phase := range []Phase{PhaseImplement, PhaseVerify, PhaseLearn} {
		if _, err := coord.Transition("spec-no-wiki", phase); err != nil {
			t.Fatalf("Transition to %s: %v", phase, err)
		}
	}

	final, err := coord.Transition("spec-no-wiki", PhaseComplete)
	if err != nil {
		t.Fatalf("Transition learn→complete must succeed with nil wiki store: %v", err)
	}
	if final.Phase != PhaseComplete {
		t.Errorf("Phase: got %s want complete", final.Phase)
	}
}
