package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

// approvalAdapter lets a test raise a native approval request on an open team session.
type approvalAdapter struct {
	emit chan func(teams.Event)
	done chan struct{}
}

func (approvalAdapter) Preflight(context.Context, string) teams.Availability {
	return teams.Availability{Runtime: "codex", Available: true, Models: []teams.Model{{ID: "gpt-6.1-sol", Efforts: []string{"high"}}}}
}

func (a approvalAdapter) Open(_ context.Context, _ teams.SessionSpec, emit func(teams.Event)) (teams.Session, error) {
	a.emit <- emit
	return approvalSession{done: a.done}, nil
}

type approvalSession struct{ done chan struct{} }

func (s approvalSession) Submit(ctx context.Context, _ teams.Input) (teams.Result, error) {
	select {
	case <-s.done:
	case <-ctx.Done():
	}
	return teams.Result{}, ctx.Err()
}
func (approvalSession) Approve(context.Context, string, bool) error { return nil }
func (approvalSession) Interrupt(context.Context) error             { return nil }
func (s approvalSession) Close() error                              { return nil }

// decisionServer is a server with a team manager (fake adapter) and a workflow coordinator.
func decisionServer(t *testing.T) (*Server, approvalAdapter) {
	t.Helper()
	s := teamAPIServer(t)
	adapter := approvalAdapter{emit: make(chan func(teams.Event), 4), done: make(chan struct{})}
	t.Cleanup(func() { close(adapter.done) })
	m, err := teams.NewManager(s.db.SQL(), map[string]teams.Adapter{"codex": adapter}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	s.teams = m
	s.teamAdapters = map[string]teams.Adapter{"codex": adapter}
	s.coordinator = orchestration.NewCoordinator(s.db)
	return s, adapter
}

type decisionsBody struct {
	Items []struct {
		ID        string `json:"id"`
		Kind      string `json:"kind"`
		Severity  string `json:"severity"`
		Title     string `json:"title"`
		CreatedAt string `json:"created_at"`
		Link      string `json:"link"`
		Actions   []struct {
			Name   string         `json:"name"`
			Method string         `json:"method"`
			Path   string         `json:"path"`
			Body   map[string]any `json:"body"`
		} `json:"actions"`
	} `json:"items"`
	Summary struct {
		NewItemsSince int  `json:"new_items_since"`
		GuardianNew   int  `json:"guardian_new"`
		TeamsUpdated  bool `json:"teams_updated"`
	} `json:"summary"`
}

func getDecisions(t *testing.T, s *Server, query string) (*httptest.ResponseRecorder, decisionsBody) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/decisions"+query, nil)
	w := httptest.NewRecorder()
	s.handleDecisions(w, req)
	var out decisionsBody
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return w, out
}

func TestHandleDecisions_GuardianOnlyActionableUndismissed(t *testing.T) {
	s, _ := decisionServer(t)
	keep, _ := s.db.SaveGuardianAlert("stale_agent", "warning", "agent idle", map[string]interface{}{"workflow_id": "spec-x"})
	dismissed, _ := s.db.SaveGuardianAlert("token_budget", "critical", "over budget", nil)
	if err := s.db.DismissGuardianAlert(dismissed); err != nil {
		t.Fatal(err)
	}
	s.db.SaveGuardianAlert("coverage_drift", "info", "routine health", nil)

	w, out := getDecisions(t, s, "")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d %s", w.Code, w.Body.String())
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %+v", out.Items)
	}
	it := out.Items[0]
	if it.Kind != "guardian" || it.ID != "guardian:"+itoa(keep) || it.Link != "#/workflow/spec-x" {
		t.Errorf("item = %+v", it)
	}
	if len(it.Actions) != 1 || it.Actions[0].Name != "dismiss" || it.Actions[0].Method != "PUT" || it.Actions[0].Path != "/guardian/alerts/"+itoa(keep)+"/dismiss" {
		t.Errorf("actions = %+v", it.Actions)
	}
}

func TestHandleDecisions_WorkflowAttentionHasStableID(t *testing.T) {
	s, id := agentTestServer(t)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"r1","agent_type":"delivery-code-reviewer"}`)
	postAgentEvent(t, s, `{"event":"stop","session_id":"sess-1","agent_id":"r1","last_assistant_message":"Verdict: FAIL\n[must_fix] x — a.go:1"}`)

	_, out := getDecisions(t, s, "")
	if len(out.Items) != 1 {
		t.Fatalf("items = %+v", out.Items)
	}
	it := out.Items[0]
	if it.ID != "workflow:"+id+":review_failed" || it.Kind != "workflow" || it.Link != "#/workflow/"+id {
		t.Errorf("item = %+v", it)
	}
	if len(it.Actions) != 0 {
		t.Errorf("workflow rows only link, got actions %+v", it.Actions)
	}
}

func TestHandleDecisions_TeamApprovalAndRecovery(t *testing.T) {
	s, adapter := decisionServer(t)
	tm := createAPITeam(t, s)
	if _, err := s.teams.Enqueue(context.Background(), tm.ID, teams.TaskRequest{From: "user", To: "lead", Text: "work"}); err != nil {
		t.Fatal(err)
	}
	var emit func(teams.Event)
	select {
	case emit = <-adapter.emit:
	case <-time.After(5 * time.Second):
		t.Fatal("session never opened")
	}
	emit(teams.Event{Kind: "approval", ID: "ap-1", Text: "May I write?"})

	var out decisionsBody
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, out = getDecisions(t, s, "")
		if len(out.Items) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %+v", out.Items)
	}
	it := out.Items[0]
	if it.ID != "team:"+tm.ID+":lead:ap-1" || it.Kind != "team_approval" {
		t.Errorf("item = %+v", it)
	}
	if len(it.Actions) != 2 || it.Actions[0].Name != "approve" || it.Actions[1].Name != "deny" {
		t.Fatalf("actions = %+v", it.Actions)
	}
	a := it.Actions[0]
	if a.Method != "POST" || a.Path != "/teams/"+tm.ID+"/approvals" || a.Body["member_id"] != "lead" || a.Body["approval_id"] != "ap-1" || a.Body["allow"] != true {
		t.Errorf("approve = %+v", a)
	}
	if it.Actions[1].Body["allow"] != false {
		t.Errorf("deny = %+v", it.Actions[1])
	}

}

func TestHandleDecisions_TeamRecoveryWhenApprovalLacksIdentity(t *testing.T) {
	s, adapter := decisionServer(t)
	tm := createAPITeam(t, s)
	if _, err := s.teams.Enqueue(context.Background(), tm.ID, teams.TaskRequest{From: "user", To: "lead", Text: "work"}); err != nil {
		t.Fatal(err)
	}
	emit := <-adapter.emit
	emit(teams.Event{Kind: "approval"})

	var out decisionsBody
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, out = getDecisions(t, s, "")
		if len(out.Items) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(out.Items) != 1 || out.Items[0].ID != "team:"+tm.ID+":lead:recover" || out.Items[0].Kind != "team_recover" {
		t.Fatalf("items = %+v", out.Items)
	}
	a := out.Items[0].Actions
	if len(a) != 1 || a[0].Name != "recover" || a[0].Path != "/teams/"+tm.ID+"/recover" || a[0].Body["member_id"] != "lead" {
		t.Errorf("actions = %+v", a)
	}
}

func TestHandleDecisions_InvalidSince(t *testing.T) {
	s, _ := decisionServer(t)
	w, _ := getDecisions(t, s, "?since=yesterday")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestHandleDecisions_SummaryCountsSince(t *testing.T) {
	s, _ := decisionServer(t)
	s.db.SaveGuardianAlert("agent_failed", "critical", "failed", nil)
	createAPITeam(t, s)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

	_, out := getDecisions(t, s, "?since="+past)
	if out.Summary.GuardianNew != 1 || out.Summary.NewItemsSince != 1 || !out.Summary.TeamsUpdated {
		t.Errorf("summary since past = %+v", out.Summary)
	}
	_, out = getDecisions(t, s, "?since="+future)
	if out.Summary.GuardianNew != 0 || out.Summary.NewItemsSince != 0 || out.Summary.TeamsUpdated {
		t.Errorf("summary since future = %+v", out.Summary)
	}
	if len(out.Items) != 1 {
		t.Errorf("since must not filter items: %+v", out.Items)
	}
}

func TestHandleDecisions_ItemsCapped(t *testing.T) {
	s, _ := decisionServer(t)
	for i := 0; i < decisionLimit+5; i++ {
		s.db.SaveGuardianAlert("agent_failed", "critical", "failed", nil)
	}
	_, out := getDecisions(t, s, "")
	if len(out.Items) != decisionLimit {
		t.Fatalf("items = %d", len(out.Items))
	}
}

func TestAttentionItems_HandleAttentionUnchanged(t *testing.T) {
	s, id := agentTestServer(t)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"r1","agent_type":"delivery-code-reviewer"}`)
	postAgentEvent(t, s, `{"event":"stop","session_id":"sess-1","agent_id":"r1","last_assistant_message":"Verdict: FAIL\n[must_fix] x — a.go:1"}`)
	items, err := s.attentionItems()
	if err != nil || len(items) != 1 || items[0].WorkflowID != id || !items[0].ReviewFailed {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func decisionIDs(out decisionsBody) []string {
	ids := []string{}
	for _, it := range out.Items {
		ids = append(ids, it.ID)
	}
	return ids
}

func TestHandleDecisions_GuardianDuplicatesOfWorkflowRowsSkipped(t *testing.T) {
	s, id := agentTestServer(t)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"r1","agent_type":"delivery-code-reviewer"}`)
	postAgentEvent(t, s, `{"event":"stop","session_id":"sess-1","agent_id":"r1","last_assistant_message":"Verdict: FAIL\n[must_fix] x — a.go:1"}`)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"a1","agent_type":"x"}`)
	if _, err := s.db.SQL().Exec(`UPDATE workflow_agents SET last_activity='2000-01-01T00:00:00.000Z' WHERE id='a1'`); err != nil {
		t.Fatal(err)
	}
	s.db.SaveGuardianAlert("review_failed", "warning", "dup review", map[string]interface{}{"workflow_id": id})
	s.db.SaveGuardianAlert("stale_agent", "warning", "dup stale", map[string]interface{}{"workflow_id": id, "agent_id": "a1"})
	budget, _ := s.db.SaveGuardianAlert("token_budget", "warning", "budget", map[string]interface{}{"workflow_id": id})
	other, _ := s.db.SaveGuardianAlert("review_failed", "warning", "other wf", map[string]interface{}{"workflow_id": "spec-other"})

	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	_, out := getDecisions(t, s, "?since="+past)
	got := map[string]bool{}
	for _, i := range decisionIDs(out) {
		got[i] = true
	}
	want := []string{"workflow:" + id + ":review_failed", "workflow:" + id + ":stale_agents", "guardian:" + itoa(budget), "guardian:" + itoa(other)}
	if len(out.Items) != len(want) {
		t.Fatalf("items = %v", decisionIDs(out))
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing %s in %v", w, decisionIDs(out))
		}
	}
	if out.Summary.GuardianNew != 2 {
		t.Errorf("guardian_new = %d, want 2 (duplicates must not count)", out.Summary.GuardianNew)
	}
}

func TestHandleDecisions_GuardianLinkFromAgentID(t *testing.T) {
	s, id := agentTestServer(t)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"a1","agent_type":"x"}`)
	s.db.SaveGuardianAlert("agent_failed", "warning", "failed", map[string]interface{}{"agent_id": "a1"})
	_, out := getDecisions(t, s, "")
	if len(out.Items) != 1 || out.Items[0].Link != "#/workflow/"+id {
		t.Fatalf("items = %+v", out.Items)
	}
}

func TestHandleDecisions_WorkflowStaleAgentsAndConflicts(t *testing.T) {
	s, id := agentTestServer(t)
	for _, a := range []string{"a1", "a2"} {
		postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"`+a+`","agent_type":"x"}`)
		postAgentEvent(t, s, `{"event":"tool","session_id":"sess-1","agent_id":"`+a+`","tool_name":"Edit","files":["same.go"]}`)
	}
	if _, err := s.db.SQL().Exec(`UPDATE workflow_agents SET last_activity='2000-01-01T00:00:00.000Z' WHERE id='a1'`); err != nil {
		t.Fatal(err)
	}
	_, out := getDecisions(t, s, "")
	got := map[string]bool{}
	for _, i := range decisionIDs(out) {
		got[i] = true
	}
	if len(out.Items) != 2 || !got["workflow:"+id+":stale_agents"] || !got["workflow:"+id+":conflicts"] {
		t.Fatalf("items = %v", decisionIDs(out))
	}
}

func TestHandleDecisions_NewestFirst(t *testing.T) {
	s, _ := decisionServer(t)
	var ids [3]int64
	for i := range ids {
		ids[i], _ = s.db.SaveGuardianAlert("agent_failed", "warning", "failed", nil)
	}
	for id, ts := range map[int64]string{ids[0]: "2026-01-01T00:00:00.000Z", ids[1]: "2026-03-01T00:00:00.000Z", ids[2]: "2026-02-01T00:00:00.000Z"} {
		if _, err := s.db.SQL().Exec(`UPDATE guardian_alerts SET created_at=? WHERE id=?`, ts, id); err != nil {
			t.Fatal(err)
		}
	}
	_, out := getDecisions(t, s, "")
	want := []string{"guardian:" + itoa(ids[1]), "guardian:" + itoa(ids[2]), "guardian:" + itoa(ids[0])}
	got := decisionIDs(out)
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestDecisionsFromTeams_StoppedTeamAndSpentApproval(t *testing.T) {
	approval := &teams.Event{Kind: "approval", ID: "ap-1", Text: "May I write?"}
	stopped := teams.Team{ID: "t-stopped", State: "stopped", Members: []teams.Member{{ID: "lead", Role: "lead", State: "needs_user", Approval: approval}}}
	answered := teams.Team{ID: "t-answered", State: "running", Members: []teams.Member{{ID: "lead", Role: "lead", State: "needs_user", Approval: approval, ApprovalResponseID: "ap-1"}}}

	items, _ := decisionsFromTeams([]teams.Team{stopped, answered}, time.Time{})
	for _, it := range items {
		if it.Kind == "team_approval" {
			t.Errorf("unexpected approval row %s", it.ID)
		}
		if it.ID == "team:t-stopped:lead:recover" || it.ID == "team:t-stopped:lead:ap-1" {
			t.Errorf("stopped team yielded %s", it.ID)
		}
	}
	// An answered but uncertain approval leaves the member waiting for an explicit recovery.
	if len(items) != 1 || items[0].ID != "team:t-answered:lead:recover" {
		t.Fatalf("items = %+v", items)
	}
}

func TestHandleDecisions_OtherProjectTeamExcluded(t *testing.T) {
	s, adapter := decisionServer(t)
	other, err := s.teams.Create(context.Background(), teams.CreateRequest{
		ProjectID: "another-project", ProjectRoot: t.TempDir(),
		Members: []teams.MemberConfig{{ID: "lead", Role: "lead", Profile: teams.Profile{Runtime: "codex", Model: "gpt-6.1-sol", Effort: "high"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.teams.Enqueue(context.Background(), other.ID, teams.TaskRequest{From: "user", To: "lead", Text: "work"}); err != nil {
		t.Fatal(err)
	}
	emit := <-adapter.emit
	emit(teams.Event{Kind: "approval", ID: "ap-x", Text: "secret"})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := s.teams.Get(other.ID); got != nil && got.Members[0].Approval != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got, _ := s.teams.Get(other.ID); got == nil || got.Members[0].Approval == nil {
		t.Fatal("approval never raised on the other project's team")
	}
	_, out := getDecisions(t, s, "")
	if len(out.Items) != 0 {
		t.Fatalf("other project's team leaked: %+v", out.Items)
	}
}

func TestHandleDecisions_TeamTokenSkipsTeamSection(t *testing.T) {
	s, adapter := decisionServer(t)
	tm := createAPITeam(t, s)
	if _, err := s.teams.Enqueue(context.Background(), tm.ID, teams.TaskRequest{From: "user", To: "lead", Text: "work"}); err != nil {
		t.Fatal(err)
	}
	emit := <-adapter.emit
	emit(teams.Event{Kind: "approval", ID: "ap-1", Text: "May I write?"})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, out := getDecisions(t, s, ""); len(out.Items) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.db.SaveGuardianAlert("agent_failed", "warning", "failed", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/decisions", nil)
	req.Header.Set("X-Stratus-Team-Token", "member-token")
	w := httptest.NewRecorder()
	s.handleDecisions(w, req)
	var out decisionsBody
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].Kind != "guardian" {
		t.Fatalf("a managed member must not see team approvals: %+v", out.Items)
	}
}
