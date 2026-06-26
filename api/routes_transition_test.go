package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

// TestHandleTransitionPhase_CaseInsensitive verifies that an agent-supplied
// phase with non-canonical casing (e.g. "Plan" from a skill heading) is
// normalized to the lowercase state-machine constant instead of failing with
// "invalid transition". This guards the complex spec flow's governance → plan step.
func TestHandleTransitionPhase_CaseInsensitive(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	coord := orchestration.NewCoordinator(database)
	server := &Server{db: database, coordinator: coord, hub: NewHub()}

	const id = "spec-case-test"
	if _, err := coord.Start(id, orchestration.WorkflowSpec, orchestration.ComplexityComplex, "Case Test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Advance plan → discovery → design → governance.
	for _, p := range []orchestration.Phase{orchestration.PhaseDiscovery, orchestration.PhaseDesign, orchestration.PhaseGovernance} {
		if _, err := coord.Transition(id, p); err != nil {
			t.Fatalf("Transition to %s: %v", p, err)
		}
	}

	// Governance → "Plan" (capitalized, as written in the spec-complex skill).
	req := httptest.NewRequest(http.MethodPut, "/api/workflows/"+id+"/phase", strings.NewReader(`{"phase":"Plan"}`))
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()

	server.handleTransitionPhase(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["phase"] != "plan" {
		t.Errorf("phase = %v, want plan", resp["phase"])
	}
}
