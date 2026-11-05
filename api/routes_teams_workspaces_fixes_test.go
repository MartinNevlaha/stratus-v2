package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

func TestTeamVerify_TransientFailureIsRetriedOnTheSameRevision(t *testing.T) {
	s, _, team, _ := workspaceFixture(t)
	dev1 := wsMember(t, team, "dev1")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")
	integrate(t, s, team.ID)

	orig := runGateCmd
	t.Cleanup(func() { runGateCmd = orig })
	builds := 0
	runGateCmd = func(dir, name string, args ...string) (string, error) {
		if name == "go" && len(args) > 0 && args[0] == "build" {
			builds++
			if builds == 1 {
				return "network unreachable", errors.New("exit status 1")
			}
		}
		return orig(dir, name, args...)
	}
	first := verifyOK(t, s, team.ID)
	if first["checks"].(map[string]any)["checks_ok"] != false {
		t.Fatalf("first run should fail: %v", first)
	}
	second := verifyOK(t, s, team.ID)
	if second["cached"] != false || second["checks"].(map[string]any)["checks_ok"] != true || builds != 2 {
		t.Fatalf("retry on the same SHA did not re-run and pass (builds=%d): %v", builds, second)
	}
	if third := verifyOK(t, s, team.ID); third["cached"] != true || builds != 2 {
		t.Fatalf("a passing result should now be cached (builds=%d): %v", builds, third)
	}
}

func TestTeamIntegrate_FailingMergeHookIsAFailureNotAConflict(t *testing.T) {
	s, root, team, _ := workspaceFixture(t)
	dev1 := wsMember(t, team, "dev1")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")
	hook := filepath.Join(root, ".git", "hooks", "pre-merge-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho \"hook refuses $PWD\" >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := integrate(t, s, team.ID)
	if c, _ := res["conflicts"].([]any); len(c) != 0 {
		t.Fatalf("hook failure reported as a conflict: %v", c)
	}
	failed, _ := res["failed"].([]any)
	if len(failed) != 1 {
		t.Fatalf("failed %v", res["failed"])
	}
	entry, _ := failed[0].(map[string]any)
	reason, _ := entry["reason"].(string)
	if reason == "" || strings.Contains(reason, root) {
		t.Fatalf("reason %q must be present and free of absolute paths", reason)
	}
	if len(strs(res["merged"])) != 0 || res["dirty"] != false {
		t.Fatalf("result %v", res)
	}
	got, _ := s.teams.Get(team.ID)
	if _, dirty, err := gitRevision(got.IntegrationDir); err != nil || dirty {
		t.Fatalf("integration tree not clean (err %v)", err)
	}
	if out, _ := runGit(got.IntegrationDir, "rev-parse", "-q", "--verify", "MERGE_HEAD"); strings.TrimSpace(out) != "" {
		t.Fatal("merge still in progress")
	}
}

func TestMergeBranch_UntrackedFileInTheWayIsAFailureNotAConflict(t *testing.T) {
	s, _, team, _ := workspaceFixture(t)
	integrate(t, s, team.ID) // creates the integration worktree
	got, _ := s.teams.Get(team.ID)
	dev1 := wsMember(t, *got, "dev1")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")
	gateWrite(t, got.IntegrationDir, "a.go", "package main\n\nfunc mine() {}\n")

	entry, conflict, err := newGitWorkspaces().mergeBranch(context.Background(), got.IntegrationDir, nil, dev1)
	if err != nil {
		t.Fatal(err)
	}
	if conflict || entry == nil || entry.Reason == "" || strings.Contains(entry.Reason, got.IntegrationDir) {
		t.Fatalf("conflict=%v entry=%+v", conflict, entry)
	}
	if data, _ := os.ReadFile(filepath.Join(got.IntegrationDir, "a.go")); !strings.Contains(string(data), "mine") {
		t.Fatal("the untracked file was overwritten")
	}
}

func TestTeamIntegrate_FallsBackToADeterministicIdentity(t *testing.T) {
	s, _, team, _ := workspaceFixture(t)
	dev1, dev2 := wsMember(t, team, "dev1"), wsMember(t, team, "dev2")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")
	wsCommit(t, dev2.WorkspaceDir, "b.go", "package main\n\nfunc b() {}\n")
	// No identity anywhere: blank environment, no global or system configuration.
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if out, err := runGit(dev1.WorkspaceDir, "var", "GIT_COMMITTER_IDENT"); err == nil {
		t.Skipf("this machine always has a git identity: %s", out)
	}

	res := integrate(t, s, team.ID)
	if len(strs(res["merged"])) != 2 {
		t.Fatalf("merges failed without a configured identity: %v", res)
	}
	got, _ := s.teams.Get(team.ID)
	cmd := runGateCmd
	out, err := cmd(got.IntegrationDir, "git", "-c", "log.showSignature=false", "log", "-1", "--format=%an <%ae>")
	if err != nil || !strings.Contains(out, "Stratus <stratus@localhost>") {
		t.Fatalf("merge commit author %q (%v)", out, err)
	}
}

func TestTeamIntegrate_RecreatesADeletedIntegrationWorktree(t *testing.T) {
	s, root, team, _ := workspaceFixture(t)
	dev1 := wsMember(t, team, "dev1")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")
	first := integrate(t, s, team.ID)
	got, _ := s.teams.Get(team.ID)
	if err := os.RemoveAll(got.IntegrationDir); err != nil {
		t.Fatal(err)
	}

	again := integrate(t, s, team.ID)
	if again["revision"] != first["revision"] || len(strs(again["nothing_to_merge"])) != 2 {
		t.Fatalf("integration lost its merges when its directory was deleted: first %v, again %v", first, again)
	}
	if _, err := os.Stat(filepath.Join(got.IntegrationDir, "a.go")); err != nil {
		t.Fatal(err)
	}
	_ = root
}

func TestTeamIntegrate_ContinuesAfterRestart(t *testing.T) {
	s, _, team, adapter := workspaceFixture(t)
	dev1 := wsMember(t, team, "dev1")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")
	first := integrate(t, s, team.ID)
	if err := s.teams.Close(); err != nil {
		t.Fatal(err)
	}
	m2, err := teams.NewManager(s.db.SQL(), map[string]teams.Adapter{"codex": adapter}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m2.Close() })
	m2.SetWorkspaceProvider(newGitWorkspaces())
	s.teams = m2

	wsCommit(t, dev1.WorkspaceDir, "c.go", "package main\n\nfunc c() {}\n")
	second := integrate(t, s, team.ID)
	if len(strs(second["merged"])) != 1 || second["revision"] == first["revision"] {
		t.Fatalf("integration after restart: %v", second)
	}
	if again := integrate(t, s, team.ID); again["revision"] != second["revision"] || len(strs(again["merged"])) != 0 {
		t.Fatalf("not idempotent after restart: %v", again)
	}
}

func TestTeamWorkspaceRemove_ForceNeedsASameOriginRequest(t *testing.T) {
	s, _, team, _ := workspaceFixture(t)
	dev1 := wsMember(t, team, "dev1")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")
	for name, origin := range map[string]string{"no origin": "", "other host": "https://evil.example", "garbage": "::not a url"} {
		if w := forceRemove(t, s, team.ID, origin); w.Code != http.StatusForbidden {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if _, err := os.Stat(dev1.WorkspaceDir); err != nil {
		t.Fatal("a refused request still removed the worktree")
	}
	if w := forceRemove(t, s, team.ID, "http://example.com"); w.Code != http.StatusOK {
		t.Fatalf("same-origin force: %d %s", w.Code, w.Body)
	}
}

func TestTeamWorkspaceRemove_BlockedBodyListsBlockers(t *testing.T) {
	s, _, team, _ := workspaceFixture(t)
	dev1, dev2 := wsMember(t, team, "dev1"), wsMember(t, team, "dev2")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")
	gateWrite(t, dev2.WorkspaceDir, "wip.go", "package main\n")
	w := commsDo(t, s.handleTeamWorkspaceRemove, "DELETE", "/api/teams/"+team.ID+"/workspaces", "", map[string]string{"id": team.ID})
	if w.Code != http.StatusConflict {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var body struct {
		Error    string   `json:"error"`
		Blockers []string `json:"blockers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(body.Blockers, "|")
	if len(body.Blockers) != 2 || !strings.Contains(joined, "dev1") || !strings.Contains(joined, "dev2") || body.Error == "" {
		t.Fatalf("body %s", w.Body)
	}
}

// pathFailingProvider fails Create with an error that names an absolute path.
type pathFailingProvider struct{ teams.WorkspaceProvider }

func (pathFailingProvider) Create(context.Context, string, string, string) (string, string, error) {
	return "", "", errors.New("git worktree add /home/secret/project/.stratus/worktrees/x: fatal")
}

func TestTeamCreate_WorkspaceErrorsDoNotLeakPathsAndBadIDsAre400(t *testing.T) {
	s, _, team, _ := workspaceFixture(t)
	if w := commsDo(t, s.handleTeamAction, "POST", "/api/teams/x/actions", `{"action":"stop"}`, map[string]string{"id": team.ID}); w.Code != http.StatusOK {
		t.Fatalf("stop: %d", w.Code)
	}
	s.teams.SetWorkspaceProvider(pathFailingProvider{newGitWorkspaces()})
	body := `{"isolated_workspaces":true,"members":[{"id":"dev","role":"implementation","profile":` + wsProfile + `}]}`
	w := commsDo(t, s.handleCreateTeam, "POST", "/api/teams", body, nil)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "/home/secret") || !strings.Contains(w.Body.String(), "see the server log") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	s.teams.SetWorkspaceProvider(newGitWorkspaces())
	for _, id := range []string{"x-", "Integration", "INTEGRATION"} {
		body = `{"isolated_workspaces":true,"members":[{"id":"` + id + `","role":"implementation","profile":` + wsProfile + `}]}`
		if w = commsDo(t, s.handleCreateTeam, "POST", "/api/teams", body, nil); w.Code != http.StatusBadRequest {
			t.Fatalf("member id %q: %d %s", id, w.Code, w.Body)
		}
	}
	body = `{"isolated_workspaces":true,"members":[{"id":"Dev","role":"implementation","profile":` + wsProfile + `},{"id":"dev","role":"implementation","profile":` + wsProfile + `}]}`
	if w = commsDo(t, s.handleCreateTeam, "POST", "/api/teams", body, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("case-colliding ids: %d %s", w.Code, w.Body)
	}
}

func TestRunCodeGates_ProjectWithoutGoModIsNotApplicable(t *testing.T) {
	dir := t.TempDir()
	rev := missionRevision{Dir: dir, Kind: dirKindIntegration}
	outcomes := runCodeGates(rev, "")
	if len(outcomes) < 2 {
		t.Fatalf("outcomes %+v", outcomes)
	}
	for i, want := range []string{"go build: not applicable (no go.mod in " + dir + ")", "go vet: not applicable (no go.mod in " + dir + ")"} {
		o := outcomes[i]
		if o.verdict != "info" || o.content != want {
			t.Fatalf("outcome %d: %q %q", i, o.verdict, o.content)
		}
		if name := gateName(db.SwarmEvidence{Agent: qualityGateAgent, Content: o.content}); name != []string{"build", "vet"}[i] {
			t.Fatalf("gate %d classified as %q", i, name)
		}
	}
}

func TestMissionHandoff_ProjectWithoutGoModNeverReadsAsChecksOK(t *testing.T) {
	s, _, wt := handoffFixture(t)
	gateGit(t, wt, "rm", "-q", "go.mod", "main.go")
	gateWrite(t, wt, "README.md", "docs only\n")
	gateGit(t, wt, "add", ".")
	gateGit(t, wt, "commit", "-q", "-m", "not a go project")
	s.runQualityGates("m1")

	code, h := getHandoff(t, s, "m1")
	if code != http.StatusOK || h.ChecksOK {
		t.Fatalf("checks_ok=%v (%d)", h.ChecksOK, code)
	}
	if v, _, ok := h.gate("build"); !ok || v != "info" {
		t.Fatalf("build gate %q found=%v", v, ok)
	}
	if !hasWarning(h.Warnings, "no applicable automated build/vet checks for this project type; verify manually") {
		t.Fatalf("warnings %v", h.Warnings)
	}
}

func TestTeamHandoff_ProjectWithoutGoModNeverReadsAsChecksOK(t *testing.T) {
	s, _, team, _ := workspaceFixtureWith(t, false)
	dev1 := wsMember(t, team, "dev1")
	wsCommit(t, dev1.WorkspaceDir, "notes.md", "hello\n")
	integrate(t, s, team.ID)
	res := verifyOK(t, s, team.ID)
	checks := res["checks"].(map[string]any)
	if checks["checks_ok"] != false {
		t.Fatalf("verify %v", res)
	}
	gates := map[string]string{}
	for _, g := range checks["gates"].([]any) {
		gate := g.(map[string]any)
		gates[gate["name"].(string)] = gate["verdict"].(string)
	}
	if gates["build"] != "info" || gates["vet"] != "info" {
		t.Fatalf("gates %v", gates)
	}
	_, h := wsCall(t, s.handleTeamHandoff, "GET", "/api/teams/"+team.ID+"/handoff", team.ID)
	if hc := h["checks"].(map[string]any); hc["checks_ok"] != false {
		t.Fatalf("handoff checks %v", hc)
	}
	if !strings.Contains(strings.Join(strs(h["warnings"]), "\n"), "no applicable automated build/vet checks for this project type; verify manually") {
		t.Fatalf("warnings %v", h["warnings"])
	}
}

// captureHub registers a fake client on the hub and returns the channel it receives on.
func captureHub(h *Hub) chan Message {
	c := &wsClient{send: make(chan Message, 32)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c.send
}

func expectTeamUpdated(t *testing.T, ch chan Message, what string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case m := <-ch:
			if m.Type == "team_updated" {
				return
			}
		case <-deadline:
			t.Fatalf("no team_updated broadcast after %s", what)
		}
	}
}

func TestTeamWorkspaceRoutes_BroadcastTeamUpdatedWhenTheyFinish(t *testing.T) {
	s, _, team, _ := workspaceFixture(t)
	ch := captureHub(s.hub)
	dev1 := wsMember(t, team, "dev1")
	wsCommit(t, dev1.WorkspaceDir, "a.go", "package main\n\nfunc a() {}\n")

	integrate(t, s, team.ID)
	expectTeamUpdated(t, ch, "integrate")

	verifyOK(t, s, team.ID)
	expectTeamUpdated(t, ch, "a passing verify")

	orig := runGateCmd
	t.Cleanup(func() { runGateCmd = orig })
	runGateCmd = func(dir, name string, args ...string) (string, error) {
		if name == "go" && len(args) > 0 && args[0] == "build" {
			return "broken", errors.New("exit status 1")
		}
		return orig(dir, name, args...)
	}
	wsCommit(t, dev1.WorkspaceDir, "b.go", "package main\n\nfunc b() {}\n")
	integrate(t, s, team.ID)
	expectTeamUpdated(t, ch, "integrate")
	verifyOK(t, s, team.ID)
	expectTeamUpdated(t, ch, "a failing verify")

	if w := forceRemove(t, s, team.ID, "http://example.com"); w.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", w.Code, w.Body)
	}
	expectTeamUpdated(t, ch, "remove")
}

func TestMissionHandoff_EmptyDirGivesNoNoChecksWarning(t *testing.T) {
	s, _ := gateFixture(t)
	s.projectRoot = "" // git then runs in the test's directory, which has no go.mod
	_, h := getHandoff(t, s, "m1")
	if hasWarning(h.Warnings, noChecksWarning) {
		t.Fatalf("warning for an unknown directory: %v", h.Warnings)
	}
}
