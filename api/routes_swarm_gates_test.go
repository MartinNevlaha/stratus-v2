package api

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/swarm"
)

func gateGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gateWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gateFixture builds a git repo holding a valid Go module on main, a mission m1 (base
// main, merge branch "merge") with one ticket, and a server wired to it.
func gateFixture(t *testing.T) (*Server, string) {
	t.Helper()
	return gateFixtureWithBase(t, "main")
}

// gateFixtureWithBase is gateFixture with the mission's base branch set to base.
func gateFixtureWithBase(t *testing.T, base string) (*Server, string) {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "test", "GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		t.Setenv(k, v)
	}
	s, wfID := agentTestServer(t)
	root := t.TempDir()
	gateGit(t, root, "init", "-q", "-b", "main")
	gateWrite(t, root, "go.mod", "module gatetest\n\ngo 1.21\n")
	gateWrite(t, root, "main.go", "package main\n\nfunc main() {}\n")
	gateGit(t, root, "add", ".")
	gateGit(t, root, "commit", "-q", "-m", "base")

	s.projectRoot = root
	s.swarm = swarm.NewStore(s.db, root)
	if err := s.db.CreateMission("m1", wfID, "mission", base, "merge", ""); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}
	if err := s.db.CreateTicket("t1", "m1", "t1", "", "backend", 1, "[]", "[]"); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	return s, root
}

func gateEvidence(t *testing.T, s *Server) []db.SwarmEvidence {
	t.Helper()
	ev, err := s.swarm.ListTicketEvidence("t1")
	if err != nil {
		t.Fatalf("ListTicketEvidence: %v", err)
	}
	return ev
}

func findGate(ev []db.SwarmEvidence, prefix string) *db.SwarmEvidence {
	for i := range ev {
		if strings.HasPrefix(ev[i].Content, prefix) {
			return &ev[i]
		}
	}
	return nil
}

func TestRunQualityGates_UsesIntegrationWorktree(t *testing.T) {
	s, root := gateFixture(t)
	wt := filepath.Join(root, ".stratus", "worktrees", "merge")
	gateGit(t, root, "worktree", "add", "-q", "-b", "merge", wt, "main")
	gateWrite(t, wt, "broken.go", "package main\n\nfunc broken() { undefinedSymbol() }\n")

	s.runQualityGates("m1")

	ev := gateEvidence(t, s)
	build := findGate(ev, "go build FAILED")
	if build == nil || build.Verdict != "fail" {
		t.Fatalf("expected failed build gate, got %+v", ev)
	}
	if !strings.Contains(build.Content, wt) {
		t.Errorf("build evidence does not name the integration worktree %q:\n%s", wt, build.Content)
	}
}

func TestRunQualityGates_FallsBackToProjectRoot(t *testing.T) {
	s, root := gateFixture(t)

	s.runQualityGates("m1")

	ev := gateEvidence(t, s)
	build := findGate(ev, "go build: OK")
	if build == nil || build.Verdict != "pass" {
		t.Fatalf("expected passing build gate, got %+v", ev)
	}
	if !strings.Contains(build.Content, root) {
		t.Errorf("build evidence does not name the project root %q:\n%s", root, build.Content)
	}
}

func TestRunQualityGates_PlaceholderScanDiffsAgainstBaseBranch(t *testing.T) {
	s, root := gateFixture(t)
	wt := filepath.Join(root, ".stratus", "worktrees", "merge")
	gateGit(t, root, "worktree", "add", "-q", "-b", "merge", wt, "main")
	// The marker sits more than five commits before HEAD: a HEAD~5 diff would miss it.
	gateWrite(t, wt, "todo.go", "package main\n\n// TODO finish this\nfunc todo() {}\n")
	gateGit(t, wt, "add", ".")
	gateGit(t, wt, "commit", "-q", "-m", "todo")
	for i := 0; i < 6; i++ {
		gateWrite(t, wt, "extra.txt", fmt.Sprintf("line %d\n", i))
		gateGit(t, wt, "add", ".")
		gateGit(t, wt, "commit", "-q", "-m", "extra")
	}

	s.runQualityGates("m1")

	ev := gateEvidence(t, s)
	scan := findGate(ev, "Found ")
	if scan == nil || scan.Verdict != "fail" {
		t.Fatalf("expected failed placeholder scan, got %+v", ev)
	}
	if !strings.Contains(scan.Content, "TODO finish this") || !strings.Contains(scan.Content, wt) {
		t.Errorf("placeholder evidence should name the marker and %q:\n%s", wt, scan.Content)
	}
}

func TestRunQualityGates_PlaceholderScanSkippedForBadBaseRef(t *testing.T) {
	s, root := gateFixtureWithBase(t, "no-such-branch")

	s.runQualityGates("m1")

	ev := gateEvidence(t, s)
	for _, e := range ev {
		if strings.HasPrefix(e.Content, "Placeholder scan") && e.Verdict == "pass" {
			t.Fatalf("a failed git diff must never record a passing placeholder scan: %+v", e)
		}
	}
	skipped := findGate(ev, "Placeholder scan skipped")
	if skipped == nil || skipped.Verdict != "info" {
		t.Fatalf("expected info 'Placeholder scan skipped' evidence, got %+v", ev)
	}
	if !strings.Contains(skipped.Content, root) {
		t.Errorf("skipped evidence should name %q:\n%s", root, skipped.Content)
	}
}

func TestRunQualityGates_PlaceholderScanSkippedForEmptyBaseBranch(t *testing.T) {
	s, _ := gateFixtureWithBase(t, "")

	s.runQualityGates("m1")

	ev := gateEvidence(t, s)
	if skipped := findGate(ev, "Placeholder scan skipped"); skipped == nil || skipped.Verdict != "info" {
		t.Fatalf("expected info 'Placeholder scan skipped' evidence, got %+v", ev)
	}
}
