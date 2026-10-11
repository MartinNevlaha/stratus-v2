package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSettings(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMCPDisabledNotice_FlagsDisabledStratusServer(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, dir, "settings.local.json", `{"disabledMcpjsonServers":["stratus"]}`)
	got := mcpDisabledNotice([]string{dir})
	if !strings.Contains(got, "DISABLED") || !strings.Contains(got, "/mcp") {
		t.Errorf("notice = %q", got)
	}
}

func TestMCPDisabledNotice_ChecksSharedSettingsToo(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, dir, "settings.json", `{"disabledMcpjsonServers":["other","stratus"]}`)
	if got := mcpDisabledNotice([]string{dir}); got == "" {
		t.Error("expected a notice for settings.json")
	}
}

func TestMCPDisabledNotice_QuietWhenEnabledOrAbsent(t *testing.T) {
	dir := t.TempDir()
	if got := mcpDisabledNotice([]string{dir, ""}); got != "" {
		t.Errorf("no settings: %q", got)
	}
	writeSettings(t, dir, "settings.local.json", `{"disabledMcpjsonServers":["playwright"],"enabledMcpjsonServers":["stratus"]}`)
	if got := mcpDisabledNotice([]string{dir}); got != "" {
		t.Errorf("stratus not disabled but notice = %q", got)
	}
}

func TestMCPDisabledNotice_IgnoresMalformedSettings(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, dir, "settings.local.json", `{not json`)
	if got := mcpDisabledNotice([]string{dir}); got != "" {
		t.Errorf("malformed settings must not raise a notice: %q", got)
	}
}

func TestSessionStart_WarnsAboutDisabledMCPEvenWithoutWorkflows(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, dir, "settings.local.json", `{"disabledMcpjsonServers":["stratus"]}`)
	t.Setenv("STRATUS_PORT", "1") // API unreachable: the warning must not depend on it
	d := SessionStart(HookEvent{HookEventName: "SessionStart", SessionID: "s1", Cwd: dir})
	if !d.Continue || !strings.Contains(d.Context, "DISABLED") {
		t.Errorf("decision = %+v", d)
	}
}
