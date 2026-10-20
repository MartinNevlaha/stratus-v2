package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func hookRoot(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	root := filepath.Join(t.TempDir(), "project with spaces")
	os.MkdirAll(filepath.Join(root, "nested"), 0755)
	os.WriteFile(filepath.Join(root, ".stratus.json"), []byte(fmt.Sprintf(`{"port":%d}`, port)), 0600)
	return root
}
func TestCodexHook_DenialJSON(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"workflows":[{"id":"spec-codex","runtime":"codex","session_id":"codex:s1","phase":"verify"}]}`))
	})
	raw, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "session_id": "s1", "agent_type": "delivery-backend-engineer", "tool_name": "apply_patch", "tool_input": map[string]any{"command": "*** Begin Patch\n*** Add File: a.go\n+x\n*** End Patch"}})
	var out, stderr bytes.Buffer
	code := Run("guard", bytes.NewReader(raw), &out, &stderr, nil)
	var v map[string]any
	json.Unmarshal(out.Bytes(), &v)
	d, _ := v["hookSpecificOutput"].(map[string]any)
	if code != 0 || d["permissionDecision"] != "deny" || d["hookEventName"] != "PreToolUse" {
		t.Fatal(code, out.String(), stderr.String())
	}
}
func TestCodexHook_TelemetryOutage(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "offline", 503) })
	raw := fmt.Sprintf(`{"hook_event_name":"Stop","cwd":%q,"session_id":"s1"}`, root)
	var out, stderr bytes.Buffer
	if code := Run("activity", strings.NewReader(raw), &out, &stderr, nil); code != 0 || stderr.Len() == 0 {
		t.Fatal(code, out.String(), stderr.String())
	}
}
func TestCodexHook_ProjectRootWithSpaces(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"workflows":[]}`)) })
	cfg, e := ResolveProject(filepath.Join(root, "nested"))
	if e != nil || cfg.ProjectRoot != root || cfg.Port == 41777 {
		t.Fatal(cfg.ProjectRoot, cfg.Port, e)
	}
}
func TestCodexHook_UnknownSchema(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Run("guard", strings.NewReader(`{"hook_event_name":"Unknown"}`), &out, &stderr, nil); code != 0 {
		t.Fatal(code)
	}
	if strings.Contains(out.String(), `"continue":false`) {
		t.Fatal(out.String())
	}
}
