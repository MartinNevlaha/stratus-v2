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

func TestTeamProfiles_ProjectLayerOverridesAndReportsSource(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".claude", "agents"), 0700)
	os.WriteFile(filepath.Join(root, ".claude", "agents", "delivery-system-architect.md"), []byte("---\nmodel: fable\neffort: high\n---\n"), 0600)
	os.WriteFile(filepath.Join(root, "opencode.json"), []byte(`{"agent":{"delivery-code-reviewer":{"model":"openai/custom","variant":"max"}}}`), 0600)
	os.WriteFile(filepath.Join(root, ".stratus.json"), []byte(`{"managed_team":{"profiles":{
		"codex":{"review":{"model":"gpt-6.1-sol","effort":"medium","complex_effort":"high"},"bogus":{"model":"x"}},
		"claude-code":{"design":{"model":"opus","effort":"low"}},
		"unknown-runtime":{"review":{"model":"x"}}}}}`), 0600)
	set := loadTeamProfiles(root)
	if len(set.Warnings) != 0 {
		t.Fatal(set.Warnings)
	}
	if p := set.Profiles["codex"]["review"]; p.Model != "gpt-6.1-sol" || p.Effort != "medium" {
		t.Fatalf("project layer not applied: %+v", p)
	}
	if p := set.Profiles["claude-code"]["design"]; p.Model != "opus" || p.Effort != "low" {
		t.Fatalf("project layer must beat agent file: %+v", p)
	}
	if _, ok := set.Profiles["codex"]["bogus"]; ok {
		t.Fatal("unknown role leaked into profiles")
	}
	if set.ComplexEfforts["codex"]["review"] != "high" {
		t.Fatalf("complex effort lost: %+v", set.ComplexEfforts)
	}
	want := map[[2]string]string{
		{"codex", "review"}:          "project",
		{"claude-code", "design"}:    "project",
		{"codex", "implementation"}:  "default",
		{"opencode", "review"}:       "opencode",
		{"claude-code", "review"}:    "default",
		{"opencode", "design"}:       "default",
		{"claude-code", "diagnosis"}: "default",
	}
	for key, source := range want {
		if got := set.Sources[key[0]][key[1]]; got != source {
			t.Errorf("source %v = %q, want %q", key, got, source)
		}
	}
}

func TestTeamProfiles_AgentFileSource(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".claude", "agents"), 0700)
	os.WriteFile(filepath.Join(root, ".claude", "agents", "delivery-system-architect.md"), []byte("---\nmodel: fable\n---\n"), 0600)
	set := loadTeamProfiles(root)
	if set.Sources["claude-code"]["design"] != "agent-file" || set.Sources["claude-code"]["review"] != "default" {
		t.Fatalf("sources: %+v", set.Sources["claude-code"])
	}
}
