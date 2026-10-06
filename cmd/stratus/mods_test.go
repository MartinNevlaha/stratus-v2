package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readSettings(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	return settings
}

// Stratus' plugins are skills-directory plugins in .claude/skills/<plugin>/: Claude Code
// loads them from this project only, once the folder is trusted, with nothing in settings.
func TestInitClaudeCode_WritesSkillsDirPlugins(t *testing.T) {
	root := t.TempDir()
	initClaudeCode(root, map[string]string{})

	for _, rel := range []string{
		".claude/skills/stratus/.claude-plugin/plugin.json",
		".claude/skills/stratus/hooks/hooks.json",
		".claude/skills/mdview/.claude-plugin/plugin.json",
		".claude/skills/mdview/hooks/register.tsx",
		".claude/skills/mdview/LICENSE",
		".claude/skills/stratus-hud/.claude-plugin/plugin.json",
		".claude/skills/stratus-hud/hooks/register.tsx",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	settings := readSettings(t, root)
	for _, key := range []string{"hooks", "extraKnownMarketplaces", "enabledPlugins"} {
		if v, ok := settings[key]; ok {
			t.Errorf("%s = %v; the plugins need no settings entries", key, v)
		}
	}
}
