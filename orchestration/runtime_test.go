package orchestration

import (
	"errors"
	"github.com/MartinNevlaha/stratus-v2/db"
	"path/filepath"
	"testing"
)

func ownedCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	d, e := db.Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	return NewCoordinator(d)
}
func TestStartOwned_PersistsOwnershipAtomically(t *testing.T) {
	c := ownedCoordinator(t)
	w, e := c.StartOwned("spec-codex", WorkflowSpec, ComplexitySimple, "Codex", WorkflowOwner{"codex", "s1"})
	if e != nil {
		t.Fatal(e)
	}
	got, e := c.Get(w.ID)
	if e != nil || got.SessionID != "codex:s1" || got.Runtime != "codex" || got.RuntimeSessionID != "s1" {
		t.Fatal(got, e)
	}
	if _, e = c.StartOwned(w.ID, WorkflowSpec, ComplexitySimple, "Again", WorkflowOwner{"codex", "s1"}); e != nil {
		t.Fatal(e)
	}
}
func TestStartOwned_RejectsExistingOwner(t *testing.T) {
	c := ownedCoordinator(t)
	c.StartOwned("spec-codex", WorkflowSpec, ComplexitySimple, "x", WorkflowOwner{"codex", "s1"})
	for _, o := range []WorkflowOwner{{"codex", "s2"}, {"", "s1"}} {
		if _, e := c.StartOwned("spec-codex", WorkflowSpec, ComplexitySimple, "takeover", o); !errors.Is(e, ErrOwnershipConflict) {
			t.Fatal(e)
		}
	}
	if _, e := c.StartOwned("spec-empty", WorkflowSpec, ComplexitySimple, "x", WorkflowOwner{"codex", ""}); e == nil {
		t.Fatal("missing identity accepted")
	}
}
func TestBindOwner_RejectsCrossRuntime(t *testing.T) {
	c := ownedCoordinator(t)
	c.Start("spec-legacy", WorkflowSpec, ComplexitySimple, "x")
	if _, e := c.BindOwner("spec-legacy", WorkflowOwner{"codex", "s1"}); !errors.Is(e, ErrOwnershipConflict) {
		t.Fatal(e)
	}
	c.StartOwned("spec-codex", WorkflowSpec, ComplexitySimple, "x", WorkflowOwner{"codex", "s1"})
	if _, e := c.UpdateSessionID("spec-codex", "legacy"); !errors.Is(e, ErrOwnershipConflict) {
		t.Fatal(e)
	}
	w, e := c.BindOwner("spec-codex", WorkflowOwner{"codex", "s2"})
	if e != nil || w.SessionID != "codex:s2" || w.RuntimeSessionID != "s2" {
		t.Fatal(w, e)
	}
}
func TestResolveOwned_NoGlobalFallback(t *testing.T) {
	c := ownedCoordinator(t)
	legacy, _ := c.Start("spec-legacy", WorkflowSpec, ComplexitySimple, "x")
	a, _ := c.StartOwned("spec-a", WorkflowSpec, ComplexitySimple, "x", WorkflowOwner{"codex", "s1"})
	b, _ := c.StartOwned("spec-b", WorkflowSpec, ComplexitySimple, "x", WorkflowOwner{"codex", "s1"})
	if w, e := ResolveOwned([]*WorkflowState{legacy}, "", WorkflowOwner{"codex", "s1"}); e != nil || w != nil {
		t.Fatal(w, e)
	}
	if _, e := ResolveOwned([]*WorkflowState{a, b}, "", WorkflowOwner{"codex", "s1"}); !errors.Is(e, ErrAmbiguousWorkflow) {
		t.Fatal(e)
	}
	if w, e := ResolveOwned([]*WorkflowState{a, b}, "spec-b", WorkflowOwner{"codex", "s1"}); e != nil || w.ID != "spec-b" {
		t.Fatal(w, e)
	}
}
func TestRuntimeMetadata_SurvivesCoordinatorUpdates(t *testing.T) {
	c := ownedCoordinator(t)
	c.StartOwned("spec-codex", WorkflowSpec, ComplexitySimple, "x", WorkflowOwner{"codex", "s1"})
	c.SetTasks("spec-codex", []string{"one"})
	c.SetPlanContent("spec-codex", "plan")
	w, e := c.Get("spec-codex")
	if e != nil || w.RuntimeSessionID != "s1" {
		t.Fatal(w, e)
	}
}
