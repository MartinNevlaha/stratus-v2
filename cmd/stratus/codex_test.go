package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexInstall_ExistingProjectPreserved(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".stratus.json"), []byte(`{"port":42881,"data_dir":"custom-data","fixture_secret":"fake-value","sync_state":{"asset_hashes":{"skills/spec/SKILL.md":"legacy"},"synced_version":"old"}}`), 0600)
	if e := installCodex(root, false); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(root, ".stratus.json"))
	var v map[string]any
	json.Unmarshal(b, &v)
	if v["port"] != float64(42881) || v["fixture_secret"] != "fake-value" || v["data_dir"] != "custom-data" {
		t.Fatal(v)
	}
	sync := v["sync_state"].(map[string]any)
	if sync["asset_hashes"].(map[string]any)["skills/spec/SKILL.md"] != "legacy" || sync["synced_version"] != "old" {
		t.Fatal(sync)
	}
	if _, e := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(e) {
		t.Fatal("Claude assets changed")
	}
	if e := installCodex(root, false); e != nil {
		t.Fatal(e)
	}
}
func TestCodexInstall_DefaultTargetsUnchanged(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"stratus", "init"}
	_, target := parseInitFlags()
	if target != "both" {
		t.Fatal(target)
	}
}
func TestCodexRefresh_CustomizedAsset(t *testing.T) {
	root := t.TempDir()
	if e := installCodex(root, false); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(root, ".codex/stratus/instructions.md")
	os.WriteFile(p, []byte("my custom instructions"), 0600)
	if e := installCodex(root, true); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "my custom instructions" {
		t.Fatal(string(b))
	}
}
func TestCodexInstall_PreflightFailureWritesNothing(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".codex"), 0755)
	p := filepath.Join(root, ".codex/config.toml")
	os.WriteFile(p, []byte("[invalid"), 0600)
	if e := installCodex(root, false); e == nil {
		t.Fatal("invalid config accepted")
	}
	if _, e := os.Stat(filepath.Join(root, ".stratus.json")); !os.IsNotExist(e) {
		t.Fatal("shared config written")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "[invalid" {
		t.Fatal(string(b))
	}
}
func TestCodexInstall_OldOpenCodePluginNeedsRefresh(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".opencode/plugin"), 0755)
	p := filepath.Join(root, ".opencode/plugin/stratus.ts")
	os.WriteFile(p, []byte("custom old plugin"), 0600)
	e := installCodex(root, false)
	if e == nil || !strings.Contains(e.Error(), "refresh --target opencode") {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(p)
	if !bytes.Equal(b, []byte("custom old plugin")) {
		t.Fatal(string(b))
	}
}
func TestCodexInstall_PartialWriteCanResume(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".codex/stratus"), 0755)
	asset, _ := codexAssetsFS.ReadFile("codex-assets/instructions.md")
	os.WriteFile(filepath.Join(root, ".codex/stratus/instructions.md"), asset, 0600)
	if e := installCodex(root, false); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(root, ".codex/hooks.json")); e != nil {
		t.Fatal(e)
	}
}
func TestCodexInstall_SymlinkRejects(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(root, ".codex"))
	if e := installCodex(root, false); e == nil {
		t.Fatal("external symlink followed")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal(entries)
	}
}
