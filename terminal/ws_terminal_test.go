package terminal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func dialTerminal(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func sendMsg(t *testing.T, conn *websocket.Conn, typ string, data any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, map[string]any{"type": typ, "data": data}); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readUntil reads server messages until pred returns true; returns the create
// status seen and all output concatenated.
func readUntil(t *testing.T, conn *websocket.Conn, pred func(status, output string) bool) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var status, output string
	for !pred(status, output) {
		var msg struct {
			Type string `json:"type"`
			Data any    `json:"data"`
		}
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			t.Fatalf("read (status=%q output=%q): %v", status, output, err)
		}
		switch msg.Type {
		case MsgCreate:
			if m, ok := msg.Data.(map[string]any); ok {
				status, _ = m["status"].(string)
			}
		case MsgOutput:
			if s, ok := msg.Data.(string); ok {
				output += s
			}
		}
	}
	return status, output
}

func TestServeWS_CreateExistingID_Attaches(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	srv := httptest.NewServer(http.HandlerFunc(m.ServeWS))
	defer srv.Close()

	c1 := dialTerminal(t, srv)
	sendMsg(t, c1, MsgCreate, map[string]string{"id": "persist"})
	status, _ := readUntil(t, c1, func(s, _ string) bool { return s != "" })
	if status != "ok" {
		t.Fatalf("first create status = %q, want ok", status)
	}
	first, _ := m.Get("persist")

	sendMsg(t, c1, MsgInput, map[string]string{"id": "persist", "data": "echo before-close\n"})
	readUntil(t, c1, func(_, o string) bool { return strings.Contains(o, "before-close") })
	c1.Close(websocket.StatusNormalClosure, "bye")

	c2 := dialTerminal(t, srv)
	defer c2.Close(websocket.StatusNormalClosure, "bye")
	sendMsg(t, c2, MsgCreate, map[string]string{"id": "persist"})
	status, out := readUntil(t, c2, func(s, o string) bool {
		return s != "" && strings.Contains(o, "before-close")
	})
	if status != "attached" {
		t.Fatalf("second create status = %q, want attached", status)
	}
	if !strings.Contains(out, "before-close") {
		t.Fatalf("replay missing earlier output: %q", out)
	}
	second, ok := m.Get("persist")
	if !ok || second != first {
		t.Fatal("reattach created a different session")
	}
}

func TestServeWS_Disconnect_KeepsSessionAlive(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	srv := httptest.NewServer(http.HandlerFunc(m.ServeWS))
	defer srv.Close()

	c := dialTerminal(t, srv)
	sendMsg(t, c, MsgCreate, map[string]string{"id": "alive"})
	readUntil(t, c, func(s, _ string) bool { return s != "" })
	c.Close(websocket.StatusNormalClosure, "bye")

	time.Sleep(300 * time.Millisecond)
	sess, ok := m.Get("alive")
	if !ok {
		t.Fatal("session removed after disconnect")
	}
	select {
	case <-sess.Done():
		t.Fatal("session killed after disconnect")
	default:
	}
	_ = sess.Close()
}

func TestServeWS_DistinctIDs_AreIndependentSessions(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	srv := httptest.NewServer(http.HandlerFunc(m.ServeWS))
	defer srv.Close()

	c1 := dialTerminal(t, srv)
	defer c1.Close(websocket.StatusNormalClosure, "bye")
	c2 := dialTerminal(t, srv)
	defer c2.Close(websocket.StatusNormalClosure, "bye")

	sendMsg(t, c1, MsgCreate, map[string]string{"id": "w1"})
	sendMsg(t, c2, MsgCreate, map[string]string{"id": "w2"})
	s1, _ := readUntil(t, c1, func(s, _ string) bool { return s != "" })
	s2, _ := readUntil(t, c2, func(s, _ string) bool { return s != "" })
	if s1 != "ok" || s2 != "ok" {
		t.Fatalf("statuses = %q, %q, want ok, ok", s1, s2)
	}

	// Output typed in window 1 must keep flowing to window 1 after window 2 attached.
	sendMsg(t, c1, MsgInput, map[string]string{"id": "w1", "data": "echo only-in-one\n"})
	readUntil(t, c1, func(_, o string) bool { return strings.Contains(o, "only-in-one") })
}

func TestServeWS_Adopt_TakesDetachedSessionAndReportsID(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	srv := httptest.NewServer(http.HandlerFunc(m.ServeWS))
	defer srv.Close()

	c1 := dialTerminal(t, srv)
	sendMsg(t, c1, MsgCreate, map[string]string{"id": "old"})
	readUntil(t, c1, func(s, _ string) bool { return s != "" })
	sendMsg(t, c1, MsgInput, map[string]string{"id": "old", "data": "echo kept-work\n"})
	readUntil(t, c1, func(_, o string) bool { return strings.Contains(o, "kept-work") })
	c1.Close(websocket.StatusNormalClosure, "bye")
	time.Sleep(200 * time.Millisecond)

	c2 := dialTerminal(t, srv)
	defer c2.Close(websocket.StatusNormalClosure, "bye")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, c2, map[string]any{
		"type": MsgCreate, "data": map[string]any{"id": "fresh", "adopt": true},
	}); err != nil {
		t.Fatal(err)
	}
	var created map[string]any
	for created == nil {
		var msg struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if err := wsjson.Read(ctx, c2, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Type == MsgCreate {
			created = msg.Data
		}
	}
	if created["id"] != "old" || created["status"] != "attached" {
		t.Fatalf("create = %v, want id=old status=attached", created)
	}
}

func TestServeWS_Adopt_NoDetachedSession_CreatesWithGivenID(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager()
	srv := httptest.NewServer(http.HandlerFunc(m.ServeWS))
	defer srv.Close()

	c := dialTerminal(t, srv)
	defer c.Close(websocket.StatusNormalClosure, "bye")
	sendMsg(t, c, MsgCreate, map[string]any{"id": "fresh", "adopt": true})
	status, _ := readUntil(t, c, func(s, _ string) bool { return s != "" })
	if status != "ok" {
		t.Fatalf("status = %q, want ok", status)
	}
	if _, ok := m.Get("fresh"); !ok {
		t.Fatal("session fresh was not created")
	}
}
