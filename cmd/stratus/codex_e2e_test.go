package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/MartinNevlaha/stratus-v2/api"
	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/swarm"
	"github.com/MartinNevlaha/stratus-v2/terminal"
	"github.com/MartinNevlaha/stratus-v2/vexor"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type e2eFixture struct {
	root, bin   string
	api         *httptest.Server
	coordinator *orchestration.Coordinator
	database    *db.DB
	env         []string
}

func newCodexE2E(t *testing.T) *e2eFixture {
	t.Helper()
	root := t.TempDir()
	bin := os.Getenv("STRATUS_E2E_BINARY")
	if bin == "" {
		bin = filepath.Join(t.TempDir(), "stratus")
		cmd := exec.Command("go", "build", "-o", bin, ".")
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(out))
		}
	}
	d, e := db.Open(filepath.Join(root, "data/state.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	c := orchestration.NewCoordinator(d)
	static, _ := fs.Sub(staticFiles, "static")
	cfg := config.Default()
	cfg.ProjectRoot = root
	cfg.DataDir = filepath.Join(root, "data")
	srv := api.NewServer(d, c, vexor.New("stratus-no-vexor", "", 1, 1), api.NewHub(), terminal.NewManager(), root, "", "", static, Version, Version, nil, swarm.NewStore(d, root), nil, &cfg)
	var terminals atomic.Int64
	done := make(chan struct{})
	var finish sync.Once
	handler := srv.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__qa/status" {
			json.NewEncoder(w).Encode(map[string]any{"terminal_requests": terminals.Load()})
			return
		}
		if r.URL.Path == "/__qa/complete" {
			finish.Do(func() { close(done) })
			w.WriteHeader(200)
			return
		}
		if r.URL.Path == "/api/terminal/ws" {
			terminals.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	b, _ := json.Marshal(map[string]any{"port": port, "data_dir": cfg.DataDir})
	os.WriteFile(filepath.Join(root, ".stratus.json"), b, 0600)
	env := append(os.Environ(), "STRATUS_PORT="+u.Port(), "STRATUS_DATA_DIR="+cfg.DataDir)
	fixture := &e2eFixture{root, bin, server, c, d, env}
	if manifest := os.Getenv("STRATUS_BROWSER_MANIFEST"); manifest != "" {
		b, _ := json.Marshal(map[string]any{"root": root, "bin": bin, "url": server.URL, "port": port, "data_dir": cfg.DataDir})
		if e := os.WriteFile(manifest, b, 0600); e != nil {
			t.Fatal(e)
		}
		liveFixtureDone = done
	}
	return fixture
}
func (f *e2eFixture) command(t *testing.T, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.bin, args...)
	cmd.Dir = f.root
	cmd.Env = append(f.env, "PATH="+filepath.Dir(f.bin))
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal(args, e, string(out))
	}
	return out
}

type e2eMCP struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func (f *e2eFixture) mcp(t *testing.T, runtime string) *e2eMCP {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	args := []string{"mcp-serve"}
	if runtime != "" {
		args = append(args, "--runtime", runtime)
	}
	cmd := exec.CommandContext(ctx, f.bin, args...)
	cmd.Dir = f.root
	cmd.Env = f.env
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { in.Close(); cmd.Wait(); cancel() })
	m := &e2eMCP{cmd, in, bufio.NewReader(out)}
	m.request(t, "initialize", map[string]any{"protocolVersion": "2025-06-18"})
	return m
}
func (m *e2eMCP) request(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	json.NewEncoder(m.in).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	line, e := m.out.ReadBytes('\n')
	if e != nil {
		t.Fatal(e)
	}
	var response map[string]any
	if e = json.Unmarshal(line, &response); e != nil {
		t.Fatal(e, string(line))
	}
	if response["error"] != nil {
		t.Fatal(response)
	}
	return response["result"].(map[string]any)
}
func (m *e2eMCP) tool(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	r := m.request(t, "tools/call", map[string]any{"name": name, "arguments": args})
	if r["isError"] == true {
		t.Fatal(r)
	}
	content := r["content"].([]any)[0].(map[string]any)["text"].(string)
	var out map[string]any
	if e := json.Unmarshal([]byte(content), &out); e != nil {
		t.Fatal(e, content)
	}
	return out
}
func TestCodexE2E_MCPAndEvents(t *testing.T) {
	f := newCodexE2E(t)
	m := f.mcp(t, "codex")
	if tools := m.request(t, "tools/list", map[string]any{})["tools"].([]any); len(tools) == 0 {
		t.Fatal("no tools")
	}
	w := m.tool(t, "register_workflow", map[string]any{"id": "spec-codex", "type": "spec", "title": "Codex E2E", "session_id": "s1"})
	if w["runtime"] != "codex" || w["session_id"] != "codex:s1" {
		t.Fatal(w)
	}
	dispatch := m.tool(t, "delivery_dispatch", map[string]any{"session_id": "s1"})
	encoded, _ := json.Marshal(dispatch)
	if !bytes.Contains(encoded, []byte("spec-codex")) {
		t.Fatal("owned workflow missing from real MCP dispatch", string(encoded))
	}
	contextBody, _ := json.Marshal(map[string]any{"hook_event_name": "SessionStart", "cwd": f.root, "session_id": "s1"})
	contextCmd := exec.Command(f.bin, "codex-hook", "context")
	contextCmd.Dir, contextCmd.Env, contextCmd.Stdin = f.root, f.env, bytes.NewReader(contextBody)
	contextOutput, err := contextCmd.CombinedOutput()
	if err != nil || !bytes.Contains(contextOutput, []byte("Owned workflow: spec-codex")) || bytes.Contains(contextOutput, []byte("API unavailable")) {
		t.Fatal("real hook context failed", err, string(contextOutput))
	}
	for _, kind := range []string{"SubagentStart", "PostToolUse", "SubagentStop"} {
		body, _ := json.Marshal(map[string]any{"hook_event_name": kind, "cwd": f.root, "session_id": "s1", "agent_id": "a1", "agent_type": "worker", "turn_id": "t1", "tool_name": "Bash", "tool_use_id": "u1", "tool_input": map[string]any{"command": "pwd"}})
		cmd := exec.Command(f.bin, "codex-hook", "activity")
		cmd.Dir = f.root
		cmd.Env = f.env
		cmd.Stdin = bytes.NewReader(body)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(out))
		}
	}
	a, e := f.database.ListWorkflowAgents("spec-codex")
	if e != nil || len(a) != 1 || a[0].ToolCalls != 1 || a[0].Status != "done" {
		t.Fatal(a, e)
	}
}
func TestCodexE2E_MixedRuntimes(t *testing.T) {
	f := newCodexE2E(t)
	for _, id := range []string{"spec-claude", "spec-opencode"} {
		f.coordinator.Start(id, orchestration.WorkflowSpec, orchestration.ComplexitySimple, id)
		f.coordinator.SetSessionID(id, id+"-session")
	}
	m := f.mcp(t, "codex")
	m.tool(t, "register_workflow", map[string]any{"id": "spec-codex", "type": "spec", "title": "Codex", "session_id": "s1"})
	legacy := f.mcp(t, "")
	for _, id := range []string{"spec-claude", "spec-opencode"} {
		v := legacy.tool(t, "delivery_dispatch", map[string]any{"session_id": id + "-session"})
		b, _ := json.Marshal(v)
		if !bytes.Contains(b, []byte(id)) {
			t.Fatal(string(b))
		}
	}
	response := m.request(t, "tools/call", map[string]any{"name": "complete_task", "arguments": map[string]any{"workflow_id": "spec-claude", "index": 0}})
	if response["isError"] != true {
		t.Fatal("cross runtime write accepted", response)
	}
}
func TestCodexE2E_StopDoesNotComplete(t *testing.T) {
	f := newCodexE2E(t)
	m := f.mcp(t, "codex")
	m.tool(t, "register_workflow", map[string]any{"id": "spec-codex", "type": "spec", "title": "Codex", "session_id": "s1"})
	body := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s1","cwd":%q}`, f.root)
	cmd := exec.Command(f.bin, "codex-hook", "activity")
	cmd.Dir = f.root
	cmd.Env = f.env
	cmd.Stdin = strings.NewReader(body)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	w, _ := f.coordinator.Get("spec-codex")
	if w.Phase != orchestration.PhasePlan {
		t.Fatal(w)
	}
}
func TestCodexE2E_DisableLeavesLegacyUsable(t *testing.T) {
	f := newCodexE2E(t)
	f.command(t, "init", "--target", "both", "--force")
	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, dir := range []string{".claude", ".opencode"} {
			filepath.WalkDir(filepath.Join(f.root, dir), func(p string, d fs.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if !d.IsDir() {
					b, _ := os.ReadFile(p)
					out[p] = sha256hex(b)
				}
				return nil
			})
		}
		return out
	}
	before := snapshot()
	f.command(t, "init", "--target", "codex", "--force")
	f.command(t, "refresh", "--target", "codex")
	configPath := filepath.Join(f.root, ".codex/config.toml")
	b, _ := os.ReadFile(configPath)
	b = append(b, []byte("enabled = false\n")...)
	os.WriteFile(configPath, b, 0600)
	os.WriteFile(filepath.Join(f.root, ".codex/hooks.json"), []byte(`{"hooks":{}}`), 0600)
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("legacy assets changed")
	}
	legacy := f.mcp(t, "")
	legacy.tool(t, "register_workflow", map[string]any{"id": "spec-after-disable", "type": "spec", "title": "Legacy", "session_id": "legacy"})
}
func TestCodexE2E_CLITargetMatrix(t *testing.T) {
	f := newCodexE2E(t)
	for _, target := range []string{"default", "both", "claude-code", "opencode", "codex"} {
		root := t.TempDir()
		ff := *f
		ff.root = root
		for _, force := range []bool{false, true} {
			args := []string{"init"}
			if target != "default" {
				args = append(args, "--target", target)
			}
			if force {
				args = append(args, "--force")
			}
			ff.command(t, args...)
		}
		args := []string{"refresh"}
		if target != "default" {
			args = append(args, "--target", target)
		}
		ff.command(t, args...)
		for _, dir := range []string{".claude", ".opencode", ".codex"} {
			_, e := os.Stat(filepath.Join(root, dir))
			want := target == "default" || target == "both" || target == "claude-code" && dir == ".claude" || target == "opencode" && (dir == ".claude" || dir == ".opencode") || target == "codex" && dir == ".codex"
			if dir == ".codex" && target != "codex" {
				want = false
			}
			if (e == nil) != want {
				t.Fatalf("target=%s dir=%s exists=%v want=%v", target, dir, e == nil, want)
			}
		}
	}
}

var liveFixtureDone <-chan struct{}

func TestCodexE2E_LiveBrowser(t *testing.T) {
	if os.Getenv("STRATUS_BROWSER_MANIFEST") == "" {
		t.Skip("manual browser smoke harness")
	}
	f := newCodexE2E(t)
	git := exec.Command("git", "init", "--quiet", f.root)
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	f.command(t, "init", "--target", "both", "--force")
	f.command(t, "init", "--target", "codex")
	f.coordinator.StartOwned("spec-browser-smoke", orchestration.WorkflowSpec, orchestration.ComplexitySimple, "Codex adapter live dashboard", orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: "browser-session"})
	t.Log("isolated dashboard:", f.api.URL)
	select {
	case <-liveFixtureDone:
	case <-time.After(8 * time.Minute):
		t.Fatal("browser smoke timed out")
	}
}

func TestCodexE2E_ReusedWorkersAndCurrentReview(t *testing.T) {
	f := newCodexE2E(t)
	m := f.mcp(t, "codex")
	m.tool(t, "register_workflow", map[string]any{"id": "spec-codex", "type": "spec", "title": "Live event repair", "session_id": "s1"})
	for _, phase := range []orchestration.Phase{orchestration.PhaseImplement, orchestration.PhaseVerify} {
		if _, err := f.coordinator.Transition("spec-codex", phase); err != nil {
			t.Fatal(err)
		}
	}
	for id, role := range map[string]string{"worker": "delivery-backend-engineer", "reviewer": "delivery-code-reviewer"} {
		m.tool(t, "bind_agent_role", map[string]any{"workflow_id": "spec-codex", "session_id": "s1", "agent_id": id, "delivery_role": role})
	}
	hook := func(kind, event, id, turn, report string) string {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"hook_event_name": event, "cwd": f.root, "session_id": "s1", "agent_id": id, "turn_id": turn, "agent_type": "default", "last_assistant_message": report, "tool_name": "Bash", "tool_use_id": event + id + turn, "tool_input": map[string]any{"command": "touch a.go"}})
		c := exec.Command(f.bin, "codex-hook", kind)
		c.Dir = f.root
		c.Env = f.env
		c.Stdin = bytes.NewReader(b)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		return string(out)
	}
	review := func(want string) {
		t.Helper()
		res, err := http.Get(f.api.URL + "/api/workflows/spec-codex/agents")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var got struct {
			Review db.ReviewState `json:"review"`
		}
		if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Review.Status != want {
			t.Fatalf("review %+v want %s", got.Review, want)
		}
	}
	hook("activity", "SubagentStart", "worker", "w1", "")
	hook("activity", "SubagentStart", "reviewer", "r1", "")
	hook("activity", "SubagentStop", "reviewer", "r1", "Verdict: PASS")
	review("PASS")
	if out := hook("guard", "PreToolUse", "worker", "w1", ""); !strings.Contains(out, `"deny"`) {
		t.Fatal("bound default worker bypassed verify write guard", out)
	}
	hook("activity", "PostToolUse", "worker", "w1", "")
	review("outdated")
	hook("activity", "PostToolUse", "reviewer", "r2", "")
	review("running")
	hook("activity", "SubagentStop", "reviewer", "r2", "### Important Finding\nRequires a fix")
	review("unverified")
	hook("activity", "SubagentStop", "reviewer", "r1", "Verdict: PASS")
	review("unverified")
	agents, err := f.database.ListWorkflowAgents("spec-codex")
	if err != nil || len(agents) != 3 {
		t.Fatal(agents, err)
	}
	for _, a := range agents {
		if a.AgentType == "default" {
			t.Fatal("lost bound role", a)
		}
		if a.TurnID == "r1" && (a.Status != "done" || a.Verdict != "PASS") {
			t.Fatal("historical run changed", a)
		}
	}
}
