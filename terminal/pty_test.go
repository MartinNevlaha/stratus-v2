package terminal

import (
	"strings"
	"testing"
	"time"
)

func TestScrollback_TruncatesToCap(t *testing.T) {
	var sb scrollback
	chunk := []byte(strings.Repeat("x", 1000))
	for i := 0; i < 400; i++ {
		sb.write(chunk)
	}
	if got := len(sb.bytes()); got != scrollbackCap {
		t.Fatalf("len = %d, want %d", got, scrollbackCap)
	}
}

func TestScrollback_KeepsNewestBytes(t *testing.T) {
	var sb scrollback
	sb.write([]byte(strings.Repeat("a", scrollbackCap)))
	sb.write([]byte("tail"))
	got := string(sb.bytes())
	if !strings.HasSuffix(got, "tail") || len(got) != scrollbackCap {
		t.Fatalf("unexpected buffer: len=%d suffix=%q", len(got), got[len(got)-4:])
	}
}

func TestManager_Attach_ReplaysBufferedOutput(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	sess, err := m.Create("a")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	if err := sess.Write([]byte("echo replay-marker\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		replay, detach := sess.Attach(func([]byte) {})
		detach()
		return strings.Contains(string(replay), "replay-marker")
	})
}

func TestManager_Attach_DeliversLiveOutputAfterReplay(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	sess, err := m.Create("a")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	got := make(chan string, 64)
	_, detach := sess.Attach(func(b []byte) { got <- string(b) })
	defer detach()

	if err := sess.Write([]byte("echo live-marker\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	var all string
	for !strings.Contains(all, "live-marker") {
		select {
		case s := <-got:
			all += s
		case <-deadline:
			t.Fatalf("no live output, got %q", all)
		}
	}
}

func TestManager_Reaper_KillsStaleDetached(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	sess, err := m.Create("a")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	m.reapStale(time.Now(), detachedTTL)
	if _, ok := m.Get("a"); !ok {
		t.Fatal("fresh session was reaped")
	}

	m.reapStale(time.Now().Add(detachedTTL+time.Minute), detachedTTL)
	select {
	case <-sess.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("stale session was not killed")
	}
}

func TestManager_Reaper_KeepsAttachedSession(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	sess, err := m.Create("a")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	_, detach := sess.Attach(func([]byte) {})
	defer detach()

	m.reapStale(time.Now().Add(detachedTTL+time.Hour), detachedTTL)
	select {
	case <-sess.Done():
		t.Fatal("attached session was killed")
	case <-time.After(300 * time.Millisecond):
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestManager_Adopt_PicksDetachedSession(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	sess, err := m.Create("a")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	var got *Session
	if !m.Adopt(func(s *Session) { got = s }) {
		t.Fatal("Adopt found nothing")
	}
	if got != sess {
		t.Fatal("Adopt returned a different session")
	}
}

func TestManager_Adopt_SkipsAttachedSession(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	sess, err := m.Create("a")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	_, detach := sess.Attach(func([]byte) {})
	defer detach()

	if m.Adopt(func(*Session) { t.Fatal("attached session was adopted") }) {
		t.Fatal("Adopt should find nothing")
	}
}

func TestManager_Adopt_ClaimsEachSessionOnce(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	sess, err := m.Create("a")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	attach := func(s *Session) { s.Attach(func([]byte) {}) }
	if !m.Adopt(attach) {
		t.Fatal("first Adopt found nothing")
	}
	if m.Adopt(attach) {
		t.Fatal("second Adopt claimed the same session")
	}
}
