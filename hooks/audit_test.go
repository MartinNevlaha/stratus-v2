package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readDenialLog(t *testing.T, dir string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, denialLogName))
	if err != nil {
		t.Fatalf("read denial log: %v", err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decode denial line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func TestAuditDenialWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STRATUS_DATA_DIR", dir)
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "bug-x", "session_id": "session-a", "type": "bug", "phase": "review"},
		},
	})

	decision := PhaseGuard(HookEvent{
		ToolName:  "Bash",
		SessionID: "session-a",
		AgentType: "delivery-code-reviewer",
		ToolInput: map[string]any{"command": "rm -rf build"},
	})
	if decision.Continue {
		t.Fatalf("expected the write command to be blocked")
	}

	records := readDenialLog(t, dir)
	if len(records) != 1 {
		t.Fatalf("expected exactly one denial record, got %d", len(records))
	}
	rec := records[0]
	for key, want := range map[string]string{
		"hook":        "phase_guard",
		"tool_name":   "Bash",
		"session_id":  "session-a",
		"agent_type":  "delivery-code-reviewer",
		"workflow_id": "bug-x",
		"phase":       "review",
		"command":     "rm -rf build",
	} {
		if got, _ := rec[key].(string); got != want {
			t.Errorf("denial record %s = %q, want %q", key, got, want)
		}
	}
	if reason, _ := rec["reason"].(string); reason == "" {
		t.Error("denial record is missing the reason text")
	}
	if ts, _ := rec["ts"].(string); ts == "" {
		t.Error("denial record is missing a timestamp")
	}
}

// Every guard must leave the same trace, not just PhaseGuard.
func TestAuditDenialRecordsOtherGuards(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STRATUS_DATA_DIR", dir)
	t.Setenv("CLAUDE_AGENT_ID", "delivery-backend-engineer")
	setDashboardState(t, dashboardState{Workflows: []map[string]any{}})

	decision := BashWriteGuard(HookEvent{
		ToolName:  "Bash",
		SessionID: "session-a",
		ToolInput: map[string]any{"command": "rm -rf build"},
	})
	if decision.Continue {
		t.Fatalf("expected block for a write command without an active workflow")
	}

	records := readDenialLog(t, dir)
	if len(records) != 1 {
		t.Fatalf("expected exactly one denial record, got %d", len(records))
	}
	if hook, _ := records[0]["hook"].(string); hook != "bash_write_guard" {
		t.Errorf("hook = %q, want bash_write_guard", hook)
	}
}
