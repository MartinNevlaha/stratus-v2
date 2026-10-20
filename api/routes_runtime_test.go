package api

import (
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeMetadata_API(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	s := &Server{db: d, coordinator: orchestration.NewCoordinator(d), hub: NewHub(), projectRoot: t.TempDir()}
	post := func(body string) int {
		r := httptest.NewRequest("POST", "/api/workflows", strings.NewReader(body))
		w := httptest.NewRecorder()
		s.handleStartWorkflow(w, r)
		return w.Code
	}
	for _, c := range []struct {
		body string
		want int
	}{
		{`{"id":"spec-codex","runtime":"codex","runtime_session_id":"s1","session_id":"codex:s1"}`, 200},
		{`{"id":"spec-codex","runtime":"codex","runtime_session_id":"s2","session_id":"codex:s2"}`, 409},
		{`{"id":"spec-codex"}`, 409},
		{`{"id":"spec-empty","runtime":"codex"}`, 400},
		{`{"id":"spec-legacy","session_id":"s1"}`, 200},
	} {
		if got := post(c.body); got != c.want {
			t.Errorf("%s: %d want %d", c.body, got, c.want)
		}
	}
	r := httptest.NewRequest("PATCH", "/api/workflows/spec-codex/session", strings.NewReader(`{"session_id":"legacy"}`))
	r.SetPathValue("id", "spec-codex")
	w := httptest.NewRecorder()
	s.handleSetWorkflowSession(w, r)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	wf, _ := s.coordinator.Get("spec-codex")
	if wf.RuntimeSessionID != "s1" {
		t.Fatal(wf)
	}
}
