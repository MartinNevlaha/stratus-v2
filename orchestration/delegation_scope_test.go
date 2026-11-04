package orchestration

import (
	"errors"
	"github.com/MartinNevlaha/stratus-v2/db"
	"path/filepath"
	"testing"
)

func TestScopedDelegationRejectsChangedOwnerOrPhase(t *testing.T) {
	d, e := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	c := NewCoordinator(d)
	w, e := c.StartOwned("spec-scoped", WorkflowSpec, ComplexitySimple, "scope", WorkflowOwner{Runtime: "codex", NativeSessionID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	if e = c.RecordDelegationScoped(w.ID, "delivery-system-architect", w.SessionID, string(w.Phase), w.CurrentTask, w.ReviewGeneration); e != nil {
		t.Fatal(e)
	}
	if e = c.RecordDelegationScoped(w.ID, "delivery-code-reviewer", "codex:other", string(w.Phase), w.CurrentTask, w.ReviewGeneration); !errors.Is(e, ErrOwnershipConflict) {
		t.Fatalf("owner: %v", e)
	}
	if _, e = c.Transition(w.ID, PhaseImplement); e != nil {
		t.Fatal(e)
	}
	if e = c.RecordDelegationScoped(w.ID, "delivery-code-reviewer", w.SessionID, string(w.Phase), w.CurrentTask, w.ReviewGeneration); !errors.Is(e, ErrOwnershipConflict) {
		t.Fatalf("stale phase: %v", e)
	}
	current, _ := c.Get(w.ID)
	if len(current.Delegated["implement"]) != 0 {
		t.Fatal("old delegation attached to new phase")
	}
}
