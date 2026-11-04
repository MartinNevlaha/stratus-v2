package teamruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teams"
)

// The subprocess fixtures speak the native wire protocols, not Adapter mocks.
// These tests fail if Submit mistakes acceptance/output for completion, loses
// session identity, approves without a decision, or silently changes profiles.
func fixtureAdapter(runtime string) *nativeAdapter {
	return &nativeAdapter{runtime: runtime, binary: os.Args[0], prefix: []string{"-test.run=TestNativeProcessFixture", "--", runtime}}
}

func fixtureSpec(t *testing.T, runtime string) teams.SessionSpec {
	t.Helper()
	model := "gpt-test"
	if runtime == "claude-code" {
		model = "claude-test"
	}
	if runtime == "opencode" {
		model = "fixture/test"
	}
	return teams.SessionSpec{ProjectRoot: t.TempDir(), TeamID: "team", MemberID: "member", Token: "fixture-secret", Instructions: "Stay in the assigned project.", Profile: teams.Profile{Runtime: runtime, Model: model, Effort: "high"}}
}

func TestNativeSessions_UseCurrentMCPAndInheritCapability(t *testing.T) {
	for _, runtime := range []string{"codex", "claude-code", "opencode"} {
		t.Run(runtime, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			spec := fixtureSpec(t, runtime)
			spec.MCPBinary = "/tmp/current-stratus"
			spec.APIBase = "http://127.0.0.1:31415"
			t.Setenv("STRATUS_FIXTURE_MCP", "1")
			t.Setenv("OPENCODE_CONFIG_CONTENT", `{"model":"keep/existing","mcp":{"other":{"type":"local","command":["other-mcp"]}}}`)
			s, err := fixtureAdapter(runtime).Open(ctx, spec, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err = s.Submit(ctx, teams.Input{ID: "second", Text: "second"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeSessions_TwoTurnsAndExplicitApproval(t *testing.T) {
	for _, runtime := range []string{"codex", "claude-code", "opencode"} {
		for _, allow := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", runtime, allow), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				events := make(chan teams.Event, 32)
				s, err := fixtureAdapter(runtime).Open(ctx, fixtureSpec(t, runtime), func(e teams.Event) { events <- e })
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				done := make(chan teams.Result, 1)
				errs := make(chan error, 1)
				go func() { r, e := s.Submit(ctx, teams.Input{ID: "task-1", Text: "permission"}); done <- r; errs <- e }()
				var approval teams.Event
				for approval.Kind != "approval" {
					select {
					case approval = <-events:
					case <-ctx.Done():
						t.Fatal("no approval request")
					}
				}
				select {
				case <-done:
					t.Fatal("accepted/output was mistaken for completion")
				default:
				}
				if err := s.Approve(ctx, "not-pending", true); err == nil {
					t.Fatal("unknown approval accepted")
				}
				if err := s.Approve(ctx, approval.ID, allow); err != nil {
					t.Fatal(err)
				}
				var first teams.Result
				select {
				case first = <-done:
				case <-ctx.Done():
					t.Fatal("approval did not release the turn")
				}
				if err := <-errs; err != nil {
					t.Fatal(err)
				}
				want := "denied"
				if allow {
					want = "approved"
				}
				native := "native-fixture"
				if runtime == "opencode" {
					native = "ses_fixture"
				}
				if first.Text != want || first.NativeID != native {
					t.Fatalf("first result: %+v", first)
				}
				if first.InputTokens == nil || *first.InputTokens != 11 || first.OutputTokens == nil || *first.OutputTokens != 3 {
					t.Fatalf("usage: %+v", first)
				}
				second, err := s.Submit(ctx, teams.Input{ID: "task-2", Text: "second"})
				if err != nil || second.Text != "second turn" || second.NativeID != first.NativeID {
					t.Fatalf("second turn: %+v, %v", second, err)
				}
				if second.Model != fixtureSpec(t, runtime).Profile.Model {
					t.Fatalf("observed model: %q", second.Model)
				}
				if err := s.Approve(ctx, approval.ID, true); err == nil {
					t.Fatal("consumed approval replay accepted")
				}
			})
		}
	}
}

func TestNativeSessions_RejectUnknownProfileBeforeSubmission(t *testing.T) {
	for _, runtime := range []string{"codex", "claude-code", "opencode"} {
		t.Run(runtime, func(t *testing.T) {
			for _, field := range []string{"model", "effort"} {
				spec := fixtureSpec(t, runtime)
				if field == "model" {
					spec.Profile.Model = "missing"
				} else {
					spec.Profile.Effort = "impossible"
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				s, err := fixtureAdapter(runtime).Open(ctx, spec, nil)
				cancel()
				if s != nil {
					s.Close()
				}
				if err == nil {
					t.Fatalf("unsupported %s accepted", field)
				}
			}
		})
	}
}

func TestClaudeRequiresAuthenticationBeforeOpening(t *testing.T) {
	t.Setenv("STRATUS_FIXTURE_NO_AUTH", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, err := fixtureAdapter("claude-code").Open(ctx, fixtureSpec(t, "claude-code"), nil)
	if s != nil {
		s.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("missing authentication accepted: %v", err)
	}
}

func TestClaudeAcceptsOnlyItsAdvertisedAliasResolution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	spec := fixtureSpec(t, "claude-code")
	spec.Profile.Model = "native-alias"
	s, err := fixtureAdapter("claude-code").Open(ctx, spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Submit(ctx, teams.Input{ID: "alias", Text: "second"})
	if err != nil || r.Model != "claude-test" {
		t.Fatalf("native alias resolution: %+v %v", r, err)
	}
}

func TestNativeSessions_OpenContextDoesNotOwnLifetime(t *testing.T) {
	for _, runtime := range []string{"codex", "claude-code", "opencode"} {
		t.Run(runtime, func(t *testing.T) {
			openCtx, cancel := context.WithCancel(context.Background())
			s, err := fixtureAdapter(runtime).Open(openCtx, fixtureSpec(t, runtime), nil)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			if _, err := s.Submit(ctx, teams.Input{ID: "after-open", Text: "second"}); err != nil {
				t.Fatalf("initialization cancellation killed the owned session: %v", err)
			}
		})
	}
}

func TestNativeSessions_InterruptAndTransportFailure(t *testing.T) {
	for _, runtime := range []string{"codex", "claude-code", "opencode"} {
		for _, mode := range []string{"wait", "crash", "wrong-model"} {
			t.Run(runtime+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				defer cancel()
				events := make(chan teams.Event, 32)
				s, err := fixtureAdapter(runtime).Open(ctx, fixtureSpec(t, runtime), func(e teams.Event) { events <- e })
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				done := make(chan error, 1)
				go func() { _, err := s.Submit(ctx, teams.Input{ID: "task", Text: mode}); done <- err }()
				if mode == "wait" {
					for {
						e := <-events
						if e.Kind == "status" && e.Text == "accepted" {
							break
						}
					}
					if _, err := s.Submit(ctx, teams.Input{ID: "collision", Text: "second"}); err == nil {
						t.Fatal("concurrent turn accepted")
					}
					if err := s.Interrupt(ctx); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("failure returned success")
					}
				case <-ctx.Done():
					t.Fatal("turn never resolved")
				}
			})
		}
	}
}

func TestNativeSessions_CancelAndCloseResolvePending(t *testing.T) {
	for _, runtime := range []string{"codex", "claude-code", "opencode"} {
		t.Run(runtime, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			s, err := fixtureAdapter(runtime).Open(ctx, fixtureSpec(t, runtime), nil)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := s.Submit(ctx, teams.Input{ID: "task", Text: "wait"}); done <- err }()
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("closed session succeeded")
				}
			case <-ctx.Done():
				t.Fatal("close left pending submit")
			}
			if _, err := s.Submit(ctx, teams.Input{ID: "late", Text: "second"}); err == nil {
				t.Fatal("closed session accepted input")
			}
		})
	}
}

func TestNativeProcessFixture(t *testing.T) {
	idx := -1
	for i, a := range os.Args {
		if a == "--" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	runtime := os.Args[idx+1]
	if runtime == "claude-code" && idx+3 < len(os.Args) && os.Args[idx+2] == "auth" && os.Args[idx+3] == "status" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"loggedIn": os.Getenv("STRATUS_FIXTURE_NO_AUTH") != "1"})
		os.Exit(0)
	}
	if runtime == "opencode" {
		runOpenCodeFixture()
		os.Exit(0)
	}
	if os.Getenv("STRATUS_FIXTURE_MCP") == "1" && os.Getenv("STRATUS_TEAM_TOKEN") != "fixture-secret" {
		os.Exit(12)
	}
	if os.Getenv("STRATUS_FIXTURE_MCP") == "1" && runtime == "claude-code" {
		var config map[string]any
		for i, a := range os.Args {
			if a == "--mcp-config" && i+1 < len(os.Args) {
				_ = json.Unmarshal([]byte(os.Args[i+1]), &config)
			}
			if a == "--strict-mcp-config" {
				os.Exit(34)
			}
		}
		if str(obj(obj(config, "mcpServers"), "stratus"), "command") != "/tmp/current-stratus" || os.Getenv("STRATUS_TEAM_API_BASE") != "http://127.0.0.1:31415" {
			os.Exit(35)
		}
	}
	enc := json.NewEncoder(os.Stdout)
	send := func(v any) {
		if enc.Encode(v) != nil {
			os.Exit(13)
		}
	}
	model := "gpt-test"
	if runtime == "claude-code" {
		model = "claude-test"
	}
	turn := 0
	turnID := ""
	inputID := ""
	finish := func(text, status string) {
		if runtime == "codex" {
			send(map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "native-fixture", "turnId": turnID, "tokenUsage": map[string]any{"last": map[string]any{"inputTokens": 11, "outputTokens": 3}, "total": map[string]any{"inputTokens": 11 * turn, "outputTokens": 3 * turn}}}})
			send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "native-fixture", "turn": map[string]any{"id": turnID, "status": status, "items": []any{map[string]any{"type": "agentMessage", "id": "item", "text": text}}}}})
		} else {
			send(map[string]any{"type": "assistant", "session_id": "native-fixture", "message": map[string]any{"model": model, "content": []any{map[string]any{"type": "text", "text": text}}}})
			send(map[string]any{"type": "result", "subtype": map[bool]string{true: "success", false: "error_during_execution"}[status == "completed"], "is_error": status != "completed", "session_id": "native-fixture", "result": text, "user_message_uuid": inputID, "usage": map[string]any{"input_tokens": 11, "output_tokens": 3}})
		}
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var m map[string]any
		if json.Unmarshal(scan.Bytes(), &m) != nil {
			os.Exit(14)
		}
		if runtime == "codex" {
			method, _ := m["method"].(string)
			p, _ := m["params"].(map[string]any)
			reply := func(r any) { send(map[string]any{"id": m["id"], "result": r}) }
			switch method {
			case "initialize":
				reply(map[string]any{"userAgent": "fixture"})
			case "initialized":
			case "account/read":
				reply(map[string]any{"account": map[string]any{"type": "chatgpt"}, "requiresOpenaiAuth": true})
			case "model/list":
				reply(map[string]any{"data": []any{map[string]any{"id": "gpt-test", "model": "gpt-test", "supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "high"}}}}})
			case "thread/start", "thread/resume":
				if os.Getenv("STRATUS_FIXTURE_MCP") == "1" {
					config := obj(p, "config")
					if config["mcp_servers.stratus.command"] != "/tmp/current-stratus" || os.Getenv("STRATUS_TEAM_API_BASE") != "http://127.0.0.1:31415" {
						os.Exit(36)
					}
				}
				if p["model"] != "gpt-test" || p["developerInstructions"] != "Stay in the assigned project." {
					os.Exit(15)
				}
				reply(map[string]any{"thread": map[string]any{"id": "native-fixture"}, "model": "gpt-test", "reasoningEffort": "high"})
			case "turn/start":
				if p["threadId"] != "native-fixture" || p["model"] != "gpt-test" || p["effort"] != "high" {
					os.Exit(16)
				}
				turn++
				turnID = fmt.Sprintf("turn-%d", turn)
				text := p["input"].([]any)[0].(map[string]any)["text"].(string)
				reply(map[string]any{"turn": map[string]any{"id": turnID, "status": "inProgress", "items": []any{}}})
				if text == "crash" {
					os.Exit(17)
				}
				if text == "wait" {
					continue
				}
				if text == "wrong-model" {
					send(map[string]any{"method": "model/rerouted", "params": map[string]any{"threadId": "native-fixture", "turnId": turnID, "fromModel": "gpt-test", "toModel": "other", "reason": "fixture"}})
					finish("wrong", "completed")
					continue
				}
				if text == "permission" {
					send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "other-thread", "turn": map[string]any{"id": "stale", "status": "completed", "items": []any{}}}})
					send(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "native-fixture", "turnId": turnID, "delta": "working"}})
					send(map[string]any{"id": 901, "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": "native-fixture", "turnId": turnID, "itemId": "cmd", "command": "touch output", "reason": "write"}})
				} else {
					finish("second turn", "completed")
				}
			case "turn/interrupt":
				reply(map[string]any{})
				finish("interrupted", "interrupted")
			case "":
				decision := m["result"].(map[string]any)["decision"]
				if decision == "accept" {
					finish("approved", "completed")
				} else if decision == "decline" {
					finish("denied", "completed")
				} else {
					os.Exit(18)
				}
			default:
				os.Exit(19)
			}
		} else {
			switch m["type"] {
			case "control_request":
				r := m["request"].(map[string]any)
				data := map[string]any{}
				if r["subtype"] == "initialize" {
					data["models"] = []any{map[string]any{"value": "claude-test", "resolvedModel": "claude-test", "supportsEffort": true, "supportedEffortLevels": []string{"high"}}, map[string]any{"value": "native-alias", "resolvedModel": "claude-test", "supportedEffortLevels": []string{"high"}}}
				}
				send(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": m["request_id"], "response": data}})
				if r["subtype"] == "interrupt" {
					finish("interrupted", "interrupted")
				}
			case "user":
				inputID, _ = m["uuid"].(string)
				text := m["message"].(map[string]any)["content"].(string)
				if turn > 0 && m["session_id"] != "native-fixture" {
					os.Exit(20)
				}
				turn++
				send(map[string]any{"type": "system", "subtype": "init", "session_id": "native-fixture", "model": "claude-test"})
				if text == "crash" {
					os.Exit(21)
				}
				if text == "wait" {
					continue
				}
				if text == "wrong-model" {
					model = "other"
					finish("wrong", "completed")
					continue
				}
				if text == "permission" {
					send(map[string]any{"type": "control_request", "request_id": "approval-1", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Write", "input": map[string]any{"file_path": "output", "content": "x"}}})
				} else {
					finish("second turn", "completed")
				}
			case "control_response":
				r := m["response"].(map[string]any)["response"].(map[string]any)
				if r["behavior"] == "allow" {
					if r["updatedInput"].(map[string]any)["content"] != "x" {
						os.Exit(22)
					}
					finish("approved", "completed")
				} else {
					finish("denied", "completed")
				}
			default:
				if !strings.HasPrefix(fmt.Sprint(m["type"]), "keep") {
					os.Exit(23)
				}
			}
		}
	}
	os.Exit(0)
}
