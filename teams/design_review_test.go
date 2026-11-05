package teams

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func designMembers() []MemberConfig {
	p := Profile{Runtime: "codex"}
	return []MemberConfig{
		{ID: "lead", Role: "lead", Profile: p},
		{ID: "architect", Role: "delivery-system-architect", Profile: p},
		{ID: "critic", Role: "delivery-code-reviewer", Profile: p},
	}
}

func createDesignTeam(t *testing.T, m *Manager, dr *DesignReviewConfig) (*Team, error) {
	t.Helper()
	return m.Create(context.Background(), CreateRequest{ProjectID: "project-d", ProjectRoot: t.TempDir(), Members: designMembers(), DesignReview: dr})
}

func TestCreateDesignReview_Validation(t *testing.T) {
	cases := []struct {
		name string
		cfg  *DesignReviewConfig
		want string
	}{
		{"missing reviewer", &DesignReviewConfig{Enabled: true}, "requires reviewer_member_id"},
		{"unknown reviewer", &DesignReviewConfig{Enabled: true, ReviewerMemberID: "ghost"}, "not a member"},
		{"lead reviewer", &DesignReviewConfig{Enabled: true, ReviewerMemberID: "lead"}, "cannot be the team lead"},
		{"negative rounds", &DesignReviewConfig{Enabled: true, ReviewerMemberID: "critic", MaxRounds: -1}, "max_rounds"},
		{"too many rounds", &DesignReviewConfig{Enabled: true, ReviewerMemberID: "critic", MaxRounds: 11}, "max_rounds"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := testManager(t, newFixture())
			_, err := createDesignTeam(t, m, c.cfg)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err=%v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestCreateDesignReview_DefaultsAndDisabled(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm, err := createDesignTeam(t, m, &DesignReviewConfig{Enabled: true, ReviewerMemberID: "critic"})
	if err != nil {
		t.Fatal(err)
	}
	if tm.DesignReview == nil || tm.DesignReview.MaxRounds != DefaultDesignReviewRounds || tm.DesignReview.ReviewerMemberID != "critic" {
		t.Fatalf("design review config=%+v", tm.DesignReview)
	}
	m2, _ := testManager(t, newFixture())
	for _, bad := range []*DesignReviewConfig{{Enabled: false, ReviewerMemberID: "ghost"}, {Enabled: false, MaxRounds: 99}, {Enabled: false, ReviewerMemberID: "lead"}, {Enabled: false, MaxRounds: -2}} {
		if _, err := createDesignTeam(t, m2, bad); err == nil {
			t.Fatalf("nonsense accepted although disabled: %+v", bad)
		}
	}
	for _, cfg := range []*DesignReviewConfig{nil, {Enabled: false}, {Enabled: false, ReviewerMemberID: "critic", MaxRounds: 5}} {
		off, err := createDesignTeam(t, m2, cfg)
		if err != nil {
			t.Fatalf("a sane disabled option must be accepted: %v", err)
		}
		if off.DesignReview != nil {
			t.Fatalf("disabled option stored: %+v", off.DesignReview)
		}
		if err = func() error { _, e := m2.Action(context.Background(), off.ID, "stop"); return e }(); err != nil {
			t.Fatal(err)
		}
	}
}

// callFor answers the wake-ups of other tasks ("ok") until the submission for id arrives.
func callFor(t *testing.T, a *fixtureAdapter, id string) submission {
	t.Helper()
	for i := 0; i < 8; i++ {
		c := nextCall(t, a)
		if c.Input.ID == id {
			return c
		}
		complete(c, "ok")
	}
	t.Fatalf("no submission for %s", id)
	return submission{}
}

func TestEnqueueSystem_IdleLeadReviewTask(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm, err := createDesignTeam(t, m, &DesignReviewConfig{Enabled: true, ReviewerMemberID: "critic"})
	if err != nil {
		t.Fatal(err)
	}
	// The lead has no running assignment: a normal enqueue from it is refused.
	if _, err = m.Enqueue(context.Background(), tm.ID, TaskRequest{ID: "plain", From: "lead", To: "critic", Text: "review"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("plain enqueue from an idle lead: %v", err)
	}
	task, err := m.EnqueueSystem(context.Background(), tm.ID, TaskRequest{ID: "dr-1", From: "lead", To: "critic", Role: "", Text: "review the design", Kind: "task", Revision: "hash1", ReturnTo: "lead"}, PurposeDesignReview)
	if err != nil {
		t.Fatal(err)
	}
	if task.Purpose != PurposeDesignReview || task.Role != "review" || task.From != "lead" || task.ReturnTo != "lead" || task.Revision != "hash1" {
		t.Fatalf("task=%+v", task)
	}
	again, err := m.EnqueueSystem(context.Background(), tm.ID, TaskRequest{ID: "dr-1", From: "lead", To: "critic", Role: "", Text: "review the design", Kind: "task", Revision: "hash1", ReturnTo: "lead"}, PurposeDesignReview)
	if err != nil || again.ID != task.ID {
		t.Fatalf("repeat must be idempotent: %v %+v", err, again)
	}
	if _, err = m.EnqueueSystem(context.Background(), tm.ID, TaskRequest{From: "architect", To: "critic", Text: "x"}, PurposeDesignReview); !errors.Is(err, ErrForbidden) {
		t.Fatalf("system enqueue must act for the lead only: %v", err)
	}
	if _, err = m.EnqueueSystem(context.Background(), tm.ID, TaskRequest{From: "lead", To: "critic", Text: "x"}, "other"); err == nil {
		t.Fatal("unknown purpose accepted")
	}
	c := nextCall(t, a)
	complete(c, "Verdict: PASS")
	got := awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "dr-1") == "complete" && taskState(tm, "result:dr-1") != "" })
	if r := findTask(got, "result:dr-1"); r.To != "lead" {
		t.Fatalf("verdict must wake the lead: %+v", r)
	}
	if _, err = m.Enqueue(context.Background(), tm.ID, TaskRequest{ID: "forged", From: "user", To: "critic", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := findTask(awaitTeam(t, m, tm.ID, func(*Team) bool { return true }), "forged"); got.Purpose != "" {
		t.Fatalf("a plain request must not carry a purpose: %+v", got)
	}
}

func TestPurposeValidator_ReplacesRevisionCheck(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm, err := createDesignTeam(t, m, &DesignReviewConfig{Enabled: true, ReviewerMemberID: "critic"})
	if err != nil {
		t.Fatal(err)
	}
	// Whole-tree validator says "stale"; a design review must ignore it (unrelated edits).
	m.SetRevisionValidator(func(_, _ string) bool { return false })
	var docsCurrent atomic.Bool
	docsCurrent.Store(true)
	m.SetPurposeValidator(func(_ Team, task Task) bool { return docsCurrent.Load() && task.Revision == "docs-1" })
	if _, err = m.EnqueueSystem(context.Background(), tm.ID, TaskRequest{ID: "fresh", From: "lead", To: "critic", Text: "review", Kind: "task", Revision: "docs-1"}, PurposeDesignReview); err != nil {
		t.Fatal(err)
	}
	complete(nextCall(t, a), "Verdict: PASS")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "fresh") == "complete" })

	if _, err = m.EnqueueSystem(context.Background(), tm.ID, TaskRequest{ID: "edited", From: "lead", To: "critic", Text: "review again", Kind: "task", Revision: "docs-2"}, PurposeDesignReview); err != nil {
		t.Fatal(err)
	}
	complete(callFor(t, a, "edited"), "Verdict: PASS")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "edited") == "needs_user" })
}

func TestTaskFinishedHook_CalledOnceOutsideLock(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm, err := createDesignTeam(t, m, &DesignReviewConfig{Enabled: true, ReviewerMemberID: "critic"})
	if err != nil {
		t.Fatal(err)
	}
	type call struct {
		id, state, purpose string
	}
	calls := make(chan call, 8)
	m.SetTaskFinishedHook(func(snapshot Team, task Task) {
		// Calling back into the manager would deadlock if the lock were still held.
		if _, e := m.Get(snapshot.ID); e != nil {
			t.Errorf("hook cannot use the manager: %v", e)
		}
		calls <- call{task.ID, task.State, task.Purpose}
	})
	// A task without a purpose does not call the hook.
	enqueue(t, m, tm.ID, TaskRequest{ID: "plain", To: "critic", Text: "unrelated"})
	complete(nextCall(t, a), "fine")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "plain") == "complete" })
	if _, err = m.EnqueueSystem(context.Background(), tm.ID, TaskRequest{ID: "dr", From: "lead", To: "critic", Text: "review", Kind: "task", Revision: "h"}, PurposeDesignReview); err != nil {
		t.Fatal(err)
	}
	complete(callFor(t, a, "dr"), "Verdict: FAIL")
	select {
	case c := <-calls:
		if c.id != "dr" || c.state != "complete" || c.purpose != PurposeDesignReview {
			t.Fatalf("hook call=%+v", c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hook not called")
	}
	select {
	case c := <-calls:
		t.Fatalf("hook called again: %+v", c)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTaskFinishedHook_PanicDoesNotBreakManager(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm, err := createDesignTeam(t, m, &DesignReviewConfig{Enabled: true, ReviewerMemberID: "critic"})
	if err != nil {
		t.Fatal(err)
	}
	m.SetTaskFinishedHook(func(Team, Task) { panic("boom") })
	if _, err = m.EnqueueSystem(context.Background(), tm.ID, TaskRequest{ID: "dr", From: "lead", To: "critic", Text: "review", Kind: "task", Revision: "h"}, PurposeDesignReview); err != nil {
		t.Fatal(err)
	}
	complete(nextCall(t, a), "Verdict: PASS")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "dr") == "complete" })
	enqueue(t, m, tm.ID, TaskRequest{ID: "after", To: "architect", Text: "still works"})
	complete(callFor(t, a, "after"), "ok")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "after") == "complete" })
}

func TestRecordDesignReviewWaiver_PersistsAndDedupes(t *testing.T) {
	a := newFixture()
	m, db := testManager(t, a)
	tm, err := createDesignTeam(t, m, &DesignReviewConfig{Enabled: true, ReviewerMemberID: "critic"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.RecordDesignReviewWaiver("missing", DesignReviewWaiver{WorkflowID: "w", DocHash: "h"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown team: %v", err)
	}
	if err = m.RecordDesignReviewWaiver(tm.ID, DesignReviewWaiver{WorkflowID: "w"}); err == nil {
		t.Fatal("waiver without a hash accepted")
	}
	w := DesignReviewWaiver{WorkflowID: "w", DocHash: "h", Reason: "ok by user"}
	for i := 0; i < 2; i++ {
		if err = m.RecordDesignReviewWaiver(tm.ID, w); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := m.Get(tm.ID)
	if len(got.DesignReviewWaivers) != 1 || got.DesignReviewWaivers[0].Reason != "ok by user" || got.DesignReviewWaivers[0].At.IsZero() {
		t.Fatalf("waivers=%+v", got.DesignReviewWaivers)
	}
	m2, err := NewManager(db, map[string]Adapter{"codex": a}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	reloaded, err := m2.Get(tm.ID)
	if err != nil || len(reloaded.DesignReviewWaivers) != 1 || reloaded.DesignReview == nil || reloaded.DesignReview.ReviewerMemberID != "critic" {
		t.Fatalf("not persisted: %v %+v", err, reloaded)
	}
}

func TestEnqueue_ReservesDesignReviewRequestIDs(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm, err := createDesignTeam(t, m, &DesignReviewConfig{Enabled: true, ReviewerMemberID: "critic"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"dr-review:wf:abc:1", "dr-rework:x", "dr-"} {
		if _, err = m.Enqueue(context.Background(), tm.ID, TaskRequest{ID: id, From: "user", To: "critic", Text: "squat"}); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	if _, err = m.EnqueueSystem(context.Background(), tm.ID, TaskRequest{ID: "dr-review:wf:abc:1", From: "lead", To: "critic", Text: "real", Revision: "h"}, PurposeDesignReview); err != nil {
		t.Fatalf("the system path must keep working: %v", err)
	}
}

func TestEnqueueSystem_DesignReviewAlwaysGoesToTheChosenReviewer(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm, err := createDesignTeam(t, m, &DesignReviewConfig{Enabled: true, ReviewerMemberID: "critic"})
	if err != nil {
		t.Fatal(err)
	}
	for i, rev := range []string{"hash-1", "hash-2", "hash-3"} {
		id := "dr-review:wf:" + rev
		task, err := m.EnqueueSystem(context.Background(), tm.ID, TaskRequest{ID: id, From: "lead", To: "critic", Text: "review", Kind: "task", WorkflowID: "wf" + string(rune('a'+i%1)), Revision: rev}, PurposeDesignReview)
		if err != nil {
			t.Fatal(err)
		}
		if task.To != "critic" {
			t.Fatalf("round %d went to %s", i+1, task.To)
		}
		complete(callFor(t, a, id), "Verdict: PASS")
		awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, id) == "complete" })
	}
	if got, _ := m.Get(tm.ID); len(got.Members) != 3 {
		t.Fatalf("members=%d, a clone was added", len(got.Members))
	}
}
