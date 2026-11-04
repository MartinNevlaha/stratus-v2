package teams

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type attachedFixture struct {
	calls      chan submission
	interrupts atomic.Int32
	closes     atomic.Int32
}

func newAttachedFixture() *attachedFixture { return &attachedFixture{calls: make(chan submission, 8)} }
func (s *attachedFixture) Submit(ctx context.Context, in Input) (Result, error) {
	c := submission{Input: in, Reply: make(chan fixtureReply, 1)}
	select {
	case s.calls <- c:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	select {
	case r := <-c.Reply:
		return r.Result, r.Err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}
func (s *attachedFixture) Approve(context.Context, string, bool) error {
	return errors.New("native host owns approval")
}
func (s *attachedFixture) Interrupt(context.Context) error { s.interrupts.Add(1); return nil }
func (s *attachedFixture) Close() error                    { s.closes.Add(1); return nil }
func bindingFor(tm *Team) AttachmentBinding {
	return AttachmentBinding{ID: "binding", Identity: AttachmentIdentity{Runtime: "codex", HostID: "desktop", NativeID: "existing-chat", ProjectID: tm.ProjectID, ProjectRoot: tm.ProjectRoot, Profile: Profile{Runtime: "codex", Model: "custom-native-model", Effort: ""}}, Capabilities: AttachmentCapabilities{Queue: true, ObserveUserTurns: true, CorrelateResults: true, Wake: true}}
}
func attachFixture(t *testing.T, m *Manager, tm *Team, member string) (*attachedFixture, uint64) {
	t.Helper()
	s := newAttachedFixture()
	if err := m.Attach(context.Background(), tm.ID, member, bindingFor(tm), s); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Get(tm.ID)
	return s, findMember(got, member).Attachment.Generation
}
func observe(t *testing.T, m *Manager, team, member string, gen uint64, turn NativeTurn) {
	t.Helper()
	if err := m.ObserveNativeTurn(team, member, gen, turn); err != nil {
		t.Fatal(err)
	}
}
func attachedCall(t *testing.T, s *attachedFixture) submission {
	t.Helper()
	select {
	case c := <-s.calls:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("no attached input")
		return submission{}
	}
}
func noAttachedCall(t *testing.T, s *attachedFixture) {
	t.Helper()
	select {
	case c := <-s.calls:
		t.Fatalf("unexpected attached input %s", c.Input.ID)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestAttachmentPreservesNativeIdentityAndProfile(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	got, _ := m.Get(tm.ID)
	lead := findMember(got, "lead")
	if lead.NativeID != "existing-chat" || lead.Profile.Model != "custom-native-model" || lead.Profile.Effort != "" {
		t.Fatalf("native profile was replaced: %+v", lead)
	}
	enqueue(t, m, tm.ID, TaskRequest{ID: "attached-input", To: "lead", Text: "continue existing chat"})
	noAttachedCall(t, s)
	observe(t, m, tm.ID, "lead", g, NativeTurn{State: "idle"})
	c := attachedCall(t, s)
	got, _ = m.Get(tm.ID)
	if findMember(got, "lead").ActiveTaskID != "" {
		t.Fatal("queued native input gained active-turn authority")
	}
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "native-team-turn", InputID: c.Input.ID, Origin: "team", State: "running"})
	c.Reply <- fixtureReply{Result: Result{Text: "native result", NativeID: "existing-chat", Model: "custom-native-model"}}
	got = awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "attached-input") == "complete" })
	if got.Turns != 1 {
		t.Fatalf("turn count=%d", got.Turns)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.opens) != 0 {
		t.Fatal("attachment opened an owned session")
	}
}

func TestExternalUserTurnCanDelegateWithinScope(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	if err := m.BindWorkflow(tm.ID, "lead", "wf", "spec", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unobserved turn authorized: %v", err)
	}
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
	if err := m.BindWorkflow(tm.ID, "lead", "wf", "spec", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue(context.Background(), tm.ID, TaskRequest{From: "lead", To: "worker", Text: "escape", WorkflowID: "other"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("escaped workflow: %v", err)
	}
	child := enqueue(t, m, tm.ID, TaskRequest{ID: "child-external", From: "lead", To: "worker", Text: "bounded work", WorkflowID: "wf"})
	c := nextCall(t, a)
	complete(c, "worker result")
	got := awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "child-external") == "complete" })
	parent := findTask(got, child.ParentTaskID)
	if parent == nil || parent.Origin != "external" || parent.Kind != "external" || parent.StartedAt == nil {
		t.Fatalf("missing genuine external scope: %+v", parent)
	}
	noAttachedCall(t, s)
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "complete"})
	ret := attachedCall(t, s)
	if ret.Input.ID != "result:child-external" {
		t.Fatalf("wrong return %s", ret.Input.ID)
	}
	if _, err := m.Enqueue(context.Background(), tm.ID, TaskRequest{From: "lead", To: "worker", Text: "before native start"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("pending submission gained authority: %v", err)
	}
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "return-turn", InputID: ret.Input.ID, Origin: "team", State: "running"})
	ret.Reply <- fixtureReply{Result: Result{Text: "received"}}
}

func TestExternalTurnIsNotCountedAsAutomaticWork(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	_, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "complete", Result: &Result{Text: "human chat"}})
	got, _ := m.Get(tm.ID)
	if got.Turns != 0 || got.AutoWakeups != 0 || !got.UsageKnown {
		t.Fatalf("external turn used team budget: %+v", got)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].Origin != "external" || got.Tasks[0].State != "complete" {
		t.Fatalf("external receipt missing: %+v", got.Tasks)
	}
}

func TestAttachmentFencesStaleConnection(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	if err := m.Detach(context.Background(), tm.ID, "lead"); err != nil {
		t.Fatal(err)
	}
	s2, g2 := attachFixture(t, m, tm, "lead")
	_ = s2
	if g2 <= g {
		t.Fatal("generation did not advance")
	}
	if err := m.ObserveNativeTurn(tm.ID, "lead", g, NativeTurn{ID: "stale", Origin: "user", State: "running"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale connection accepted: %v", err)
	}
	if s.interrupts.Load() != 0 || s.closes.Load() != 1 {
		t.Fatal("detach changed native host lifecycle")
	}
}

func TestAttachmentStopDoesNotOwnNativeHost(t *testing.T) {
	for _, action := range []string{"stop", "close", "detach"} {
		t.Run(action, func(t *testing.T) {
			m, _ := testManager(t, newFixture())
			tm := createFixtureTeam(t, m, Limits{})
			s, g := attachFixture(t, m, tm, "lead")
			observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
			switch action {
			case "stop":
				_, _ = m.Action(context.Background(), tm.ID, "stop")
			case "close":
				_ = m.Close()
			case "detach":
				_ = m.Detach(context.Background(), tm.ID, "lead")
			}
			if s.interrupts.Load() != 0 || s.closes.Load() != 1 {
				t.Fatalf("native lifecycle changed: interrupts=%d closes=%d", s.interrupts.Load(), s.closes.Load())
			}
		})
	}
}

func TestAttachmentUniqueIdentityAndExplicitLead(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "joined")
	got, _ := m.Get(tm.ID)
	if findMember(got, "joined").Role != "member" {
		t.Fatal("new attachment silently became lead")
	}
	if err := m.Attach(context.Background(), tm.ID, "other", bindingFor(tm), newAttachedFixture()); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate native identity accepted: %v", err)
	}
	if err := m.SetLead(context.Background(), tm.ID, "joined"); err != nil {
		t.Fatal(err)
	}
	got, _ = m.Get(tm.ID)
	if CanonicalRole(findMember(got, "lead").Role) == "lead" || CanonicalRole(findMember(got, "joined").Role) != "lead" {
		t.Fatal("lead not canonical")
	}
	observe(t, m, tm.ID, "joined", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
	if err := m.SetLead(context.Background(), tm.ID, "lead"); !errors.Is(err, ErrConflict) {
		t.Fatalf("lead changed during active turn: %v", err)
	}
	noAttachedCall(t, s)
}

func TestAttachmentRestoreNeverOpensOwnedNativeSession(t *testing.T) {
	a := newFixture()
	m, db := testManager(t, a)
	tm := createFixtureTeam(t, m, Limits{})
	_, _ = attachFixture(t, m, tm, "lead")
	enqueue(t, m, tm.ID, TaskRequest{ID: "restored-input", To: "lead", Text: "queued"})
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	m2, err := NewManager(db, map[string]Adapter{"codex": a}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	_, _ = m2.Action(context.Background(), tm.ID, "resume")
	noCall(t, a)
	got, _ := m2.Get(tm.ID)
	lead := findMember(got, "lead")
	if lead.Attachment == nil || lead.Attachment.State != "disconnected" {
		t.Fatalf("restore forgot transport ownership: %+v", lead)
	}
}

func TestAttachmentRoutesIncompatibleRoleToOwnedWorker(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{State: "idle"})
	task := enqueue(t, m, tm.ID, TaskRequest{ID: "review-native", To: "lead", Role: "review", Text: "review"})
	if task.To == "lead" {
		t.Fatal("incompatible role mutated attached profile")
	}
	c := nextCall(t, a)
	complete(c, "reviewed")
	noAttachedCall(t, s)
}

func TestAttachmentAcknowledgementPreservesConcurrentHumanScope(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{State: "idle"})
	enqueue(t, m, tm.ID, TaskRequest{ID: "coalesced", To: "lead", Text: "team task", WorkflowID: "team-wf"})
	c := attachedCall(t, s)
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "real-human-turn", Origin: "user", State: "running"})
	if err := m.BindWorkflow(tm.ID, "lead", "human-wf", "spec", ""); err != nil {
		t.Fatal(err)
	}
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "real-human-turn", InputID: "coalesced", Origin: "team", State: "running"})
	got, _ := m.Get(tm.ID)
	lead := findMember(got, "lead")
	parent := findTask(got, lead.ActiveTaskID)
	if lead.WorkflowID != "human-wf" || parent == nil || parent.Origin != "external" {
		t.Fatalf("team ACK stole human scope: %+v", lead)
	}
	if err := m.BindWorkflowTask(tm.ID, "lead", "coalesced", "team-wf", "", ""); err != nil {
		t.Fatal(err)
	}
	child := enqueue(t, m, tm.ID, TaskRequest{ID: "coalesced-child", From: "lead", To: "worker", ParentTaskID: "coalesced", Text: "team-owned delegation", WorkflowID: "team-wf"})
	if child.ParentTaskID != "coalesced" {
		t.Fatal("lost explicit acknowledged scope")
	}
	worker := nextCall(t, a)
	complete(worker, "bounded result")
	c.Reply <- fixtureReply{Result: Result{Text: "team done"}}
	got = awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "coalesced") == "complete" })
	lead = findMember(got, "lead")
	if lead.WorkflowID != "human-wf" || findTask(got, lead.ActiveTaskID).Origin != "external" {
		t.Fatal("team completion cleared live human authority")
	}
}

func TestAttachmentUnacknowledgedResultIsUncertain(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{State: "idle"})
	enqueue(t, m, tm.ID, TaskRequest{ID: "unacknowledged", To: "lead", Text: "team task"})
	c := attachedCall(t, s)
	c.Reply <- fixtureReply{Result: Result{Text: "unverified result"}}
	got := awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "unacknowledged") == "needs_user" })
	if got.Turns != 0 {
		t.Fatal("unacknowledged input counted as native turn")
	}
}

func TestAttachmentExpiredConnectionCannotDispatchOrDelegate(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	s := newAttachedFixture()
	b := bindingFor(tm)
	expires := time.Now().Add(time.Hour)
	b.ExpiresAt = &expires
	if err := m.Attach(context.Background(), tm.ID, "lead", b, s); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Get(tm.ID)
	g := findMember(got, "lead").Attachment.Generation
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
	m.mu.Lock()
	expired := time.Now().Add(-time.Second)
	findMember(m.teams[tm.ID].team, "lead").Attachment.ExpiresAt = &expired
	m.mu.Unlock()
	if _, err := m.Enqueue(context.Background(), tm.ID, TaskRequest{From: "lead", To: "worker", Text: "expired"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expired connection authorized: %v", err)
	}
	m.expireAttachments()
	got = awaitTeam(t, m, tm.ID, func(tm *Team) bool { return findMember(tm, "lead").Attachment.State == "expired" })
	if findMember(got, "lead").State != "needs_user" {
		t.Fatal("expiry not visible")
	}
	if s.interrupts.Load() != 0 {
		t.Fatal("expiry interrupted native host")
	}
}

func TestAttachmentReservesBudgetBeforeNativeAcknowledgement(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{MaxTurns: 1})
	s, g := attachFixture(t, m, tm, "lead")
	second := newAttachedFixture()
	b := bindingFor(tm)
	b.ID = "binding-2"
	b.Identity.NativeID = "second-chat"
	if err := m.Attach(context.Background(), tm.ID, "worker", b, second); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Get(tm.ID)
	g2 := findMember(got, "worker").Attachment.Generation
	observe(t, m, tm.ID, "lead", g, NativeTurn{State: "idle"})
	observe(t, m, tm.ID, "worker", g2, NativeTurn{State: "idle"})
	enqueue(t, m, tm.ID, TaskRequest{ID: "reserve-one", To: "lead", Text: "one"})
	first := attachedCall(t, s)
	enqueue(t, m, tm.ID, TaskRequest{ID: "reserve-two", To: "worker", Text: "two"})
	noAttachedCall(t, second)
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "native-one", InputID: first.Input.ID, Origin: "team", State: "running"})
	first.Reply <- fixtureReply{Result: Result{Text: "done"}}
	got = awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "reserve-one") == "complete" })
	if got.Turns != 1 || taskState(got, "reserve-two") != "queued" {
		t.Fatal("budget reservation lost")
	}
}

func TestAttachmentTaskResultDoesNotInventNativeTurnCompletion(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{State: "idle"})
	enqueue(t, m, tm.ID, TaskRequest{ID: "first-result", To: "lead", Text: "first"})
	first := attachedCall(t, s)
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "still-running", InputID: first.Input.ID, Origin: "team", State: "running"})
	enqueue(t, m, tm.ID, TaskRequest{ID: "after-result", To: "lead", Text: "next"})
	first.Reply <- fixtureReply{Result: Result{Text: "explicit task result while native turn runs"}}
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "first-result") == "complete" })
	noAttachedCall(t, s)
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "still-running", InputID: first.Input.ID, Origin: "team", State: "complete"})
	next := attachedCall(t, s)
	if next.Input.ID != "after-result" {
		t.Fatal("wrong queued input")
	}
}

func TestAttachmentStaleStartingRunCannotClearNewHumanScope(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	_, _ = attachFixture(t, m, tm, "lead")
	_, _ = m.Action(context.Background(), tm.ID, "pause")
	enqueue(t, m, tm.ID, TaskRequest{ID: "stale-start", To: "lead", Text: "old queued work"})
	m.mu.Lock()
	old := m.slots[slotKey(tm.ID, "lead")]
	old.busy = true
	old.taskID = "stale-start"
	findTask(m.teams[tm.ID].team, "stale-start").State = "starting"
	m.active++
	m.wg.Add(1)
	m.mu.Unlock()
	if err := m.Detach(context.Background(), tm.ID, "lead"); err != nil {
		t.Fatal(err)
	}
	_, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "fresh-human", Origin: "user", State: "running"})
	m.run(tm.ID, "lead", "stale-start", old)
	got, _ := m.Get(tm.ID)
	lead := findMember(got, "lead")
	if lead.ActiveTaskID == "" || lead.NativeTurn.ID != "fresh-human" || lead.State != "working" {
		t.Fatalf("stale run changed new scope: %+v", lead)
	}
}

func TestCreateAttachedUnknownProfileAndAtomicClaim(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	root := t.TempDir()
	tm := &Team{ProjectID: "attached-project", ProjectRoot: root}
	b := bindingFor(tm)
	request := CreateRequest{ProjectID: tm.ProjectID, ProjectRoot: root, Members: []MemberConfig{{ID: "native-lead", Role: "lead", Profile: Profile{Runtime: "codex"}}}}
	b.Identity.Profile = Profile{Runtime: "codex"}
	got, err := m.CreateAttached(context.Background(), request, "native-lead", b, newAttachedFixture())
	if err != nil {
		t.Fatal(err)
	}
	lead := findMember(got, "native-lead")
	if lead.Attachment == nil || lead.NativeID != "existing-chat" || lead.Profile.Model != "" || lead.Profile.Effort != "" {
		t.Fatalf("native profile replaced: %+v", lead)
	}
	a.mu.Lock()
	checks := a.preflights
	opens := len(a.opens)
	a.mu.Unlock()
	if checks != 0 || opens != 0 {
		t.Fatal("attached lead used owned model validation")
	}
	request.ProjectID = "another-project"
	b.Identity.ProjectID = request.ProjectID
	if _, err = m.CreateAttached(context.Background(), request, "native-lead", b, newAttachedFixture()); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate native claim accepted: %v", err)
	}
	all, _ := m.List("")
	if len(all) != 1 {
		t.Fatal("failed attach left orphan team")
	}
}

func TestAttachmentReconnectCannotRedirectExistingChatWork(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	_, _ = attachFixture(t, m, tm, "lead")
	enqueue(t, m, tm.ID, TaskRequest{ID: "original-chat", To: "lead", Text: "private work for original chat"})
	if err := m.Detach(context.Background(), tm.ID, "lead"); err != nil {
		t.Fatal(err)
	}
	b := bindingFor(tm)
	b.Identity.NativeID = "different-chat"
	if err := m.Attach(context.Background(), tm.ID, "lead", b, newAttachedFixture()); !errors.Is(err, ErrConflict) {
		t.Fatalf("redirected existing chat: %v", err)
	}
}

func TestAttachmentPendingInputDoesNotOverwriteHumanWorkflow(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
	if err := m.BindWorkflow(tm.ID, "lead", "human-wf", "spec", ""); err != nil {
		t.Fatal(err)
	}
	enqueue(t, m, tm.ID, TaskRequest{ID: "same-workflow", To: "lead", Text: "same workflow task", WorkflowID: "human-wf"})
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "needs_user"})
	noAttachedCall(t, s)
	got, _ := m.Get(tm.ID)
	if findMember(got, "lead").WorkflowID != "human-wf" {
		t.Fatal("queued input changed native workflow")
	}
}

func TestAttachmentNativeTerminalCannotClearUncertainResult(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{State: "idle"})
	enqueue(t, m, tm.ID, TaskRequest{ID: "uncertain-native", To: "lead", Text: "uncertain"})
	c := attachedCall(t, s)
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "uncertain-turn", InputID: c.Input.ID, Origin: "team", State: "running"})
	c.Reply <- fixtureReply{Err: errors.New("result connection lost")}
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "uncertain-native") == "needs_user" })
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "uncertain-turn", InputID: c.Input.ID, Origin: "team", State: "complete"})
	got, _ := m.Get(tm.ID)
	if findMember(got, "lead").State != "needs_user" {
		t.Fatal("native idle cleared uncertain work")
	}
}

func TestAttachmentHumanTurnsCannotClearUncertainDelivery(t *testing.T) {
	for _, coalesced := range []bool{true, false} {
		t.Run(fmt.Sprintf("coalesced=%t", coalesced), func(t *testing.T) {
			m, _ := testManager(t, newFixture())
			tm := createFixtureTeam(t, m, Limits{})
			s, g := attachFixture(t, m, tm, "lead")
			observe(t, m, tm.ID, "lead", g, NativeTurn{State: "idle"})
			enqueue(t, m, tm.ID, TaskRequest{ID: "uncertain-human", To: "lead", Text: "uncertain"})
			call := attachedCall(t, s)
			if coalesced {
				observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
			}
			call.Reply <- fixtureReply{Err: errors.New("explicit result was lost")}
			awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "uncertain-human") == "needs_user" })
			// Both repeated human observations and a new manual turn preserve
			// the delivery hold while retaining genuine manual authority.
			observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
			got, _ := m.Get(tm.ID)
			member := findMember(got, "lead")
			if member.State != "needs_user" || member.Error == "" {
				t.Fatalf("manual activity erased uncertainty: %+v", member)
			}
			if task := findTask(got, member.ActiveTaskID); task == nil || task.Origin != "external" || task.State != "running" {
				t.Fatal("uncertainty hold erased the genuine human assignment")
			}
			if err := m.BindWorkflow(tm.ID, "lead", "manual-workflow", "spec", "simple"); err != nil {
				t.Fatalf("manual workflow was blocked by automatic delivery hold: %v", err)
			}
			observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "complete"})
			got, _ = m.Get(tm.ID)
			if findMember(got, "lead").State != "needs_user" {
				t.Fatal("human completion erased uncertain delivery hold")
			}
			enqueue(t, m, tm.ID, TaskRequest{ID: "after-uncertain-human", To: "lead", Text: "must wait for reconciliation"})
			m.dispatchOne()
			got, _ = m.Get(tm.ID)
			if taskState(got, "after-uncertain-human") != "queued" {
				t.Fatal("automatic delivery resumed without reconciliation")
			}
		})
	}
}

func TestCreateRejectsMultipleCoordinators(t *testing.T) {
	m, _ := testManager(t, newFixture())
	_, err := m.Create(context.Background(), CreateRequest{ProjectID: "multiple-leads", ProjectRoot: t.TempDir(), Members: []MemberConfig{{ID: "one", Role: "lead", Profile: Profile{Runtime: "codex"}}, {ID: "two", Role: "coordinator", Profile: Profile{Runtime: "codex"}}}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("multiple canonical coordinators accepted: %v", err)
	}
}

func TestAttachmentHumanRequestRoutesThroughSoleLeadAndReturns(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm := createFixtureTeam(t, m, Limits{})
	enqueue(t, m, tm.ID, TaskRequest{ID: "old-lead-work", To: "lead", WorkflowID: "old-workflow", Text: "old work"})
	old := nextCall(t, a)
	complete(old, "done")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "old-lead-work") == "complete" })
	s, g := attachFixture(t, m, tm, "requester")
	if _, err := m.RequestFromAttached(context.Background(), tm.ID, "requester", "too-early", "request"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unobserved native user authorized: %v", err)
	}
	observe(t, m, tm.ID, "requester", g, NativeTurn{ID: "human-request", Origin: "user", State: "running"})
	task, err := m.RequestFromAttached(context.Background(), tm.ID, "requester", "new-team-request", "solve this together")
	if err != nil {
		t.Fatal(err)
	}
	if task.To != "lead" || task.ReturnTo != "requester" || task.WorkflowID != "" || task.ParentTaskID == "" {
		t.Fatalf("request lost origin/lead: %+v", task)
	}
	work := nextCall(t, a)
	if work.Spec.MemberID != "lead" {
		t.Fatal("request created second coordinator")
	}
	complete(work, "team result")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "new-team-request") == "complete" })
	noAttachedCall(t, s)
	observe(t, m, tm.ID, "requester", g, NativeTurn{ID: "human-request", Origin: "user", State: "complete"})
	ret := attachedCall(t, s)
	if ret.Input.ID != "result:new-team-request" {
		t.Fatalf("wrong requester response: %s", ret.Input.ID)
	}
}

func TestAttachmentHumanRequestCannotForwardBoundWorkflow(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	_, g := attachFixture(t, m, tm, "requester")
	observe(t, m, tm.ID, "requester", g, NativeTurn{ID: "human-bound", Origin: "user", State: "running"})
	m.mu.Lock()
	member := findMember(m.teams[tm.ID].team, "requester")
	findTask(m.teams[tm.ID].team, member.ActiveTaskID).WorkflowID = "existing-user-workflow"
	m.mu.Unlock()
	if _, err := m.RequestFromAttached(context.Background(), tm.ID, "requester", "bad-forward", "forward my workflow"); !errors.Is(err, ErrConflict) {
		t.Fatalf("workflow owner escaped: %v", err)
	}
}

func TestAttachmentAbortedSubmissionPreservesConcurrentHumanTurn(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	_, g := attachFixture(t, m, tm, "lead")
	_, _ = m.Action(context.Background(), tm.ID, "pause")
	enqueue(t, m, tm.ID, TaskRequest{ID: "paused-start", To: "lead", Text: "queued task"})
	m.mu.Lock()
	slot := m.slots[slotKey(tm.ID, "lead")]
	slot.busy = true
	slot.taskID = "paused-start"
	findTask(m.teams[tm.ID].team, "paused-start").State = "starting"
	m.active++
	m.wg.Add(1)
	m.mu.Unlock()
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "concurrent-human", Origin: "user", State: "running"})
	m.run(tm.ID, "lead", "paused-start", slot)
	got, _ := m.Get(tm.ID)
	lead := findMember(got, "lead")
	if lead.ActiveTaskID == "" || lead.State != "working" {
		t.Fatalf("aborted team input cleared human authority: %+v", lead)
	}
}

func TestAttachmentNativeDisconnectClosesOnlyTransport(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
	observe(t, m, tm.ID, "lead", g, NativeTurn{State: "disconnected"})
	if s.interrupts.Load() != 0 || s.closes.Load() != 1 {
		t.Fatalf("disconnect did not release only our transport: interrupts=%d closes=%d", s.interrupts.Load(), s.closes.Load())
	}
}

func TestAttachmentUnknownNativeTurnHasReadinessWithoutAuthority(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "old", Origin: "unknown", State: "complete"})
	for _, state := range []string{"running", "waiting", "needs_user"} {
		observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "unknown-turn", Origin: "unknown", State: state})
		got, _ := m.Get(tm.ID)
		if len(got.Tasks) != 0 || findMember(got, "lead").ActiveTaskID != "" {
			t.Fatal("unknown origin granted assignment")
		}
		if err := m.BindWorkflow(tm.ID, "lead", "wf", "spec", ""); !errors.Is(err, ErrForbidden) {
			t.Fatalf("unknown workflow authority: %v", err)
		}
	}
	enqueue(t, m, tm.ID, TaskRequest{ID: "waiting-for-native", To: "lead", Text: "team work"})
	noAttachedCall(t, s)
	if err := m.ObserveNativeTurn(tm.ID, "lead", g, NativeTurn{ID: "different", Origin: "unknown", State: "complete"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("unrelated terminal cleared active native turn: %v", err)
	}
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "unknown-turn", Origin: "unknown", State: "complete"})
	call := attachedCall(t, s)
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "team-turn", Origin: "unknown", State: "running"})
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "team-turn", InputID: call.Input.ID, Origin: "team", State: "running"})
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "team-turn", Origin: "unknown", State: "running"})
	got, _ := m.Get(tm.ID)
	lead := findMember(got, "lead")
	if lead.NativeTurn.Origin != "team" || lead.NativeTurn.InputID != call.Input.ID || lead.ActiveTaskID != call.Input.ID {
		t.Fatalf("partial observation erased ACK %+v", lead)
	}
	call.Reply <- fixtureReply{Result: Result{Text: "explicit result"}}
}
func TestAttachmentUnknownSnapshotPreservesObservedHumanScope(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm := createFixtureTeam(t, m, Limits{})
	_, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "unknown", State: "running"})
	if err := m.BindWorkflow(tm.ID, "lead", "wf", "spec", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Get(tm.ID)
	taskID := findMember(got, "lead").ActiveTaskID
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "human", Origin: "unknown", State: "complete"})
	got, _ = m.Get(tm.ID)
	if findTask(got, taskID).State != "complete" || findMember(got, "lead").NativeTurn.Origin != "user" || len(got.Tasks) != 1 {
		t.Fatalf("human lifecycle lost %+v", got)
	}
}

func TestAttachmentUnknownActivityCannotClearUncertainResult(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	s, g := attachFixture(t, m, tm, "lead")
	observe(t, m, tm.ID, "lead", g, NativeTurn{State: "idle"})
	enqueue(t, m, tm.ID, TaskRequest{ID: "uncertain-unknown", To: "lead", Text: "uncertain"})
	call := attachedCall(t, s)
	call.Reply <- fixtureReply{Err: errors.New("delivery lost")}
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "uncertain-unknown") == "needs_user" })
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "unverified-activity", Origin: "unknown", State: "running"})
	observe(t, m, tm.ID, "lead", g, NativeTurn{ID: "unverified-activity", Origin: "unknown", State: "complete"})
	got, _ := m.Get(tm.ID)
	if findMember(got, "lead").State != "needs_user" {
		t.Fatal("unknown activity cleared uncertain delivery")
	}
}
