package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAgentClaudeCode_KeepsEffortAndColor(t *testing.T) {
	dir := t.TempDir()
	in := &AgentDef{Name: "delivery-x", Description: "X", Tools: []string{"Read", "mcp__stratus__retrieve"}, Model: "opus", Effort: "high", Color: "red", Body: "# X\n"}
	if err := WriteAgentClaudeCode(dir, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := ParseAgentFile(filepath.Join(dir, "delivery-x.md"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.Effort != "high" || out.Color != "red" || out.Model != "opus" {
		t.Fatalf("round trip lost fields: effort=%q color=%q model=%q", out.Effort, out.Color, out.Model)
	}
	if len(out.Tools) != 2 || out.Tools[1] != "mcp__stratus__retrieve" {
		t.Fatalf("tools = %v", out.Tools)
	}
}

func TestParseAgentFile_ReadsEffortAndColor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.md")
	content := "---\nname: a\ndescription: \"A\"\ntools: Read\nmodel: sonnet\neffort: high\ncolor: purple\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := ParseAgentFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Effort != "high" || a.Color != "purple" {
		t.Fatalf("effort=%q color=%q", a.Effort, a.Color)
	}
}

// The dashboard edits name, description, disable-model-invocation, argument-hint and the
// body; frontmatter it does not show (context, agent, allowed-tools, paths, ...) survives.
func TestWriteSkill_KeepsUnmanagedFrontmatter(t *testing.T) {
	dir := t.TempDir()
	orig := "---\nname: review\ndescription: \"Old\"\ncontext: fork\nagent: stratus:delivery-code-reviewer\nallowed-tools: mcp__stratus\npaths:\n  - \"**/*.go\"\n---\n\nbody\n"
	if err := os.MkdirAll(filepath.Join(dir, "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review", "SKILL.md"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err := ParseSkillFile(filepath.Join(dir, "review"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	skill.Description = "New"
	if err := WriteSkill(dir, skill); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "review", "SKILL.md"))
	for _, want := range []string{`description: "New"`, "context: fork\n", "agent: stratus:delivery-code-reviewer\n", "allowed-tools: mcp__stratus\n", "paths:\n  - \"**/*.go\"\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("rewritten skill lacks %q:\n%s", want, data)
		}
	}
}

// A block scalar may hold blank lines; dropping them changes the YAML value.
func TestWriteSkill_KeepsBlankLinesInUnmanagedBlock(t *testing.T) {
	dir := t.TempDir()
	orig := "---\nname: notes\ndescription: \"d\"\nmetadata:\n  notes: |\n    para1\n\n    para2\nagent: x\n---\n\nbody\n"
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes", "SKILL.md"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err := ParseSkillFile(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := WriteSkill(dir, skill); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "notes", "SKILL.md"))
	if !strings.Contains(string(data), "    para1\n\n    para2\nagent: x\n") {
		t.Fatalf("blank line inside the block was lost:\n%s", data)
	}
}
