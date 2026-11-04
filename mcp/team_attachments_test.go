package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teamruntime/attachments"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

func TestAttachmentMetadataRejectsConflictingSources(t *testing.T) {
	s := &Server{tools: map[string]Tool{}}
	s.Register(Tool{Name: "echo", Handler: func(args map[string]any) (any, error) { return args, nil }})
	for _, meta := range []string{
		`{"x-codex-turn-metadata":"{\"thread_id\":\"one\",\"turn_id\":\"turn\"}","openai/threadId":"two"}`,
		`{"x-codex-turn-metadata":{"thread_id":"one","turn_id":"turn"},"openai/turnId":"other"}`,
		`{"openai/threadId":123}`,
	} {
		params := `{"name":"echo","arguments":{},"_meta":` + meta + `}`
		r := result(t, call(t, s, 1, "tools/call", params))
		if r["isError"] != true {
			b, _ := json.Marshal(r)
			t.Fatalf("untrusted/conflicting metadata accepted: %s", b)
		}
	}
}

func TestAttachmentMetadataSelectsActualSourceAcrossNativeEncodings(t *testing.T) {
	f := newAttachmentFixture(t, "one", "two")
	for _, tc := range []struct{ id, meta string }{
		{"one", `{"x-codex-turn-metadata":"{\"thread_id\":\"one\",\"turn_id\":\"human-one\"}"}`},
		{"two", `{"x-codex-turn-metadata":{"thread_id":"two","turn_id":"human-two"},"openai/threadId":"two","openai/turnId":"human-two"}`},
	} {
		response := result(t, call(t, f.s, 1, "tools/call", `{"name":"team_join","arguments":{},"_meta":`+tc.meta+`}`))
		if response["isError"] == true {
			t.Fatalf("native metadata rejected: %+v", response)
		}
		if f.s.attachments.lookup(tc.id) == nil {
			t.Fatalf("metadata source %s was not bound", tc.id)
		}
	}
	requests := f.requestsAt("/api/team-attachments/join")
	if len(requests) != 2 || requests[0].body["native_id"] != "one" || requests[1].body["native_id"] != "two" {
		t.Fatalf("native metadata crossed identities: %+v", requests)
	}
}

type fakeAttachmentNative struct {
	mu          sync.Mutex
	snapshot    attachments.Snapshot
	snapshotErr error
	sendErr     error
	sends       []string
	closed      int
	sent        chan string
	sendEntered chan struct{}
	sendRelease <-chan struct{}
}

func (f *fakeAttachmentNative) Snapshot(ctx context.Context) (attachments.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return attachments.Snapshot{}, err
	}
	if f.snapshotErr != nil {
		return attachments.Snapshot{}, f.snapshotErr
	}
	return f.snapshot, nil
}
func (f *fakeAttachmentNative) Send(ctx context.Context, text string) error {
	f.mu.Lock()
	entered, release := f.sendEntered, f.sendRelease
	f.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	f.sends = append(f.sends, text)
	err := f.sendErr
	f.mu.Unlock()
	f.sent <- text
	return err
}

func TestAttachmentTerminalCannotFailInputBeforeNativeAdmission(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	b := f.s.attachments.lookup("one")
	f.natives["one"].change(func(s *attachments.Snapshot) { s.State = "idle"; s.TurnStatus = "completed" })
	if err := b.refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	// A new human turn finishes after the last idle observation, before Send
	// acquires the native host's gate. Its terminal is unrelated to this input.
	f.natives["one"].change(func(s *attachments.Snapshot) { s.TurnID = "finished-before-send" })
	entered, release := make(chan struct{}, 1), make(chan struct{})
	f.natives["one"].mu.Lock()
	f.natives["one"].sendEntered = entered
	f.natives["one"].sendRelease = release
	f.natives["one"].mu.Unlock()
	f.inputs["private-one"] <- teams.Input{ID: "new-input", Text: "Scoped task"}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("native send not entered")
	}
	if err := b.refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	if len(f.requestsAt("/api/team-attachments/fail")) != 0 {
		t.Fatal("unrelated pre-admission terminal failed an input that has not been sent")
	}
	close(release)
	select {
	case <-f.natives["one"].sent:
	case <-time.After(time.Second):
		t.Fatal("native input was not accepted")
	}
	awaitAttachment(t, func() bool { return len(f.requestsAt("/api/team-attachments/next")) >= 2 })
	if err := b.refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	if len(f.requestsAt("/api/team-attachments/fail")) != 0 {
		t.Fatal("retained old terminal was mistaken for the accepted input's turn")
	}
	f.natives["one"].change(func(s *attachments.Snapshot) {
		s.TurnID = "delivered-turn"
		s.TurnStatus = "inProgress"
		s.State = "active"
		s.Origin = "team"
	})
	if err := b.refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	f.natives["one"].change(func(s *attachments.Snapshot) { s.TurnStatus = "completed"; s.State = "idle" })
	if err := b.refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	if len(f.requestsAt("/api/team-attachments/fail")) != 1 {
		t.Fatal("observed post-admission turn without ACK did not become uncertain")
	}
}

func TestAttachmentAckAndResultCanArriveBeforeNativeSendReceipt(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	entered, release := make(chan struct{}, 1), make(chan struct{})
	f.natives["one"].mu.Lock()
	f.natives["one"].sendEntered = entered
	f.natives["one"].sendRelease = release
	f.natives["one"].mu.Unlock()
	f.inputs["private-one"] <- teams.Input{ID: "coalesced-input", Text: "Scoped task"}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("native send not entered")
	}
	completed := make(chan error, 1)
	go func() {
		_, err := f.call("one", "human-one", "team_ack", map[string]any{"task_id": "coalesced-input"})
		if err == nil {
			_, err = f.call("one", "human-one", "team_result", map[string]any{"task_id": "coalesced-input", "text": "Finished before receipt"})
		}
		completed <- err
	}()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("native receipt blocked genuine same-turn ACK/result")
	}
	close(release)
	awaitAttachment(t, func() bool { return len(f.requestsAt("/api/team-attachments/next")) >= 2 })
	f.natives["one"].change(func(s *attachments.Snapshot) { s.TurnStatus = "completed"; s.State = "idle" })
	if err := f.s.attachments.lookup("one").refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	if len(f.requestsAt("/api/team-attachments/result")) != 1 || len(f.requestsAt("/api/team-attachments/fail")) != 0 {
		t.Fatal("late admission receipt resurrected or failed a completed task")
	}
}

func TestAttachmentClosedTransportFallsBackToGenuineNativeIdentity(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	f.s.attachments.lookup("one").close()
	if _, err := f.call("one", "human-one", "register_workflow", map[string]any{"id": "spec-manual", "type": "spec", "title": "Continue manually", "session_id": "forged"}); err != nil {
		t.Fatalf("closed bridge blocked ordinary native workflow: %v", err)
	}
	requests := f.requestsAt("/api/workflows")
	if len(requests) != 1 || requests[0].token != "" || requests[0].body["runtime_session_id"] != "one" {
		t.Fatalf("closed bridge leaked credential or accepted argument identity: %+v", requests)
	}
}

func TestAttachmentLeaveAfterConnectionClosedIsIdempotent(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	f.s.attachments.lookup("one").close()
	before := len(f.requestsAt("/api/team-attachments/leave"))
	for i := 0; i < 2; i++ {
		if _, err := f.call("one", "human-one", "team_leave", map[string]any{}); err != nil {
			t.Fatalf("already detached chat cannot leave safely: %v", err)
		}
	}
	if len(f.requestsAt("/api/team-attachments/leave")) != before {
		t.Fatal("repeated leave reused a revoked transport capability")
	}
}

func TestAttachmentExplicitJoinRebindsAfterUnreportedAckTerminal(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	deliverAttachment(t, f, "one", "unfinished")
	if _, err := f.call("one", "human-one", "team_ack", map[string]any{"task_id": "unfinished"}); err != nil {
		t.Fatal(err)
	}
	f.natives["one"].change(func(s *attachments.Snapshot) { s.State = "idle"; s.TurnStatus = "completed" })
	if err := f.s.attachments.lookup("one").refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	old := f.s.attachments.lookup("one")
	f.natives["one"].change(func(s *attachments.Snapshot) {
		s.State = "active"
		s.TurnStatus = "inProgress"
		s.TurnID = "human-reconnect"
	})
	if _, err := f.call("one", "human-reconnect", "team_join", nil); err != nil {
		t.Fatal(err)
	}
	if len(f.requestsAt("/api/team-attachments/join")) != 2 || len(f.requestsAt("/api/team-attachments/leave")) != 1 || f.s.attachments.lookup("one") == old {
		t.Fatal("explicit reconnect returned the unhealthy old binding")
	}
	f.natives["one"].mu.Lock()
	sends := len(f.natives["one"].sends)
	f.natives["one"].mu.Unlock()
	if sends != 1 || len(f.requestsAt("/api/team-attachments/result")) != 0 {
		t.Fatal("reconnect replayed or completed uncertain old work")
	}
}
func (f *fakeAttachmentNative) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}
func (f *fakeAttachmentNative) change(fn func(*attachments.Snapshot)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(&f.snapshot)
}

type attachmentRequest struct {
	path, token string
	body        map[string]any
}
type attachmentFixture struct {
	mu           sync.Mutex
	requests     []attachmentRequest
	natives      map[string]*fakeAttachmentNative
	inputs       map[string]chan teams.Input
	s            *Server
	api          *httptest.Server
	needsTeam    bool
	rejectResult bool
}

func newAttachmentFixture(t *testing.T, ids ...string) *attachmentFixture {
	t.Helper()
	t.Setenv("STRATUS_TEAM_TOKEN", "")
	f := &attachmentFixture{natives: map[string]*fakeAttachmentNative{}, inputs: map[string]chan teams.Input{}}
	for _, id := range ids {
		f.natives[id] = &fakeAttachmentNative{snapshot: attachments.Snapshot{NativeID: id, ProjectRoot: "/project", State: "active", TurnID: "human-" + id, TurnStatus: "inProgress", Origin: "user"}, sent: make(chan string, 8)}
		f.inputs["private-"+id] = make(chan teams.Input, 8)
	}
	f.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		token := r.Header.Get("X-Stratus-Team-Token")
		f.mu.Lock()
		f.requests = append(f.requests, attachmentRequest{r.URL.Path, token, body})
		needsTeam := f.needsTeam
		rejectResult := f.rejectResult
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/team-attachments/join":
			if needsTeam {
				io.WriteString(w, `{"status":"needs_team"}`)
				return
			}
			id, _ := body["native_id"].(string)
			json.NewEncoder(w).Encode(map[string]any{"team_id": "team-" + id, "member_id": "member-" + id, "native_session_id": id, "generation": 1, "token": "private-" + id})
		case "/api/team-attachments/next":
			select {
			case input := <-f.inputs[token]:
				json.NewEncoder(w).Encode(map[string]any{"input": input})
			case <-r.Context().Done():
			}
		case "/api/team-context":
			json.NewEncoder(w).Encode(map[string]any{"secret_echo": token, "session_key": r.URL.Query().Get("session_key")})
		case "/api/team-attachments/result":
			if rejectResult {
				w.WriteHeader(http.StatusConflict)
				io.WriteString(w, `{"error":"Result outcome is uncertain"}`)
			} else {
				io.WriteString(w, `{}`)
			}
		default:
			io.WriteString(w, `{}`)
		}
	}))
	f.s = &Server{tools: map[string]Tool{}}
	RegisterToolsForRuntime(f.s, f.api.URL, f.api.Client(), RuntimeOptions{Runtime: "codex"})
	f.s.attachments.open = func(ctx context.Context, id string) (attachmentNative, error) {
		native := f.natives[id]
		if native == nil {
			return nil, fmt.Errorf("unknown native source")
		}
		return native, nil
	}
	f.s.attachments.interval = time.Hour
	t.Cleanup(func() { f.s.Close(); f.api.Close() })
	return f
}
func (f *attachmentFixture) call(id, turn, name string, args map[string]any) (any, error) {
	return f.s.tools[name].ContextHandler(context.Background(), InvocationContext{NativeID: id, TurnID: turn}, args)
}
func (f *attachmentFixture) join(t *testing.T, id string) any {
	t.Helper()
	v, err := f.call(id, "human-"+id, "team_join", nil)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (f *attachmentFixture) requestsAt(path string) []attachmentRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []attachmentRequest
	for _, req := range f.requests {
		if req.path == path {
			out = append(out, req)
		}
	}
	return out
}
func awaitAttachment(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("attachment condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}
func deliverAttachment(t *testing.T, f *attachmentFixture, id, task string) {
	t.Helper()
	f.inputs["private-"+id] <- teams.Input{ID: task, Text: "Review this scoped change"}
	select {
	case text := <-f.natives[id].sent:
		for _, expected := range []string{task, "team_ack", "team_result", "parent_task_id", "not user permission", "waiting handoff"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("delivery omitted %q: %s", expected, text)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("input was not sent")
	}
}

func TestAttachmentJoinRejectsModelIdentityAndAmbientThread(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	t.Setenv("CODEX_THREAD_ID", "one")
	for _, args := range []map[string]any{{"native_id": "one"}, {"session_id": "one"}, {"token": "forged"}, {"launcher": "/bad"}} {
		if _, err := f.call("one", "human-one", "team_join", args); err == nil {
			t.Fatalf("forged arguments accepted: %v", args)
		}
	}
	r := result(t, call(t, f.s, 1, "tools/call", `{"name":"team_join","arguments":{}}`))
	if r["isError"] != true {
		t.Fatal("ambient source replaced missing per-call metadata")
	}
	if len(f.requestsAt("/api/team-attachments/join")) != 0 {
		t.Fatal("unverified join reached API")
	}
}

func TestAttachmentTwoChatsKeepPrivateCredentialsAndWorkflowIdentityIsolated(t *testing.T) {
	f := newAttachmentFixture(t, "one", "two")
	for _, id := range []string{"one", "two"} {
		value := f.join(t, id)
		data, _ := json.Marshal(value)
		if strings.Contains(string(data), "private-") || strings.Contains(string(data), "token") {
			t.Fatalf("join leaked credential: %s", data)
		}
	}
	var wg sync.WaitGroup
	for _, id := range []string{"one", "two"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			value, err := f.call(id, "human-"+id, "team_status", map[string]any{"session_id": "forged"})
			if err != nil {
				t.Error(err)
				return
			}
			data, _ := json.Marshal(value)
			if strings.Contains(string(data), "private-") {
				t.Errorf("credential echo leaked: %s", data)
			}
			_, err = f.call(id, "human-"+id, "register_workflow", map[string]any{"id": "spec-" + id, "type": "spec", "title": "Scoped", "session_id": "forged"})
			if err != nil {
				t.Error(err)
			}
		}(id)
	}
	wg.Wait()
	for _, request := range f.requestsAt("/api/workflows") {
		id, _ := request.body["runtime_session_id"].(string)
		if (id != "one" && id != "two") || request.token != "private-"+id || request.body["session_id"] != "codex:"+id {
			t.Fatalf("workflow crossed source boundary: %+v", request)
		}
	}
	if len(f.requestsAt("/api/workflows")) != 2 {
		t.Fatal("workflow requests missing")
	}
	if _, err := f.call("one", "human-two", "team_status", nil); err == nil {
		t.Fatal("stale/wrong turn was accepted")
	}
}

func TestAttachmentExplicitResultInCoalescedHumanTurn(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	deliverAttachment(t, f, "one", "task-1")
	if _, err := f.call("one", "human-one", "team_result", map[string]any{"task_id": "task-1", "text": "early"}); err == nil {
		t.Fatal("result without acknowledgement accepted")
	}
	if _, err := f.call("one", "wrong-turn", "team_ack", map[string]any{"task_id": "task-1"}); err == nil {
		t.Fatal("ack with wrong native turn accepted")
	}
	if _, err := f.call("one", "human-one", "team_ack", map[string]any{"task_id": "task-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.call("one", "human-one", "team_delegate", map[string]any{"text": "Review", "role": "review", "parent_task_id": "task-1"}); err != nil {
		t.Fatal(err)
	}
	requests := f.requestsAt("/api/team-delegate")
	if len(requests) != 1 || requests[0].body["parent_task_id"] != "task-1" {
		t.Fatalf("ack task scope missing: %+v", requests)
	}
	if _, err := f.call("one", "human-one", "team_result", map[string]any{"task_id": "task-1", "text": "Waiting for review; handoff complete"}); err != nil {
		t.Fatal(err)
	}
	f.natives["one"].change(func(s *attachments.Snapshot) { s.State = "idle"; s.TurnStatus = "completed" })
	if err := f.s.attachments.lookup("one").refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	results := f.requestsAt("/api/team-attachments/result")
	if len(results) != 1 || results[0].body["native_turn_id"] != "human-one" {
		t.Fatalf("uncorrelated result: %+v", results)
	}
	observed := f.requestsAt("/api/team-attachments/observe")
	last := observed[len(observed)-1].body["turn"].(map[string]any)
	if last["state"] != "complete" || last["origin"] != "user" || last["input_id"] != nil {
		t.Fatalf("human terminal was falsified: %+v", last)
	}
}

func TestAttachmentNativeIdleDoesNotCompleteUnreportedTask(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	deliverAttachment(t, f, "one", "task-1")
	if _, err := f.call("one", "human-one", "team_ack", map[string]any{"task_id": "task-1"}); err != nil {
		t.Fatal(err)
	}
	f.natives["one"].change(func(s *attachments.Snapshot) { s.State = "idle"; s.TurnStatus = "completed" })
	if err := f.s.attachments.lookup("one").refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	if len(f.requestsAt("/api/team-attachments/result")) != 0 {
		t.Fatal("native terminal fabricated a team result")
	}
	observed := f.requestsAt("/api/team-attachments/observe")
	if observed[len(observed)-1].body["turn"].(map[string]any)["state"] != "complete" {
		t.Fatal("real terminal not reported for broker reconciliation")
	}
}

func TestAttachmentUnknownNativeSendIsNotRetried(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.natives["one"].sendErr = errors.New("accepted then connection lost")
	f.join(t, "one")
	deliverAttachment(t, f, "one", "task-1")
	awaitAttachment(t, func() bool { return len(f.requestsAt("/api/team-attachments/fail")) == 1 })
	awaitAttachment(t, func() bool { return f.s.attachments.lookup("one") == nil })
	f.natives["one"].mu.Lock()
	sends := len(f.natives["one"].sends)
	f.natives["one"].mu.Unlock()
	if sends != 1 {
		t.Fatalf("uncertain send retried %d times", sends)
	}
	if len(f.requestsAt("/api/team-attachments/result")) != 0 {
		t.Fatal("send acceptance became completion")
	}
}

func TestAttachmentDuplicateClaimIsNeverSentTwice(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	deliverAttachment(t, f, "one", "task-1")
	f.inputs["private-one"] <- teams.Input{ID: "task-1", Text: "duplicate"}
	awaitAttachment(t, func() bool { return f.s.attachments.lookup("one") == nil })
	f.natives["one"].mu.Lock()
	sends := len(f.natives["one"].sends)
	f.natives["one"].mu.Unlock()
	if sends != 1 {
		t.Fatalf("claimed input sent %d times", sends)
	}
}

func TestAttachmentLeaveAndEOFOnlyCloseOwnedTransport(t *testing.T) {
	for _, eof := range []bool{false, true} {
		t.Run(fmt.Sprint(eof), func(t *testing.T) {
			f := newAttachmentFixture(t, "one")
			f.join(t, "one")
			deliverAttachment(t, f, "one", "task-1")
			if eof {
				f.s.reader = bufio.NewReader(strings.NewReader(""))
				if err := f.s.Serve(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := f.call("one", "human-one", "team_leave", nil); err != nil {
				t.Fatal(err)
			}
			f.natives["one"].mu.Lock()
			closed := f.natives["one"].closed
			sends := len(f.natives["one"].sends)
			f.natives["one"].mu.Unlock()
			if closed != 1 || sends != 1 || len(f.requestsAt("/api/team-attachments/leave")) != 1 {
				t.Fatalf("incorrect detach: closes=%d sends=%d", closed, sends)
			}
			if len(f.requestsAt("/api/team-attachments/result")) != 0 {
				t.Fatal("detach fabricated result")
			}
		})
	}
}

func TestAttachmentNeedsTeamDoesNotCreateBinding(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.needsTeam = true
	value, err := f.call("one", "human-one", "team_join", map[string]any{"create_team": false})
	if err != nil {
		t.Fatal(err)
	}
	if value.(map[string]string)["status"] != "needs_team" || f.s.attachments.lookup("one") != nil {
		t.Fatalf("unjoined source marked ready: %v", value)
	}
	f.natives["one"].mu.Lock()
	closed := f.natives["one"].closed
	f.natives["one"].mu.Unlock()
	if closed != 1 {
		t.Fatal("unused helper was not closed")
	}
}

func TestAttachmentJoinRejectsUnknownOrigin(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.natives["one"].change(func(s *attachments.Snapshot) { s.Origin = "unknown" })
	if _, err := f.call("one", "human-one", "team_join", nil); err == nil {
		t.Fatal("unknown native origin granted human authority")
	}
	if len(f.requestsAt("/api/team-attachments/join")) != 0 {
		t.Fatal("unknown native source reached join")
	}
}

func TestAttachmentRejectsProjectChangeAfterJoin(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	f.natives["one"].change(func(s *attachments.Snapshot) { s.ProjectRoot = "/other-project" })
	if _, err := f.call("one", "human-one", "team_delegate", map[string]any{"role": "review", "text": "Cross project"}); err == nil {
		t.Fatal("changed native project retained team authority")
	}
	if len(f.requestsAt("/api/team-delegate")) != 0 {
		t.Fatal("cross-project request reached API")
	}
}

func TestAttachmentTerminalBeforeAckFailsClaimWithoutInventingResult(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	deliverAttachment(t, f, "one", "unacknowledged")
	awaitAttachment(t, func() bool { return len(f.requestsAt("/api/team-attachments/next")) >= 2 })
	f.natives["one"].change(func(s *attachments.Snapshot) { s.State = "idle"; s.TurnStatus = "completed" })
	if err := f.s.attachments.lookup("one").refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	if len(f.requestsAt("/api/team-attachments/fail")) != 1 {
		t.Fatal("unacknowledged terminal left a claimed task waiting forever")
	}
	if len(f.requestsAt("/api/team-attachments/result")) != 0 {
		t.Fatal("terminal invented a task result")
	}
}

func TestAttachmentUnobservedAcceptedInputHasBoundedAckDeadline(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	b := f.s.attachments.lookup("one")
	f.natives["one"].change(func(s *attachments.Snapshot) { s.State = "idle"; s.TurnStatus = "completed" })
	if err := b.refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	deliverAttachment(t, f, "one", "unobserved")
	awaitAttachment(t, func() bool { return len(f.requestsAt("/api/team-attachments/next")) >= 2 })
	b.mu.Lock()
	b.pendingDeadline = time.Now().Add(-time.Second)
	b.mu.Unlock()
	if err := b.refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	failures := f.requestsAt("/api/team-attachments/fail")
	if len(failures) != 1 || failures[0].body["task_id"] != "unobserved" || len(f.requestsAt("/api/team-attachments/result")) != 0 {
		t.Fatalf("accepted task did not stop as uncertain after ACK deadline: %+v", failures)
	}
	if err := b.refresh(context.Background(), InvocationContext{}); err != nil {
		t.Fatal(err)
	}
	if len(f.requestsAt("/api/team-attachments/fail")) != 1 {
		t.Fatal("expired task failed repeatedly")
	}
}

func TestAttachmentExplicitRejoinRecoversResultAndSnapshotUncertainty(t *testing.T) {
	for _, failure := range []string{"result", "snapshot"} {
		t.Run(failure, func(t *testing.T) {
			f := newAttachmentFixture(t, "one")
			f.join(t, "one")
			old := f.s.attachments.lookup("one")
			if failure == "result" {
				deliverAttachment(t, f, "one", "uncertain-result")
				if _, err := f.call("one", "human-one", "team_ack", map[string]any{"task_id": "uncertain-result"}); err != nil {
					t.Fatal(err)
				}
				f.mu.Lock()
				f.rejectResult = true
				f.mu.Unlock()
				if _, err := f.call("one", "human-one", "team_result", map[string]any{"task_id": "uncertain-result", "text": "Result"}); err == nil {
					t.Fatal("uncertain server result accepted")
				}
			} else {
				f.natives["one"].mu.Lock()
				f.natives["one"].snapshotErr = errors.New("native observation unavailable")
				f.natives["one"].mu.Unlock()
				if _, err := f.call("one", "human-one", "team_status", nil); err == nil {
					t.Fatal("missing native observation accepted")
				}
				f.natives["one"].mu.Lock()
				f.natives["one"].snapshotErr = nil
				f.natives["one"].mu.Unlock()
			}
			if _, err := f.call("one", "human-one", "team_join", nil); err != nil {
				t.Fatal(err)
			}
			if f.s.attachments.lookup("one") == old || len(f.requestsAt("/api/team-attachments/join")) != 2 {
				t.Fatal("explicit rejoin kept an uncertain binding")
			}
		})
	}
}

func TestAttachmentHealthyJoinRemainsIdempotent(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	old := f.s.attachments.lookup("one")
	f.join(t, "one")
	if old != f.s.attachments.lookup("one") || len(f.requestsAt("/api/team-attachments/join")) != 1 || len(f.requestsAt("/api/team-attachments/leave")) != 0 {
		t.Fatal("healthy repeat join replaced the live connection")
	}
}

func TestAttachmentJoinedWorkflowSchemaDoesNotRequireGuessedSessionID(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	for _, name := range []string{"register_workflow", "bind_workflow_session", "bind_agent_role"} {
		for _, required := range f.s.tools[name].InputSchema["required"].([]string) {
			if required == "session_id" {
				t.Fatalf("%s requires model-guessed identity despite transport binding", name)
			}
		}
	}
}

func TestAttachmentQuickNextTurnReconcilesVerifiedPriorTerminal(t *testing.T) {
	for _, ack := range []bool{false, true} {
		t.Run(fmt.Sprint(ack), func(t *testing.T) {
			f := newAttachmentFixture(t, "one")
			f.join(t, "one")
			if ack {
				deliverAttachment(t, f, "one", "task-1")
				if _, err := f.call("one", "human-one", "team_ack", map[string]any{"task_id": "task-1"}); err != nil {
					t.Fatal(err)
				}
			}
			f.natives["one"].change(func(s *attachments.Snapshot) {
				// A real native snapshot, not the previous state inferred from a new ID.
				if err := json.Unmarshal([]byte(`{"native_id":"one","project_root":"/project","state":"active","turn_id":"human-next","turn_status":"inProgress","origin":"user","previous_turns":[{"id":"human-one","status":"completed"}]}`), s); err != nil {
					t.Fatal(err)
				}
			})
			b := f.s.attachments.lookup("one")
			if err := b.refresh(context.Background(), InvocationContext{NativeID: "one", TurnID: "human-next"}); err != nil {
				t.Fatalf("quick next native prompt disconnected: %v", err)
			}
			observations := f.requestsAt("/api/team-attachments/observe")
			if len(observations) != 3 {
				t.Fatalf("wanted old running, verified old terminal, then new running: %+v", observations)
			}
			prior := observations[1].body["turn"].(map[string]any)
			latest := observations[2].body["turn"].(map[string]any)
			if prior["id"] != "human-one" || prior["state"] != "complete" || latest["id"] != "human-next" || latest["state"] != "running" {
				t.Fatalf("native succession evidence lost: %+v", observations)
			}
			if len(f.requestsAt("/api/team-attachments/result")) != 0 {
				t.Fatal("ACK lacking result became a fabricated completion")
			}
			if b.ctx.Err() != nil {
				t.Fatal("verified native succession closed connection")
			}
		})
	}
}

func TestAttachmentNextTurnWithoutTerminalEvidenceDoesNotInventPriorCompletion(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.join(t, "one")
	f.natives["one"].change(func(s *attachments.Snapshot) { s.TurnID = "unproven-next" })
	if err := f.s.attachments.lookup("one").refresh(context.Background(), InvocationContext{}); err == nil {
		t.Fatal("missing terminal evidence was inferred from a newer native ID")
	}
	if len(f.requestsAt("/api/team-attachments/observe")) != 1 || len(f.requestsAt("/api/team-attachments/result")) != 0 {
		t.Fatal("missing native evidence produced terminal output")
	}
}

func TestAttachmentObserverRunsWhileNextInputLongPollWaits(t *testing.T) {
	f := newAttachmentFixture(t, "one")
	f.s.attachments.interval = 5 * time.Millisecond
	f.join(t, "one")
	awaitAttachment(t, func() bool { return len(f.requestsAt("/api/team-attachments/next")) == 1 })
	f.natives["one"].change(func(s *attachments.Snapshot) { s.TurnStatus = "completed"; s.State = "idle" })
	awaitAttachment(t, func() bool { return len(f.requestsAt("/api/team-attachments/observe")) == 2 })
	if len(f.requestsAt("/api/team-attachments/next")) != 1 {
		t.Fatal("observer needed to restart the blocked input poll")
	}
	if len(f.requestsAt("/api/team-attachments/result")) != 0 {
		t.Fatal("metadata polling completed a task")
	}
}

func TestAttachmentToolsExposeOnlyCurrentChatJoin(t *testing.T) {
	t.Setenv("STRATUS_TEAM_TOKEN", "")
	s := &Server{tools: map[string]Tool{}}
	RegisterToolsForRuntime(s, "http://127.0.0.1:1", nil, RuntimeOptions{Runtime: "codex"})
	for _, name := range []string{"team_join", "team_leave", "team_ack", "team_result", "team_request"} {
		tool, ok := s.tools[name]
		if !ok {
			t.Fatalf("current-chat attachment tool %s is missing", name)
		}
		properties, _ := tool.InputSchema["properties"].(map[string]any)
		for _, forbidden := range []string{"native_id", "session_id", "token", "launcher", "server_path"} {
			if _, ok := properties[forbidden]; ok {
				t.Fatalf("%s accepts model-supplied %s", name, forbidden)
			}
		}
	}
}
