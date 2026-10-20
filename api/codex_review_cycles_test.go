package api

import (
	"encoding/json"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"net/http/httptest"
	"testing"
)

func TestCodexReview_DoesNotReviveAcrossRepairCycles(t *testing.T) {
	for _, tc := range []struct {
		id             string
		kind           orchestration.WorkflowType
		initial        []orchestration.Phase
		repair, review orchestration.Phase
	}{
		{"spec-cycle", orchestration.WorkflowSpec, []orchestration.Phase{orchestration.PhaseImplement, orchestration.PhaseVerify}, orchestration.PhaseImplement, orchestration.PhaseVerify},
		{"bug-cycle", orchestration.WorkflowBug, []orchestration.Phase{orchestration.PhaseFix, orchestration.PhaseReview}, orchestration.PhaseFix, orchestration.PhaseReview},
	} {
		t.Run(tc.id, func(t *testing.T) {
			s := codexTestServer(t)
			if _, err := s.coordinator.StartOwned(tc.id, tc.kind, orchestration.ComplexitySimple, "Cycle", orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: "s1"}); err != nil {
				t.Fatal(err)
			}
			transition := func(p orchestration.Phase) {
				t.Helper()
				if _, err := s.coordinator.Transition(tc.id, p); err != nil {
					t.Fatal(err)
				}
			}
			for _, p := range tc.initial {
				transition(p)
			}
			post := func(body string) {
				t.Helper()
				w := postCodex(t, s, body)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			post(`{"event":"agent_stop","workflow_id":"` + tc.id + `","session_id":"s1","agent_id":"reviewer","turn_id":"r1","agent_type":"delivery-code-reviewer","report":"Verdict: PASS"}`)
			current := func() string {
				t.Helper()
				r := httptest.NewRequest("GET", "/", nil)
				r.SetPathValue("id", tc.id)
				w := httptest.NewRecorder()
				s.handleListWorkflowAgents(w, r)
				var got struct {
					Review db.ReviewState `json:"review"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				return got.Review.Status
			}
			if got := current(); got != "PASS" {
				t.Fatal(got)
			}
			transition(tc.repair)
			post(`{"event":"tool","workflow_id":"` + tc.id + `","session_id":"s1","agent_id":"repair","turn_id":"w2","tool_use_id":"u2","tool_name":"apply_patch"}`)
			transition(tc.review)
			if got := current(); got == "PASS" {
				t.Fatal("old PASS revived after repair cycle")
			}
			post(`{"event":"agent_stop","workflow_id":"` + tc.id + `","session_id":"s1","agent_id":"reviewer","turn_id":"r2","agent_type":"delivery-code-reviewer","report":"Verdict: PASS"}`)
			if got := current(); got != "PASS" {
				t.Fatal("fresh review was not accepted", got)
			}
		})
	}
}
