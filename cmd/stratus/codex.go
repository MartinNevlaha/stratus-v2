package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed codex-assets
var codexAssetsFS embed.FS

func codexHooksJSON() []byte {
	events := map[string]any{}
	for _, event := range []string{"SessionStart", "PreToolUse", "PostToolUse", "SubagentStart", "SubagentStop", "PermissionRequest", "Stop", "Interrupt", "SessionEnd"} {
		handler := "activity"
		if event == "SessionStart" {
			handler = "context"
		}
		if event == "PreToolUse" {
			handler = "guard"
		}
		h := map[string]any{"type": "command", "command": "stratus codex-hook " + handler, "timeout": 3}
		if handler == "context" {
			h["additionalContextLimit"] = 2500
		}
		events[event] = []any{map[string]any{"hooks": []any{h}}}
	}
	b, _ := json.Marshal(map[string]any{"hooks": events})
	return b
}
func readOptional(path string) ([]byte, error) {
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return nil, nil
	}
	return b, e
}
func safeCodexDestination(root, path string) error {
	rel, e := filepath.Rel(root, path)
	if e != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("destination outside project")
	}
	p := root
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		p = filepath.Join(p, part)
		st, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("project-local destination is a symlink: %s", p)
		}
		if i == len(parts)-1 && !st.Mode().IsRegular() {
			return fmt.Errorf("destination is not a regular file: %s", p)
		}
		if i < len(parts)-1 && !st.IsDir() {
			return fmt.Errorf("destination parent is not a directory: %s", p)
		}
	}
	return nil
}
func atomicCodexWrite(path string, b []byte) error {
	old, e := os.ReadFile(path)
	if e == nil && bytes.Equal(old, b) {
		return nil
	}
	if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	mode := os.FileMode(0600)
	if st, e := os.Stat(path); e == nil {
		mode = st.Mode().Perm()
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".stratus-write-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
func writeCodexSyncState(src []byte, hashes map[string]string, skipped []string) ([]byte, error) {
	root := map[string]json.RawMessage{}
	if len(src) > 0 {
		if e := json.Unmarshal(src, &root); e != nil {
			return nil, e
		}
	}
	if root == nil {
		return nil, fmt.Errorf("shared configuration must be an object")
	}
	sync := map[string]json.RawMessage{}
	if v := root["sync_state"]; v != nil {
		if e := json.Unmarshal(v, &sync); e != nil {
			return nil, e
		}
	}
	if sync == nil {
		return nil, fmt.Errorf("sync_state must be an object")
	}
	all := map[string]string{}
	if v := sync["asset_hashes"]; v != nil {
		if e := json.Unmarshal(v, &all); e != nil {
			return nil, e
		}
	}
	if all == nil {
		all = map[string]string{}
	}
	for k, v := range hashes {
		all[k] = v
	}
	b, _ := json.Marshal(all)
	sync["asset_hashes"] = b
	var previous []string
	if v := sync["skipped_files"]; v != nil {
		if e := json.Unmarshal(v, &previous); e != nil {
			return nil, e
		}
	}
	kept := []string{}
	for _, v := range previous {
		if !strings.HasPrefix(v, "codex-assets/") {
			kept = append(kept, v)
		}
	}
	kept = append(kept, skipped...)
	sort.Strings(kept)
	b, _ = json.Marshal(kept)
	sync["skipped_files"] = b
	if sync["synced_version"] == nil {
		b, _ := json.Marshal(Version)
		sync["synced_version"] = b
	}
	b, _ = json.Marshal(Version)
	sync["codex_synced_version"] = b
	b, _ = json.Marshal(sync)
	root["sync_state"] = b
	b, e := json.MarshalIndent(root, "", "  ")
	return append(b, '\n'), e
}
func installCodex(root string, refresh bool) error {
	root, e := filepath.Abs(root)
	if e != nil {
		return e
	}
	_ = refresh // Both entry points preserve customizations.
	legacyPath := filepath.Join(root, ".opencode/plugin/stratus.ts")
	if b, e := readOptional(legacyPath); e != nil {
		return e
	} else if b != nil {
		compatible, _ := pluginsOpenCodeFS.ReadFile("plugins-opencode/stratus.ts")
		if !bytes.Equal(b, compatible) {
			return fmt.Errorf("existing OpenCode Stratus plugin needs stratus refresh --target opencode; customized files must be reviewed through sync-stratus before installing Codex")
		}
	}
	configPath := filepath.Join(root, ".codex/config.toml")
	hookPath := filepath.Join(root, ".codex/hooks.json")
	sharedPath := filepath.Join(root, ".stratus.json")
	for _, p := range []string{configPath, hookPath, sharedPath} {
		if e = safeCodexDestination(root, p); e != nil {
			return e
		}
	}
	configBytes, e := readOptional(configPath)
	if e != nil {
		return e
	}
	mergedConfig, e := mergeCodexTOML(configBytes)
	if e != nil {
		return e
	}
	hookBytes, e := readOptional(hookPath)
	if e != nil {
		return e
	}
	mergedHooks, e := mergeCodexHooks(hookBytes, codexHooksJSON())
	if e != nil {
		return e
	}
	shared, e := readOptional(sharedPath)
	if e != nil {
		return e
	}
	if shared == nil {
		shared = []byte(`{"port":41777}`)
	}
	var old struct {
		SyncState struct {
			AssetHashes map[string]string `json:"asset_hashes"`
		} `json:"sync_state"`
	}
	if e = json.Unmarshal(shared, &old); e != nil {
		return fmt.Errorf(".stratus.json: %w", e)
	}
	stored := old.SyncState.AssetHashes
	if stored == nil {
		stored = map[string]string{}
	}
	stage, e := os.MkdirTemp("", "stratus-codex-assets-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	dest := filepath.Join(root, ".codex/stratus")
	e = fs.WalkDir(codexAssetsFS, "codex-assets", func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel("codex-assets", path)
		target := filepath.Join(dest, rel)
		if e = safeCodexDestination(root, target); e != nil {
			return e
		}
		if b, e := readOptional(target); e != nil {
			return e
		} else if b != nil {
			p := filepath.Join(stage, rel)
			if e = os.MkdirAll(filepath.Dir(p), 0755); e != nil {
				return e
			}
			return os.WriteFile(p, b, 0600)
		}
		return nil
	})
	if e != nil {
		return e
	}
	result, e := writeAssetsTo(codexAssetsFS, "codex-assets", stage, stored)
	if e != nil {
		return e
	}
	mergedShared, e := writeCodexSyncState(shared, result.hashes, result.skipped)
	if e != nil {
		return e
	}
	for embedded := range result.hashes {
		rel, _ := filepath.Rel("codex-assets", embedded)
		b, e := os.ReadFile(filepath.Join(stage, rel))
		if e != nil {
			return e
		}
		if e = atomicCodexWrite(filepath.Join(dest, rel), b); e != nil {
			return fmt.Errorf("write Codex asset %s: %w", rel, e)
		}
	}
	for _, item := range []struct {
		path string
		b    []byte
	}{{configPath, mergedConfig}, {hookPath, mergedHooks}, {sharedPath, mergedShared}} {
		if e = atomicCodexWrite(item.path, item.b); e != nil {
			return fmt.Errorf("write %s: %w", item.path, e)
		}
	}
	for _, p := range result.skipped {
		fmt.Printf("Preserved customized Codex asset: %s\n", p)
	}
	fmt.Println("Codex project files installed. Start a fresh trusted Codex session and review/activate its MCP and hooks; active capabilities have not been verified.")
	return nil
}
