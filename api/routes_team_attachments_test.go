package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/projectidentity"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

func attachmentCall(t *testing.T, s *Server, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.RemoteAddr = "127.0.0.1:1234"
	if token != "" {
		r.Header.Set("X-Stratus-Team-Token", token)
	}
	mux := http.NewServeMux()
	s.registerTeamRoutes(mux)
	w := httptest.NewRecorder()
	s.managedTeamGuard(mux).ServeHTTP(w, r)
	return w
}
func joinAttachment(t *testing.T, s *Server, native string) map[string]any {
	t.Helper()
	w := attachmentCall(t, s, "POST", "/api/team-attachments/join", map[string]any{"runtime": "codex", "host_id": "local", "native_id": native, "project_root": s.projectRoot}, "")
	if w.Code != 200 {
		t.Fatalf("join %d %s", w.Code, w.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}
func TestAttachmentJoinScopesIdentityAndNeverStealsLead(t *testing.T) {
	s := teamAPIServer(t)
	tm := createAPITeam(t, s)
	joined := joinAttachment(t, s, "existing-chat")
	if joined["token"] == "" || joined["team_id"] != tm.ID || joined["native_session_id"] != "existing-chat" {
		t.Fatalf("bad grant %v", joined)
	}
	actual, _ := s.teams.Get(tm.ID)
	if actual.Members[0].Role != "lead" || len(actual.Members) != 2 || actual.Members[1].Role != "member" {
		t.Fatalf("lead stolen: %+v", actual.Members)
	}
	cross := attachmentCall(t, s, "POST", "/api/team-attachments/join", map[string]any{"runtime": "codex", "host_id": "local", "native_id": "other", "project_root": t.TempDir(), "team_id": tm.ID}, "")
	if cross.Code != 403 {
		t.Fatalf("cross project %d %s", cross.Code, cross.Body)
	}
	forged := attachmentCall(t, s, "POST", "/api/team-attachments/join", map[string]any{"runtime": "codex", "host_id": "local", "native_id": "other", "project_root": s.projectRoot, "generation": 99}, "")
	if forged.Code != 400 {
		t.Fatalf("forged generation %d", forged.Code)
	}
	denied := attachmentCall(t, s, "POST", "/api/team-attachments/join", map[string]any{}, joined["token"].(string))
	if denied.Code != 403 {
		t.Fatalf("managed bootstrap %d", denied.Code)
	}
}
func TestAttachmentJoinRejectsRemoteAndBrowserBootstrap(t *testing.T) {
	s := teamAPIServer(t)
	for _, tc := range []struct{ remote, origin string }{{"192.0.2.1:2000", ""}, {"127.0.0.1:2000", "https://evil.example"}} {
		r := httptest.NewRequest("POST", "/api/team-attachments/join", bytes.NewBufferString(`{}`))
		r.RemoteAddr = tc.remote
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.handleTeamAttachmentJoin(w, r)
		if w.Code != 403 {
			t.Fatalf("bootstrap %+v: %d", tc, w.Code)
		}
	}
}
func TestAttachmentJoinNeedsTeamAndGenerationRevocation(t *testing.T) {
	s := teamAPIServer(t)
	noTeam := joinAttachment(t, s, "existing-chat")
	if noTeam["status"] != "needs_team" || noTeam["token"] != nil {
		t.Fatalf("needs team %v", noTeam)
	}
	tm := createAPITeam(t, s)
	first := joinAttachment(t, s, "existing-chat")
	second := joinAttachment(t, s, "existing-chat")
	if first["member_id"] != second["member_id"] || second["generation"].(float64) <= first["generation"].(float64) || first["token"] == second["token"] {
		t.Fatalf("not fenced: %v %v", first, second)
	}
	old := attachmentCall(t, s, "POST", "/api/team-attachments/observe", map[string]any{"generation": first["generation"], "turn": teams.NativeTurn{State: "idle"}}, first["token"].(string))
	if old.Code != 403 {
		t.Fatalf("stale grant %d", old.Code)
	}
	current := attachmentCall(t, s, "POST", "/api/team-attachments/observe", map[string]any{"generation": second["generation"], "turn": teams.NativeTurn{State: "idle"}}, second["token"].(string))
	if current.Code != 200 {
		t.Fatalf("current grant %d %s", current.Code, current.Body)
	}
	left := attachmentCall(t, s, "POST", "/api/team-attachments/leave", map[string]any{"generation": second["generation"]}, second["token"].(string))
	if left.Code != 200 {
		t.Fatalf("leave %d %s", left.Code, left.Body)
	}
	actual, _ := s.teams.Get(tm.ID)
	if actual.Members[1].Attachment.State != "detached" {
		t.Fatal("not detached")
	}
}
func TestAttachmentStoppedTeamPreservesOriginalPeer(t *testing.T) {
	s := teamAPIServer(t)
	tm := createAPITeam(t, s)
	joined := joinAttachment(t, s, "original")
	if err := s.syncManagedTeamState(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.teams.Action(context.Background(), tm.ID, "stop"); err != nil {
		t.Fatal(err)
	}
	if err := s.syncManagedTeamState(); err != nil {
		t.Fatal(err)
	}
	peer, err := s.db.GetPeerBySession("codex:" + joined["native_session_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if peer.EndedAt != nil {
		t.Fatal("stopped team ended original chat peer")
	}
}

func attachmentObserve(t *testing.T, s *Server, grant map[string]any, turn teams.NativeTurn) {
	t.Helper()
	w := attachmentCall(t, s, "POST", "/api/team-attachments/observe", map[string]any{"generation": grant["generation"], "turn": turn}, grant["token"].(string))
	if w.Code != 200 {
		t.Fatalf("observe %d %s", w.Code, w.Body)
	}
}
func attachmentPendingInput(t *testing.T, s *Server, tm teams.Team, grant map[string]any) teams.Input {
	t.Helper()
	attachmentObserve(t, s, grant, teams.NativeTurn{State: "idle"})
	_, err := s.teams.Enqueue(context.Background(), tm.ID, teams.TaskRequest{ID: "input", From: "user", To: grant["member_id"].(string), Text: "handle team work"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := httptest.NewRequest("GET", "/api/team-attachments/next?generation="+strconv.Itoa(int(grant["generation"].(float64))), nil).WithContext(ctx)
	r.Header.Set("X-Stratus-Team-Token", grant["token"].(string))
	w := httptest.NewRecorder()
	s.handleTeamAttachmentNext(w, r)
	if w.Code != 200 {
		t.Fatalf("next %d %s", w.Code, w.Body)
	}
	var result struct {
		Input teams.Input `json:"input"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Input.ID != "input" {
		t.Fatalf("no pending input %+v", result)
	}
	return result.Input
}
func TestAttachmentACKResultAndCoalescedHumanScope(t *testing.T) {
	s := teamAPIServer(t)
	tm := createAPITeam(t, s)
	grant := joinAttachment(t, s, "existing")
	input := attachmentPendingInput(t, s, tm, grant)
	receipt := map[string]any{"generation": grant["generation"], "task_id": input.ID, "native_turn_id": "native-turn"}
	denied := attachmentCall(t, s, "POST", "/api/team-attachments/ack", receipt, grant["token"].(string))
	if denied.Code != 409 {
		t.Fatalf("unobserved ACK %d", denied.Code)
	}
	attachmentObserve(t, s, grant, teams.NativeTurn{ID: "native-turn", Origin: "user", State: "running"})
	ack := attachmentCall(t, s, "POST", "/api/team-attachments/ack", receipt, grant["token"].(string))
	if ack.Code != 200 {
		t.Fatalf("ACK %d %s", ack.Code, ack.Body)
	}
	message := attachmentCall(t, s, "POST", "/api/team-message", map[string]any{"id": "child", "to": "lead", "text": "inspect the team input", "parent_task_id": input.ID}, grant["token"].(string))
	if message.Code != 200 {
		t.Fatalf("scoped message %d %s", message.Code, message.Body)
	}
	var child teams.Task
	if err := json.Unmarshal(message.Body.Bytes(), &child); err != nil {
		t.Fatal(err)
	}
	if child.ParentTaskID != input.ID {
		t.Fatalf("team scope replaced with human scope: %+v", child)
	}
	idle := attachmentCall(t, s, "POST", "/api/team-attachments/observe", map[string]any{"generation": grant["generation"], "turn": teams.NativeTurn{State: "idle"}}, grant["token"].(string))
	if idle.Code != 409 {
		t.Fatalf("idle erased active input: %d", idle.Code)
	}
	actual, _ := s.teams.Get(tm.ID)
	for _, task := range actual.Tasks {
		if task.ID == input.ID && task.State != "running" {
			t.Fatalf("native terminal finished input: %+v", task)
		}
	}
	receipt["text"] = "explicit result"
	completed := attachmentCall(t, s, "POST", "/api/team-attachments/result", receipt, grant["token"].(string))
	if completed.Code != 200 {
		t.Fatalf("result %d %s", completed.Code, completed.Body)
	}
	duplicate := attachmentCall(t, s, "POST", "/api/team-attachments/result", receipt, grant["token"].(string))
	if duplicate.Code != 200 {
		t.Fatalf("duplicate %d", duplicate.Code)
	}
	receipt["text"] = "different"
	conflict := attachmentCall(t, s, "POST", "/api/team-attachments/result", receipt, grant["token"].(string))
	if conflict.Code != 409 {
		t.Fatalf("changed result %d", conflict.Code)
	}
}
func TestAttachmentRequestRequiresHumanScopeAndReturnsToCaller(t *testing.T) {
	s := teamAPIServer(t)
	tm := createAPITeam(t, s)
	grant := joinAttachment(t, s, "requester")
	req := map[string]any{"id": "request", "text": "please coordinate this work"}
	denied := attachmentCall(t, s, "POST", "/api/team-request", req, grant["token"].(string))
	if denied.Code != 403 {
		t.Fatalf("unobserved request %d", denied.Code)
	}
	attachmentObserve(t, s, grant, teams.NativeTurn{ID: "human", Origin: "user", State: "running"})
	w := attachmentCall(t, s, "POST", "/api/team-request", req, grant["token"].(string))
	if w.Code != 200 {
		t.Fatalf("request %d %s", w.Code, w.Body)
	}
	var task teams.Task
	json.Unmarshal(w.Body.Bytes(), &task)
	if task.To != tm.Members[0].ID || task.ReturnTo != grant["member_id"] || task.From != grant["member_id"] || task.WorkflowID != "" {
		t.Fatalf("incorrect request routing %+v", task)
	}
}

func TestAttachmentHumanCanContinueOnlyOwnPersonalWorkflow(t *testing.T) {
	s := teamAPIServer(t)
	createAPITeam(t, s)
	grant := joinAttachment(t, s, "personal-chat")
	s.coordinator = orchestration.NewCoordinator(s.db)
	for _, owner := range []string{"personal-chat", "other-chat"} {
		if _, err := s.coordinator.StartOwned("wf-"+owner, orchestration.WorkflowSpec, orchestration.ComplexitySimple, "personal", orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: owner}); err != nil {
			t.Fatal(err)
		}
	}
	attachmentObserve(t, s, grant, teams.NativeTurn{ID: "personal-turn", Origin: "user", State: "running"})
	for _, tc := range []struct {
		path    string
		allowed bool
	}{{"/api/workflows/wf-personal-chat/phase", true}, {"/api/workflows/wf-other-chat/phase", false}, {"/api/workflows", false}} {
		called := false
		r := httptest.NewRequest("PUT", tc.path, bytes.NewBufferString(`{"phase":"implement"}`))
		r.Header.Set("X-Stratus-Team-Token", grant["token"].(string))
		w := httptest.NewRecorder()
		s.managedTeamGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })).ServeHTTP(w, r)
		if called != tc.allowed {
			t.Fatalf("%s allowed=%v status=%d %s", tc.path, called, w.Code, w.Body)
		}
	}
}

type attachmentProfileAdapter struct{ teamTestAdapter }

func (attachmentProfileAdapter) Preflight(context.Context, string) teams.Availability {
	return teams.Availability{Runtime: "codex", Available: true, Models: []teams.Model{{ID: "gpt-6.1-sol", Efforts: []string{"high", "xhigh"}}, {ID: "gpt-6-astra", Efforts: []string{"high", "xhigh"}}, {ID: "gpt-6-luna", Efforts: []string{"medium"}}, {ID: "custom-model", Efforts: []string{"high", "xhigh"}}}}
}
func TestAttachmentCreateUnknownLeadAndConfiguredWorkers(t *testing.T) {
	s := teamAPIServer(t)
	s.teams.Close()
	manager, err := teams.NewManager(s.db.SQL(), map[string]teams.Adapter{"codex": attachmentProfileAdapter{}}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	s.teams = manager
	t.Cleanup(func() { manager.Close() })
	if err := os.WriteFile(filepath.Join(s.projectRoot, ".stratus.json"), []byte(`{"managed_team":{"profiles":{"codex":{"implementation":{"model":"custom-model"}}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	w := attachmentCall(t, s, "POST", "/api/team-attachments/join", map[string]any{"runtime": "codex", "host_id": "local", "native_id": "new-lead", "project_root": s.projectRoot, "create_team": true}, "")
	if w.Code != 200 {
		t.Fatalf("create attached %d %s", w.Code, w.Body)
	}
	var grant map[string]any
	json.Unmarshal(w.Body.Bytes(), &grant)
	actual, err := s.teams.Get(grant["team_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if len(actual.Members) != 6 || actual.Members[0].Role != "lead" || actual.Members[0].Profile.Model != "" || actual.Members[0].Attachment == nil {
		t.Fatalf("wrong team %+v", actual)
	}
	for _, m := range actual.Members[1:] {
		if m.Attachment != nil || !m.Configured {
			t.Fatalf("wrong worker %+v", m)
		}
		if m.Role == "implementation" && m.Profile.Model != "custom-model" {
			t.Fatalf("override lost %+v", m)
		}
		if m.Role == "review" && (m.ComplexProfile == nil || m.ComplexProfile.Effort != "xhigh") {
			t.Fatalf("complex review profile missing %+v", m)
		}
	}
}

func TestAttachmentExplicitWorkflowBindingPreservesHumanWorkflow(t *testing.T) {
	s := teamAPIServer(t)
	tm := createAPITeam(t, s)
	grant := joinAttachment(t, s, "coordinator")
	memberID := grant["member_id"].(string)
	if err := s.teams.SetLead(context.Background(), tm.ID, memberID); err != nil {
		t.Fatal(err)
	}
	s.coordinator = orchestration.NewCoordinator(s.db)
	for _, id := range []string{"human-wf", "team-wf", "foreign-wf"} {
		native := "coordinator"
		if id == "foreign-wf" {
			native = "somebody-else"
		}
		if _, err := s.coordinator.StartOwned(id, orchestration.WorkflowSpec, orchestration.ComplexitySimple, "work", orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: native}); err != nil {
			t.Fatal(err)
		}
	}
	input := attachmentPendingInput(t, s, tm, grant)
	attachmentObserve(t, s, grant, teams.NativeTurn{ID: "shared-turn", Origin: "user", State: "running"})
	if err := s.teams.BindWorkflow(tm.ID, memberID, "human-wf", "spec", "simple"); err != nil {
		t.Fatal(err)
	}
	ack := attachmentCall(t, s, "POST", "/api/team-attachments/ack", map[string]any{"generation": grant["generation"], "task_id": input.ID, "native_turn_id": "shared-turn"}, grant["token"].(string))
	if ack.Code != 200 {
		t.Fatalf("ACK %d %s", ack.Code, ack.Body)
	}
	request := map[string]any{"id": "work-child", "to": "lead", "text": "scoped colleague work", "parent_task_id": input.ID, "workflow_id": "team-wf"}
	child := attachmentCall(t, s, "POST", "/api/team-message", request, grant["token"].(string))
	if child.Code != 200 {
		t.Fatalf("workflow child %d %s", child.Code, child.Body)
	}
	actual, _ := s.teams.Get(tm.ID)
	for _, m := range actual.Members {
		if m.ID == memberID && m.WorkflowID != "human-wf" {
			t.Fatalf("human workflow replaced %+v", m)
		}
	}
	for _, task := range actual.Tasks {
		if task.ID == input.ID && task.WorkflowID != "team-wf" {
			t.Fatalf("explicit input not bound %+v", task)
		}
		if task.Origin == "external" && task.WorkflowID != "human-wf" {
			t.Fatalf("external workflow changed %+v", task)
		}
	}
	request["id"] = "foreign-child"
	request["workflow_id"] = "foreign-wf"
	foreign := attachmentCall(t, s, "POST", "/api/team-message", request, grant["token"].(string))
	if foreign.Code != 403 {
		t.Fatalf("foreign workflow allowed %d %s", foreign.Code, foreign.Body)
	}
}

func TestAttachmentCapabilitiesRequireServerProjectAndLiveBroker(t *testing.T) {
	s := teamAPIServer(t)
	createAPITeam(t, s)
	grant := joinAttachment(t, s, "native")
	s.teamAttachmentsMu.Lock()
	delete(s.teamAttachments, attachmentKey(grant["team_id"].(string), grant["member_id"].(string)))
	s.teamAttachmentsMu.Unlock()
	missing := attachmentCall(t, s, "POST", "/api/team-attachments/observe", map[string]any{"generation": grant["generation"], "turn": teams.NativeTurn{State: "idle"}}, grant["token"].(string))
	if missing.Code != 409 {
		t.Fatalf("no live transport accepted %d", missing.Code)
	}
	project, root, err := projectidentity.Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := s.teams.Create(context.Background(), teams.CreateRequest{ProjectID: project, ProjectRoot: root, Members: []teams.MemberConfig{{ID: "lead", Role: "lead", Profile: teams.Profile{Runtime: "codex", Model: "gpt-6.1-sol", Effort: "high"}}}})
	if err != nil {
		t.Fatal(err)
	}
	w := attachmentCall(t, s, "GET", "/api/team-context", nil, foreign.Members[0].Token)
	if w.Code != 403 {
		t.Fatalf("foreign project token accepted %d %s", w.Code, w.Body)
	}
}

func TestAttachmentUnknownObservationsNeverGrantHumanAuthority(t *testing.T) {
	s := teamAPIServer(t)
	tm := createAPITeam(t, s)
	grant := joinAttachment(t, s, "unknown-native")
	attachmentObserve(t, s, grant, teams.NativeTurn{ID: "previous-turn", Origin: "unknown", State: "complete"})
	attachmentObserve(t, s, grant, teams.NativeTurn{ID: "busy-turn", Origin: "unknown", State: "running"})
	actual, _ := s.teams.Get(tm.ID)
	for _, m := range actual.Members {
		if m.ID == grant["member_id"] && m.ActiveTaskID != "" {
			t.Fatalf("unknown gained authority %+v", m)
		}
	}
	denied := attachmentCall(t, s, "POST", "/api/team-request", map[string]any{"text": "unverified user request"}, grant["token"].(string))
	if denied.Code != 403 {
		t.Fatalf("unknown origin request %d", denied.Code)
	}
	attachmentObserve(t, s, grant, teams.NativeTurn{ID: "busy-turn", Origin: "unknown", State: "complete"})
}

func TestAttachmentContextExplainsExplicitReceiptsWithoutCredential(t *testing.T) {
	s := teamAPIServer(t)
	createAPITeam(t, s)
	grant := joinAttachment(t, s, "context-chat")
	w := attachmentCall(t, s, "GET", "/api/team-context", nil, grant["token"].(string))
	if w.Code != 200 {
		t.Fatalf("context %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	instruction, _ := result["instruction"].(string)
	if !strings.Contains(instruction, "team_ack") || !strings.Contains(instruction, "team_result") || !strings.Contains(instruction, "parent_task_id") {
		t.Fatalf("receipt contract omitted: %s", instruction)
	}
	if strings.Contains(w.Body.String(), grant["token"].(string)) {
		t.Fatal("capability exposed to model")
	}
}
