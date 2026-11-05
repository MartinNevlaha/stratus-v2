package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

const wsProfile = `{"runtime":"codex","model":"gpt-6.1-sol","effort":"high"}`

// wsAdapter opens sessions that answer every turn with a fixed report and
// remember the directory each session was started in.
type wsAdapter struct {
	mu    sync.Mutex
	roots []string
	reply string
}

func (a *wsAdapter) Preflight(context.Context, string) teams.Availability {
	return teamTestAdapter{}.Preflight(context.Background(), "")
}
func (a *wsAdapter) Open(_ context.Context, spec teams.SessionSpec, _ func(teams.Event)) (teams.Session, error) {
	a.mu.Lock()
	a.roots = append(a.roots, spec.ProjectRoot)
	a.mu.Unlock()
	return wsSession{reply: a.reply, native: "native-" + spec.MemberID}, nil
}

type wsSession struct{ reply, native string }

func (s wsSession) Submit(context.Context, teams.Input) (teams.Result, error) {
	return teams.Result{Text: s.reply, NativeID: s.native, Model: "gpt-6.1-sol", Effort: "high"}, nil
}
func (wsSession) Approve(context.Context, string, bool) error { return nil }
func (wsSession) Interrupt(context.Context) error             { return nil }
func (wsSession) Close() error                                { return nil }

// workspaceFixture builds a git project (a valid Go module on main), a server whose
// team manager has the real workspace provider, and one isolated team of
// lead + dev1 + dev2 + rev.
func workspaceFixture(t *testing.T) (*Server, string, teams.Team, *wsAdapter) {
	t.Helper()
	return workspaceFixtureWith(t, true)
}

// workspaceFixtureWith is workspaceFixture; withGo=false makes the project a plain
// repository without go.mod, like a non-Go project.
func workspaceFixtureWith(t *testing.T, withGo bool) (*Server, string, teams.Team, *wsAdapter) {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "test", "GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		t.Setenv(k, v)
	}
	root := t.TempDir()
	gateGit(t, root, "init", "-q", "-b", "main")
	if withGo {
		gateWrite(t, root, "go.mod", "module wstest\n\ngo 1.21\n")
		gateWrite(t, root, "main.go", "package main\n\nfunc main() {}\n")
	} else {
		gateWrite(t, root, "README.md", "not a Go project\n")
	}
	gateGit(t, root, "add", ".")
	gateGit(t, root, "commit", "-q", "-m", "base")

	d, err := db.Open(filepath.Join(t.TempDir(), "teams.db"))
	if err != nil {
		t.Fatal(err)
	}
	adapter := &wsAdapter{reply: "Verdict: PASS"}
	adapters := map[string]teams.Adapter{"codex": adapter}
	m, err := teams.NewManager(d.SQL(), adapters, func() {})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: d, projectRoot: root, hub: NewHub(), teams: m, teamAdapters: adapters}
	m.SetRevisionValidator(func(root, revision string) bool {
		current, err := teamCodeRevision(root)
		return err == nil && current == revision
	})
	m.SetWorkspaceProvider(newGitWorkspaces())
	t.Cleanup(func() { m.Close(); d.Close() })

	body := `{"isolated_workspaces":true,"members":[` +
		`{"id":"lead","role":"lead","profile":` + wsProfile + `},` +
		`{"id":"dev1","role":"implementation","profile":` + wsProfile + `},` +
		`{"id":"dev2","role":"implementation","profile":` + wsProfile + `},` +
		`{"id":"rev","role":"review","profile":` + wsProfile + `}]}`
	w := commsDo(t, s.handleCreateTeam, "POST", "/api/teams", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("create isolated team: %d %s", w.Code, w.Body)
	}
	var team teams.Team
	if err := json.Unmarshal(w.Body.Bytes(), &team); err != nil {
		t.Fatal(err)
	}
	return s, root, team, adapter
}

func wsMember(t *testing.T, team teams.Team, id string) teams.Member {
	t.Helper()
	for _, m := range team.Members {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("no member %s", id)
	return teams.Member{}
}

func wsCommit(t *testing.T, dir, name, content string) {
	t.Helper()
	gateWrite(t, dir, name, content)
	gateGit(t, dir, "add", ".")
	gateGit(t, dir, "commit", "-q", "-m", "add "+name)
}

func wsCall(t *testing.T, h http.HandlerFunc, method, target, id string) (int, map[string]any) {
	t.Helper()
	w := commsDo(t, h, method, target, "", map[string]string{"id": id})
	var out map[string]any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", w.Body, err)
		}
	}
	return w.Code, out
}

func strs(v any) []string {
	var out []string
	if list, ok := v.([]any); ok {
		for _, x := range list {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func integrate(t *testing.T, s *Server, id string) map[string]any {
	t.Helper()
	code, res := wsCall(t, s.handleTeamIntegrate, "POST", "/api/teams/"+id+"/integrate", id)
	if code != http.StatusOK {
		t.Fatalf("integrate %d %v", code, res)
	}
	return res
}

func TestTeamWorkspaces_CreateGivesWriteMembersWorktrees(t *testing.T) {
	s, root, team, _ := workspaceFixture(t)
	if !team.IsolatedWorkspaces {
		t.Fatal("team not isolated")
	}
	base := filepath.Join(root, ".stratus", "worktrees")
	for _, id := range []string{"dev1", "dev2"} {
		m := wsMember(t, team, id)
		if m.WorkspaceDir != filepath.Join(base, "team-"+team.ID+"-"+id) || m.WorkspaceBranch != "team/"+team.ID+"/"+id {
			t.Fatalf("%s: %q on %q", id, m.WorkspaceDir, m.WorkspaceBranch)
		}
		if _, err := os.Stat(filepath.Join(m.WorkspaceDir, "main.go")); err != nil {
			t.Fatalf("%s worktree not checked out: %v", id, err)
		}
		gateGit(t, root, "rev-parse", "--verify", "refs/heads/"+m.WorkspaceBranch)
	}
	for _, id := range []string{"lead", "rev"} {
		if m := wsMember(t, team, id); m.WorkspaceDir != "" {
			t.Fatalf("%s got a workspace %q", id, m.WorkspaceDir)
		}
	}
	_ = s
}

func TestTeamWorkspaces_CreateRejectsUnsafeMemberIDWithoutLeavingWorktrees(t *testing.T) {
	s, root, _, _ := workspaceFixture(t)
	// A second team is refused (one active team per project), so stop the first.
	stopped := commsDo(t, s.handleTeamAction, "POST", "/api/teams/x/actions", `{"action":"stop"}`, map[string]string{"id": firstTeamID(t, s)})
	if stopped.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", stopped.Code, stopped.Body)
	}
	before, _ := os.ReadDir(filepath.Join(root, ".stratus", "worktrees"))
	for _, id := range []string{"../evil", "a/b", "dev..x", "integration"} {
		body := `{"isolated_workspaces":true,"members":[{"id":"` + id + `","role":"implementation","profile":` + wsProfile + `}]}`
		w := commsDo(t, s.handleCreateTeam, "POST", "/api/teams", body, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("member id %q: %d %s", id, w.Code, w.Body)
		}
	}
	after, _ := os.ReadDir(filepath.Join(root, ".stratus", "worktrees"))
	if len(after) != len(before) {
		t.Fatalf("worktrees changed: %d -> %d", len(before), len(after))
	}
	if _, err := os.Stat(filepath.Join(root, ".stratus", "evil")); err == nil {
		t.Fatal("path traversal created a directory")
	}
}

func firstTeamID(t *testing.T, s *Server) string {
	t.Helper()
	id, _, err := s.teamProject()
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.teams.List(id)
	if err != nil || len(list) == 0 {
		t.Fatalf("no team: %v", err)
	}
	return list[0].ID
}

func TestTeamIntegrate_MergesTwoMemberBranches(t *testing.T) {
	s, root, team, _ := workspaceFixture(t)
	dev1, dev2 := wsMember(t, team, "dev1"), wsMember(t, team, "dev2")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")
	wsCommit(t, dev2.WorkspaceDir, "b.go", "package main\n\nfunc b() {}\n")

	res := integrate(t, s, team.ID)
	merged := strs(res["merged"])
	if len(merged) != 2 || res["dirty"] != false {
		t.Fatalf("result %v", res)
	}
	rev1, _ := res["revision"].(string)
	got, _ := s.teams.Get(team.ID)
	if got.IntegrationDir == "" || got.IntegrationBranch != "team/"+team.ID+"/integration" || got.IntegrationBase == "" {
		t.Fatalf("integration not recorded: %+v", got)
	}
	for _, f := range []string{"a.go", "b.go"} {
		if _, err := os.Stat(filepath.Join(got.IntegrationDir, f)); err != nil {
			t.Fatalf("%s missing from the integration tree: %v", f, err)
		}
	}
	head, _, err := gitRevision(root)
	if err != nil || rev1 == "" || rev1 == head {
		t.Fatalf("integration revision %q must differ from project HEAD %q (%v)", rev1, head, err)
	}

	again := integrate(t, s, team.ID)
	if again["revision"] != rev1 || len(strs(again["merged"])) != 0 || len(strs(again["nothing_to_merge"])) != 2 {
		t.Fatalf("second integration is not idempotent: %v", again)
	}
	wsCommit(t, dev1.WorkspaceDir, "c.go", "package main\n\nfunc c() {}\n")
	third := integrate(t, s, team.ID)
	if third["revision"] == rev1 || len(strs(third["merged"])) != 1 {
		t.Fatalf("new commit was not merged: %v", third)
	}
}

func TestTeamIntegrate_ConflictIsReportedAndAborted(t *testing.T) {
	s, _, team, _ := workspaceFixture(t)
	dev1, dev2 := wsMember(t, team, "dev1"), wsMember(t, team, "dev2")
	wsCommit(t, dev1.WorkspaceDir, "main.go", "package main\n\nfunc main() { println(1) }\n")
	wsCommit(t, dev2.WorkspaceDir, "main.go", "package main\n\nfunc main() { println(2) }\n")

	res := integrate(t, s, team.ID)
	if len(strs(res["merged"])) != 1 {
		t.Fatalf("exactly one branch should merge: %v", res)
	}
	conflicts, _ := res["conflicts"].([]any)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts %v", res["conflicts"])
	}
	c, _ := conflicts[0].(map[string]any)
	if files := strs(c["files"]); len(files) != 1 || files[0] != "main.go" || !strings.HasPrefix(c["branch"].(string), "team/"+team.ID+"/") {
		t.Fatalf("conflict detail %v", c)
	}
	got, _ := s.teams.Get(team.ID)
	if _, dirty, err := gitRevision(got.IntegrationDir); err != nil || dirty {
		t.Fatalf("integration tree left dirty after the aborted merge (err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(got.IntegrationDir, ".git")); err != nil {
		t.Fatal(err)
	}
	if out, _ := runGit(got.IntegrationDir, "rev-parse", "-q", "--verify", "MERGE_HEAD"); strings.TrimSpace(out) != "" {
		t.Fatal("merge still in progress")
	}
	// The integration worktree is still usable: the same conflict is reported again.
	if again := integrate(t, s, team.ID); len(again["conflicts"].([]any)) != 1 {
		t.Fatalf("second pass: %v", again)
	}
}

func TestTeamIntegrate_UncommittedMemberWorkIsAWarning(t *testing.T) {
	s, _, team, _ := workspaceFixture(t)
	dev1 := wsMember(t, team, "dev1")
	gateWrite(t, dev1.WorkspaceDir, "wip.go", "package main\n\nfunc wip() {}\n")
	res := integrate(t, s, team.ID)
	warnings := strings.Join(strs(res["warnings"]), "\n")
	if !strings.Contains(warnings, "dev1") || !strings.Contains(warnings, "uncommitted") {
		t.Fatalf("warnings %q", warnings)
	}
	if len(strs(res["merged"])) != 0 {
		t.Fatalf("uncommitted work was merged: %v", res)
	}
}

func TestTeamWorkspaces_NonIsolatedTeamIsRejected(t *testing.T) {
	s := teamAPIServer(t)
	s.teams.SetWorkspaceProvider(newGitWorkspaces())
	tm := createAPITeam(t, s)
	for name, h := range map[string]http.HandlerFunc{"integrate": s.handleTeamIntegrate, "verify": s.handleTeamVerify} {
		if code, _ := wsCall(t, h, "POST", "/api/teams/"+tm.ID+"/"+name, tm.ID); code != http.StatusConflict {
			t.Fatalf("%s on a non-isolated team: %d", name, code)
		}
	}
	if code, _ := wsCall(t, s.handleTeamWorkspaceRemove, "DELETE", "/api/teams/"+tm.ID+"/workspaces", tm.ID); code != http.StatusConflict {
		t.Fatalf("cleanup on a non-isolated team: %d", code)
	}
	code, res := wsCall(t, s.handleTeamHandoff, "GET", "/api/teams/"+tm.ID+"/handoff", tm.ID)
	if code != http.StatusOK || res["isolated"] != false {
		t.Fatalf("handoff %d %v", code, res)
	}
}

func TestTeamWorkspaces_OtherProjectsTeamIs404(t *testing.T) {
	s, _, _, _ := workspaceFixture(t)
	other, err := s.teams.Create(context.Background(), teams.CreateRequest{ProjectID: "project-other", ProjectRoot: t.TempDir(), Members: []teams.MemberConfig{{ID: "lead", Role: "lead", Profile: teams.Profile{Runtime: "codex", Model: "gpt-6.1-sol", Effort: "high"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for name, h := range map[string]http.HandlerFunc{"integrate": s.handleTeamIntegrate, "verify": s.handleTeamVerify, "handoff": s.handleTeamHandoff, "workspaces": s.handleTeamWorkspaceRemove} {
		method := "POST"
		if name == "handoff" {
			method = "GET"
		} else if name == "workspaces" {
			method = "DELETE"
		}
		if code, _ := wsCall(t, h, method, "/api/teams/"+other.ID+"/"+name, other.ID); code != http.StatusNotFound {
			t.Fatalf("%s on another project's team: %d", name, code)
		}
	}
}

func TestTeamVerify_RecordsBuildFailureOnlyVisibleAfterMergeAndCachesByRevision(t *testing.T) {
	s, _, team, _ := workspaceFixture(t)
	if code, _ := wsCall(t, s.handleTeamVerify, "POST", "/api/teams/"+team.ID+"/verify", team.ID); code != http.StatusConflict {
		t.Fatalf("verify before any integration: %d", code)
	}
	dev1, dev2 := wsMember(t, team, "dev1"), wsMember(t, team, "dev2")
	// Each branch builds alone; merged, both declare shared().
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc shared() {}\n")
	wsCommit(t, dev2.WorkspaceDir, "b.go", "package main\n\nfunc shared() {}\n")
	for _, dir := range []string{dev1.WorkspaceDir, dev2.WorkspaceDir} {
		if out, err := runGateCmd(dir, "go", "build", "-o", "/dev/null", "./..."); err != nil {
			t.Fatalf("branch should build alone: %v\n%s", err, out)
		}
	}
	res := integrate(t, s, team.ID)
	revision, _ := res["revision"].(string)

	code, first := wsCall(t, s.handleTeamVerify, "POST", "/api/teams/"+team.ID+"/verify", team.ID)
	if code != http.StatusOK {
		t.Fatalf("verify %d %v", code, first)
	}
	checks, _ := first["checks"].(map[string]any)
	if checks["checks_ok"] != false || checks["current"] != true || checks["revision"] != revision || first["cached"] != false {
		t.Fatalf("checks %v", first)
	}
	gates := map[string]string{}
	for _, g := range checks["gates"].([]any) {
		gate := g.(map[string]any)
		gates[gate["name"].(string)] = gate["verdict"].(string)
	}
	if gates["build"] != "fail" {
		t.Fatalf("build must fail on the merged tree: %v", gates)
	}

	_, second := wsCall(t, s.handleTeamVerify, "POST", "/api/teams/"+team.ID+"/verify", team.ID)
	if second["cached"] != false {
		t.Fatalf("a failing result must be re-run, not served from the cache: %v", second)
	}
	code, handoff := wsCall(t, s.handleTeamHandoff, "GET", "/api/teams/"+team.ID+"/handoff", team.ID)
	hc, _ := handoff["checks"].(map[string]any)
	if code != http.StatusOK || hc["current"] != true || hc["checks_ok"] != false {
		t.Fatalf("handoff checks %d %v", code, handoff["checks"])
	}

	// A fix on a member branch gives a new revision: the cached result is no longer current.
	wsCommit(t, dev2.WorkspaceDir, "b.go", "package main\n\nfunc other() {}\n")
	integrate(t, s, team.ID)
	_, handoff = wsCall(t, s.handleTeamHandoff, "GET", "/api/teams/"+team.ID+"/handoff", team.ID)
	if hc, _ := handoff["checks"].(map[string]any); hc["current"] != false || hc["checks_ok"] != false {
		t.Fatalf("stale checks reported current: %v", hc)
	}
	_, third := wsCall(t, s.handleTeamVerify, "POST", "/api/teams/"+team.ID+"/verify", team.ID)
	if third["cached"] != false || third["checks"].(map[string]any)["checks_ok"] != true {
		t.Fatalf("the fixed revision must be re-verified and pass: %v", third)
	}
	if fourth := verifyOK(t, s, team.ID); fourth["cached"] != true {
		t.Fatalf("a passing clean result should be cached: %v", fourth)
	}
}

func verifyOK(t *testing.T, s *Server, id string) map[string]any {
	t.Helper()
	code, res := wsCall(t, s.handleTeamVerify, "POST", "/api/teams/"+id+"/verify", id)
	if code != http.StatusOK {
		t.Fatalf("verify %d %v", code, res)
	}
	return res
}

func TestTeamHandoff_ReviewIsValidOnlyForTheCurrentIntegrationRevision(t *testing.T) {
	s, _, team, adapter := workspaceFixture(t)
	dev1 := wsMember(t, team, "dev1")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")
	integrate(t, s, team.ID)
	got, _ := s.teams.Get(team.ID)

	revision, err := teamCodeRevision(got.ReviewRoot())
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.teams.Enqueue(context.Background(), team.ID, teams.TaskRequest{From: "user", To: "rev", Role: "review", Kind: "task", Text: "review the merge", Revision: revision})
	if err != nil {
		t.Fatal(err)
	}
	awaitTeamTask(t, s, team.ID, task.ID)
	adapter.mu.Lock()
	roots := append([]string(nil), adapter.roots...)
	adapter.mu.Unlock()
	foundReviewer := false
	for _, r := range roots {
		if r == got.IntegrationDir {
			foundReviewer = true
		}
	}
	if !foundReviewer {
		t.Fatalf("reviewer did not run in the integration tree %q: %v", got.IntegrationDir, roots)
	}

	_, handoff := wsCall(t, s.handleTeamHandoff, "GET", "/api/teams/"+team.ID+"/handoff", team.ID)
	review, _ := handoff["review"].(map[string]any)
	if review["verdict"] != "pass" || review["valid"] != true || review["current"] != true {
		t.Fatalf("review should be valid: %v", handoff["review"])
	}

	wsCommit(t, dev1.WorkspaceDir, "c.go", "package main\n\nfunc c() {}\n")
	integrate(t, s, team.ID)
	_, handoff = wsCall(t, s.handleTeamHandoff, "GET", "/api/teams/"+team.ID+"/handoff", team.ID)
	review, _ = handoff["review"].(map[string]any)
	if review["valid"] != false || review["current"] != false || review["reason"] == "" {
		t.Fatalf("review of an older revision stayed valid: %v", review)
	}
}

func awaitTeamTask(t *testing.T, s *Server, teamID, taskID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tm, err := s.teams.Get(teamID)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range tm.Tasks {
			if task.ID == taskID && task.State == "complete" {
				return
			}
			if task.ID == taskID && (task.State == "needs_user" || task.State == "cancelled") {
				t.Fatalf("task %s: %s %s", taskID, task.State, task.Error)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("task did not finish")
}

func TestTeamHandoff_ReportsMembersDiffAndWarnings(t *testing.T) {
	s, _, team, _ := workspaceFixture(t)
	dev1, dev2 := wsMember(t, team, "dev1"), wsMember(t, team, "dev2")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")
	gateWrite(t, dev2.WorkspaceDir, "wip.go", "package main\n")

	code, h := wsCall(t, s.handleTeamHandoff, "GET", "/api/teams/"+team.ID+"/handoff", team.ID)
	if code != http.StatusOK || h["isolated"] != true || h["integration"] != nil {
		t.Fatalf("before integration: %d %v", code, h)
	}
	integrate(t, s, team.ID)
	_, h = wsCall(t, s.handleTeamHandoff, "GET", "/api/teams/"+team.ID+"/handoff", team.ID)
	integration, _ := h["integration"].(map[string]any)
	if integration["dir_kind"] != "integration" || integration["dirty"] != false || integration["revision"] == "" {
		t.Fatalf("integration %v", integration)
	}
	members := map[string]map[string]any{}
	for _, m := range h["members"].([]any) {
		member := m.(map[string]any)
		members[member["id"].(string)] = member
	}
	if len(members) != 2 || members["dev1"]["ahead"] != float64(0) || members["dev2"]["uncommitted"] != float64(1) {
		t.Fatalf("members %v", members)
	}
	if strs(h["merged"])[0] != "dev1" {
		t.Fatalf("merged %v", h["merged"])
	}
	if stat, _ := h["diff_stat"].(map[string]any); stat["files_changed"] != float64(1) {
		t.Fatalf("diff_stat %v", h["diff_stat"])
	}
	if !strings.Contains(strings.Join(strs(h["warnings"]), "\n"), "dev2") {
		t.Fatalf("warnings %v", h["warnings"])
	}
}

func TestTeamWorkspaceRemove_RefusesUnmergedWorkUnlessForced(t *testing.T) {
	s, root, team, _ := workspaceFixture(t)
	dev1 := wsMember(t, team, "dev1")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")

	code, res := wsCall(t, s.handleTeamWorkspaceRemove, "DELETE", "/api/teams/"+team.ID+"/workspaces", team.ID)
	if code != http.StatusConflict || !strings.Contains(res["error"].(string), "dev1") {
		t.Fatalf("unmerged commits not refused: %d %v", code, res)
	}
	if _, err := os.Stat(dev1.WorkspaceDir); err != nil {
		t.Fatal("refused removal still deleted the worktree")
	}
	// Merged into the integration branch is not enough: that branch is not in the project HEAD yet.
	integrate(t, s, team.ID)
	if code, _ = wsCall(t, s.handleTeamWorkspaceRemove, "DELETE", "/api/teams/"+team.ID+"/workspaces", team.ID); code != http.StatusConflict {
		t.Fatalf("unmerged integration branch not refused: %d", code)
	}

	w := forceRemove(t, s, team.ID, "http://example.com")
	if w.Code != http.StatusOK {
		t.Fatalf("forced removal: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(dev1.WorkspaceDir); !os.IsNotExist(err) {
		t.Fatal("forced removal left the worktree")
	}
	if out, _ := runGit(root, "branch", "--list", "team/"+team.ID+"/*"); strings.TrimSpace(out) != "" {
		t.Fatalf("branches left behind: %s", out)
	}
	got, _ := s.teams.Get(team.ID)
	if got.IsolatedWorkspaces || got.IntegrationDir != "" || wsMember(t, *got, "dev1").WorkspaceDir != "" {
		t.Fatalf("team still isolated after removal: %+v", got)
	}
}

func TestTeamWorkspaceRemove_EmptyWorkspacesNeedNoForceAndDirtyOnesDo(t *testing.T) {
	s, root, team, _ := workspaceFixture(t)
	dev2 := wsMember(t, team, "dev2")
	gateWrite(t, dev2.WorkspaceDir, "wip.go", "package main\n")
	if code, res := wsCall(t, s.handleTeamWorkspaceRemove, "DELETE", "/api/teams/"+team.ID+"/workspaces", team.ID); code != http.StatusConflict || !strings.Contains(res["error"].(string), "uncommitted") {
		t.Fatalf("uncommitted changes not refused: %d %v", code, res)
	}
	if err := os.Remove(filepath.Join(dev2.WorkspaceDir, "wip.go")); err != nil {
		t.Fatal(err)
	}
	if code, res := wsCall(t, s.handleTeamWorkspaceRemove, "DELETE", "/api/teams/"+team.ID+"/workspaces", team.ID); code != http.StatusOK {
		t.Fatalf("empty workspaces should be removable: %d %v", code, res)
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".stratus", "worktrees"))
	if len(entries) != 0 {
		t.Fatalf("worktree directories left: %v", entries)
	}
}

// forceRemove calls DELETE .../workspaces?force=true with the given Origin ("" for none).
func forceRemove(t *testing.T, s *Server, id, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("DELETE", "/api/teams/"+id+"/workspaces?force=true", nil)
	req.SetPathValue("id", id)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.handleTeamWorkspaceRemove(w, req)
	return w
}
