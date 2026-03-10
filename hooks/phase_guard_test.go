package hooks

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestWorkflowExistenceGuardBlocksWithoutSessionWorkflow(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "wf-other", "session_id": "session-other", "phase": "plan"},
		},
	})

	decision := WorkflowExistenceGuard(HookEvent{
		ToolName:  "Task",
		SessionID: "session-current",
	})

	if decision.Continue {
		t.Fatalf("expected guard to block when no session workflow exists")
	}
	if decision.Reason != noActiveWorkflowReason {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
}

func TestWorkflowExistenceGuardAllowsExactSessionWorkflow(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "wf-current", "session_id": "session-current", "phase": "plan"},
			{"id": "wf-other", "session_id": "session-other", "phase": "implement"},
		},
	})

	decision := WorkflowExistenceGuard(HookEvent{
		ToolName:  "Task",
		SessionID: "session-current",
	})

	if !decision.Continue {
		t.Fatalf("expected guard to allow exact session workflow, got %#v", decision)
	}
}

func TestDelegationGuardUsesExactSessionWorkflow(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "wf-other", "session_id": "session-other", "phase": "plan"},
		},
	})

	decision := DelegationGuard(HookEvent{
		ToolName:  "Task",
		SessionID: "session-current",
		ToolInput: map[string]any{
			"subagent_type": "delivery-backend-engineer",
		},
	})

	if decision.Continue {
		t.Fatalf("expected delegation guard to block when only another session has a workflow")
	}
	if decision.Reason != noActiveWorkflowReason {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
}

func setDashboardState(t *testing.T, state dashboardState) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/dashboard/state" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(state)
	}))
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	t.Setenv("STRATUS_PORT", port)
}
