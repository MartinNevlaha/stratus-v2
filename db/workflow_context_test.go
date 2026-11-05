package db

import "testing"

func openWorkflowContextDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func saveCtxEvent(t *testing.T, d *DB, in SaveEventInput) int64 {
	t.Helper()
	id, err := d.SaveEvent(in)
	if err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}
	return id
}

func TestLatestWorkflowEvent_PicksNewestForWorkflowAndPhase(t *testing.T) {
	d := openWorkflowContextDB(t)
	mk := func(wf, phase, title string) int64 {
		return saveCtxEvent(t, d, SaveEventInput{Type: "context_prefetch", Title: title, Text: "x",
			Refs: map[string]any{"workflow_id": wf, "phase": phase}})
	}
	mk("wf-a", "plan", "a-plan-old")
	newPlan := mk("wf-a", "plan", "a-plan-new")
	implement := mk("wf-a", "implement", "a-impl")
	mk("wf-b", "plan", "b-plan")
	saveCtxEvent(t, d, SaveEventInput{Type: "discovery", Title: "other type", Text: "x",
		Refs: map[string]any{"workflow_id": "wf-a", "phase": "plan"}})

	got, err := d.LatestWorkflowEvent("context_prefetch", "wf-a", "plan")
	if err != nil || got == nil || got.ID != newPlan {
		t.Fatalf("phase lookup = %+v, %v; want id %d", got, err, newPlan)
	}
	got, err = d.LatestWorkflowEvent("context_prefetch", "wf-a", "")
	if err != nil || got == nil || got.ID != implement {
		t.Fatalf("any-phase lookup = %+v, %v; want id %d", got, err, implement)
	}
	if got.Refs["workflow_id"] != "wf-a" {
		t.Fatalf("refs not decoded: %+v", got.Refs)
	}
}

func TestLatestWorkflowEvent_NoMatchReturnsNil(t *testing.T) {
	d := openWorkflowContextDB(t)
	saveCtxEvent(t, d, SaveEventInput{Type: "context_prefetch", Text: "x", Refs: map[string]any{"workflow_id": "wf-a", "phase": "plan"}})
	for _, tc := range [][2]string{{"wf-zzz", ""}, {"wf-a", "verify"}} {
		got, err := d.LatestWorkflowEvent("context_prefetch", tc[0], tc[1])
		if err != nil || got != nil {
			t.Fatalf("lookup %v = %+v, %v; want nil, nil", tc, got, err)
		}
	}
}

func TestRecentWorkflowDecisions_FiltersByWorkflowNewestFirstWithLimit(t *testing.T) {
	d := openWorkflowContextDB(t)
	byRef1 := saveCtxEvent(t, d, SaveEventInput{Type: "decision", Title: "ref1", Text: "x", Refs: map[string]any{"workflow_id": "wf-a"}})
	byTag := saveCtxEvent(t, d, SaveEventInput{Type: "decision", Title: "tag", Text: "x", Tags: []string{"decision", "wf-a"}})
	byRef2 := saveCtxEvent(t, d, SaveEventInput{Type: "decision", Title: "ref2", Text: "x", Refs: map[string]any{"workflow_id": "wf-a"}})
	saveCtxEvent(t, d, SaveEventInput{Type: "decision", Title: "other wf", Text: "x", Refs: map[string]any{"workflow_id": "wf-b"}})
	saveCtxEvent(t, d, SaveEventInput{Type: "decision", Title: "unscoped", Text: "x"})
	saveCtxEvent(t, d, SaveEventInput{Type: "discovery", Title: "not a decision", Text: "x", Refs: map[string]any{"workflow_id": "wf-a"}})

	got, err := d.RecentWorkflowDecisions("wf-a", 10)
	if err != nil {
		t.Fatalf("RecentWorkflowDecisions: %v", err)
	}
	var ids []int64
	for _, e := range got {
		ids = append(ids, e.ID)
	}
	want := []int64{byRef2, byTag, byRef1}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Fatalf("ids = %v, want %v", ids, want)
	}

	got, err = d.RecentWorkflowDecisions("wf-a", 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("limit: len=%d err=%v", len(got), err)
	}
}

func TestRecentWorkflowDecisions_TagMatchIsExact(t *testing.T) {
	d := openWorkflowContextDB(t)
	saveCtxEvent(t, d, SaveEventInput{Type: "decision", Title: "ab by tag", Text: "x", Tags: []string{"decision", "spec-ab"}})
	saveCtxEvent(t, d, SaveEventInput{Type: "decision", Title: "ab by ref", Text: "x", Refs: map[string]any{"workflow_id": "spec-ab"}})
	only := saveCtxEvent(t, d, SaveEventInput{Type: "decision", Title: "a", Text: "x", Tags: []string{"decision", "spec-a"}})

	got, err := d.RecentWorkflowDecisions("spec-a", 10)
	if err != nil {
		t.Fatalf("RecentWorkflowDecisions: %v", err)
	}
	if len(got) != 1 || got[0].ID != only {
		t.Fatalf("spec-a decisions = %+v, want only id %d", got, only)
	}
}

func TestOpen_CreatesEventsTypeIDIndex(t *testing.T) {
	d := openWorkflowContextDB(t)
	var name string
	err := d.sql.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_events_type_id'`).Scan(&name)
	if err != nil {
		t.Fatalf("idx_events_type_id missing: %v", err)
	}
}
