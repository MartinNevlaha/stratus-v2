package orchestration

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// A swarm launches several delivery agents in one message; each delegation guard records
// its agent at once. Every read-modify-write of a workflow must see the previous one.
func TestRecordDelegation_ConcurrentCallsKeepEveryAgent(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	coord := NewCoordinator(database)

	for round := 0; round < 10; round++ {
		id := fmt.Sprintf("spec-race-%d", round)
		if _, err := coord.Start(id, WorkflowSpec, ComplexitySimple, "Race"); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if _, err := coord.SetTasks(id, []string{"a", "b"}); err != nil {
			t.Fatalf("SetTasks: %v", err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if _, err := coord.RecordDelegation(id, fmt.Sprintf("delivery-agent-%d", i)); err != nil {
					t.Errorf("RecordDelegation: %v", err)
				}
			}(i)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := coord.CompleteTask(id, 0); err != nil {
				t.Errorf("CompleteTask: %v", err)
			}
		}()
		wg.Wait()

		state, err := coord.Get(id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got := len(state.Delegated["plan"]); got != 6 {
			t.Fatalf("round %d: %d of 6 delegations kept: %v", round, got, state.Delegated["plan"])
		}
		if state.Tasks[0].Status != "done" {
			t.Fatalf("round %d: task 0 = %q, want done", round, state.Tasks[0].Status)
		}
	}
}
