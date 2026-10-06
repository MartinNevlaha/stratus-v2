package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Skills and agents stay project files with bare names, as before the plugin: plugin skills
// would lose /bug, /resume, /code-review and /security-review to Claude Code's own commands,
// and OpenCode reads .claude/skills recursively. The stratus plugin carries only the hooks.
func TestInitClaudeCode_WritesProjectAssetsAndHooksPlugin(t *testing.T) {
	root := t.TempDir()
	initClaudeCode(root, map[string]string{})

	review, err := os.ReadFile(filepath.Join(root, ".claude", "skills", "code-review", "SKILL.md"))
	if err != nil {
		t.Fatalf("project skill missing: %v", err)
	}
	if !strings.Contains(string(review), "agent: delivery-code-reviewer\n") {
		t.Errorf("forked skill should name the project agent by its bare name:\n%s", review)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "agents", "delivery-code-reviewer.md")); err != nil {
		t.Errorf("project agent missing: %v", err)
	}
	for _, rel := range []string{".claude/skills/stratus/.claude-plugin/plugin.json", ".claude/skills/stratus/hooks/hooks.json"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	for _, rel := range []string{".claude/skills/stratus/skills", ".claude/skills/stratus/agents"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			t.Errorf("%s exists; skills and agents are project files", rel)
		}
	}
}

func TestInitOpenCode_SharesClaudeSkills(t *testing.T) {
	root := t.TempDir()
	initOpenCode(root, map[string]string{})

	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "spec", "SKILL.md")); err != nil {
		t.Errorf("OpenCode reads Stratus' skills from .claude/skills: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".opencode", "skills")); err == nil {
		t.Errorf(".opencode/skills was written; OpenCode would load every skill twice")
	}
}

// A project initialized by an earlier version keeps its layout: refresh updates the files it
// wrote in place and leaves a customized one alone.
func TestRefreshClaudeCode_UpdatesAnOlderInstallInPlace(t *testing.T) {
	root := t.TempDir()
	stored := map[string]string{}
	older := func(embedded, rel, content string) {
		stored[embedded] = sha256hex([]byte(content))
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	older("skills/spec/SKILL.md", ".claude/skills/spec/SKILL.md", "an older spec skill\n")
	older("agents/delivery-debugger.md", ".claude/agents/delivery-debugger.md", "an older debugger\n")
	older("skills/bug/SKILL.md", ".claude/skills/bug/SKILL.md", "an older bug skill\n")
	if err := os.WriteFile(filepath.Join(root, ".claude", "skills", "bug", "SKILL.md"), []byte("my own bug workflow\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	skipped := refreshClaudeCode(root, stored, map[string]string{})

	spec, _ := skillsFS.ReadFile("skills/spec/SKILL.md")
	if got, _ := os.ReadFile(filepath.Join(root, ".claude", "skills", "spec", "SKILL.md")); string(got) != string(spec) {
		t.Errorf("an unchanged older skill was not updated in place")
	}
	debugger, _ := agentsFS.ReadFile("agents/delivery-debugger.md")
	if got, _ := os.ReadFile(filepath.Join(root, ".claude", "agents", "delivery-debugger.md")); string(got) != string(debugger) {
		t.Errorf("an unchanged older agent was not updated in place")
	}
	if got, _ := os.ReadFile(filepath.Join(root, ".claude", "skills", "bug", "SKILL.md")); string(got) != "my own bug workflow\n" {
		t.Errorf("a customized skill was overwritten: %q", got)
	}
	if !slices.Contains(skipped, "skills/bug/SKILL.md") {
		t.Errorf("skipped = %v, want the customized bug skill reported", skipped)
	}
}

// With --target both, Claude Code and OpenCode both write .claude/rules: the second write
// finds the new content already on disk, which is no customization to report.
func TestWriteAssetsTo_FileAlreadyCurrentIsNotSkipped(t *testing.T) {
	root := t.TempDir()
	data, err := rulesFS.ReadFile("rules/workflow-governance.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflow-governance.md"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	stored := map[string]string{"rules/workflow-governance.md": "hash-of-an-older-version"}

	res, err := writeAssetsTo(rulesFS, "rules", root, stored)
	if err != nil {
		t.Fatalf("writeAssetsTo: %v", err)
	}
	if slices.Contains(res.skipped, "rules/workflow-governance.md") {
		t.Fatalf("a file already holding the new content was reported as customized")
	}
	if res.hashes["rules/workflow-governance.md"] != sha256hex(data) {
		t.Fatalf("the current hash was not recorded")
	}
}

// A file Stratus never wrote, sitting where a new Stratus file goes (a mod someone installed
// themselves in .claude/skills/mdview, say), is left alone and reported.
func TestWriteAssetsTo_KeepsSomeoneElsesFile(t *testing.T) {
	root := t.TempDir()
	mine := filepath.Join(root, "mdview", "LICENSE")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("someone else's mdview\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := writeAssetsTo(modsFS, "mods", root, map[string]string{})
	if err != nil {
		t.Fatalf("writeAssetsTo: %v", err)
	}
	if got, _ := os.ReadFile(mine); string(got) != "someone else's mdview\n" {
		t.Fatalf("someone else's file was overwritten: %q", got)
	}
	if !slices.Contains(res.skipped, "mods/mdview/LICENSE") {
		t.Fatalf("skipped = %v, want mods/mdview/LICENSE", res.skipped)
	}
}
