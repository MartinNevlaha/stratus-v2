package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func commsTestServer(t *testing.T) *Server {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "comms.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return &Server{db: database, hub: NewHub(), projectRoot: t.TempDir()}
}

func commsDo(t *testing.T, h http.HandlerFunc, method, target, body string, pathVals map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range pathVals {
		req.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func registerTestPeer(t *testing.T, s *Server, key, runtime string) db.Peer {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"session_key": key, "runtime": runtime, "project": "p", "project_root": s.projectRoot})
	w := commsDo(t, s.handleRegisterPeer, "POST", "/api/peers", string(body), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("register %s: %d %s", key, w.Code, w.Body)
	}
	var p db.Peer
	json.Unmarshal(w.Body.Bytes(), &p)
	return p
}

func TestHandleRegisterPeer_Validation(t *testing.T) {
	s := commsTestServer(t)
	if w := commsDo(t, s.handleRegisterPeer, "POST", "/api/peers", `{"session_key":""}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("empty key: %d", w.Code)
	}
	if w := commsDo(t, s.handleRegisterPeer, "POST", "/api/peers", `{"session_key":"x","runtime":"cursor"}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("bad runtime: %d", w.Code)
	}
	if p := registerTestPeer(t, s, "codex:1", "codex"); !strings.HasPrefix(p.Handle, "codex-") {
		t.Fatalf("handle = %s", p.Handle)
	}
}

func TestHandleSendMessage_StatusCodes(t *testing.T) {
	s := commsTestServer(t)
	a := registerTestPeer(t, s, "s1", "claude-code")
	registerTestPeer(t, s, "codex:1", "codex")

	w := commsDo(t, s.handleSendMessage, "POST", "/api/messages", `{"from":"s1","to":"codex","body":"done with X","kind":"done"}`, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"recipients":1`) {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	w = commsDo(t, s.handleSendMessage, "POST", "/api/messages", `{"from":"s1","to":"ghost","body":"x"}`, nil)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "codex-") {
		t.Fatalf("unknown recipient should list active handles: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"to":"codex","body":"x"}`, `{"from":"s1","to":"codex","body":""}`, `{"from":"s1","to":"codex","body":"x","kind":"nope"}`, `{"from":"s1","body":"x"}`} {
		if w := commsDo(t, s.handleSendMessage, "POST", "/api/messages", body, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
	w = commsDo(t, s.handleListMessages, "GET", "/api/messages?peer="+a.Handle, "", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "done with X") {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
}

func TestHandleClaimInbox_DeliversOnce(t *testing.T) {
	s := commsTestServer(t)
	registerTestPeer(t, s, "s1", "claude-code")
	registerTestPeer(t, s, "codex:1", "codex")
	commsDo(t, s.handleSendMessage, "POST", "/api/messages", `{"from":"s1","to":"codex:1","body":"hello"}`, nil)

	claim := func() []db.Message {
		w := commsDo(t, s.handleClaimInbox, "POST", "/api/peers/codex:1/inbox/claim", ``, map[string]string{"key": "codex:1"})
		if w.Code != http.StatusOK {
			t.Fatalf("claim: %d %s", w.Code, w.Body)
		}
		var out struct{ Messages []db.Message }
		json.Unmarshal(w.Body.Bytes(), &out)
		return out.Messages
	}
	if got := claim(); len(got) != 1 || got[0].Body != "hello" {
		t.Fatalf("first claim = %+v", got)
	}
	if got := claim(); len(got) != 0 {
		t.Fatalf("second claim = %+v", got)
	}
}

func TestHandleWaitInbox_TimeoutBounds(t *testing.T) {
	s := commsTestServer(t)
	for _, q := range []string{"timeout_s=0", "timeout_s=601", "timeout_s=abc"} {
		w := commsDo(t, s.handleWaitInbox, "POST", "/api/peers/s1/inbox/wait?"+q, ``, map[string]string{"key": "s1"})
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, w.Code)
		}
	}
}

func TestHandleWaitInbox_ReturnsWhenMessageArrives(t *testing.T) {
	s := commsTestServer(t)
	registerTestPeer(t, s, "s1", "claude-code")
	registerTestPeer(t, s, "codex:1", "codex")
	go func() {
		time.Sleep(300 * time.Millisecond)
		commsDo(t, s.handleSendMessage, "POST", "/api/messages", `{"from":"s1","to":"codex:1","body":"your turn","kind":"done"}`, nil)
	}()
	start := time.Now()
	w := commsDo(t, s.handleWaitInbox, "POST", "/api/peers/codex:1/inbox/wait?timeout_s=5&from=claude", ``, map[string]string{"key": "codex:1"})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "your turn") || strings.Contains(w.Body.String(), `"timed_out":true`) {
		t.Fatalf("wait: %d %s", w.Code, w.Body)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("wait took %s", time.Since(start))
	}
}

func TestHandleWaitInbox_TimesOut(t *testing.T) {
	s := commsTestServer(t)
	registerTestPeer(t, s, "s1", "claude-code")
	w := commsDo(t, s.handleWaitInbox, "POST", "/api/peers/s1/inbox/wait?timeout_s=1", ``, map[string]string{"key": "s1"})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"timed_out":true`) {
		t.Fatalf("timeout: %d %s", w.Code, w.Body)
	}
}

func TestHandleRenamePeer(t *testing.T) {
	s := commsTestServer(t)
	registerTestPeer(t, s, "s1", "claude-code")
	registerTestPeer(t, s, "s2", "claude-code")
	if w := commsDo(t, s.handleRenamePeer, "PATCH", "/api/peers/s1", `{"handle":"backend"}`, map[string]string{"key": "s1"}); w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body)
	}
	if w := commsDo(t, s.handleRenamePeer, "PATCH", "/api/peers/s2", `{"handle":"backend"}`, map[string]string{"key": "s2"}); w.Code != http.StatusConflict {
		t.Fatalf("taken: %d", w.Code)
	}
	if w := commsDo(t, s.handleRenamePeer, "PATCH", "/api/peers/s2", `{"handle":"Bad Name"}`, map[string]string{"key": "s2"}); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid: %d", w.Code)
	}
	if w := commsDo(t, s.handleRenamePeer, "PATCH", "/api/peers/zz", `{"handle":"fine"}`, map[string]string{"key": "zz"}); w.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", w.Code)
	}
}

func TestHandleSharedTasks_Lifecycle(t *testing.T) {
	s := commsTestServer(t)
	registerTestPeer(t, s, "s1", "claude-code")
	c := registerTestPeer(t, s, "codex:1", "codex")

	w := commsDo(t, s.handleCreateSharedTask, "POST", "/api/shared-tasks", `{"created_by":"s1","title":"Add index","assignee":"`+c.Handle+`"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var task db.SharedTask
	json.Unmarshal(w.Body.Bytes(), &task)

	id := func(n int64) map[string]string { return map[string]string{"id": strconv.FormatInt(n, 10)} }
	if w := commsDo(t, s.handleUpdateSharedTask, "PATCH", "/api/shared-tasks/x", `{"actor":"codex:1","status":"bogus"}`, id(task.ID)); w.Code != http.StatusBadRequest {
		t.Fatalf("bad status: %d", w.Code)
	}
	if w := commsDo(t, s.handleUpdateSharedTask, "PATCH", "/api/shared-tasks/x", `{"actor":"codex:1","status":"done","result":"ok"}`, id(task.ID)); w.Code != http.StatusOK {
		t.Fatalf("done: %d %s", w.Code, w.Body)
	}
	if w := commsDo(t, s.handleUpdateSharedTask, "PATCH", "/api/shared-tasks/x", `{"actor":"codex:1","status":"done"}`, id(999)); w.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", w.Code)
	}
	if w := commsDo(t, s.handleCreateSharedTask, "POST", "/api/shared-tasks", `{"created_by":"s1","title":""}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("empty title: %d", w.Code)
	}
	w = commsDo(t, s.handleListSharedTasks, "GET", "/api/shared-tasks?status=done", "", nil)
	if !strings.Contains(w.Body.String(), "Add index") {
		t.Fatalf("list: %s", w.Body)
	}
	if w := commsDo(t, s.handleListSharedTasks, "GET", "/api/shared-tasks?status=nope", "", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("list bad status: %d", w.Code)
	}
}

func TestHandleListAndEndPeers(t *testing.T) {
	s := commsTestServer(t)
	registerTestPeer(t, s, "s1", "claude-code")
	registerTestPeer(t, s, "codex:1", "codex")
	if w := commsDo(t, s.handleEndPeer, "POST", "/api/peers/codex:1/end", ``, map[string]string{"key": "codex:1"}); w.Code != http.StatusOK {
		t.Fatalf("end: %d %s", w.Code, w.Body)
	}
	if w := commsDo(t, s.handleEndPeer, "POST", "/api/peers/zz/end", ``, map[string]string{"key": "zz"}); w.Code != http.StatusNotFound {
		t.Fatalf("end missing: %d", w.Code)
	}
	var out struct{ Peers []db.Peer }
	w := commsDo(t, s.handleListPeers, "GET", "/api/peers?active=1", "", nil)
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != http.StatusOK || len(out.Peers) != 1 || out.Peers[0].SessionKey != "s1" {
		t.Fatalf("active peers: %d %s", w.Code, w.Body)
	}
	w = commsDo(t, s.handleListPeers, "GET", "/api/peers", "", nil)
	json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.Peers) != 2 {
		t.Fatalf("all peers: %s", w.Body)
	}
}

func TestHandleClaimInbox_UnknownSessionAndRegister(t *testing.T) {
	s := commsTestServer(t)
	if w := commsDo(t, s.handleClaimInbox, "POST", "/api/peers/ghost/inbox/claim", ``, map[string]string{"key": "ghost"}); w.Code != http.StatusNotFound {
		t.Fatalf("unknown session: %d", w.Code)
	}
	if w := commsDo(t, s.handleClaimInbox, "POST", "/api/peers/s9/inbox/claim?register=1", ``, map[string]string{"key": "s9"}); w.Code != http.StatusOK {
		t.Fatalf("register claim: %d %s", w.Code, w.Body)
	}
}

func TestHandleInbox_FilterValidation(t *testing.T) {
	s := commsTestServer(t)
	registerTestPeer(t, s, "s1", "claude-code")
	for _, q := range []string{"kind=donee", "from=nobody"} {
		if w := commsDo(t, s.handleWaitInbox, "POST", "/api/peers/s1/inbox/wait?timeout_s=1&"+q, ``, map[string]string{"key": "s1"}); w.Code != http.StatusBadRequest {
			t.Errorf("wait %s: %d", q, w.Code)
		}
		if w := commsDo(t, s.handleClaimInbox, "POST", "/api/peers/s1/inbox/claim?"+q, ``, map[string]string{"key": "s1"}); w.Code != http.StatusBadRequest {
			t.Errorf("claim %s: %d", q, w.Code)
		}
	}
}

func TestHandleSendMessage_LimitsUnknownSenderAndEndedRecipient(t *testing.T) {
	s := commsTestServer(t)
	registerTestPeer(t, s, "s1", "claude-code")
	c := registerTestPeer(t, s, "codex:1", "codex")
	long := strings.Repeat("x", db.MaxMessageBody+1)
	if w := commsDo(t, s.handleSendMessage, "POST", "/api/messages", `{"from":"s1","to":"codex","body":"`+long+`"}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("long body: %d", w.Code)
	}
	if w := commsDo(t, s.handleSendMessage, "POST", "/api/messages", `{"from":"ghost","to":"codex","body":"x"}`, nil); w.Code != http.StatusNotFound {
		t.Fatalf("unknown sender: %d", w.Code)
	}
	commsDo(t, s.handleEndPeer, "POST", "/", ``, map[string]string{"key": "codex:1"})
	w := commsDo(t, s.handleSendMessage, "POST", "/api/messages", `{"from":"s1","to":"`+c.Handle+`","body":"x"}`, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "has ended") {
		t.Fatalf("ended recipient note: %d %s", w.Code, w.Body)
	}
}

func TestHandleUpdateSharedTask_ClaimTakenConflicts(t *testing.T) {
	s := commsTestServer(t)
	registerTestPeer(t, s, "s1", "claude-code")
	c := registerTestPeer(t, s, "codex:1", "codex")
	registerTestPeer(t, s, "opencode:1", "opencode")
	w := commsDo(t, s.handleCreateSharedTask, "POST", "/api/shared-tasks", `{"created_by":"s1","title":"T","assignee":"`+c.Handle+`"}`, nil)
	var task db.SharedTask
	json.Unmarshal(w.Body.Bytes(), &task)
	w = commsDo(t, s.handleUpdateSharedTask, "PATCH", "/", `{"actor":"opencode:1","status":"claimed"}`, map[string]string{"id": strconv.FormatInt(task.ID, 10)})
	if w.Code != http.StatusConflict {
		t.Fatalf("taken: %d %s", w.Code, w.Body)
	}
}

func TestHandleClaimInbox_HandleIsNotAnIdentity(t *testing.T) {
	s := commsTestServer(t)
	registerTestPeer(t, s, "s1", "claude-code")
	c := registerTestPeer(t, s, "codex:1", "codex")
	commsDo(t, s.handleSendMessage, "POST", "/api/messages", `{"from":"s1","to":"`+c.Handle+`","body":"secret"}`, nil)
	if w := commsDo(t, s.handleClaimInbox, "POST", "/", ``, map[string]string{"key": c.Handle}); w.Code != http.StatusNotFound {
		t.Fatalf("claim by handle: %d %s", w.Code, w.Body)
	}
	if w := commsDo(t, s.handleSendMessage, "POST", "/api/messages", `{"from":"`+c.Handle+`","to":"s1","body":"spoof"}`, nil); w.Code != http.StatusNotFound {
		t.Fatalf("send as handle: %d %s", w.Code, w.Body)
	}
}
