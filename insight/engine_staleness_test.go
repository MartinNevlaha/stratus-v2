package insight

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

// initGitRepo initialises a bare git repo in dir with one commit,
// returning the initial HEAD SHA.
func initGitRepo(t *testing.T, dir string) string {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	run("commit", "--allow-empty", "-m", "initial")

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return string(out[:len(out)-1]) // trim newline
}

// addFileCommit adds a file and commits it, returning the new HEAD SHA.
func addFileCommit(t *testing.T, dir, filename, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", filename)
	run("commit", "-m", "add "+filename)

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return string(out[:len(out)-1])
}

func TestCheckStartupStaleness_FirstRun_StoresCurrentHead(t *testing.T) {
	dir := t.TempDir()
	sha := initGitRepo(t, dir)

	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	e := NewEngine(database, cfg)
	e.SetProjectRoot(dir)

	e.checkStartupStaleness()

	stored, err := database.GetGuardianBaseline("wiki_last_head_sha")
	if err != nil {
		t.Fatalf("GetGuardianBaseline: %v", err)
	}
	if stored != sha {
		t.Errorf("stored SHA = %q, want %q", stored, sha)
	}
}

func TestCheckStartupStaleness_SameSha_NoUpdate(t *testing.T) {
	dir := t.TempDir()
	sha := initGitRepo(t, dir)

	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	e := NewEngine(database, cfg)
	e.SetProjectRoot(dir)

	// Pre-store the current SHA so we simulate "same SHA at startup".
	if err := database.SetGuardianBaseline("wiki_last_head_sha", sha); err != nil {
		t.Fatalf("SetGuardianBaseline: %v", err)
	}

	// Insert a wiki page and make it referenced to a source file.
	page := &db.WikiPage{
		PageType: "summary", Title: "T", Content: "C", Status: "published",
		StalenessScore: 0.1,
	}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	e.checkStartupStaleness()

	// Staleness score must remain unchanged.
	got, err := database.GetWikiPage(page.ID)
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got.StalenessScore != 0.1 {
		t.Errorf("staleness changed unexpectedly: got %v, want 0.1", got.StalenessScore)
	}
}

func TestCheckStartupStaleness_DifferentSha_BoostsStaleness(t *testing.T) {
	dir := t.TempDir()
	oldSHA := initGitRepo(t, dir)
	newSHA := addFileCommit(t, dir, "foo.go", "package main")

	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	e := NewEngine(database, cfg)
	e.SetProjectRoot(dir)

	// Store the old SHA so the engine sees a diff.
	if err := database.SetGuardianBaseline("wiki_last_head_sha", oldSHA); err != nil {
		t.Fatalf("SetGuardianBaseline: %v", err)
	}

	// Create a wiki page that references foo.go.
	page := &db.WikiPage{
		PageType: "summary", Title: "Foo", Content: "desc", Status: "published",
		StalenessScore: 0.2,
	}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}
	ref := &db.WikiPageRef{
		PageID: page.ID, SourceType: "artifact", SourceID: "foo.go",
	}
	if err := database.SaveWikiPageRef(ref); err != nil {
		t.Fatalf("SaveWikiPageRef: %v", err)
	}

	e.checkStartupStaleness()

	// Staleness must be boosted by 0.3 → 0.5.
	got, err := database.GetWikiPage(page.ID)
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	want := 0.5
	if got.StalenessScore != want {
		t.Errorf("staleness = %v, want %v", got.StalenessScore, want)
	}

	// Stored SHA must now be the new one.
	stored, err := database.GetGuardianBaseline("wiki_last_head_sha")
	if err != nil {
		t.Fatalf("GetGuardianBaseline: %v", err)
	}
	if stored != newSHA {
		t.Errorf("stored SHA = %q, want %q", stored, newSHA)
	}
}

func TestCheckStartupStaleness_StalenessClampedAt1(t *testing.T) {
	dir := t.TempDir()
	oldSHA := initGitRepo(t, dir)
	addFileCommit(t, dir, "bar.go", "package main")

	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	e := NewEngine(database, cfg)
	e.SetProjectRoot(dir)

	if err := database.SetGuardianBaseline("wiki_last_head_sha", oldSHA); err != nil {
		t.Fatalf("SetGuardianBaseline: %v", err)
	}

	// Create a page already at staleness 0.9 — boost would exceed 1.0.
	page := &db.WikiPage{
		PageType: "summary", Title: "Bar", Content: "desc", Status: "published",
		StalenessScore: 0.9,
	}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}
	ref := &db.WikiPageRef{
		PageID: page.ID, SourceType: "artifact", SourceID: "bar.go",
	}
	if err := database.SaveWikiPageRef(ref); err != nil {
		t.Fatalf("SaveWikiPageRef: %v", err)
	}

	e.checkStartupStaleness()

	got, err := database.GetWikiPage(page.ID)
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got.StalenessScore > 1.0 {
		t.Errorf("staleness exceeded 1.0: got %v", got.StalenessScore)
	}
	if got.StalenessScore != 1.0 {
		t.Errorf("staleness = %v, want 1.0", got.StalenessScore)
	}
}

func TestCheckStartupStaleness_NotGitRepo_Skips(t *testing.T) {
	dir := t.TempDir() // no git init

	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	e := NewEngine(database, cfg)
	e.SetProjectRoot(dir)

	// Should not panic or return an error — just silently skip.
	e.checkStartupStaleness()

	stored, err := database.GetGuardianBaseline("wiki_last_head_sha")
	if err != nil {
		t.Fatalf("GetGuardianBaseline: %v", err)
	}
	// Nothing should be stored.
	if stored != "" {
		t.Errorf("expected empty stored SHA for non-git dir, got %q", stored)
	}
}

func TestCheckStartupStaleness_NilDB_Skips(t *testing.T) {
	e := &Engine{database: nil}
	// Must not panic.
	e.checkStartupStaleness()
}
