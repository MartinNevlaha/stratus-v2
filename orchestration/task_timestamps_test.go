package orchestration

import (
	"path/filepath"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func TestTask_StartAndCompleteRecordTimestamps(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	coord := NewCoordinator(database)
	if _, err := coord.Start("spec-ts", WorkflowSpec, ComplexitySimple, "TS"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := coord.SetTasks("spec-ts", []string{"a"}); err != nil {
		t.Fatalf("SetTasks: %v", err)
	}

	st, err := coord.StartTask("spec-ts", 0)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if st.Tasks[0].StartedAt == "" || st.Tasks[0].CompletedAt != "" {
		t.Fatalf("after start: %+v", st.Tasks[0])
	}

	st, err = coord.CompleteTask("spec-ts", 0)
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if st.Tasks[0].CompletedAt == "" {
		t.Fatalf("after complete: %+v", st.Tasks[0])
	}
}
