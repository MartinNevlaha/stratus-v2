package codex

import (
	"slices"
	"strings"
	"testing"
)

func TestNormalize_Lifecycle(t *testing.T) {
	for raw, want := range map[string]string{"SessionStart": "session_start", "SubagentStart": "agent_start", "SubagentStop": "agent_stop", "PermissionRequest": "waiting", "Stop": "turn_stop", "Interrupt": "interrupt", "SessionEnd": "session_end"} {
		h, e := Decode(strings.NewReader(`{"hook_event_name":"` + raw + `","session_id":"s1","agent_id":"a1","unknown":true}`))
		if e != nil {
			t.Fatal(e)
		}
		v, e := Normalize(h)
		if e != nil || v.Kind != want {
			t.Fatal(v, e)
		}
	}
}
func TestNormalize_RequiresIdentity(t *testing.T) {
	for _, raw := range []string{`{`, `{"hook_event_name":"SessionStart"}`, `{"hook_event_name":"SubagentStart","session_id":"s1"}`, `{"hook_event_name":"PostToolUse","session_id":"s1"}`} {
		h, e := Decode(strings.NewReader(raw))
		if e == nil {
			_, e = Normalize(h)
		}
		if e == nil {
			t.Fatal("accepted", raw)
		}
	}
}
func TestNormalize_NoInferredAgent(t *testing.T) {
	h, _ := Decode(strings.NewReader(`{"hook_event_name":"PostToolUse","session_id":"s1","tool_name":"Bash","tool_input":{"command":"pwd"}}`))
	v, e := Normalize(h)
	if e != nil || v.MainAgent || v.NativeAgentID != "" {
		t.Fatal(v, e)
	}
}
func TestEventKey_Stable(t *testing.T) {
	h, _ := Decode(strings.NewReader(`{"hook_event_name":"PostToolUse","session_id":"s1","turn_id":"t1","tool_use_id":"u1","tool_name":"Bash"}`))
	v, _ := Normalize(h)
	a := EventKey(v)
	if a == "" || EventKey(v) != a {
		t.Fatal(a)
	}
	v.ToolUseID = "u2"
	if EventKey(v) == a {
		t.Fatal("collision")
	}
	v.ToolUseID = ""
	if EventKey(v) != "" {
		t.Fatal("invented stable identity")
	}
}
func TestPatchPaths_MovesAndMultipleFiles(t *testing.T) {
	paths, e := PatchPaths("*** Begin Patch\n*** Add File: src/a.go\n+x\n*** Update File: old.go\n*** Move to: new.go\n@@\n-x\n+y\n*** End Patch")
	if e != nil || !slices.Equal(paths, []string{"src/a.go", "old.go", "new.go"}) {
		t.Fatal(paths, e)
	}
	for _, s := range []string{"", "*** Begin Patch\n*** Add File: ../outside\n*** End Patch"} {
		if _, e := PatchPaths(s); e == nil {
			t.Fatal("accepted", s)
		}
	}
}
func TestNormalize_EditOutcome(t *testing.T) {
	for _, response := range []string{`{"success":false}`, `{"unrecognized":true}`} {
		h, _ := Decode(strings.NewReader(`{"hook_event_name":"PostToolUse","session_id":"s1","tool_name":"apply_patch","tool_input":{"command":"*** Begin Patch\n*** Add File: a.go\n+x\n*** End Patch"},"tool_response":` + response + `}`))
		v, e := Normalize(h)
		if e != nil || len(v.Files) != 0 {
			t.Fatal(v, e)
		}
	}
}
