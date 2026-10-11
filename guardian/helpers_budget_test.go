package guardian

import (
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

func orchestrationFor(t *testing.T, database *db.DB, id string) *orchestration.Coordinator {
	t.Helper()
	coord := orchestration.NewCoordinator(database)
	if _, err := coord.Start(id, orchestration.WorkflowSpec, orchestration.ComplexitySimple, "Budget"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return coord
}
