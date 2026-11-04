package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestCommsSessionScopesDoNotExposeOtherProjects(t *testing.T) {
	s := commsTestServer(t)
	root := t.TempDir()
	a, e := s.db.UpsertPeerInProject("a", "claude-code", "same", root)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.db.UpsertPeerInProject("b", "codex", "same", t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.db.CreateSharedTask(b.SessionKey, "private task", "", "")
	if e != nil {
		t.Fatal(e)
	}
	w := commsDo(t, s.handleListPeers, "GET", "/api/peers?active=1&session_id="+url.QueryEscape(a.SessionKey), "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), b.Handle) {
		t.Fatalf("peer leak: %d %s", w.Code, w.Body)
	}
	w = commsDo(t, s.handleListSharedTasks, "GET", "/api/shared-tasks?session_id=a", "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), task.Title) {
		t.Fatalf("task leak: %d %s", w.Code, w.Body)
	}
	body, _ := json.Marshal(map[string]string{"from": "a", "to": "missing", "body": "hello"})
	w = commsDo(t, s.handleSendMessage, "POST", "/api/messages", string(body), nil)
	if w.Code != 404 || strings.Contains(w.Body.String(), b.Handle) {
		t.Fatalf("error leak: %d %s", w.Code, w.Body)
	}
	w = commsDo(t, s.handleListPeers, "GET", "/api/peers?session_id=ghost", "", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown caller: %d %s", w.Code, w.Body)
	}
}
