package db

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var ctx = context.Background()

// openCommsDB uses a file DB: ":memory:" gives every pooled connection its own database.
func openCommsDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "comms.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func mustPeer(t *testing.T, d *DB, key, runtime string) *Peer {
	t.Helper()
	p, err := d.UpsertPeer(key, runtime, "proj")
	if err != nil {
		t.Fatalf("upsert peer %s: %v", key, err)
	}
	return p
}

func TestUpsertPeer_AssignsRuntimeHandle(t *testing.T) {
	d := openCommsDB(t)
	cases := map[string]string{"s1": "claude-code", "codex:n1": "codex", "opencode:o1": "opencode"}
	prefixes := map[string]string{"claude-code": "claude-", "codex": "codex-", "opencode": "opencode-"}
	for key, rt := range cases {
		p := mustPeer(t, d, key, rt)
		if !strings.HasPrefix(p.Handle, prefixes[rt]) || p.Runtime != rt || !p.Active {
			t.Errorf("peer %s = %+v", key, p)
		}
	}
}

func TestUpsertPeer_ReRegisterKeepsHandleAndReactivates(t *testing.T) {
	d := openCommsDB(t)
	p := mustPeer(t, d, "s1", "claude-code")
	if err := d.EndPeer("s1"); err != nil {
		t.Fatal(err)
	}
	again := mustPeer(t, d, "s1", "claude-code")
	if again.Handle != p.Handle || !again.Active || again.EndedAt != nil {
		t.Fatalf("re-register: %+v", again)
	}
}

func TestUpsertPeer_InfersRuntimeFromKey(t *testing.T) {
	d := openCommsDB(t)
	if p := mustPeer(t, d, "codex:abc", ""); p.Runtime != "codex" {
		t.Fatalf("runtime = %s", p.Runtime)
	}
	if p := mustPeer(t, d, "plain", ""); p.Runtime != "claude-code" {
		t.Fatalf("runtime = %s", p.Runtime)
	}
}

func TestRenamePeer_Validation(t *testing.T) {
	d := openCommsDB(t)
	mustPeer(t, d, "s1", "claude-code")
	mustPeer(t, d, "s2", "codex")
	if _, err := d.RenamePeer("s1", "backend"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := d.RenamePeer("s2", "backend"); !errors.Is(err, ErrHandleTaken) {
		t.Fatalf("duplicate: %v", err)
	}
	for _, bad := range []string{"Bad Name", "codex", "user", "*", "x", strings.Repeat("a", 40)} {
		if _, err := d.RenamePeer("s2", bad); !errors.Is(err, ErrInvalidHandle) {
			t.Errorf("handle %q: %v", bad, err)
		}
	}
	if _, err := d.RenamePeer("nope", "fine-name"); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("missing peer: %v", err)
	}
}

func TestSendMessage_ResolvesRecipients(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "s1", "claude-code")
	mustPeer(t, d, "codex:1", "codex")
	mustPeer(t, d, "codex:2", "codex")
	o := mustPeer(t, d, "opencode:1", "opencode")

	tests := []struct {
		to   string
		want int
	}{
		{o.Handle, 1},
		{"opencode:1", 1},
		{"codex", 2},
		{"*", 3}, // excludes the sender
		{"user", 0},
	}
	for _, tc := range tests {
		_, recips, err := d.SendMessage(a.SessionKey, tc.to, "", "hi", nil)
		if err != nil {
			t.Fatalf("send to %s: %v", tc.to, err)
		}
		if len(recips) != tc.want {
			t.Errorf("to %s: %d recipients, want %d", tc.to, len(recips), tc.want)
		}
	}
	if _, _, err := d.SendMessage(a.SessionKey, "ghost", "", "hi", nil); !errors.Is(err, ErrUnknownRecipient) {
		t.Fatalf("unknown: %v", err)
	}
	d.EndPeer(o.SessionKey)
	if _, _, err := d.SendMessage(a.SessionKey, "opencode", "", "hi", nil); !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("no active opencode: %v", err)
	}
	if _, _, err := d.SendMessage(a.SessionKey, "codex", "bogus", "hi", nil); !errors.Is(err, ErrInvalidKind) {
		t.Fatalf("kind: %v", err)
	}
	if _, _, err := d.SendMessage(a.SessionKey, "codex", "", "  ", nil); !errors.Is(err, ErrEmptyBody) {
		t.Fatalf("body: %v", err)
	}
}

func TestClaimInbox_BroadcastDeliveredOncePerPeer(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "s1", "claude-code")
	mustPeer(t, d, "codex:1", "codex")
	mustPeer(t, d, "codex:2", "codex")
	if _, _, err := d.SendMessage(a.SessionKey, "*", "done", "finished X", nil); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"codex:1", "codex:2"} {
		msgs, err := d.ClaimInbox(ctx, key, InboxFilter{}, 0, false)
		if err != nil || len(msgs) != 1 {
			t.Fatalf("%s first claim: %v %v", key, msgs, err)
		}
		if msgs[0].FromHandle != a.Handle || msgs[0].FromRuntime != "claude-code" || msgs[0].Kind != "done" {
			t.Errorf("message = %+v", msgs[0])
		}
		again, _ := d.ClaimInbox(ctx, key, InboxFilter{}, 0, false)
		if len(again) != 0 {
			t.Fatalf("%s second claim: %v", key, again)
		}
	}
	if own, _ := d.ClaimInbox(ctx, "s1", InboxFilter{}, 0, false); len(own) != 0 {
		t.Fatalf("sender got own broadcast: %v", own)
	}
}

func TestClaimInbox_FilterLeavesOthersUndelivered(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "s1", "claude-code")
	c := mustPeer(t, d, "codex:1", "codex")
	o := mustPeer(t, d, "opencode:1", "opencode")
	d.SendMessage(c.SessionKey, a.Handle, "", "from codex", nil)
	d.SendMessage(o.SessionKey, a.Handle, "", "from opencode", nil)

	got, err := d.ClaimInbox(ctx, "s1", InboxFilter{From: "codex"}, 0, false)
	if err != nil || len(got) != 1 || got[0].Body != "from codex" {
		t.Fatalf("filtered claim: %v %v", got, err)
	}
	got, _ = d.ClaimInbox(ctx, "s1", InboxFilter{From: o.Handle}, 0, false)
	if len(got) != 1 || got[0].Body != "from opencode" {
		t.Fatalf("by handle: %v", got)
	}
}

func TestClaimInbox_UnknownPeerNeedsRegister(t *testing.T) {
	d := openCommsDB(t)
	if _, err := d.ClaimInbox(ctx, "codex:new", InboxFilter{}, 0, false); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("claim without register: %v", err)
	}
	if _, err := d.GetPeer("codex:new"); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("ghost peer created: %v", err)
	}
	if _, err := d.ClaimInbox(ctx, "codex:new", InboxFilter{}, 0, true); err != nil {
		t.Fatal(err)
	}
	p, err := d.GetPeer("codex:new")
	if err != nil || p.Runtime != "codex" {
		t.Fatalf("peer = %+v %v", p, err)
	}
}

func TestClaimInbox_WithoutRegisterKeepsEndedPeerEnded(t *testing.T) {
	d := openCommsDB(t)
	mustPeer(t, d, "s1", "claude-code")
	d.EndPeer("s1")
	if _, err := d.ClaimInbox(ctx, "s1", InboxFilter{}, 0, false); err != nil {
		t.Fatal(err)
	}
	if p, _ := d.GetPeer("s1"); p.EndedAt == nil {
		t.Fatal("non-register claim reactivated an ended peer")
	}
	d.ClaimInbox(ctx, "s1", InboxFilter{}, 0, true)
	if p, _ := d.GetPeer("s1"); p.EndedAt != nil || !p.Active {
		t.Fatalf("register claim did not reactivate: %+v", p)
	}
}

func TestClaimInbox_ConcurrentClaimsDeliverOnce(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "s1", "claude-code")
	mustPeer(t, d, "codex:1", "codex")
	const n = 60
	for i := range n {
		if _, _, err := d.SendMessage(a.SessionKey, "codex:1", "", fmt.Sprintf("m%d", i), nil); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5 {
				msgs, err := d.ClaimInbox(ctx, "codex:1", InboxFilter{}, 7, false)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				for _, m := range msgs {
					seen[m.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for len(seen) < n {
		msgs, _ := d.ClaimInbox(ctx, "codex:1", InboxFilter{}, 0, false)
		if len(msgs) == 0 {
			break
		}
		for _, m := range msgs {
			seen[m.ID]++
		}
	}
	if len(seen) != n {
		t.Fatalf("delivered %d distinct messages, want %d", len(seen), n)
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("message %d delivered %d times", id, c)
		}
	}
}

func TestUpsertPeer_ConcurrentFirstRegistration(t *testing.T) {
	d := openCommsDB(t)
	var wg sync.WaitGroup
	handles := make(chan string, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := d.UpsertPeer("s1", "claude-code", "proj")
			if err != nil {
				t.Error(err)
				return
			}
			handles <- p.Handle
		}()
	}
	wg.Wait()
	close(handles)
	first := ""
	for h := range handles {
		if first == "" {
			first = h
		} else if h != first {
			t.Fatalf("handles differ: %s vs %s", first, h)
		}
	}
}

func TestListPeers_ActiveWindowAndEnded(t *testing.T) {
	d := openCommsDB(t)
	mustPeer(t, d, "live", "claude-code")
	mustPeer(t, d, "ended", "codex")
	mustPeer(t, d, "stale", "opencode")
	d.EndPeer("ended")
	old := time.Now().UTC().Add(-PeerActiveWindow - time.Minute).Format(commsTSLayout)
	if _, err := d.sql.Exec(`UPDATE peers SET last_seen_at = ? WHERE session_key = 'stale'`, old); err != nil {
		t.Fatal(err)
	}
	active, err := d.ListPeers(true)
	if err != nil || len(active) != 1 || active[0].SessionKey != "live" {
		t.Fatalf("active = %+v %v", active, err)
	}
	all, _ := d.ListPeers(false)
	if len(all) != 3 {
		t.Fatalf("all = %d", len(all))
	}
	for _, p := range all {
		if (p.SessionKey == "live") != p.Active {
			t.Errorf("%s active = %v", p.SessionKey, p.Active)
		}
	}
	if _, _, err := d.SendMessage("live", "*", "", "hi", nil); !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("broadcast reached inactive peers: %v", err)
	}
}

func TestSendMessage_UnknownSenderAndLimits(t *testing.T) {
	d := openCommsDB(t)
	mustPeer(t, d, "s1", "claude-code")
	mustPeer(t, d, "codex:1", "codex")
	if _, _, err := d.SendMessage("hallucinated", "codex", "", "hi", nil); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("unknown sender: %v", err)
	}
	if _, _, err := d.SendMessage("s1", "codex", "", strings.Repeat("x", MaxMessageBody+1), nil); !errors.Is(err, ErrTooLong) {
		t.Fatalf("long body: %v", err)
	}
	if _, err := d.CreateSharedTask("s1", strings.Repeat("t", MaxTaskTitle+1), "", ""); !errors.Is(err, ErrTooLong) {
		t.Fatalf("long title: %v", err)
	}
}

func TestValidateInboxFilter(t *testing.T) {
	d := openCommsDB(t)
	c := mustPeer(t, d, "codex:1", "codex")
	for _, f := range []InboxFilter{{}, {From: "claude"}, {From: c.Handle}, {From: UserKey}, {Kind: "done"}} {
		if err := d.ValidateInboxFilter(f); err != nil {
			t.Errorf("%+v: %v", f, err)
		}
	}
	if err := d.ValidateInboxFilter(InboxFilter{Kind: "donee"}); !errors.Is(err, ErrInvalidKind) {
		t.Errorf("bad kind: %v", err)
	}
	if err := d.ValidateInboxFilter(InboxFilter{From: "nobody"}); !errors.Is(err, ErrPeerNotFound) {
		t.Errorf("bad from: %v", err)
	}
}

func TestSharedTask_LifecycleNotifiesCreatorAndAssignee(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "s1", "claude-code")
	c := mustPeer(t, d, "codex:1", "codex")

	task, err := d.CreateSharedTask(a.SessionKey, "Write migrations", "", c.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "open" || task.Assignee != c.SessionKey || task.AssigneeHandle != c.Handle {
		t.Fatalf("task = %+v", task)
	}
	inbox, _ := d.ClaimInbox(ctx, c.SessionKey, InboxFilter{}, 0, false)
	if len(inbox) != 1 || inbox[0].Kind != "task_assigned" || inbox[0].TaskID == nil || *inbox[0].TaskID != task.ID || inbox[0].ToSpec != c.Handle {
		t.Fatalf("assignee inbox = %+v", inbox)
	}

	done := "done"
	res := "migrations 001-003 added"
	if _, err := d.UpdateSharedTask(task.ID, c.SessionKey, SharedTaskUpdate{Status: &done, Result: &res}); err != nil {
		t.Fatal(err)
	}
	inbox, _ = d.ClaimInbox(ctx, a.SessionKey, InboxFilter{}, 0, false)
	if len(inbox) != 1 || inbox[0].Kind != "task_done" || !strings.Contains(inbox[0].Body, res) {
		t.Fatalf("creator inbox = %+v", inbox)
	}

	bad := "finished"
	if _, err := d.UpdateSharedTask(task.ID, c.SessionKey, SharedTaskUpdate{Status: &bad}); !errors.Is(err, ErrInvalidTaskStatus) {
		t.Fatalf("bad status: %v", err)
	}
	if _, err := d.UpdateSharedTask(999, c.SessionKey, SharedTaskUpdate{Status: &done}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestSharedTask_ClaimTakenByOtherConflicts(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "s1", "claude-code")
	c := mustPeer(t, d, "codex:1", "codex")
	o := mustPeer(t, d, "opencode:1", "opencode")
	task, _ := d.CreateSharedTask(a.SessionKey, "Fix CSS", "", c.Handle)
	claimed := "claimed"
	if _, err := d.UpdateSharedTask(task.ID, o.SessionKey, SharedTaskUpdate{Status: &claimed}); !errors.Is(err, ErrTaskTaken) {
		t.Fatalf("claim of someone else's task: %v", err)
	}
	if _, err := d.UpdateSharedTask(task.ID, c.SessionKey, SharedTaskUpdate{Status: &claimed}); err != nil {
		t.Fatalf("assignee claim: %v", err)
	}
}

func TestSharedTask_CancelAndReassignNotifyAssignees(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "s1", "claude-code")
	c := mustPeer(t, d, "codex:1", "codex")
	o := mustPeer(t, d, "opencode:1", "opencode")
	task, _ := d.CreateSharedTask(a.SessionKey, "Fix CSS", "", c.Handle)
	d.ClaimInbox(ctx, c.SessionKey, InboxFilter{}, 0, false)

	if _, err := d.UpdateSharedTask(task.ID, a.SessionKey, SharedTaskUpdate{Assignee: &o.Handle}); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.ClaimInbox(ctx, c.SessionKey, InboxFilter{}, 0, false); len(got) != 1 || got[0].Kind != "task_update" {
		t.Fatalf("previous assignee not told: %+v", got)
	}
	if got, _ := d.ClaimInbox(ctx, o.SessionKey, InboxFilter{}, 0, false); len(got) != 1 || got[0].Kind != "task_assigned" {
		t.Fatalf("new assignee not told: %+v", got)
	}
	cancelled := "cancelled"
	if _, err := d.UpdateSharedTask(task.ID, UserKey, SharedTaskUpdate{Status: &cancelled}); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.ClaimInbox(ctx, o.SessionKey, InboxFilter{}, 0, false); len(got) != 1 || !strings.Contains(got[0].Body, "cancelled") {
		t.Fatalf("assignee not told about cancel: %+v", got)
	}
}

func TestSharedTask_ClaimAssignsActor(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "s1", "claude-code")
	o := mustPeer(t, d, "opencode:1", "opencode")
	task, _ := d.CreateSharedTask(a.SessionKey, "Fix CSS", "", "")
	claimed := "claimed"
	got, err := d.UpdateSharedTask(task.ID, o.SessionKey, SharedTaskUpdate{Status: &claimed})
	if err != nil || got.Assignee != o.SessionKey || got.Status != "claimed" {
		t.Fatalf("claim = %+v %v", got, err)
	}
	inbox, _ := d.ClaimInbox(ctx, a.SessionKey, InboxFilter{}, 0, false)
	if len(inbox) != 1 || inbox[0].Kind != "task_update" {
		t.Fatalf("creator inbox = %+v", inbox)
	}
	if list, _ := d.ListSharedTasks("claimed"); len(list) != 1 {
		t.Fatalf("list = %v", list)
	}
}

func TestListMessages_PeerFilter(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "s1", "claude-code")
	c := mustPeer(t, d, "codex:1", "codex")
	mustPeer(t, d, "opencode:1", "opencode")
	d.SendMessage(a.SessionKey, c.Handle, "", "one", nil)
	d.SendMessage(UserKey, "opencode", "", "two", nil)
	all, _ := d.ListMessages("", 0)
	if len(all) != 2 {
		t.Fatalf("all = %d", len(all))
	}
	mine, _ := d.ListMessages(c.Handle, 0)
	if len(mine) != 1 || mine[0].Body != "one" || len(mine[0].Recipients) != 1 {
		t.Fatalf("codex view = %+v", mine)
	}
}

// Handles are public (peers_list, message headers), so they must never work as a
// caller identity — only for addressing.
func TestHandleIsNotAnIdentity(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "s1", "claude-code")
	b := mustPeer(t, d, "codex:1", "codex")
	if _, _, err := d.SendMessage(a.SessionKey, b.Handle, "", "secret", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ClaimInbox(ctx, b.Handle, InboxFilter{}, 0, false); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("claim by handle: %v", err)
	}
	if got, _ := d.ClaimInbox(ctx, b.Handle, InboxFilter{}, 0, true); len(got) != 0 {
		t.Fatalf("register-claim by handle returned b's messages: %v", got)
	}
	if got, _ := d.ClaimInbox(ctx, b.SessionKey, InboxFilter{}, 0, false); len(got) != 1 {
		t.Fatalf("b's message was consumed by a handle claim: %v", got)
	}
	if _, _, err := d.SendMessage(b.Handle, a.Handle, "", "spoof", nil); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("send as handle: %v", err)
	}
	task, _ := d.CreateSharedTask(a.SessionKey, "T", "", b.Handle)
	claimed := "claimed"
	if _, err := d.UpdateSharedTask(task.ID, b.Handle, SharedTaskUpdate{Status: &claimed}); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("task update as handle: %v", err)
	}
	if _, err := d.CreateSharedTask(b.Handle, "T2", "", ""); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("task create as handle: %v", err)
	}
	if _, err := d.RenamePeer(b.Handle, "hijacked"); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("rename by handle: %v", err)
	}
	if err := d.EndPeer(b.Handle); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("end by handle: %v", err)
	}
}

func TestSendMessage_BroadcastExcludesSenderByKey(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "s1", "claude-code")
	mustPeer(t, d, "s2", "claude-code")
	_, recips, err := d.SendMessage(a.SessionKey, "claude", "", "hi", nil)
	if err != nil || len(recips) != 1 || recips[0] != "s2" {
		t.Fatalf("recipients = %v %v", recips, err)
	}
}
