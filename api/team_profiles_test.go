package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTeamProfilesPreserveProjectOverrides(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".claude", "agents"), 0700)
	os.WriteFile(filepath.Join(root, ".claude", "agents", "delivery-system-architect.md"), []byte("---\nmodel: fable\neffort: high\n---\ncontent"), 0600)
	os.WriteFile(filepath.Join(root, "opencode.json"), []byte(`{"agent":{"delivery-code-reviewer":{"model":"openai/custom","variant":"max"}}}`), 0600)
	profiles, warnings := teamProfiles(root)
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	if profiles["claude-code"]["design"].Model != "fable" {
		t.Fatal("Claude override lost")
	}
	if profiles["opencode"]["review"].Model != "openai/custom" || profiles["opencode"]["review"].Effort != "max" {
		t.Fatal("OpenCode override lost")
	}
	if profiles["codex"]["implementation"].Model != "gpt-6.1-sol" || profiles["codex"]["review"].Model != "gpt-6-astra" {
		t.Fatal("exact Codex profiles lost")
	}
}
func TestTeamRevisionChangesWithUncommittedAndUntrackedWork(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	a, err := teamCodeRevision(root)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("one"), 0600)
	b, err := teamCodeRevision(root)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("two"), 0600)
	c, err := teamCodeRevision(root)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || b == c {
		t.Fatal("review revision ignores untracked content")
	}
}
