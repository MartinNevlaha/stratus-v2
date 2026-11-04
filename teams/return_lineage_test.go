package teams

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func forwardedFixture(t *testing.T, limits Limits) (*Manager, *fixtureAdapter, *Team, *attachedFixture, uint64, submission) {
	t.Helper()
	a := newFixture()
	m, _ := testManager(t, a)
	tm := createFixtureTeam(t, m, limits)
	s, g := attachFixture(t, m, tm, "requester")
	observe(t, m, tm.ID, "requester", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
	if _, err := m.RequestFromAttached(context.Background(), tm.ID, "requester", "request", "solve the request using delegated work"); err != nil {
		t.Fatal(err)
	}
	return m, a, tm, s, g, nextCall(t, a)
}

func assertRequestWaiting(t *testing.T, m *Manager, teamID string) {
	t.Helper()
	got := awaitTeam(t, m, teamID, func(tm *Team) bool { return taskState(tm, "request") != "running" })
	if request := findTask(got, "request"); request.State != "waiting" || request.FinishedAt != nil {
		t.Fatalf("request completed before delegated results were consumed: %+v", request)
	}
	if ret := findTask(got, "result:request"); ret != nil {
		t.Fatalf("premature requester result: %+v", ret)
	}
}

func TestForwardedRequestReturnsOnlyFinalSynthesis(t *testing.T) {
	for _, childFirst := range []bool{false, true} {
		name := "parent-turn-first"
		if childFirst {
			name = "child-turn-first"
		}
		t.Run(name, func(t *testing.T) {
			m, a, tm, s, g, lead := forwardedFixture(t, Limits{})
			enqueue(t, m, tm.ID, TaskRequest{ID: "child", From: "lead", To: "worker", Text: "produce evidence"})
			child := nextCall(t, a)
			if childFirst {
				complete(child, "worker evidence")
				awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "child") == "complete" })
			}
			complete(lead, "delegated work is pending")
			assertRequestWaiting(t, m, tm.ID)
			if !childFirst {
				complete(child, "worker evidence")
			}
			continuation := nextCall(t, a)
			if continuation.Input.ID != "result:child" {
				t.Fatalf("wrong continuation: %s", continuation.Input.ID)
			}
			complete(continuation, "FINAL REQUESTER ANSWER")
			got := awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "request") == "complete" })
			ret := findTask(got, "result:request")
			if ret == nil || ret.Text != "FINAL REQUESTER ANSWER" || ret.To != "requester" || findTask(got, "request").Result != ret.Text {
				t.Fatalf("final synthesis did not return to original requester: %+v", ret)
			}
			observe(t, m, tm.ID, "requester", g, NativeTurn{ID: "human", Origin: "user", State: "complete"})
			returned := attachedCall(t, s)
			if returned.Input.ID != "result:request" || !strings.Contains(returned.Input.Text, "FINAL REQUESTER ANSWER") {
				t.Fatalf("wrong final delivery: %+v", returned.Input)
			}
			observe(t, m, tm.ID, "requester", g, NativeTurn{ID: "return", InputID: returned.Input.ID, Origin: "team", State: "running"})
			returned.Reply <- fixtureReply{Result: Result{Text: "presented final answer"}}
			awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "result:request") == "complete" })
			noAttachedCall(t, s)
			noCall(t, a)
		})
	}
}

func TestForwardedRequestWaitsForEveryChild(t *testing.T) {
	m, a, tm, _, _, lead := forwardedFixture(t, Limits{MaxConcurrent: 3})
	enqueue(t, m, tm.ID, TaskRequest{ID: "first", From: "lead", To: "worker", Text: "first evidence"})
	first := nextCall(t, a)
	enqueue(t, m, tm.ID, TaskRequest{ID: "second", From: "lead", Role: "review", Text: "second evidence"})
	second := nextCall(t, a)
	complete(lead, "delegated")
	assertRequestWaiting(t, m, tm.ID)
	complete(first, "first artifact")
	partial := nextCall(t, a)
	complete(partial, "first artifact accepted; another remains")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "result:first") == "complete" })
	assertRequestWaiting(t, m, tm.ID)
	complete(second, "second artifact")
	final := nextCall(t, a)
	complete(final, "combined final answer")
	got := awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "request") == "complete" })
	if ret := findTask(got, "result:request"); ret == nil || ret.Text != "combined final answer" {
		t.Fatalf("wrong requester result: %+v", ret)
	}
}

func TestForwardedRequestPreservesNestedAndContinuedDelegation(t *testing.T) {
	m, a, tm, _, _, lead := forwardedFixture(t, Limits{MaxConcurrent: 3, MaxAutoRounds: 8})
	enqueue(t, m, tm.ID, TaskRequest{ID: "child", From: "lead", To: "worker", Text: "coordinate subtask"})
	child := nextCall(t, a)
	enqueue(t, m, tm.ID, TaskRequest{ID: "grandchild", From: "worker", Role: "review", Text: "inspect evidence"})
	grandchild := nextCall(t, a)
	complete(lead, "delegated")
	complete(child, "subtask delegated")
	assertRequestWaiting(t, m, tm.ID)
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "child") == "waiting" })
	complete(grandchild, "review artifact")
	childContinuation := nextCall(t, a)
	if childContinuation.Input.ID != "result:grandchild" {
		t.Fatalf("wrong child continuation: %s", childContinuation.Input.ID)
	}
	enqueue(t, m, tm.ID, TaskRequest{ID: "followup", From: "worker", Role: "mechanical", Text: "verify the final detail"})
	followup := nextCall(t, a)
	complete(childContinuation, "verification pending")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "result:grandchild") == "waiting" })
	assertRequestWaiting(t, m, tm.ID)
	complete(followup, "verified final detail")
	childFinal := nextCall(t, a)
	complete(childFinal, "nested worker final")
	leadFinal := nextCall(t, a)
	if leadFinal.Input.ID != "result:child" || !strings.Contains(leadFinal.Input.Text, "nested worker final") {
		t.Fatalf("lost nested result: %+v", leadFinal.Input)
	}
	complete(leadFinal, "final combined answer")
	got := awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "request") == "complete" })
	if ret := findTask(got, "result:request"); ret == nil || ret.Text != "final combined answer" {
		t.Fatalf("wrong requester result: %+v", ret)
	}
}

func TestForwardedRequestRetainsChildFailure(t *testing.T) {
	m, a, tm, _, _, lead := forwardedFixture(t, Limits{})
	enqueue(t, m, tm.ID, TaskRequest{ID: "child", From: "lead", To: "worker", Text: "produce evidence"})
	child := nextCall(t, a)
	complete(lead, "delegated")
	assertRequestWaiting(t, m, tm.ID)
	child.Reply <- fixtureReply{Err: errors.New("native delivery lost")}
	continuation := nextCall(t, a)
	if !strings.Contains(continuation.Input.Text, "needs user attention") {
		t.Fatalf("lost failure: %s", continuation.Input.Text)
	}
	complete(continuation, "cannot verify the requested change")
	got := awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "request") == "needs_user" })
	ret := findTask(got, "result:request")
	if ret == nil || !strings.Contains(ret.Text, "native delivery lost") || !strings.Contains(ret.Text, "cannot verify the requested change") {
		t.Fatalf("failure omitted from requester response: %+v", ret)
	}
	if findMember(got, "worker").State != "needs_user" {
		t.Fatal("uncertain worker was released")
	}
	noCall(t, a)
}

func TestDashboardDefaultRecipientReusesCanonicalLead(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm := createFixtureTeam(t, m, Limits{})
	enqueue(t, m, tm.ID, TaskRequest{ID: "old", To: "lead", WorkflowID: "prior-workflow", Text: "old work"})
	complete(nextCall(t, a), "old completed")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "old") == "complete" })
	next := enqueue(t, m, tm.ID, TaskRequest{ID: "next", Text: "new dashboard work"})
	got, _ := m.Get(tm.ID)
	leaders := 0
	for _, member := range got.Members {
		if CanonicalRole(member.Role) == "lead" {
			leaders++
		}
	}
	if leaders != 1 || next.To != "lead" {
		t.Fatalf("dashboard request cloned canonical lead: leaders=%d target=%s", leaders, next.To)
	}
	complete(nextCall(t, a), "new completed")
}

func TestIncompatibleLeadProfileDoesNotCloneCanonicalLead(t *testing.T) {
	m, _ := testManager(t, newFixture())
	tm := createFixtureTeam(t, m, Limits{})
	_, err := m.Enqueue(context.Background(), tm.ID, TaskRequest{ID: "new-profile", Text: "run with another lead profile", Profile: &Profile{Runtime: "codex", Model: "gpt-6-astra", Effort: "high"}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("incompatible lead profile should require explicit decision: %v", err)
	}
	got, _ := m.Get(tm.ID)
	if len(got.Members) != len(tm.Members) {
		t.Fatal("incompatible request created a second lead")
	}
}

func TestForwardedRequestsDoNotBatchDifferentRoots(t *testing.T) {
	m, a, tm, _, _, firstLead := forwardedFixture(t, Limits{})
	if _, err := m.RequestFromAttached(context.Background(), tm.ID, "requester", "request-two", "a separate request"); err != nil {
		t.Fatal(err)
	}
	enqueue(t, m, tm.ID, TaskRequest{ID: "first-child", From: "lead", To: "worker", Text: "first evidence"})
	complete(nextCall(t, a), "FIRST EVIDENCE")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "first-child") == "complete" })
	complete(firstLead, "delegated first request")
	secondLead := nextCall(t, a)
	if secondLead.Input.ID != "request-two" {
		t.Fatalf("wrong second request: %s", secondLead.Input.ID)
	}
	enqueue(t, m, tm.ID, TaskRequest{ID: "second-child", From: "lead", To: "worker", Text: "second evidence"})
	complete(nextCall(t, a), "SECOND EVIDENCE")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "second-child") == "complete" })
	complete(secondLead, "delegated second request")
	firstFinal := nextCall(t, a)
	if firstFinal.Input.ID != "result:first-child" || strings.Contains(firstFinal.Input.Text, "SECOND EVIDENCE") {
		t.Fatalf("unrelated request roots batched: %+v", firstFinal.Input)
	}
	complete(firstFinal, "FIRST ANSWER")
	secondFinal := nextCall(t, a)
	if secondFinal.Input.ID != "result:second-child" {
		t.Fatalf("lost second request continuation: %s", secondFinal.Input.ID)
	}
	complete(secondFinal, "SECOND ANSWER")
	got := awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "request-two") == "complete" })
	if findTask(got, "result:request").Text != "FIRST ANSWER" || findTask(got, "result:request-two").Text != "SECOND ANSWER" {
		t.Fatal("request answers crossed")
	}
}

func TestForwardedRequestAttachedLeadWaitsForNativeTerminal(t *testing.T) {
	a := newFixture()
	m, _ := testManager(t, a)
	tm := createFixtureTeam(t, m, Limits{})
	_, requesterGeneration := attachFixture(t, m, tm, "requester")
	leadSession := newAttachedFixture()
	binding := bindingFor(tm)
	binding.ID, binding.Identity.NativeID = "lead-binding", "lead-chat"
	if err := m.Attach(context.Background(), tm.ID, "lead", binding, leadSession); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Get(tm.ID)
	leadGeneration := findMember(got, "lead").Attachment.Generation
	observe(t, m, tm.ID, "lead", leadGeneration, NativeTurn{State: "idle"})
	observe(t, m, tm.ID, "requester", requesterGeneration, NativeTurn{ID: "human", Origin: "user", State: "running"})
	if _, err := m.RequestFromAttached(context.Background(), tm.ID, "requester", "request", "delegate this request"); err != nil {
		t.Fatal(err)
	}
	lead := attachedCall(t, leadSession)
	observe(t, m, tm.ID, "lead", leadGeneration, NativeTurn{ID: "lead-turn", InputID: lead.Input.ID, Origin: "team", State: "running"})
	enqueue(t, m, tm.ID, TaskRequest{ID: "child", From: "lead", To: "worker", Text: "produce evidence"})
	child := nextCall(t, a)
	lead.Reply <- fixtureReply{Result: Result{Text: "delegated"}}
	assertRequestWaiting(t, m, tm.ID)
	complete(child, "worker evidence")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "child") == "complete" })
	noAttachedCall(t, leadSession)
	observe(t, m, tm.ID, "lead", leadGeneration, NativeTurn{ID: "lead-turn", InputID: lead.Input.ID, Origin: "team", State: "complete"})
	continuation := attachedCall(t, leadSession)
	observe(t, m, tm.ID, "lead", leadGeneration, NativeTurn{ID: "lead-final", InputID: continuation.Input.ID, Origin: "team", State: "running"})
	continuation.Reply <- fixtureReply{Result: Result{Text: "attached lead final"}}
	got = awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "request") == "complete" })
	if ret := findTask(got, "result:request"); ret == nil || ret.Text != "attached lead final" {
		t.Fatalf("lost attached final: %+v", ret)
	}
}

func TestForwardedRequestResumesWaitingLineageAfterRestart(t *testing.T) {
	a := newFixture()
	m, db := testManager(t, a)
	tm := createFixtureTeam(t, m, Limits{})
	_, g := attachFixture(t, m, tm, "requester")
	observe(t, m, tm.ID, "requester", g, NativeTurn{ID: "human", Origin: "user", State: "running"})
	if _, err := m.RequestFromAttached(context.Background(), tm.ID, "requester", "request", "delegate this request"); err != nil {
		t.Fatal(err)
	}
	lead := nextCall(t, a)
	enqueue(t, m, tm.ID, TaskRequest{ID: "child", From: "lead", To: "worker", Text: "produce evidence"})
	child := nextCall(t, a)
	if _, err := m.Action(context.Background(), tm.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	complete(lead, "delegated")
	complete(child, "durable evidence")
	awaitTeam(t, m, tm.ID, func(tm *Team) bool {
		return taskState(tm, "child") == "complete" && taskState(tm, "request") == "waiting"
	})
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewManager(db, map[string]Adapter{"codex": a}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	assertRequestWaiting(t, restarted, tm.ID)
	if _, err := restarted.Action(context.Background(), tm.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	continuation := nextCall(t, a)
	if continuation.Input.ID != "result:child" {
		t.Fatalf("replayed native turn instead of queued return: %s", continuation.Input.ID)
	}
	complete(continuation, "final after restart")
	got := awaitTeam(t, restarted, tm.ID, func(tm *Team) bool { return taskState(tm, "request") == "complete" })
	if ret := findTask(got, "result:request"); ret == nil || ret.Text != "final after restart" {
		t.Fatalf("lost durable requester destination: %+v", ret)
	}
}

func TestForwardedRequestPropagatesBlockedReturn(t *testing.T) {
	m, a, tm, _, _, lead := forwardedFixture(t, Limits{MaxAutoRounds: 2})
	enqueue(t, m, tm.ID, TaskRequest{ID: "child", From: "lead", To: "worker", Text: "produce evidence"})
	child := nextCall(t, a)
	complete(lead, "delegated")
	assertRequestWaiting(t, m, tm.ID)
	complete(child, "OK")
	got := awaitTeam(t, m, tm.ID, func(tm *Team) bool { return taskState(tm, "request") == "needs_user" })
	if got.State != "paused" || !strings.Contains(findTask(got, "request").Error, "Automatic exchange limit") {
		t.Fatalf("blocked child return lost attention: %+v", findTask(got, "request"))
	}
	noCall(t, a)
}

func TestStoppingForwardedRequestCancelsWaitingWork(t *testing.T) {
	m, a, tm, _, _, lead := forwardedFixture(t, Limits{})
	enqueue(t, m, tm.ID, TaskRequest{ID: "child", From: "lead", To: "worker", Text: "produce evidence"})
	nextCall(t, a)
	complete(lead, "delegated")
	assertRequestWaiting(t, m, tm.ID)
	got, err := m.Action(context.Background(), tm.ID, "stop")
	if err != nil {
		t.Fatal(err)
	}
	if taskState(got, "request") != "cancelled" {
		t.Fatalf("stopped request left waiting: %+v", findTask(got, "request"))
	}
}
