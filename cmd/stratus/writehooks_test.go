package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSettings(t *testing.T, root, content string) {
	t.Helper()
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// With agent teams on, every subagent Claude names launches as a teammate, so
// delivery agents stop returning results to the coordinator like subagents do.
func TestWriteHooks_DoesNotEnableAgentTeams(t *testing.T) {
	root := t.TempDir()
	if err := writeHooks(root); err != nil {
		t.Fatalf("writeHooks: %v", err)
	}
	env, _ := readSettings(t, root)["env"].(map[string]any)
	if v, ok := env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"]; ok {
		t.Fatalf("CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS = %v, want unset", v)
	}
}

func TestWriteHooks_RemovesOldAgentTeamsDefault(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, `{"env": {"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1", "OTHER": "x"}}`)
	if err := writeHooks(root); err != nil {
		t.Fatalf("writeHooks: %v", err)
	}
	env, _ := readSettings(t, root)["env"].(map[string]any)
	if v, ok := env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"]; ok {
		t.Fatalf("CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS = %v, want removed", v)
	}
	if env["OTHER"] != "x" {
		t.Fatalf("env[OTHER] = %v, want untouched", env["OTHER"])
	}
}

func TestWriteHooks_KeepsOtherAgentTeamsValue(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, `{"env": {"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "0"}}`)
	if err := writeHooks(root); err != nil {
		t.Fatalf("writeHooks: %v", err)
	}
	env, _ := readSettings(t, root)["env"].(map[string]any)
	if env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"] != "0" {
		t.Fatalf("CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS = %v, want user's 0 kept", env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"])
	}
}

// Hooks ship in the stratus plugin now: settings.json keeps a user's own hooks and loses
// every entry an earlier Stratus wrote, legacy matchers and dead hooks included.
func TestWriteHooks_RemovesStratusHooksKeepsUsers(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, `{"hooks": {
		"PreToolUse": [
			{"matcher": "Write|Edit|Bash|NotebookEdit|MultiEdit", "hooks": [
				{"type": "command", "command": "stratus hook phase_guard"},
				{"type": "command", "command": "./my-lint.sh"}]},
			{"matcher": "Task", "hooks": [{"type": "command", "command": "stratus hook executor_routing_guard"}]}
		],
		"TeammateIdle": [{"matcher": "", "hooks": [{"type": "command", "command": "stratus hook teammate_idle"}]}]
	}}`)
	writePluginHooks(t, root)
	if err := writeHooks(root); err != nil {
		t.Fatalf("writeHooks: %v", err)
	}
	hooks, _ := readSettings(t, root)["hooks"].(map[string]any)
	if _, ok := hooks["TeammateIdle"]; ok {
		t.Errorf("an event left with no hooks should be dropped: %v", hooks["TeammateIdle"])
	}
	groups, _ := hooks["PreToolUse"].([]any)
	if len(groups) != 1 {
		t.Fatalf("PreToolUse groups = %v, want only the user's", groups)
	}
	if hasStratusHook(groups, "stratus hook phase_guard") || !hasStratusHook(groups, "./my-lint.sh") {
		t.Fatalf("PreToolUse = %v, want ./my-lint.sh and no Stratus hook", groups)
	}
}

func TestWriteHooks_FreshInstallHasNoHooks(t *testing.T) {
	root := t.TempDir()
	if err := writeHooks(root); err != nil {
		t.Fatalf("writeHooks: %v", err)
	}
	if hooks, ok := readSettings(t, root)["hooks"]; ok {
		t.Fatalf("hooks = %v, want none: the stratus plugin registers them", hooks)
	}
}

// The plugin's hooks.json and cmdHook must name the same hooks, so no handler ships
// unregistered and no hook points at a missing handler.
func TestPluginHooks_MatchHandlers(t *testing.T) {
	data, err := modsFS.ReadFile("mods/stratus/hooks/hooks.json")
	if err != nil {
		t.Fatalf("read plugin hooks.json: %v", err)
	}
	var file struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse plugin hooks.json: %v", err)
	}
	registered := map[string]bool{}
	for _, groups := range file.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				name, ok := strings.CutPrefix(h.Command, "stratus hook ")
				if !ok {
					t.Errorf("unexpected hook command %q", h.Command)
				}
				registered[name] = true
			}
		}
	}
	for name := range hookHandlers {
		if !registered[name] {
			t.Errorf("handler %q is not registered in the plugin's hooks.json", name)
		}
	}
	for name := range registered {
		if _, ok := hookHandlers[name]; !ok {
			t.Errorf("hooks.json registers %q, which cmdHook does not handle", name)
		}
	}
}

// STRATUS_EXECUTOR was written for every project but nothing ever read it.
func TestWriteHooks_DropsUnusedStratusExecutor(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, `{"env": {"STRATUS_EXECUTOR": "cc"}}`)
	if err := writeHooks(root); err != nil {
		t.Fatalf("writeHooks: %v", err)
	}
	if env, ok := readSettings(t, root)["env"]; ok {
		t.Fatalf("env = %v, want no env left once STRATUS_EXECUTOR is gone", env)
	}
}

// hasStratusHook returns true when command is already present in the hook groups slice.
func hasStratusHook(groups []any, command string) bool {
	for _, g := range groups {
		group, ok := g.(map[string]any)
		if !ok {
			continue
		}
		hooks, _ := group["hooks"].([]any)
		for _, h := range hooks {
			entry, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := entry["command"].(string); cmd == command {
				return true
			}
		}
	}
	return false
}

func TestWriteOpenCodeConfig_DropsUnusedStratusExecutor(t *testing.T) {
	root := t.TempDir()
	existing := `{"mcp": {"stratus": {"type": "local", "command": ["stratus", "mcp-serve"], "enabled": true, "environment": {"STRATUS_EXECUTOR": "oc"}}}}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeOpenCodeConfig(root); err != nil {
		t.Fatalf("writeOpenCodeConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "STRATUS_EXECUTOR") {
		t.Fatalf("opencode.json still has STRATUS_EXECUTOR:\n%s", data)
	}
}

func writePluginHooks(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, pluginDir, "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"hooks": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// If the plugin could not be written, the settings hooks stay: the guards keep running.
func TestWriteHooks_KeepsSettingsHooksWithoutThePlugin(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "stratus hook bash_write_guard"}]}]}}`)
	if err := writeHooks(root); err != nil {
		t.Fatalf("writeHooks: %v", err)
	}
	hooks, _ := readSettings(t, root)["hooks"].(map[string]any)
	groups, _ := hooks["PreToolUse"].([]any)
	if !hasStratusHook(groups, "stratus hook bash_write_guard") {
		t.Fatalf("PreToolUse = %v; without the plugin's hooks.json the settings hooks must stay", groups)
	}
}
