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

func TestReview_ReadOnlyDeliveryExplorationDuringOutage(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "offline", 503) })
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "session_id": "s1", "agent_type": "delivery-backend-engineer", "tool_name": "Bash", "tool_input": map[string]any{"command": "git diff"}})
	var out, stderr bytes.Buffer
	Run("guard", bytes.NewReader(body), &out, &stderr, nil)
	if strings.Contains(out.String(), `"deny"`) {
		t.Fatalf("read-only tool denied: %s", out.String())
	}
}
func TestReview_NestedPatchPathsUseProjectRoot(t *testing.T) {
	var event struct {
		Files []string `json:"files"`
	}
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&event)
		w.Write([]byte(`{}`))
	})
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": filepath.Join(root, "nested"), "session_id": "s1", "tool_name": "apply_patch", "tool_input": map[string]any{"command": "*** Begin Patch\n*** Add File: a.go\n+x\n*** End Patch"}, "tool_response": map[string]any{"success": true}})
	var out, stderr bytes.Buffer
	Run("activity", bytes.NewReader(body), &out, &stderr, nil)
	if len(event.Files) != 1 || event.Files[0] != "nested/a.go" {
		t.Fatalf("wrong project-relative files: %#v, stderr=%s", event.Files, stderr.String())
	}
}
func TestReview_NativeSpawnMessageCarriesDeliveryRole(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"workflows":[]}`)) })
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "session_id": "s1", "tool_name": "spawn_agent", "tool_input": map[string]any{"agent_type": "default", "message": "Workflow ID: spec-missing\nDelivery role: delivery-backend-engineer\nImplement the change."}})
	var out, stderr bytes.Buffer
	Run("guard", bytes.NewReader(body), &out, &stderr, nil)
	if !strings.Contains(out.String(), `"deny"`) {
		t.Fatalf("identifiable native delivery delegation bypassed guard: %s", out.String())
	}
}
func TestReview_NestedDocsPatchIsNotProjectDocs(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"workflows":[{"id":"spec-codex","runtime":"codex","session_id":"codex:s1","phase":"design"}]}`))
	})
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": filepath.Join(root, "nested"), "session_id": "s1", "agent_type": "delivery-system-architect", "tool_name": "apply_patch", "tool_input": map[string]any{"command": "*** Begin Patch\n*** Add File: docs/a.md\n+x\n*** End Patch"}})
	var out, stderr bytes.Buffer
	Run("guard", bytes.NewReader(body), &out, &stderr, nil)
	if !strings.Contains(out.String(), `"deny"`) {
		t.Fatalf("nested directory misread as top-level docs: %s", out.String())
	}
}

func TestReview_HookDoesNotUseDashboardLimit(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/integrations/codex/workflows" {
			w.Write([]byte(`{"workflows":[{"id":"spec-old","runtime":"codex","session_id":"codex:s1","phase":"implement"},{"id":"spec-new","runtime":"codex","session_id":"codex:s1","phase":"implement"}]}`))
			return
		}
		w.Write([]byte(`{"workflows":[{"id":"spec-new","runtime":"codex","session_id":"codex:s1","phase":"implement"}]}`))
	})
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "session_id": "s1", "agent_type": "delivery-backend-engineer", "tool_name": "Bash", "tool_input": map[string]any{"command": "touch a.go"}})
	var out, stderr bytes.Buffer
	Run("guard", bytes.NewReader(body), &out, &stderr, nil)
	if !strings.Contains(out.String(), `"deny"`) {
		t.Fatal("ambiguous write allowed", out.String())
	}
}

func TestReview_NestedDocsShellIsNotProjectDocs(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"workflows":[{"id":"spec-codex","runtime":"codex","session_id":"codex:s1","phase":"design"}]}`))
	})
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": filepath.Join(root, "nested"), "session_id": "s1", "agent_type": "delivery-system-architect", "tool_name": "Bash", "tool_input": map[string]any{"command": "printf x > docs/a.md"}})
	var out, stderr bytes.Buffer
	Run("guard", bytes.NewReader(body), &out, &stderr, nil)
	if !strings.Contains(out.String(), `"deny"`) {
		t.Fatal("nested shell directory misread as project docs", out.String())
	}
}

func TestReview_NestedArchitectCanWriteActualProjectDocs(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"workflows":[{"id":"spec-codex","runtime":"codex","session_id":"codex:s1","phase":"design"}]}`))
	})
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": filepath.Join(root, "nested"), "session_id": "s1", "agent_type": "delivery-system-architect", "tool_name": "apply_patch", "tool_input": map[string]any{"command": "*** Begin Patch\n*** Add File: " + filepath.Join(root, "docs/a.md") + "\n+x\n*** End Patch"}})
	var out, stderr bytes.Buffer
	Run("guard", bytes.NewReader(body), &out, &stderr, nil)
	if strings.Contains(out.String(), `"deny"`) {
		t.Fatal("actual project docs rejected before root discovery", out.String())
	}
}
