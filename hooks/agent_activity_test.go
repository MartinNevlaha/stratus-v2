package hooks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildAgentEvent_SubagentStart(t *testing.T) {
	got, ok := buildAgentEvent(HookEvent{
		HookEventName: "SubagentStart", SessionID: "s1", AgentID: "a1", AgentType: "delivery-backend-engineer",
		TranscriptPath: "/tmp/t.jsonl",
	})
	if !ok {
		t.Fatal("expected event")
	}
	if got["event"] != "start" || got["agent_id"] != "a1" || got["session_id"] != "s1" || got["transcript_path"] != "/tmp/t.jsonl" {
		t.Errorf("got %v", got)
	}
}

func TestBuildAgentEvent_SubagentStopCarriesLastMessage(t *testing.T) {
	got, ok := buildAgentEvent(HookEvent{
		HookEventName: "SubagentStop", SessionID: "s1", AgentID: "a1", LastAssistantMessage: "Verdict: PASS",
	})
	if !ok || got["event"] != "stop" || got["last_assistant_message"] != "Verdict: PASS" {
		t.Errorf("got %v ok=%v", got, ok)
	}
}

func TestBuildAgentEvent_PostToolUseExtractsFilesAndSummary(t *testing.T) {
	got, ok := buildAgentEvent(HookEvent{
		HookEventName: "PostToolUse", SessionID: "s1", AgentID: "a1", ToolName: "Edit",
		ToolInput: map[string]any{"file_path": "/repo/a.go"},
	})
	if !ok || got["event"] != "tool" || got["tool_name"] != "Edit" {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	if !reflect.DeepEqual(got["files"], []string{"/repo/a.go"}) || got["summary"] != "/repo/a.go" {
		t.Errorf("files/summary = %v / %v", got["files"], got["summary"])
	}
}

func TestBuildAgentEvent_BashSummaryTruncated(t *testing.T) {
	got, _ := buildAgentEvent(HookEvent{
		HookEventName: "PostToolUse", SessionID: "s1", AgentID: "a1", ToolName: "Bash",
		ToolInput: map[string]any{"command": strings.Repeat("x", 500)},
	})
	if s, _ := got["summary"].(string); len(s) > 160 {
		t.Errorf("summary len = %d, want <= 160", len(s))
	}
}

func TestBuildAgentEvent_MainAgentToolCallIgnored(t *testing.T) {
	if _, ok := buildAgentEvent(HookEvent{HookEventName: "PostToolUse", SessionID: "s1", ToolName: "Edit"}); ok {
		t.Error("tool call without agent_id must be ignored")
	}
}

func TestBuildAgentEvent_UnknownEventIgnored(t *testing.T) {
	if _, ok := buildAgentEvent(HookEvent{HookEventName: "Stop", SessionID: "s1", AgentID: "a1"}); ok {
		t.Error("unexpected event accepted")
	}
}

func writeAgentJSONL(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "agent.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFinalAgentMessage_PrefersSubagentHandback(t *testing.T) {
	p := writeAgentJSONL(t,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"working"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"SubagentHandback","input":{"message":"## Review Verdict: FAIL\n[must_fix] x — a.go:1"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
	)
	got := finalAgentMessage(p)
	if !strings.Contains(got, "Verdict: FAIL") {
		t.Errorf("got %q", got)
	}
}

func TestFinalAgentMessage_FallsBackToLastAssistantText(t *testing.T) {
	p := writeAgentJSONL(t,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"first"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Verdict: PASS"}]}}`,
	)
	if got := finalAgentMessage(p); got != "Verdict: PASS" {
		t.Errorf("got %q", got)
	}
}

func TestFinalAgentMessage_MissingFileIsEmpty(t *testing.T) {
	if got := finalAgentMessage("/nonexistent/agent.jsonl"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestBuildAgentEvent_SubagentStopReadsTranscriptWhenNoMessage(t *testing.T) {
	p := writeAgentJSONL(t,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Verdict: PASS"}]}}`,
	)
	got, ok := buildAgentEvent(HookEvent{HookEventName: "SubagentStop", SessionID: "s1", AgentID: "a1", AgentTranscriptPath: p})
	if !ok || got["last_assistant_message"] != "Verdict: PASS" {
		t.Errorf("got %v ok=%v", got, ok)
	}
}

func TestBuildAgentEvent_HandbackPseudoToolIgnored(t *testing.T) {
	if _, ok := buildAgentEvent(HookEvent{HookEventName: "PostToolUse", SessionID: "s1", AgentID: "a1", ToolName: "SubagentHandback"}); ok {
		t.Error("SubagentHandback must not count as a tool call")
	}
}

func TestBashWrittenPaths(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want []string
	}{
		{"redirect", `echo three > e.txt`, []string{"e.txt"}},
		{"append", `echo four >> c.txt`, []string{"c.txt"}},
		{"quoted target", `printf 'x' > "dir/my file.txt"`, nil},
		{"tee", `echo hi | tee -a log.txt`, []string{"log.txt"}},
		{"dev null ignored", `grep x f 2>/dev/null`, nil},
		{"fd dup ignored", `go test ./... 2>&1`, nil},
		{"two targets", `echo a > a.txt && echo b >> b.txt`, []string{"a.txt", "b.txt"}},
		{"read only", `cat note.txt`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := bashWrittenPaths(c.cmd)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("bashWrittenPaths(%q) = %v, want %v", c.cmd, got, c.want)
			}
		})
	}
}

func TestBuildAgentEvent_BashRedirectCountsAsFile(t *testing.T) {
	got, ok := buildAgentEvent(HookEvent{
		HookEventName: "PostToolUse", SessionID: "s1", AgentID: "a1", ToolName: "Bash",
		ToolInput: map[string]any{"command": "echo four >> c.txt"},
	})
	if !ok || !reflect.DeepEqual(got["files"], []string{"c.txt"}) {
		t.Errorf("got %v ok=%v", got["files"], ok)
	}
}

func TestBuildAgentEvent_NotificationBecomesWaiting(t *testing.T) {
	got, ok := buildAgentEvent(HookEvent{
		HookEventName: "Notification", SessionID: "s1",
		Message: "Claude needs your permission to use Bash", NotificationType: "permission_prompt",
	})
	if !ok || got["event"] != "waiting" || got["message"] != "Claude needs your permission to use Bash" || got["notification_type"] != "permission_prompt" {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	if _, has := got["agent_id"]; has && got["agent_id"] != "" {
		t.Errorf("waiting from the main agent must not carry an agent id: %v", got)
	}
}

func TestBuildAgentEvent_AskUserQuestionBecomesWaiting(t *testing.T) {
	got, ok := buildAgentEvent(HookEvent{
		HookEventName: "PreToolUse", SessionID: "s1", ToolName: "AskUserQuestion",
		ToolInput: map[string]any{"questions": []any{map[string]any{"question": "Approve the plan?"}}},
	})
	if !ok || got["event"] != "waiting" || got["message"] != "Approve the plan?" {
		t.Fatalf("got %v ok=%v", got, ok)
	}
}

func TestBuildAgentEvent_OtherPreToolUseIgnored(t *testing.T) {
	if _, ok := buildAgentEvent(HookEvent{HookEventName: "PreToolUse", SessionID: "s1", ToolName: "Bash"}); ok {
		t.Error("PreToolUse for other tools must be ignored")
	}
}

func TestBuildAgentEvent_WaitingNeedsSession(t *testing.T) {
	if _, ok := buildAgentEvent(HookEvent{HookEventName: "Notification", Message: "x"}); ok {
		t.Error("no session id must be ignored")
	}
}

func TestBuildAgentEvent_NotificationTypesThatAreNotWaitingIgnored(t *testing.T) {
	for _, typ := range []string{"agent_completed", "auth_success", "quota_auto_resume_fired", "elicitation_complete"} {
		if _, ok := buildAgentEvent(HookEvent{HookEventName: "Notification", SessionID: "s1", Message: "x", NotificationType: typ}); ok {
			t.Errorf("notification_type %q must not raise a waiting event", typ)
		}
	}
	for _, typ := range []string{"", "permission_prompt", "idle_prompt", "elicitation_dialog", "agent_needs_input"} {
		if _, ok := buildAgentEvent(HookEvent{HookEventName: "Notification", SessionID: "s1", Message: "x", NotificationType: typ}); !ok {
			t.Errorf("notification_type %q should raise a waiting event", typ)
		}
	}
}
