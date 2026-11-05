package api

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

// denialTestAdapter opens sessions that stay in Submit until released, so a test
// can raise a native approval request while the member works on a task.
type denialTestAdapter struct {
	mu      sync.Mutex
	session *denialTestSession
	opened  chan struct{}
}

func (a *denialTestAdapter) Preflight(context.Context, string) teams.Availability {
	return teams.Availability{Runtime: "codex", Available: true, Models: []teams.Model{{ID: "gpt-6.1-sol", Efforts: []string{"high"}}}}
}

func (a *denialTestAdapter) Open(_ context.Context, _ teams.SessionSpec, event func(teams.Event)) (teams.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.session = &denialTestSession{emit: event, release: make(chan struct{})}
	close(a.opened)
	return a.session, nil
}

type denialTestSession struct {
	emit    func(teams.Event)
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	allowed []bool
}

func (s *denialTestSession) Submit(ctx context.Context, in teams.Input) (teams.Result, error) {
	select {
	case <-s.release:
	case <-ctx.Done():
		return teams.Result{}, ctx.Err()
	}
	return teams.Result{Text: "done"}, nil
}
func (s *denialTestSession) Approve(_ context.Context, _ string, allow bool) error {
	s.mu.Lock()
	s.allowed = append(s.allowed, allow)
	s.mu.Unlock()
	return nil
}
func (s *denialTestSession) Interrupt(context.Context) error {
	s.once.Do(func() { close(s.release) })
	return nil
}
func (s *denialTestSession) Close() error { s.once.Do(func() { close(s.release) }); return nil }

// approvalServer returns a server with a team whose lead works on a task that
// carries workflowID (none when empty) and has raised approval "ap-1".
func denialServer(t *testing.T, workflowID, approvalText string) (*Server, teams.Team) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "approvals.db"))
	if err != nil {
		t.Fatal(err)
	}
	adapter := &denialTestAdapter{opened: make(chan struct{})}
	adapters := map[string]teams.Adapter{"codex": adapter}
	m, err := teams.NewManager(d.SQL(), adapters, func() {})
	if err != nil {
		t.Fatal(err)
	}
	coord := orchestration.NewCoordinator(d)
	if _, err := coord.Start("spec-x", orchestration.WorkflowSpec, orchestration.ComplexitySimple, "Add caching"); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: d, projectRoot: t.TempDir(), hub: NewHub(), teams: m, teamAdapters: adapters, coordinator: coord}
	t.Cleanup(func() { m.Close(); d.Close() })
	tm := createAPITeam(t, s)
	if _, err := m.Enqueue(context.Background(), tm.ID, teams.TaskRequest{ID: "task-1", To: tm.Members[0].ID, Text: "work", WorkflowID: workflowID}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	select {
	case <-adapter.opened:
	case <-time.After(5 * time.Second):
		t.Fatal("session never opened")
	}
	adapter.session.emit(teams.Event{Kind: "approval", ID: "ap-1", Text: approvalText})
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := m.Get(tm.ID)
		if err == nil && got.Members[0].Approval != nil {
			return s, *got
		}
		if time.Now().After(deadline) {
			t.Fatal("approval never reached the member")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func postDenial(t *testing.T, s *Server, tm teams.Team, approvalID string, allow bool) int {
	t.Helper()
	body := fmt.Sprintf(`{"member_id":%q,"approval_id":%q,"allow":%t}`, tm.Members[0].ID, approvalID, allow)
	return commsDo(t, s.handleTeamApproval, "POST", "/api/teams/"+tm.ID+"/approvals", body, map[string]string{"id": tm.ID}).Code
}

func denialDecisions(t *testing.T, s *Server) []db.Event {
	t.Helper()
	evs, err := s.db.SearchEvents(db.SearchEventsInput{Type: "decision", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

const deniedText = "Run `rm -rf build` with token=ghp_abcdefghijklmnopqrstuvwxyz0123456789 \x1b[31m now?"

func TestTeamApproval_DenyRecordsRejectedDecision(t *testing.T) {
	s, tm := denialServer(t, "spec-x", deniedText)
	if code := postDenial(t, s, tm, "ap-1", false); code != 200 {
		t.Fatalf("deny status %d", code)
	}
	evs := denialDecisions(t, s)
	if len(evs) != 1 {
		t.Fatalf("decisions = %d, want exactly 1", len(evs))
	}
	e := evs[0]
	if e.Actor != "user" || e.Scope != "workflow" || e.Type != "decision" {
		t.Errorf("actor/scope/type = %s/%s/%s", e.Actor, e.Scope, e.Type)
	}
	if e.Title != "Rejected by user: approval request" {
		t.Errorf("title = %q", e.Title)
	}
	if strings.Join(e.Tags, ",") != "decision,rejected,approval_denied" {
		t.Errorf("tags = %v", e.Tags)
	}
	if e.Refs["status"] != "rejected" || e.Refs["workflow_id"] != "spec-x" || e.Refs["team_id"] != tm.ID || e.Refs["member_id"] != tm.Members[0].ID {
		t.Errorf("refs = %v", e.Refs)
	}
	raw, _ := json.Marshal(e)
	if strings.Contains(string(raw), "ghp_abcdef") || strings.Contains(string(raw), `\u001b`) {
		t.Errorf("secret or control char stored: %s", raw)
	}
	if !strings.HasPrefix(e.Text, `Agent's request: "Run `+"`rm -rf build`") {
		t.Errorf("text lost its content: %q", e.Text)
	}

	brief := s.buildBrief("spec-x", "", time.Time{})
	if !strings.Contains(brief, "Rejected - do not revisit: Rejected by user: approval request - Agent's request: \"Run `rm -rf build`") {
		t.Errorf("brief does not show the denial:\n%s", brief)
	}
}

func TestTeamApproval_AllowRecordsNothing(t *testing.T) {
	s, tm := denialServer(t, "spec-x", "Run tests?")
	if code := postDenial(t, s, tm, "ap-1", true); code != 200 {
		t.Fatalf("allow status %d", code)
	}
	if n := len(denialDecisions(t, s)); n != 0 {
		t.Fatalf("decisions = %d, want 0 for a granted approval", n)
	}
}

func TestTeamApproval_RejectedDenialRecordsNothing(t *testing.T) {
	s, tm := denialServer(t, "spec-x", "Run tests?")
	if code := postDenial(t, s, tm, "unknown", false); code != 409 {
		t.Fatalf("unknown approval status %d, want 409", code)
	}
	if n := len(denialDecisions(t, s)); n != 0 {
		t.Fatalf("decisions = %d, want 0 after a refused denial", n)
	}
}

func TestTeamApproval_DenyWithoutWorkflowIsRepoScoped(t *testing.T) {
	s, tm := denialServer(t, "", "Delete the cache directory?")
	if code := postDenial(t, s, tm, "ap-1", false); code != 200 {
		t.Fatalf("deny status %d", code)
	}
	evs := denialDecisions(t, s)
	if len(evs) != 1 || evs[0].Scope != "repo" {
		t.Fatalf("events = %+v, want one repo-scoped decision", evs)
	}
	if _, has := evs[0].Refs["workflow_id"]; has {
		t.Errorf("workflow_id must be omitted: %v", evs[0].Refs)
	}
}

func TestTeamApproval_EventSaveFailureDoesNotFailRequest(t *testing.T) {
	s, tm := denialServer(t, "spec-x", "Run tests?")
	if _, err := s.db.SQL().Exec(`CREATE TRIGGER no_decisions BEFORE INSERT ON events WHEN NEW.type = 'decision' BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	if code := postDenial(t, s, tm, "ap-1", false); code != 200 {
		t.Fatalf("deny status %d, want 200 despite the failed memory write", code)
	}
}

func TestRecordApprovalDenial_RepeatIsDeduplicated(t *testing.T) {
	s, tm := denialServer(t, "spec-x", "Run tests?")
	ap := teams.Event{Kind: "approval", ID: "ap-9", Text: "Run tests?"}
	for i := 0; i < 2; i++ {
		if err := s.recordApprovalDenial(tm.ID, tm.Members[0].ID, ap, "spec-x"); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(denialDecisions(t, s)); n != 1 {
		t.Fatalf("decisions = %d, want 1 after a repeated denial", n)
	}
}

func TestRecordApprovalDenial_ClipsTitleAndText(t *testing.T) {
	s, tm := denialServer(t, "spec-x", "x")
	ap := teams.Event{Kind: "approval", ID: "ap-long", Text: strings.Repeat("é", 500) + " " + strings.Repeat("日本語", 200)}
	if err := s.recordApprovalDenial(tm.ID, tm.Members[0].ID, ap, "spec-x"); err != nil {
		t.Fatal(err)
	}
	e := denialDecisions(t, s)[0]
	if n := utf8.RuneCountInString(strings.TrimPrefix(e.Title, "Rejected by user: ")); n > 80 {
		t.Errorf("title word = %d runes, want <= 80", n)
	}
	if n := utf8.RuneCountInString(e.Text); n > 300 {
		t.Errorf("text = %d runes, want <= 300", n)
	}
}

func TestRecordApprovalDenial_ClosedDBReturnsError(t *testing.T) {
	s, tm := denialServer(t, "spec-x", "x")
	s.db.Close()
	if err := s.recordApprovalDenial(tm.ID, tm.Members[0].ID, teams.Event{ID: "ap-2", Text: "Run?"}, ""); err == nil {
		t.Fatal("want an error when the event cannot be saved")
	}
}

func TestRecordApprovalDenial_SameApprovalIDInOtherTeamOrMemberIsNotLost(t *testing.T) {
	s, _ := denialServer(t, "spec-x", "x")
	ap := teams.Event{Kind: "approval", ID: "ap-1", Text: "Run tests?"}
	for _, ids := range [][2]string{{"team-a", "lead"}, {"team-b", "lead"}, {"team-a", "worker"}} {
		if err := s.recordApprovalDenial(ids[0], ids[1], ap, "spec-x"); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(denialDecisions(t, s)); n != 3 {
		t.Fatalf("decisions = %d, want 3: the same approval id in another team or member is a different approval", n)
	}
	if err := s.recordApprovalDenial("team-a", "lead", ap, "spec-x"); err != nil {
		t.Fatal(err)
	}
	if n := len(denialDecisions(t, s)); n != 3 {
		t.Fatalf("decisions = %d, want 3 after repeating one denial", n)
	}
}

func TestRecordApprovalDenial_ParsesToolAndRequest(t *testing.T) {
	cases := []struct {
		name, text, title, stored string
	}{
		{"codex command",
			`item/commandExecution/requestApproval: {"threadId":"th_1","turnId":"tu_1","itemId":"it_1","cwd":"/work","command":"npm test -- --run","reason":"needs network"}`,
			"Rejected by user: commandExecution", `Agent's request: "npm test -- --run"`},
		{"codex command array",
			`item/commandExecution/requestApproval: {"threadId":"th_1","command":["bash","-lc","go test ./..."]}`,
			"Rejected by user: commandExecution", `Agent's request: "bash -lc go test ./..."`},
		{"codex reason only",
			`item/fileChange/requestApproval: {"threadId":"th_1","turnId":"tu_1","reason":"write outside workspace"}`,
			"Rejected by user: fileChange", `Agent's request: "write outside workspace"`},
		{"codex real file change",
			`item/fileChange/requestApproval: {"grantRoot":null,"itemId":"exec-bf7dc6aa","reason":"command failed; retry without sandbox?","threadId":"th_9","turnId":"tu_9"}`,
			"Rejected by user: fileChange", `Agent's request: "command failed; retry without sandbox?"`},
		{"codex real command execution",
			`item/commandExecution/requestApproval: {"availableDecisions":["accept","decline","cancel"],"command":"/bin/bash -lc \"pwd && git status\"","cwd":"/work","itemId":"exec-1","threadId":"th_9","turnId":"tu_9"}`,
			"Rejected by user: commandExecution", `Agent's request: "/bin/bash -lc "pwd && git status""`},
		{"codex permissions",
			`item/permissions/requestApproval: {"itemId":"i","reason":"needs network","threadId":"t","turnId":"u"}`,
			"Rejected by user: permissions", `Agent's request: "needs network"`},
		{"method without approval suffix keeps last segment",
			`item/tool/call: {"reason":"x"}`,
			"Rejected by user: call", `Agent's request: "x"`},
		{"bare requestApproval keeps its only segment",
			`requestApproval: {"reason":"x"}`,
			"Rejected by user: requestApproval", `Agent's request: "x"`},
		{"claude bash",
			`Bash: {"command":"rm -rf build","description":"clean"}`,
			"Rejected by user: Bash", `Agent's request: "rm -rf build"`},
		{"claude edit file path",
			`Edit: {"file_path":"/repo/api/brief.go","old_string":"a","new_string":"b"}`,
			"Rejected by user: Edit", `Agent's request: "/repo/api/brief.go"`},
		{"no useful field falls back to raw text",
			`Bash: {"timeout":5}`,
			"Rejected by user: Bash", `Agent's request: "Bash: {"timeout":5}"`},
		{"malformed json falls back to raw text",
			`Bash: {"command": "rm -rf`,
			"Rejected by user: Bash", `Agent's request: "Bash: {"command": "rm -rf"`},
		{"plain text",
			`May I write to the cache?`,
			"Rejected by user: approval request", `Agent's request: "May I write to the cache?"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, tm := denialServer(t, "spec-x", "x")
			if err := s.recordApprovalDenial(tm.ID, tm.Members[0].ID, teams.Event{ID: "ap-p", Text: c.text}, "spec-x"); err != nil {
				t.Fatal(err)
			}
			e := denialDecisions(t, s)[0]
			if e.Title != c.title || e.Text != c.stored {
				t.Errorf("title=%q text=%q\nwant  %q / %q", e.Title, e.Text, c.title, c.stored)
			}
		})
	}
}

func TestRecordApprovalDenial_RedactsSecretsInParsedCommand(t *testing.T) {
	s, tm := denialServer(t, "spec-x", "x")
	text := `Bash: {"command":"curl -H 'Authorization: Bearer ghp_abcdefghijklmnopqrstuvwxyz0123456789' https://x.example --token=ghp_abcdefghijklmnopqrstuvwxyz0123456789"}`
	if err := s.recordApprovalDenial(tm.ID, tm.Members[0].ID, teams.Event{ID: "ap-s", Text: text}, "spec-x"); err != nil {
		t.Fatal(err)
	}
	e := denialDecisions(t, s)[0]
	if strings.Contains(e.Text, "ghp_abcdef") || !strings.Contains(e.Text, "curl") {
		t.Fatalf("secret stored or command lost: %q", e.Text)
	}
}

func TestRecordApprovalDenial_ClipsRequestToThreeHundredRunesAfterRedaction(t *testing.T) {
	s, tm := denialServer(t, "spec-x", "x")
	text := `Bash: {"command":"` + strings.Repeat("é", 1000) + `"}`
	if err := s.recordApprovalDenial(tm.ID, tm.Members[0].ID, teams.Event{ID: "ap-c", Text: text}, "spec-x"); err != nil {
		t.Fatal(err)
	}
	e := denialDecisions(t, s)[0]
	if n := utf8.RuneCountInString(e.Text); n > 300 || !strings.HasPrefix(e.Text, `Agent's request: "`) {
		t.Fatalf("text = %d runes: %q", n, e.Text)
	}
}

func TestTeamApproval_BriefAttributesAgentRequest(t *testing.T) {
	s, tm := denialServer(t, "spec-x", `item/commandExecution/requestApproval: {"threadId":"th_1","command":"npm publish"}`)
	if code := postDenial(t, s, tm, "ap-1", false); code != 200 {
		t.Fatalf("deny status %d", code)
	}
	want := `Rejected - do not revisit: Rejected by user: commandExecution - Agent's request: "npm publish"`
	if got := s.buildBrief("spec-x", "", time.Time{}); !strings.Contains(got, want) {
		t.Fatalf("brief missing %q:\n%s", want, got)
	}
}

func TestPendingApproval_UnknownIDIsNotFound(t *testing.T) {
	s, tm := denialServer(t, "spec-x", "Run tests?")
	if _, _, ok := pendingApproval(&tm, tm.Members[0].ID, "nope"); ok {
		t.Fatal("unknown approval id must not be found")
	}
	if _, _, ok := pendingApproval(&tm, "other-member", "ap-1"); ok {
		t.Fatal("approval of another member must not be found")
	}
	ev, wf, ok := pendingApproval(&tm, tm.Members[0].ID, "ap-1")
	if !ok || ev.ID != "ap-1" || wf != "spec-x" {
		t.Fatalf("got %+v %q %v", ev, wf, ok)
	}
	_ = s
}
