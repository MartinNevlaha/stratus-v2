package db

import (
	"errors"
	"sync"
	"testing"
)

func TestCommsProjectsIsolateRecipientsAndTasks(t *testing.T) {
	d := openCommsDB(t)
	aRoot, bRoot := t.TempDir(), t.TempDir()
	add := func(key, root string) *Peer {
		t.Helper()
		p, e := d.UpsertPeerInProject(key, "codex", "same", root)
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	a := add("codex:a", aRoot)
	aa := add("codex:aa", aRoot)
	b := add("codex:b", bRoot)
	legacy, e := d.UpsertPeer("codex:old", "codex", "same")
	if e != nil {
		t.Fatal(e)
	}
	for _, to := range []string{"*", "codex"} {
		_, recips, e := d.SendMessage(a.SessionKey, to, "text", "work", nil)
		if e != nil || len(recips) != 1 || recips[0] != aa.SessionKey {
			t.Fatalf("broadcast %s: %v %v", to, recips, e)
		}
	}
	for _, to := range []string{b.Handle, b.SessionKey, legacy.Handle} {
		if _, _, e := d.SendMessage(a.SessionKey, to, "text", "work", nil); !errors.Is(e, ErrProjectScope) {
			t.Fatalf("cross-project %s: %v", to, e)
		}
	}
	if _, _, e := d.SendMessage(legacy.SessionKey, "*", "text", "work", nil); !errors.Is(e, ErrProjectScope) {
		t.Fatalf("legacy broadcast: %v", e)
	}
	peers, e := d.ListPeersForSession(a.SessionKey, true)
	if e != nil || len(peers) != 2 {
		t.Fatalf("peers %v %v", peers, e)
	}
	if _, e := d.CreateSharedTask(a.SessionKey, "foreign assignment", "", b.Handle); !errors.Is(e, ErrProjectScope) {
		t.Fatalf("foreign task %v", e)
	}
	task, e := d.CreateSharedTask(a.SessionKey, "own task", "", aa.Handle)
	if e != nil {
		t.Fatal(e)
	}
	status := "claimed"
	if _, e := d.UpdateSharedTask(task.ID, b.SessionKey, SharedTaskUpdate{Status: &status}); !errors.Is(e, ErrProjectScope) {
		t.Fatalf("foreign update %v", e)
	}
	if _, _, e := d.SendMessage(b.SessionKey, "user", "text", "foreign task reference", &task.ID); !errors.Is(e, ErrProjectScope) {
		t.Fatalf("task reference %v", e)
	}
	tasks, e := d.ListSharedTasksForSession(b.SessionKey, "")
	if e != nil || len(tasks) != 0 {
		t.Fatalf("foreign task listing %v %v", tasks, e)
	}
	if all, e := d.ListSharedTasks(""); e != nil || len(all) != 1 {
		t.Fatalf("dashboard lost history %v %v", all, e)
	}
	if _, e := d.UpsertPeerInProject(a.SessionKey, "codex", "same", bRoot); !errors.Is(e, ErrProjectScope) {
		t.Fatalf("session moved project %v", e)
	}
}

func TestCommsLegacyUnknownNotGroupedAndHistoryPreserved(t *testing.T) {
	d := openCommsDB(t)
	old, err := d.UpsertPeer("old", "claude-code", "same")
	if err != nil {
		t.Fatal(err)
	}
	other, err := d.UpsertPeer("other", "codex", "same")
	if err != nil {
		t.Fatal(err)
	}
	if old.ProjectID != "" || old.ProjectRoot != "" {
		t.Fatal("display-only registration gained identity")
	}
	peers, err := d.ListPeersForSession(old.SessionKey, true)
	if err != nil || len(peers) != 1 || peers[0].SessionKey != old.SessionKey {
		t.Fatalf("legacy peers %v %v", peers, err)
	}
	if _, _, err := d.SendMessage(UserKey, other.Handle, "text", "historic dashboard message", nil); err != nil {
		t.Fatal(err)
	}
	if history, err := d.ListMessages("", 100); err != nil || len(history) != 1 {
		t.Fatalf("history %v %v", history, err)
	}
	verified, err := d.UpsertPeerInProject(old.SessionKey, "claude-code", "same", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	again, err := d.UpsertPeer(old.SessionKey, "claude-code", "")
	if err != nil || again.ProjectID != verified.ProjectID || again.Handle != old.Handle {
		t.Fatalf("legacy heartbeat changed identity %v %v", again, err)
	}
}

func TestCommsTaskClaimsAreAtomic(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "a", "claude-code")
	b := mustPeer(t, d, "b", "codex")
	c := mustPeer(t, d, "c", "opencode")
	task, err := d.CreateSharedTask(a.SessionKey, "claim once", "", "")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, p := range []*Peer{b, c} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			<-start
			status := "claimed"
			_, err := d.UpdateSharedTask(task.ID, key, SharedTaskUpdate{Status: &status})
			results <- err
		}(p.SessionKey)
	}
	close(start)
	wg.Wait()
	close(results)
	success, taken := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrTaskTaken) {
			taken++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || taken != 1 {
		t.Fatalf("success=%d taken=%d", success, taken)
	}
}

func TestCommsTaskCannotBeStolenByExplicitAssignment(t *testing.T) {
	d := openCommsDB(t)
	a := mustPeer(t, d, "a", "claude-code")
	b := mustPeer(t, d, "b", "codex")
	c := mustPeer(t, d, "c", "opencode")
	task, err := d.CreateSharedTask(a.SessionKey, "assigned task", "", b.Handle)
	if err != nil {
		t.Fatal(err)
	}
	claim, done := "claimed", "done"
	for _, u := range []SharedTaskUpdate{{Status: &claim, Assignee: &c.Handle}, {Status: &done}} {
		if _, err := d.UpdateSharedTask(task.ID, c.SessionKey, u); !errors.Is(err, ErrTaskTaken) {
			t.Fatalf("unassigned peer modified task: %v", err)
		}
	}
}

func TestDashboardUnassignedTaskUsesVerifiedProject(t *testing.T) {
	d := openCommsDB(t)
	root := t.TempDir()
	p, err := d.UpsertPeerInProject("a", "codex", "same", root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := d.CreateSharedTaskInProject(UserKey, "user task", "", "", root)
	if err != nil {
		t.Fatal(err)
	}
	if task.ProjectID != p.ProjectID {
		t.Fatalf("task project %q", task.ProjectID)
	}
	status := "claimed"
	if _, err := d.UpdateSharedTask(task.ID, p.SessionKey, SharedTaskUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
}
