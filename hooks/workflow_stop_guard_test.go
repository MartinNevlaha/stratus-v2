package hooks

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stopGuardAPI serves the two endpoints the Stop guard reads: the dashboard state
// and the per-workflow agent list.
func stopGuardAPI(t *testing.T, workflows []map[string]any, agents map[string][]map[string]any) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/dashboard/state":
			_ = json.NewEncoder(w).Encode(dashboardState{Workflows: workflows})
		case strings.HasPrefix(r.URL.Path, "/api/workflows/") && strings.HasSuffix(r.URL.Path, "/agents"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/workflows/"), "/agents")
			list := agents[id]
			if list == nil {
				list = []map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"agents": list})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	_, port, _ := net.SplitHostPort(u.Host)
	t.Setenv("STRATUS_PORT", port)
}

func stopEvent(session, transcript, lastMsg string) HookEvent {
	return HookEvent{
		HookEventName:        "Stop",
		SessionID:            session,
		TranscriptPath:       transcript,
		LastAssistantMessage: lastMsg,
	}
}

func wfInPhase(id, session, phase string) map[string]any {
	return map[string]any{"id": id, "session_id": session, "phase": phase}
}

func writeStopTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const (
	stopUserNudgeLine  = `{"type":"user","message":{"content":"` + workflowStopNudgeMarker + ` workflow x is in phase implement."}}`
	stopAssistantTool  = `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__stratus__transition_phase"}]}}`
	stopAssistantText  = `{"type":"assistant","message":{"content":[{"type":"text","text":"Plan is ready."}]}}`
	stopToolResultLine = `{"type":"user","message":{"content":[{"type":"tool_result","content":"` + workflowStopNudgeMarker + ` quoted from a file"}]}}`
)

func TestWorkflowStopGuard_ActiveWorkflowIncompletePhase_Nudges(t *testing.T) {
	stopGuardAPI(t, []map[string]any{wfInPhase("spec-a", "s1", "implement")}, nil)

	d := WorkflowStopGuard(stopEvent("s1", writeStopTranscript(t, stopAssistantText), "Phase 1 done."))

	if !d.Nudge {
		t.Fatalf("expected nudge, got %+v", d)
	}
	for _, want := range []string{workflowStopNudgeMarker, "spec-a", "implement"} {
		if !strings.Contains(d.Reason, want) {
			t.Errorf("reason missing %q: %s", want, d.Reason)
		}
	}
}

func TestWorkflowStopGuard_CompletePhase_Allows(t *testing.T) {
	stopGuardAPI(t, []map[string]any{wfInPhase("spec-a", "s1", "complete")}, nil)

	d := WorkflowStopGuard(stopEvent("s1", "", "All done."))

	if d.Nudge || !d.Continue {
		t.Fatalf("expected allow, got %+v", d)
	}
}

func TestWorkflowStopGuard_OnlyForeignWorkflow_Allows(t *testing.T) {
	// A single workflow owned by another session must not be bound to this one.
	stopGuardAPI(t, []map[string]any{wfInPhase("spec-a", "other-session", "implement")}, nil)

	d := WorkflowStopGuard(stopEvent("s1", "", "Hello."))

	if d.Nudge || !d.Continue {
		t.Fatalf("expected allow, got %+v", d)
	}
}

func TestWorkflowStopGuard_StopHookActive_Allows(t *testing.T) {
	stopGuardAPI(t, []map[string]any{wfInPhase("spec-a", "s1", "implement")}, nil)
	ev := stopEvent("s1", "", "Continuing.")
	ev.StopHookActive = true

	d := WorkflowStopGuard(ev)

	if d.Nudge || !d.Continue {
		t.Fatalf("expected allow, got %+v", d)
	}
}

func TestWorkflowStopGuard_LastMessageIsQuestion_Allows(t *testing.T) {
	stopGuardAPI(t, []map[string]any{wfInPhase("spec-a", "s1", "plan")}, nil)

	d := WorkflowStopGuard(stopEvent("s1", "", "Which database should I use?"))

	if d.Nudge || !d.Continue {
		t.Fatalf("expected allow, got %+v", d)
	}
}

func TestWorkflowStopGuard_QuestionOnlyInTranscript_Allows(t *testing.T) {
	// Claude Code did not send last_assistant_message: fall back to the transcript.
	stopGuardAPI(t, []map[string]any{wfInPhase("spec-a", "s1", "plan")}, nil)
	transcript := writeStopTranscript(t,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Should I proceed with option B?"}]}}`)

	d := WorkflowStopGuard(stopEvent("s1", transcript, ""))

	if d.Nudge || !d.Continue {
		t.Fatalf("expected allow, got %+v", d)
	}
}

func TestWorkflowStopGuard_NudgeIgnored_Allows(t *testing.T) {
	stopGuardAPI(t, []map[string]any{wfInPhase("spec-a", "s1", "implement")}, nil)
	// We nudged, and the model answered with text only — no tool call.
	transcript := writeStopTranscript(t, stopAssistantTool, stopUserNudgeLine, stopAssistantText)

	d := WorkflowStopGuard(stopEvent("s1", transcript, "Still waiting."))

	if d.Nudge || !d.Continue {
		t.Fatalf("expected allow after ignored nudge, got %+v", d)
	}
}

func TestWorkflowStopGuard_NudgeFollowedByWork_NudgesAgain(t *testing.T) {
	stopGuardAPI(t, []map[string]any{wfInPhase("spec-a", "s1", "implement")}, nil)
	transcript := writeStopTranscript(t, stopUserNudgeLine, stopAssistantTool, stopAssistantText)

	d := WorkflowStopGuard(stopEvent("s1", transcript, "Task 2 done."))

	if !d.Nudge {
		t.Fatalf("expected nudge after progress, got %+v", d)
	}
}

func TestWorkflowStopGuard_MarkerInToolResult_IsNotANudge(t *testing.T) {
	// The marker text can appear in a file the model read; only a real injected
	// message counts as a previous nudge.
	stopGuardAPI(t, []map[string]any{wfInPhase("spec-a", "s1", "implement")}, nil)
	transcript := writeStopTranscript(t, stopToolResultLine, stopAssistantText)

	d := WorkflowStopGuard(stopEvent("s1", transcript, "Done with step."))

	if !d.Nudge {
		t.Fatalf("expected nudge, got %+v", d)
	}
}

func TestWorkflowStopGuard_BackgroundAgentRunning_Allows(t *testing.T) {
	stopGuardAPI(t,
		[]map[string]any{wfInPhase("spec-a", "s1", "implement")},
		map[string][]map[string]any{"spec-a": {{"id": "a1", "status": "running"}}})

	d := WorkflowStopGuard(stopEvent("s1", "", "Delegated, waiting."))

	if d.Nudge || !d.Continue {
		t.Fatalf("expected allow while an agent runs, got %+v", d)
	}
}

func TestWorkflowStopGuard_FinishedAgents_Nudges(t *testing.T) {
	stopGuardAPI(t,
		[]map[string]any{wfInPhase("spec-a", "s1", "implement")},
		map[string][]map[string]any{"spec-a": {{"id": "a1", "status": "done"}, {"id": "a2", "status": "failed"}}})

	d := WorkflowStopGuard(stopEvent("s1", "", "Agent finished."))

	if !d.Nudge {
		t.Fatalf("expected nudge, got %+v", d)
	}
}

func TestWorkflowStopGuard_APIUnreachable_Allows(t *testing.T) {
	t.Setenv("STRATUS_PORT", "1")

	d := WorkflowStopGuard(stopEvent("s1", "", "Hi."))

	if d.Nudge || !d.Continue {
		t.Fatalf("expected fail-open allow, got %+v", d)
	}
}

func TestWorkflowStopGuard_NoSessionID_Allows(t *testing.T) {
	stopGuardAPI(t, []map[string]any{wfInPhase("spec-a", "", "implement")}, nil)

	d := WorkflowStopGuard(stopEvent("", "", "Hi."))

	if d.Nudge || !d.Continue {
		t.Fatalf("expected allow, got %+v", d)
	}
}
