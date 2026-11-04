package attachments

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// These subprocesses exercise the public MCP boundary. They deliberately reject
// wrong caller metadata, target IDs, model overrides, and handshake ordering.
func TestMain(m *testing.M) {
	if mode := os.Getenv("STRATUS_CODEX_FIXTURE"); mode != "" {
		runCodexFixture(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func codexFixtureConfig(t *testing.T, mode string) CodexConfig {
	t.Helper()
	server := filepath.Join(t.TempDir(), "server.mjs")
	if err := os.WriteFile(server, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return CodexConfig{
		SourceThreadID: "native-source", Launcher: os.Args[0], ServerPath: server,
		Env: []string{"STRATUS_CODEX_FIXTURE=" + mode, "CODEX_APP_TOOLS_PIPE_PATH=fixture-host-pipe", "CODEX_THREAD_ID=not-the-source"},
	}
}

func openCodexFixture(t *testing.T, mode string) *CodexClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := OpenCodex(ctx, codexFixtureConfig(t, mode))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestCodexPreservesCallerTargetAndNativeSnapshot(t *testing.T) {
	c := openCodexFixture(t, "normal")
	s, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{NativeID: "native-source", ProjectRoot: "/fixture/project", State: "active", ActiveFlags: []string{"waitingForApproval"}, TurnID: "native-turn", TurnStatus: "inProgress", Origin: "user", PreviousTurns: []TurnReceipt{{ID: "older-turn", Status: "completed"}}}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("snapshot = %#v, want %#v", s, want)
	}
	encoded, _ := json.Marshal(s)
	if strings.Contains(string(encoded), "PRIVATE_HISTORY") || strings.Contains(string(encoded), "PRIVATE_PREVIEW") {
		t.Fatalf("snapshot retained private conversation content: %s", encoded)
	}
	if err := c.Send(context.Background(), "scoped team input"); err != nil {
		t.Fatal(err)
	}
}

func TestCodexOriginUsesNewestNativeEvidence(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{{"team", "team"}, {"unknown", "unknown"}, {"mixed", "user"}} {
		t.Run(tc.mode, func(t *testing.T) {
			s, err := openCodexFixture(t, tc.mode).Snapshot(context.Background())
			if err != nil || s.Origin != tc.want || s.TurnID != "native-turn" {
				t.Fatalf("snapshot = %#v, error = %v; want newest origin %s", s, err, tc.want)
			}
		})
	}
}

func TestCodexSnapshotKeepsBoundedPriorTurnReceiptsWithoutContent(t *testing.T) {
	snapshot, err := openCodexFixture(t, "normal").Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(snapshot)
	var metadata struct {
		Previous []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"previous_turns"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if len(metadata.Previous) != 1 || metadata.Previous[0].ID != "older-turn" || metadata.Previous[0].Status != "completed" {
		t.Fatalf("prior native terminal receipt missing: %s", data)
	}
	if strings.Contains(string(data), "items") || strings.Contains(string(data), "PRIVATE") {
		t.Fatalf("prior receipt retained conversation content: %s", data)
	}
}

func TestCodexRejectsNativeIdentityMismatch(t *testing.T) {
	for _, op := range []string{"snapshot", "send"} {
		t.Run(op, func(t *testing.T) {
			c := openCodexFixture(t, "wrong-"+op)
			var err error
			if op == "snapshot" {
				_, err = c.Snapshot(context.Background())
			} else {
				err = c.Send(context.Background(), "scoped team input")
			}
			if err == nil || !strings.Contains(err.Error(), "identity") {
				t.Fatalf("native identity mismatch was not rejected: %v", err)
			}
		})
	}
}

func TestCodexSurfacesNativeToolAndProtocolErrors(t *testing.T) {
	for _, mode := range []string{"tool-error", "rpc-error", "invalid-json", "missing-result"} {
		t.Run(mode, func(t *testing.T) {
			c := openCodexFixture(t, mode)
			if err := c.Send(context.Background(), "scoped team input"); err == nil {
				t.Fatal("rejected or malformed native response reported acceptance")
			} else if mode == "tool-error" && !strings.Contains(err.Error(), "native permission pending") {
				t.Fatalf("native tool error lost: %v", err)
			}
		})
	}
}

func TestCodexDoesNotRetryAmbiguousSend(t *testing.T) {
	config := codexFixtureConfig(t, "disconnect-send")
	logPath := filepath.Join(t.TempDir(), "sends")
	config.Env = append(config.Env, "STRATUS_CODEX_FIXTURE_LOG="+logPath)
	c, err := OpenCodex(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Send(context.Background(), "scoped team input"); err == nil {
		t.Fatal("connection loss after native receipt reported acceptance")
	}
	if err := c.Send(context.Background(), "scoped team input"); err == nil {
		t.Fatal("closed connector accepted another send")
	}
	data, err := os.ReadFile(logPath)
	if err != nil || string(data) != "send\n" {
		t.Fatalf("send attempts = %q, error = %v", data, err)
	}
}

func TestCodexKeepsReceiptReceivedBeforeTransportFailure(t *testing.T) {
	// Once the complete native receipt has arrived, a following broken frame
	// must not erase that evidence of acceptance.
	for i := 0; i < 5; i++ {
		c := openCodexFixture(t, "accept-then-fail")
		if err := c.Send(context.Background(), "scoped team input"); err != nil {
			t.Fatalf("received acceptance lost to subsequent helper exit: %v", err)
		}
		_ = c.Close()
	}
}

func TestCodexCloseReleasesBlockedAndQueuedRequests(t *testing.T) {
	c := openCodexFixture(t, "block-send")
	results := make(chan error, 6)
	for i := 0; i < cap(results); i++ {
		go func() { results <- c.Send(context.Background(), "scoped team input") }()
	}
	time.Sleep(30 * time.Millisecond)
	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("close deadlocked on a blocked native request")
	}
	for i := 0; i < cap(results); i++ {
		select {
		case err := <-results:
			if err == nil {
				t.Fatal("blocked request reported acceptance after close")
			}
		case <-time.After(time.Second):
			t.Fatal("close did not resolve a waiter")
		}
	}
	if err := c.Close(); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
}

func TestCodexRequestDeadlineAndCanceledCall(t *testing.T) {
	c := openCodexFixture(t, "block-send")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if err := c.Send(ctx, "scoped team input"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked send did not respect its deadline: %v", err)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if err := c.Send(canceled, "scoped team input"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled call = %v", err)
	}
}

func TestCodexDeadlineStopsBlockedHelperWrite(t *testing.T) {
	c := openCodexFixture(t, "block-stdin")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- c.Send(ctx, strings.Repeat("x", 1<<20)) }()
	select {
	case err := <-finished:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked helper write did not respect deadline: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native helper stopped reading and send blocked past its deadline")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCodexConcurrentCallsStayCorrelated(t *testing.T) {
	c := openCodexFixture(t, "normal")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_, err := c.Snapshot(context.Background())
				if err != nil {
					t.Error(err)
				}
			} else if err := c.Send(context.Background(), "scoped team input"); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
}

func TestCodexRequiresHostPrerequisitesAndCapabilities(t *testing.T) {
	for _, mode := range []string{"no-source", "no-pipe", "missing-launcher", "missing-server", "nonexecutable-launcher", "missing-tools"} {
		t.Run(mode, func(t *testing.T) {
			config := codexFixtureConfig(t, mode)
			switch mode {
			case "no-source":
				config.SourceThreadID = ""
			case "no-pipe":
				config.Env = append(config.Env, "CODEX_APP_TOOLS_PIPE_PATH=")
			case "missing-launcher":
				config.Launcher = filepath.Join(t.TempDir(), "missing")
			case "missing-server":
				config.ServerPath = filepath.Join(t.TempDir(), "missing")
			case "nonexecutable-launcher":
				config.Launcher = config.ServerPath
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			c, err := OpenCodex(ctx, config)
			if c != nil {
				c.Close()
			}
			if err == nil {
				t.Fatal("missing native prerequisite accepted")
			}
		})
	}
}

func TestCodexDiscoversOnlyHostSuppliedResources(t *testing.T) {
	config := codexFixtureConfig(t, "normal")
	root := t.TempDir()
	base := filepath.Join(root, "plugins", "openai-bundled", "plugins", "codex-app-tools")
	if err := os.MkdirAll(filepath.Join(base, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(config.Launcher, filepath.Join(base, "scripts", "launch_codex_app_tools_mcp")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "server.mjs"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	config.Launcher, config.ServerPath = "", ""
	config.Env = append(config.Env, "CODEX_ELECTRON_RESOURCES_PATH="+root)
	c, err := OpenCodex(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func runCodexFixture(mode string) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	initialized, notified, listed := false, false, false
	for scanner.Scan() {
		var req struct {
			ID     uint64                     `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(10)
		}
		reply := func(result any) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		}
		reject := func(reason string) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32602, "message": reason}})
		}
		switch req.Method {
		case "initialize":
			if initialized || string(req.Params["protocolVersion"]) != `"2025-06-18"` {
				reject("invalid initialization")
				continue
			}
			initialized = true
			reply(map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "codex-fixture", "version": "1"}})
		case "notifications/initialized":
			if !initialized {
				os.Exit(11)
			}
			notified = true
		case "tools/list":
			if !notified {
				reject("missing initialized notification")
				continue
			}
			listed = true
			if mode == "missing-tools" {
				reply(map[string]any{"tools": []any{map[string]any{"name": "read_thread"}}})
			} else {
				reply(map[string]any{"tools": []any{map[string]any{"name": "read_thread"}, map[string]any{"name": "send_message_to_thread"}}})
			}
			if mode == "block-stdin" {
				for {
					time.Sleep(time.Hour)
				}
			}
		case "tools/call":
			var meta, args map[string]any
			var name string
			_ = json.Unmarshal(req.Params["_meta"], &meta)
			_ = json.Unmarshal(req.Params["arguments"], &args)
			_ = json.Unmarshal(req.Params["name"], &name)
			if !listed || len(meta) != 1 || meta["openai/threadId"] != "native-source" || args["threadId"] != "native-source" || os.Getenv("CODEX_APP_TOOLS_PIPE_PATH") != "fixture-host-pipe" {
				reject("invalid host-bound caller or target metadata")
				continue
			}
			var payload any
			switch name {
			case "read_thread":
				if len(args) != 4 || args["turnLimit"] != float64(3) || args["includeOutputs"] != false || args["maxOutputCharsPerItem"] != float64(1000) {
					reject("unbounded or incorrect snapshot arguments")
					continue
				}
				nativeID := "native-source"
				if mode == "wrong-snapshot" {
					nativeID = "other-thread"
				}
				items := []any{map[string]any{"type": "userMessage", "text": "PRIVATE_HISTORY"}}
				if mode == "team" || mode == "mixed" {
					items = []any{map[string]any{"type": "functionCallOutput", "namespace": "codex_app", "name": "send_message_to_thread", "output": "PRIVATE_HISTORY"}}
				}
				if mode == "mixed" {
					items = append(items, map[string]any{"type": "userMessage", "text": "PRIVATE_HISTORY"})
				}
				if mode == "unknown" {
					items = []any{map[string]any{"type": "agentMessage", "text": "PRIVATE_HISTORY"}}
				}
				payload = map[string]any{"schemaVersion": 1, "thread": map[string]any{"id": nativeID, "kind": "codex", "hostId": "local", "cwd": "/fixture/project", "preview": "PRIVATE_PREVIEW", "status": map[string]any{"type": "active", "activeFlags": []string{"waitingForApproval"}}}, "turns": []any{map[string]any{"id": "native-turn", "status": "inProgress", "items": items}, map[string]any{"id": "older-turn", "status": "completed", "items": []any{map[string]any{"type": "userMessage"}}}}}
			case "send_message_to_thread":
				if len(args) != 2 || args["prompt"] != "scoped team input" {
					reject("unexpected send arguments or profile override")
					continue
				}
				if logPath := os.Getenv("STRATUS_CODEX_FIXTURE_LOG"); logPath != "" {
					f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
					if err != nil {
						os.Exit(12)
					}
					_, _ = f.WriteString("send\n")
					_ = f.Close()
				}
				if mode == "disconnect-send" {
					return
				}
				if mode == "block-send" {
					for {
						time.Sleep(time.Hour)
					}
				}
				if mode == "tool-error" {
					reply(map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "native permission pending"}}})
					continue
				}
				if mode == "rpc-error" {
					reject("native request rejected")
					continue
				}
				if mode == "invalid-json" {
					fmt.Println("not json")
					continue
				}
				if mode == "missing-result" {
					reply(map[string]any{"content": []any{}})
					continue
				}
				nativeID := "native-source"
				if mode == "wrong-send" {
					nativeID = "other-thread"
				}
				payload = map[string]any{"threadId": nativeID}
			default:
				reject("unsupported native tool")
				continue
			}
			data, _ := json.Marshal(payload)
			result := map[string]any{"content": []any{map[string]any{"type": "text", "text": string(data)}}, "isError": false}
			if mode == "accept-then-fail" && name == "send_message_to_thread" {
				frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
				fmt.Print(string(frame) + "\nnot json\n")
				return
			}
			reply(result)
		default:
			reject("unexpected MCP method")
		}
	}
}
