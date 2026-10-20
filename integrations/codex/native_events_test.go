package codex

import (
	"bytes"
	"encoding/json"
	"github.com/MartinNevlaha/stratus-v2/integrations"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativePatch_StructuredResults(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{`{"output":"Success. Updated the following files:\nM a.go"}`, true},
		{`{"output":"Success.","metadata":{"exit_code":1}}`, false},
		{`{"output":"not a recognized result"}`, false},
	} {
		h := HookEvent{Name: "PostToolUse", SessionID: "s1", ToolName: "apply_patch", ToolInput: map[string]any{"command": "*** Begin Patch\n*** Add File: a.go\n+x\n*** End Patch"}, ToolResponse: json.RawMessage(tc.raw)}
		e, err := Normalize(h)
		if err != nil || (len(e.Files) == 1) != tc.want {
			t.Fatal(tc.raw, e, err)
		}
	}
}
func TestNativeDelegation_GuardAndOpaqueSummary(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"workflows":[]}`)) })
	body, _ := json.Marshal(HookEvent{Name: "PreToolUse", SessionID: "s1", Cwd: root, ToolName: "collaborationspawn_agent", ToolInput: map[string]any{"message": "Workflow ID: spec-missing\nDelivery role: delivery-backend-engineer\nImplement"}})
	var out, stderr bytes.Buffer
	Run("guard", bytes.NewReader(body), &out, &stderr, nil)
	if !strings.Contains(out.String(), `"deny"`) {
		t.Fatal(out.String())
	}
}
func TestNativeDelegation_OpaqueSummaryIsNotStored(t *testing.T) {
	e, err := Normalize(HookEvent{Name: "PostToolUse", SessionID: "s1", ToolName: "collaborationspawn_agent", ToolInput: map[string]any{"message": "gAAAA" + strings.Repeat("x", 300)}})
	if err != nil || strings.Contains(e.Summary, "gAAAA") {
		t.Fatal(e, err)
	}
}
func TestNativeWorker_ContextSuppliesIdentity(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	body, _ := json.Marshal(HookEvent{Name: "SubagentStart", SessionID: "s1", AgentID: "native-worker", TurnID: "t1", Cwd: root})
	var out, stderr bytes.Buffer
	Run("activity", bytes.NewReader(body), &out, &stderr, nil)
	if !strings.Contains(out.String(), "bind_agent_role") || !strings.Contains(out.String(), "native-worker") {
		t.Fatal(out.String())
	}
}
func TestNativeWorktree_PatchUsesSameRepository(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "project")
	wt := filepath.Join(dir, "linked worktree")
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		if o, e := c.CombinedOutput(); e != nil {
			t.Fatal(e, string(o))
		}
	}
	run("init", "--quiet", root)
	run("-C", root, "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "fixture")
	run("-C", root, "worktree", "add", "--quiet", "--detach", wt)
	os.MkdirAll(filepath.Join(wt, "nested"), 0755)
	h := HookEvent{Name: "PostToolUse", SessionID: "s1", Cwd: filepath.Join(wt, "nested"), ToolName: "apply_patch", ToolInput: map[string]any{"command": "*** Begin Patch\n*** Add File: a.go\n+x\n*** End Patch"}, ToolResponse: json.RawMessage(`{"success":true}`)}
	e, err := normalizeForProject(h, root)
	if err != nil || len(e.Files) != 1 || e.Files[0] != filepath.Join(wt, "nested/a.go") {
		t.Fatal(e, err)
	}
	h.Cwd = dir
	e, err = normalizeForProject(h, root)
	if err != nil || len(e.Files) != 0 {
		t.Fatal("foreign path accepted", e, err)
	}
}

func TestNativeWorker_BoundRoleEnforcesWriteGuard(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/agents/role") {
			w.Write([]byte(`{"delivery_role":"delivery-backend-engineer","workflow_id":"spec-codex"}`))
			return
		}
		w.Write([]byte(`{"workflows":[{"id":"spec-codex","runtime":"codex","session_id":"codex:s1","phase":"verify"}]}`))
	})
	body, _ := json.Marshal(HookEvent{Name: "PreToolUse", Cwd: root, SessionID: "s1", AgentID: "native-worker", AgentType: "default", ToolName: "Bash", ToolInput: map[string]any{"command": "touch a.go"}})
	var out, stderr bytes.Buffer
	Run("guard", bytes.NewReader(body), &out, &stderr, nil)
	if !strings.Contains(out.String(), `"deny"`) {
		t.Fatal(out.String())
	}
}

func TestNativeReceipts_PreserveExistingKeys(t *testing.T) {
	e := integrations.Event{Kind: "tool", NativeSessionID: "s1", NativeAgentID: "a1", TurnID: "t1", ToolUseID: "u1"}
	want := digest([]string{"codex", "s1", "tool", "a1", "t1", "u1"})
	if EventKey(e) != want {
		t.Fatal("existing receipt identity changed")
	}
}

func TestNativePatch_ExplicitFailureWins(t *testing.T) {
	for _, raw := range []string{`{"success":true,"metadata":{"exit_code":1}}`, `{"success":true,"isError":true}`} {
		var v any
		json.Unmarshal([]byte(raw), &v)
		if out := toolOutcome(v); out == nil || *out {
			t.Fatal(raw, out)
		}
	}
}

func TestNativeWorker_ReadOnlyDoesNotNeedAPI(t *testing.T) {
	for _, tool := range []string{"Bash", "exec_command"} {
		body, _ := json.Marshal(HookEvent{Name: "PreToolUse", Cwd: t.TempDir(), SessionID: "s1", AgentID: "a1", AgentType: "default", ToolName: tool, ToolInput: map[string]any{"command": "git status", "cmd": "git status"}})
		var out, stderr bytes.Buffer
		Run("guard", bytes.NewReader(body), &out, &stderr, nil)
		if strings.Contains(out.String(), `"deny"`) || stderr.Len() != 0 {
			t.Fatal(out.String(), stderr.String())
		}
	}
}

func TestNativeWorker_ExplicitWorkflowCannotHideBinding(t *testing.T) {
	root := hookRoot(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/agents/role") {
			if r.URL.Query().Get("workflow_id") == "spec-other" {
				w.Write([]byte(`{}`))
				return
			}
			w.Write([]byte(`{"delivery_role":"delivery-backend-engineer","workflow_id":"spec-codex"}`))
			return
		}
		w.Write([]byte(`{"workflows":[{"id":"spec-other","runtime":"codex","session_id":"codex:s1","phase":"implement"}]}`))
	})
	body, _ := json.Marshal(HookEvent{Name: "PreToolUse", Cwd: root, SessionID: "s1", AgentID: "a1", ToolName: "Bash", ToolInput: map[string]any{"command": "touch a.go", "workflow_id": "spec-other"}})
	var out, stderr bytes.Buffer
	Run("guard", bytes.NewReader(body), &out, &stderr, nil)
	if !strings.Contains(out.String(), `"deny"`) {
		t.Fatal(out.String())
	}
}
