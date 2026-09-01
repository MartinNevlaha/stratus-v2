package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

func TestHandleRecordDelegationWorkflowIDMismatch(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	coord := orchestration.NewCoordinator(database)
	server := &Server{db: database, coordinator: coord, hub: NewHub()}
	if _, err := coord.Start("spec-a", orchestration.WorkflowSpec, orchestration.ComplexitySimple, "A"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/workflows/spec-a/delegate", strings.NewReader(`{"workflow_id":"spec-b","agent_id":"delivery-frontend-engineer"}`))
	req.SetPathValue("id", "spec-a")
	w := httptest.NewRecorder()

	server.handleRecordDelegation(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "WORKFLOW_ID_MISMATCH" {
		t.Fatalf("code = %v, want WORKFLOW_ID_MISMATCH", resp["code"])
	}
}

func TestHandleRecordDelegationRequiresInProgressTaskWhenProvided(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	coord := orchestration.NewCoordinator(database)
	server := &Server{db: database, coordinator: coord, hub: NewHub()}
	const id = "spec-task-state"
	if _, err := coord.Start(id, orchestration.WorkflowSpec, orchestration.ComplexitySimple, "Task State"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := coord.SetTasks(id, []string{"Implement UI"}); err != nil {
		t.Fatalf("SetTasks: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/workflows/"+id+"/delegate", strings.NewReader(`{"workflow_id":"spec-task-state","agent_id":"delivery-frontend-engineer","phase":"plan","task_index":0}`))
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()

	server.handleRecordDelegation(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "WORKFLOW_TASK_NOT_IN_PROGRESS" {
		t.Fatalf("code = %v, want WORKFLOW_TASK_NOT_IN_PROGRESS", resp["code"])
	}
}

func TestHandleRecordDelegationAcceptsResolvedContext(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	coord := orchestration.NewCoordinator(database)
	server := &Server{db: database, coordinator: coord, hub: NewHub()}
	const id = "spec-delegate-ok"
	if _, err := coord.Start(id, orchestration.WorkflowSpec, orchestration.ComplexitySimple, "Delegate OK"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := coord.SetSessionID(id, "sess-a"); err != nil {
		t.Fatalf("SetSessionID: %v", err)
	}
	if _, err := coord.SetTasks(id, []string{"Implement UI"}); err != nil {
		t.Fatalf("SetTasks: %v", err)
	}
	if _, err := coord.StartTask(id, 0); err != nil {
		t.Fatalf("StartTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/workflows/"+id+"/delegate", strings.NewReader(`{"workflow_id":"spec-delegate-ok","agent_id":"delivery-frontend-engineer","phase":"plan","task_index":0,"session_id":"sess-a"}`))
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()

	server.handleRecordDelegation(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	state, err := coord.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	delegated := state.Delegated[string(state.Phase)]
	if len(delegated) != 1 || delegated[0] != "delivery-frontend-engineer" {
		t.Fatalf("delegated = %#v", delegated)
	}
}
