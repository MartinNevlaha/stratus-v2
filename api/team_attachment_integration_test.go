package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/mcp"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

// Only the native application and model execution are fixtures. The two MCP
// transports, connector, HTTP routes, capability guard, manager and SQLite are real.
func TestAttachmentIntegrationOriginalChatRoundTrip(t *testing.T) {
	f := newAttachmentIntegration(t)
	client := f.client(t)
	joined := client.tool(t, "team_join", nil, "human-1", false)
	memberID := joined["member_id"].(string)
	initial := f.team(t)
	member := integrationMember(t, initial, memberID)
	if member.NativeID != "original-chat" || member.Profile.Model != "" || member.Profile.Effort != "" || initial.Turns != 0 {
		t.Fatalf("joining changed native identity/profile or consumed an automatic turn: %+v", initial)
	}
	if initial.Members[0].Role != "lead" || member.Role != "member" {
		t.Fatalf("joining stole the existing coordinator: %+v", initial.Members)
	}
	client.noSecret(t, member.Token)
	client.tool(t, "team_status", map[string]any{"session_id": "forged-chat"}, "human-1", false)
	client.noSecret(t, member.Token)

	args := map[string]any{"to": "worker", "text": "Inspect the fixture and return one finding", "request_id": "work-1"}
	delegated := client.tool(t, "team_delegate", args, "human-1", false)
	if delegated["from"] != memberID || delegated["return_to"] != memberID || delegated["to"] != "worker" {
		t.Fatalf("delegation lost original-chat routing: %v", delegated)
	}
	client.tool(t, "team_delegate", args, "human-1", false)
	var call integrationOwnedCall
	select {
	case call = <-f.owned.calls:
	case <-time.After(5 * time.Second):
		t.Fatal("owned worker was not dispatched")
	}
	if call.spec.MemberID != "worker" || call.spec.ProjectRoot != f.server.projectRoot || call.input.ID != "work-1" {
		t.Fatalf("wrong owned worker dispatch: %+v", call)
	}
	call.reply <- "The fixture contains exactly one actionable finding."
	f.await(t, func(tm *teams.Team) bool { return integrationTask(tm, "result:work-1").State == "queued" })
	if f.native.sendCount() != 0 {
		t.Fatal("worker result interrupted the active human turn")
	}

	f.native.setTurn("human-1", "completed", "user")
	client.tool(t, "team_status", nil, "human-1", false)
	select {
	case prompt := <-f.native.sent:
		if !strings.Contains(prompt, "[Stratus colleague task: result:work-1]") || !strings.Contains(prompt, "one actionable finding") {
			t.Fatalf("return lost task identity or worker result: %s", prompt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("completed user turn did not wake the original chat")
	}
	if task := integrationTask(f.team(t), "result:work-1"); task.State == "complete" {
		t.Fatal("native acceptance was mistaken for task completion")
	}
	f.native.setTurn("team-1", "inProgress", "team")
	client.tool(t, "team_result", map[string]any{"task_id": "result:work-1", "text": "too early"}, "team-1", true)
	client.tool(t, "team_ack", map[string]any{"task_id": "result:work-1"}, "team-1", false)
	client.tool(t, "team_result", map[string]any{"task_id": "result:work-1", "text": "Finding received in the original chat."}, "team-1", false)
	client.tool(t, "team_result", map[string]any{"task_id": "result:work-1", "text": "Finding received in the original chat."}, "team-1", false)
	complete := f.await(t, func(tm *teams.Team) bool { return integrationTask(tm, "result:work-1").State == "complete" })
	if got := integrationMember(t, complete, memberID); got.NativeTurn == nil || got.NativeTurn.State != "running" || got.State != "waiting" {
		t.Fatalf("explicit task result invented native completion: %+v", got)
	}
	f.native.setTurn("team-1", "completed", "team")
	client.tool(t, "team_status", nil, "team-1", false)
	complete = f.team(t)
	if complete.Turns != 2 || complete.UsageKnown || integrationTask(complete, "work-1").State != "complete" || integrationTask(complete, "result:work-1").Result != "Finding received in the original chat." {
		t.Fatalf("incorrect final accounting/results: %+v", complete)
	}
	parent := integrationTask(complete, delegated["parent_task_id"].(string))
	if parent.Origin != "external" || parent.NativeTurnID != "human-1" || parent.State != "complete" {
		t.Fatalf("delegation did not preserve genuine human scope: %+v", parent)
	}

	// A subsequent manual turn remains active when the attachment is removed.
	f.native.setTurn("human-2", "inProgress", "user")
	client.tool(t, "team_status", nil, "human-2", false)
	client.tool(t, "team_leave", nil, "human-2", false)
	left := integrationMember(t, f.team(t), memberID)
	if left.Attachment.State != "detached" || left.Attachment.Generation <= member.Attachment.Generation {
		t.Fatalf("leave did not revoke/fence the connection: %+v", left)
	}
	f.assertRevoked(t, member.Token, member.Attachment.Generation)
	if f.native.sendCount() != 1 || f.native.currentTurn() != "human-2" {
		t.Fatal("duplicate delivery or leave changed the manual native turn")
	}
	select {
	case duplicate := <-f.owned.calls:
		t.Fatalf("identical request replayed owned work: %+v", duplicate.input)
	default:
	}
	client.noSecret(t, member.Token)
}

func TestAttachmentIntegrationForwardedRequestReturnsFinalSynthesis(t *testing.T) {
	f := newAttachmentIntegration(t)
	client := f.client(t)
	joined := client.tool(t, "team_join", nil, "human-1", false)
	requester := joined["member_id"].(string)
	request := client.tool(t, "team_request", map[string]any{"request_id": "forwarded", "text": "Have the team inspect the fixture and return the final answer."}, "human-1", false)
	if request["to"] != "lead" || request["from"] != requester || request["return_to"] != requester || request["request_task_id"] != "forwarded" {
		t.Fatalf("request lost sole-lead routing or requester lineage: %v", request)
	}
	lead := f.nextOwned(t)
	if lead.spec.MemberID != "lead" || lead.input.ID != "forwarded" {
		t.Fatalf("request did not reach the owned coordinator: %+v", lead)
	}
	// Make the requester idle before the lead's waiting handoff: premature
	// completion would now create an observable, unwanted native wakeup.
	f.native.setTurn("human-1", "completed", "user")
	client.tool(t, "team_status", nil, "human-1", false)
	body := `{"id":"forwarded-child","to":"worker","role":"mechanical","text":"Collect the evidence for the forwarded request."}`
	req, err := http.NewRequest("POST", f.api.URL+"/api/team-delegate", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Stratus-Team-Token", lead.spec.Token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var child teams.Task
	err = json.NewDecoder(response.Body).Decode(&child)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("owned lead delegation failed: HTTP %d: %v", response.StatusCode, err)
	}
	if child.From != "lead" || child.To != "worker" || child.ReturnTo != "lead" || child.ParentTaskID != "forwarded" || child.RequestTaskID != "forwarded" {
		t.Fatalf("HTTP delegation did not inherit the forwarded request: %+v", child)
	}
	worker := f.nextOwned(t)
	if worker.spec.MemberID != "worker" || worker.input.ID != "forwarded-child" {
		t.Fatalf("wrong delegated worker input: %+v", worker)
	}
	lead.reply <- "WAITING FOR THE WORKER; this is not the final answer."
	waiting := f.await(t, func(tm *teams.Team) bool { return integrationTask(tm, "forwarded").State != "running" })
	if root := integrationTask(waiting, "forwarded"); root.State != "waiting" || root.FinishedAt != nil || integrationTask(waiting, "result:forwarded").ID != "" || f.native.sendCount() != 0 {
		t.Fatalf("waiting handoff prematurely returned to the requester: %+v", waiting)
	}
	worker.reply <- "WORKER EVIDENCE: the fixture has one actionable finding."
	continuation := f.nextOwned(t)
	if continuation.spec.MemberID != "lead" || continuation.input.ID != "result:forwarded-child" || !strings.Contains(continuation.input.Text, "WORKER EVIDENCE") {
		t.Fatalf("worker evidence did not return to the sole lead: %+v", continuation)
	}
	if f.native.sendCount() != 0 {
		t.Fatal("worker completion woke requester before lead synthesis")
	}
	continuation.reply <- "FINAL SYNTHESIS: one actionable finding, verified by the coordinator."
	finished := f.await(t, func(tm *teams.Team) bool { return integrationTask(tm, "forwarded").State == "complete" })
	root, final := integrationTask(finished, "forwarded"), integrationTask(finished, "result:forwarded")
	if root.Result != final.Text || final.Text != "FINAL SYNTHESIS: one actionable finding, verified by the coordinator." || final.To != requester || final.RequestTaskID != "forwarded" || integrationTask(finished, "result:forwarded-child").RequestTaskID != "forwarded" {
		t.Fatalf("final synthesis lost durable requester lineage: %+v", finished)
	}
	select {
	case prompt := <-f.native.sent:
		if !strings.Contains(prompt, "[Stratus colleague task: result:forwarded]") || !strings.Contains(prompt, "FINAL SYNTHESIS") || strings.Contains(prompt, "WAITING FOR THE WORKER") {
			t.Fatalf("original chat received the wrong completion: %s", prompt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("final synthesis never reached the original chat")
	}
	f.native.setTurn("team-final", "inProgress", "team")
	client.tool(t, "team_ack", map[string]any{"task_id": "result:forwarded"}, "team-final", false)
	client.tool(t, "team_result", map[string]any{"task_id": "result:forwarded", "text": "Presented the final synthesis to the requester."}, "team-final", false)
	f.await(t, func(tm *teams.Team) bool { return integrationTask(tm, "result:forwarded").State == "complete" })
	f.native.setTurn("team-final", "completed", "team")
	client.tool(t, "team_status", nil, "team-final", false)
	client.tool(t, "team_leave", nil, "team-final", false)
	if f.native.sendCount() != 1 || f.team(t).Turns != 4 {
		t.Fatalf("forwarded request produced duplicate wakes or turns: %+v", f.team(t))
	}
	select {
	case extra := <-f.owned.calls:
		t.Fatalf("completed request started another owned turn: %+v", extra.input)
	default:
	}
}

func TestAttachmentIntegrationMetadataProjectAndGeneration(t *testing.T) {
	f := newAttachmentIntegration(t)
	first := f.client(t)
	first.tool(t, "team_join", map[string]any{"native_id": "forged"}, "human-1", true)
	first.tool(t, "team_join", nil, "different-turn", true)
	first.toolMeta(t, "team_join", map[string]any{}, nil, true)
	f.native.mu.Lock()
	f.native.project = t.TempDir()
	f.native.mu.Unlock()
	first.tool(t, "team_join", nil, "human-1", true)
	if len(f.team(t).Members) != 2 {
		t.Fatal("rejected join created an attachment")
	}
	f.native.mu.Lock()
	f.native.project = f.server.projectRoot
	f.native.mu.Unlock()
	joined := first.tool(t, "team_join", nil, "human-1", false)
	id := joined["member_id"].(string)
	old := integrationMember(t, f.team(t), id)

	// A second actual MCP process reconnects the same native chat. Its new
	// private capability must fence the old bridge, including its cleanup.
	second := f.client(t)
	rejoined := second.tool(t, "team_join", nil, "human-1", false)
	current := integrationMember(t, f.team(t), id)
	if rejoined["member_id"] != id || current.Attachment.Generation <= old.Attachment.Generation || current.Token == old.Token {
		t.Fatal("reconnect did not preserve member identity and rotate authority")
	}
	f.assertRevoked(t, old.Token, old.Attachment.Generation)
	first.tool(t, "team_leave", nil, "human-1", false)
	second.tool(t, "team_status", nil, "human-1", false)
	if integrationMember(t, f.team(t), id).Attachment.State != "connected" {
		t.Fatal("stale connection cleanup detached the replacement")
	}
	second.tool(t, "team_ack", map[string]any{"task_id": "not-delivered"}, "human-1", true)
	second.tool(t, "team_leave", nil, "human-1", false)
	f.assertRevoked(t, current.Token, current.Attachment.Generation)
	first.noSecret(t, old.Token)
	second.noSecret(t, current.Token)
	if f.native.sendCount() != 0 || f.team(t).Turns != 0 {
		t.Fatal("registration/reconnection/idle control invoked a model turn")
	}
}

func TestAttachmentIntegrationTurnReceiptReconciliation(t *testing.T) {
	for _, receipt := range []bool{true, false} {
		t.Run(fmt.Sprintf("terminal_receipt=%t", receipt), func(t *testing.T) {
			f := newAttachmentIntegration(t)
			client := f.client(t)
			joined := client.tool(t, "team_join", nil, "human-1", false)
			id := joined["member_id"].(string)
			original := integrationMember(t, f.team(t), id)
			// Both events happen between native polls. Only the preceding
			// terminal receipt authorizes ending the original external scope.
			f.native.mu.Lock()
			f.native.setTurnLocked("human-1", "completed", "user")
			f.native.setTurnLocked("human-2", "inProgress", "user")
			if !receipt {
				f.native.turns = f.native.turns[:1]
			}
			f.native.mu.Unlock()
			client.tool(t, "team_status", nil, "human-2", !receipt)
			tm := f.team(t)
			member := integrationMember(t, tm, id)
			previous := integrationTask(tm, original.ActiveTaskID)
			if receipt {
				if previous.State != "complete" || member.NativeTurn.ID != "human-2" || integrationTask(tm, member.ActiveTaskID).NativeTurnID != "human-2" {
					t.Fatalf("verified terminal receipt did not reconcile scopes: %+v", tm)
				}
			} else if previous.State != "running" || member.NativeTurn.ID != "human-1" {
				t.Fatalf("missing terminal evidence invented completion: %+v", tm)
			}
			client.tool(t, "team_leave", nil, "human-2", false)
			if tm.Turns != 0 || f.native.sendCount() != 0 {
				t.Fatal("observing native history created automatic model work")
			}
		})
	}
}

type integrationOwnedCall struct {
	spec  teams.SessionSpec
	input teams.Input
	reply chan string
}
type integrationOwnedAdapter struct{ calls chan integrationOwnedCall }

func (*integrationOwnedAdapter) Preflight(context.Context, string) teams.Availability {
	return teams.Availability{Runtime: "codex", Available: true, Models: []teams.Model{{ID: "gpt-6.1-sol", Efforts: []string{"high"}}}}
}
func (a *integrationOwnedAdapter) Open(_ context.Context, spec teams.SessionSpec, _ func(teams.Event)) (teams.Session, error) {
	return &integrationOwnedSession{adapter: a, spec: spec}, nil
}

type integrationOwnedSession struct {
	adapter *integrationOwnedAdapter
	spec    teams.SessionSpec
}

func (s *integrationOwnedSession) Submit(ctx context.Context, input teams.Input) (teams.Result, error) {
	call := integrationOwnedCall{spec: s.spec, input: input, reply: make(chan string, 1)}
	select {
	case s.adapter.calls <- call:
	case <-ctx.Done():
		return teams.Result{}, ctx.Err()
	}
	select {
	case result := <-call.reply:
		return teams.Result{Text: result, NativeID: "owned-" + s.spec.MemberID, Model: s.spec.Profile.Model, Effort: s.spec.Profile.Effort}, nil
	case <-ctx.Done():
		return teams.Result{}, ctx.Err()
	}
}
func (*integrationOwnedSession) Approve(context.Context, string, bool) error {
	return fmt.Errorf("unexpected approval")
}
func (*integrationOwnedSession) Interrupt(context.Context) error { return nil }
func (*integrationOwnedSession) Close() error                    { return nil }

type attachmentIntegration struct {
	server                          *Server
	api                             *httptest.Server
	teamID                          string
	owned                           *integrationOwnedAdapter
	native                          *integrationNative
	nativeURL, launcher, executable string
}

func newAttachmentIntegration(t *testing.T) *attachmentIntegration {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "integration.db"))
	if err != nil {
		t.Fatal(err)
	}
	owned := &integrationOwnedAdapter{calls: make(chan integrationOwnedCall, 8)}
	adapters := map[string]teams.Adapter{"codex": owned}
	manager, err := teams.NewManager(d.SQL(), adapters, func() {})
	if err != nil {
		d.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close(); d.Close() })
	s := &Server{db: d, projectRoot: t.TempDir(), hub: NewHub(), teams: manager, teamAdapters: adapters}
	mux := http.NewServeMux()
	s.registerTeamRoutes(mux)
	server := httptest.NewServer(s.managedTeamGuard(mux))
	t.Cleanup(server.Close)
	request := `{"members":[{"id":"lead","role":"lead","profile":{"runtime":"codex","model":"gpt-6.1-sol","effort":"high"}},{"id":"worker","role":"mechanical","profile":{"runtime":"codex","model":"gpt-6.1-sol","effort":"high"}}]}`
	response, err := http.Post(server.URL+"/api/teams", "application/json", strings.NewReader(request))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var tm teams.Team
	if err := json.NewDecoder(response.Body).Decode(&tm); err != nil || response.StatusCode != 200 {
		t.Fatalf("create team: %d %v", response.StatusCode, err)
	}
	native := &integrationNative{project: s.projectRoot, sent: make(chan string, 8)}
	native.setTurn("human-1", "inProgress", "user")
	nativeServer := httptest.NewServer(native)
	t.Cleanup(nativeServer.Close)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(t.TempDir(), "native-fixture")
	script := "#!/bin/sh\nexport STRATUS_ATTACHMENT_FIXTURE_MODE=native\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run='^TestAttachmentIntegrationProcess$'\n"
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return &attachmentIntegration{server: s, api: server, teamID: tm.ID, owned: owned, native: native, nativeURL: nativeServer.URL, launcher: launcher, executable: executable}
}

func (f *attachmentIntegration) team(t *testing.T) *teams.Team {
	t.Helper()
	tm, err := f.server.teams.Get(f.teamID)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}
func (f *attachmentIntegration) nextOwned(t *testing.T) integrationOwnedCall {
	t.Helper()
	select {
	case call := <-f.owned.calls:
		return call
	case <-time.After(5 * time.Second):
		t.Fatalf("owned session was not dispatched: %+v", f.team(t))
		return integrationOwnedCall{}
	}
}
func (f *attachmentIntegration) await(t *testing.T, predicate func(*teams.Team) bool) *teams.Team {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		tm := f.team(t)
		if predicate(tm) {
			return tm
		}
		if time.Now().After(deadline) {
			t.Fatalf("team did not reach expected state: %+v", tm)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func integrationTask(tm *teams.Team, id string) teams.Task {
	for _, task := range tm.Tasks {
		if task.ID == id {
			return task
		}
	}
	return teams.Task{}
}
func integrationMember(t *testing.T, tm *teams.Team, id string) teams.Member {
	t.Helper()
	for _, member := range tm.Members {
		if member.ID == id {
			return member
		}
	}
	t.Fatalf("missing member %s", id)
	return teams.Member{}
}
func (f *attachmentIntegration) assertRevoked(t *testing.T, token string, generation uint64) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"generation": generation, "turn": teams.NativeTurn{State: "idle"}})
	req, _ := http.NewRequest("POST", f.api.URL+"/api/team-attachments/observe", bytes.NewReader(body))
	req.Header.Set("X-Stratus-Team-Token", token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("revoked capability accepted: HTTP %d", response.StatusCode)
	}
}

// The fixture speaks the observed native schema (newest turn first) and rejects
// any native operation other than bounded reads and same-chat sends.
type integrationNative struct {
	mu      sync.Mutex
	project string
	turns   []map[string]any
	sends   int
	sent    chan string
}

func (n *integrationNative) setTurn(id, status, origin string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.setTurnLocked(id, status, origin)
}
func (n *integrationNative) setTurnLocked(id, status, origin string) {
	item := map[string]any{"type": "userMessage"}
	if origin == "team" {
		item = map[string]any{"type": "functionCallOutput", "name": "send_message_to_thread", "namespace": "codex_app"}
	}
	turn := map[string]any{"id": id, "status": status, "items": []any{item}}
	if len(n.turns) != 0 && n.turns[0]["id"] == id {
		n.turns[0] = turn
	} else {
		n.turns = append([]map[string]any{turn}, n.turns...)
	}
	if len(n.turns) > 3 {
		n.turns = n.turns[:3]
	}
}
func (n *integrationNative) sendCount() int { n.mu.Lock(); defer n.mu.Unlock(); return n.sends }
func (n *integrationNative) currentTurn() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.turns[0]["id"].(string)
}
func (n *integrationNative) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Name string         `json:"name"`
		Args map[string]any `json:"arguments"`
		Meta map[string]any `json:"_meta"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	result := func(value any) {
		data, _ := json.Marshal(value)
		json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": string(data)}}})
	}
	failure := func(message string) {
		json.NewEncoder(w).Encode(map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": message}}})
	}
	if p.Meta["openai/threadId"] != "original-chat" || len(p.Meta) != 1 || p.Args["threadId"] != "original-chat" {
		failure("native identity mismatch or fabricated invocation metadata")
		return
	}
	switch p.Name {
	case "read_thread":
		if p.Args["includeOutputs"] != false || p.Args["turnLimit"] != float64(3) {
			failure("unbounded native history read")
			return
		}
		state := "active"
		if n.turns[0]["status"] == "completed" {
			state = "idle"
		}
		result(map[string]any{"schemaVersion": 1, "thread": map[string]any{"id": "original-chat", "cwd": n.project, "status": map[string]any{"type": state, "activeFlags": []string{}}}, "turns": n.turns})
	case "send_message_to_thread":
		prompt, _ := p.Args["prompt"].(string)
		if prompt == "" || len(p.Args) != 2 {
			failure("unexpected native input or model override")
			return
		}
		n.sends++
		n.sent <- prompt
		result(map[string]any{"threadId": "original-chat"})
	default:
		failure("unexpected native action: " + p.Name)
	}
}

type integrationMCP struct {
	input      io.WriteCloser
	responses  chan []byte
	next       int
	transcript bytes.Buffer
}

func (f *attachmentIntegration) client(t *testing.T) *integrationMCP {
	t.Helper()
	cmd := exec.Command(f.executable, "-test.run=^TestAttachmentIntegrationProcess$")
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(key, "STRATUS_") && !strings.HasPrefix(key, "CODEX_APP_TOOLS_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "STRATUS_ATTACHMENT_FIXTURE_MODE=mcp", "STRATUS_ATTACHMENT_FIXTURE_API="+f.api.URL, "STRATUS_ATTACHMENT_FIXTURE_NATIVE="+f.nativeURL, "STRATUS_CODEX_APP_TOOLS_LAUNCHER="+f.launcher, "STRATUS_CODEX_APP_TOOLS_SERVER="+f.launcher, "CODEX_APP_TOOLS_PIPE_PATH=disposable-fixture-only")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &integrationMCP{input: input, responses: make(chan []byte, 16)}
	go func() {
		defer close(c.responses)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), 2<<20)
		for scanner.Scan() {
			c.responses <- append([]byte(nil), scanner.Bytes()...)
		}
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		input.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("MCP fixture exit: %v: %s", err, stderr.String())
			}
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
			t.Errorf("MCP fixture did not stop: %s", stderr.String())
		}
	})
	c.rpc(t, "initialize", map[string]any{"protocolVersion": "2025-06-18"})
	return c
}
func (c *integrationMCP) rpc(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	c.next++
	if err := json.NewEncoder(c.input).Encode(map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method, "params": params}); err != nil {
		t.Fatal(err)
	}
	select {
	case data, ok := <-c.responses:
		if !ok {
			t.Fatal("MCP fixture closed before responding")
		}
		c.transcript.Write(data)
		var response struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(data, &response); err != nil || response.ID != c.next || len(response.Error) != 0 {
			t.Fatalf("invalid MCP response: %s (%v)", data, err)
		}
		return response.Result
	case <-time.After(10 * time.Second):
		t.Fatalf("MCP %s timed out", method)
	}
	return nil
}
func (c *integrationMCP) tool(t *testing.T, name string, args map[string]any, turn string, wantError bool) map[string]any {
	t.Helper()
	return c.toolMeta(t, name, args, map[string]any{"x-codex-turn-metadata": map[string]string{"thread_id": "original-chat", "turn_id": turn}}, wantError)
}
func (c *integrationMCP) toolMeta(t *testing.T, name string, args map[string]any, meta any, wantError bool) map[string]any {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	params := map[string]any{"name": name, "arguments": args}
	if meta != nil {
		params["_meta"] = meta
	}
	raw := c.rpc(t, "tools/call", params)
	var response struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response.IsError != wantError || len(response.Content) != 1 {
		t.Fatalf("%s error=%v, want %v: %s", name, response.IsError, wantError, raw)
	}
	if wantError {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(response.Content[0].Text), &value); err != nil {
		t.Fatalf("%s: %v: %s", name, err, raw)
	}
	return value
}
func (c *integrationMCP) noSecret(t *testing.T, token string) {
	t.Helper()
	if token == "" || strings.Contains(c.transcript.String(), token) || strings.Contains(c.transcript.String(), `\"token\"`) {
		t.Fatal("private grant leaked into model-visible MCP responses")
	}
}

// Re-exec entrypoint; never touches a real Codex helper or calls a model.
func TestAttachmentIntegrationProcess(t *testing.T) {
	switch os.Getenv("STRATUS_ATTACHMENT_FIXTURE_MODE") {
	case "mcp":
		s := mcp.New()
		mcp.RegisterToolsForRuntime(s, os.Getenv("STRATUS_ATTACHMENT_FIXTURE_API"), &http.Client{Timeout: 5 * time.Second}, mcp.RuntimeOptions{Runtime: "codex"})
		if err := s.Serve(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	case "native":
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 2<<20)
		for scanner.Scan() {
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
				os.Exit(2)
			}
			if len(request.ID) == 0 {
				continue
			}
			var result any
			switch request.Method {
			case "initialize":
				result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}}
			case "tools/list":
				result = map[string]any{"tools": []any{map[string]string{"name": "read_thread"}, map[string]string{"name": "send_message_to_thread"}}}
			case "tools/call":
				response, err := http.Post(os.Getenv("STRATUS_ATTACHMENT_FIXTURE_NATIVE"), "application/json", bytes.NewReader(request.Params))
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(3)
				}
				err = json.NewDecoder(response.Body).Decode(&result)
				response.Body.Close()
				if err != nil {
					os.Exit(4)
				}
			default:
				os.Exit(5)
			}
			if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
				os.Exit(6)
			}
		}
		os.Exit(0)
	}
}
