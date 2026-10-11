package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const transcriptFixture = `{"type":"user","message":{"role":"user","content":"do it"}}
{"type":"assistant","message":{"role":"assistant","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":100,"cache_creation_input_tokens":20},"content":[{"type":"text","text":"Reading the file"},{"type":"tool_use","name":"Read","input":{"file_path":"/repo/a.go"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","message":{"role":"assistant","usage":{"input_tokens":7,"output_tokens":3},"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}
not json at all
`

func writeFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "agent.jsonl")
	if err := os.WriteFile(p, []byte(transcriptFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadAgentTranscript_StepsAndUsage(t *testing.T) {
	steps, usage, err := readAgentTranscript(writeFixture(t), 50)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("steps = %+v", steps)
	}
	if steps[0].Kind != "text" || steps[0].Text != "Reading the file" {
		t.Errorf("step0 = %+v", steps[0])
	}
	if steps[1].Kind != "tool" || steps[1].Name != "Read" || steps[1].Summary != "/repo/a.go" {
		t.Errorf("step1 = %+v", steps[1])
	}
	if steps[2].Name != "Bash" || steps[2].Summary != "go test ./..." {
		t.Errorf("step2 = %+v", steps[2])
	}
	if usage.InputTokens != 17 || usage.OutputTokens != 8 || usage.CacheReadTokens != 100 || usage.CacheCreationTokens != 20 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestReadAgentTranscript_LimitKeepsLastSteps(t *testing.T) {
	steps, _, _ := readAgentTranscript(writeFixture(t), 1)
	if len(steps) != 1 || steps[0].Name != "Bash" {
		t.Fatalf("steps = %+v", steps)
	}
}

func TestSubagentTranscriptPath_DerivesFromSessionTranscript(t *testing.T) {
	got := subagentTranscriptPath("/home/u/.claude/projects/p/abc.jsonl", "ae8b")
	if got != "/home/u/.claude/projects/p/abc/subagents/agent-ae8b.jsonl" {
		t.Errorf("got %q", got)
	}
	if subagentTranscriptPath("/x/abc.jsonl", "../evil") != "" {
		t.Error("agent id with path separators must be rejected")
	}
	if subagentTranscriptPath("", "a1") != "" {
		t.Error("empty session transcript must yield empty path")
	}
}

func TestHandleAgentTranscript_ReturnsStepsForAgentWithTranscript(t *testing.T) {
	s, id := agentTestServer(t)
	dir := t.TempDir()
	session := filepath.Join(dir, "sess.jsonl")
	sub := filepath.Join(dir, "sess", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "agent-a1.jsonl"), []byte(transcriptFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"a1","agent_type":"x","transcript_path":"`+session+`"}`)

	req := httptest.NewRequest(http.MethodGet, "/api/workflows/"+id+"/agents/a1/transcript", nil)
	req.SetPathValue("id", id)
	req.SetPathValue("agent", "a1")
	w := httptest.NewRecorder()
	s.handleAgentTranscript(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Steps []map[string]any `json:"steps"`
		Usage map[string]any   `json:"usage"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Steps) != 3 || resp.Usage["input_tokens"].(float64) != 17 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestHandleAgentTranscript_UnknownAgentIs404(t *testing.T) {
	s, id := agentTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/x", strings.NewReader(""))
	req.SetPathValue("id", id)
	req.SetPathValue("agent", "nope")
	w := httptest.NewRecorder()
	s.handleAgentTranscript(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code = %d", w.Code)
	}
}

func TestHandleAgentStop_StoresTokenUsageFromTranscript(t *testing.T) {
	s, id := agentTestServer(t)
	p := writeFixture(t)
	postAgentEvent(t, s, `{"event":"start","session_id":"sess-1","agent_id":"a1","agent_type":"x"}`)
	b, _ := json.Marshal(map[string]string{"event": "stop", "session_id": "sess-1", "agent_id": "a1", "agent_transcript_path": p})
	if w := postAgentEvent(t, s, string(b)); w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	a, _ := s.db.ListWorkflowAgents(id)
	if a[0].TokensIn != 17+100+20 || a[0].TokensOut != 8 {
		t.Errorf("tokens = in %d out %d", a[0].TokensIn, a[0].TokensOut)
	}
}

func TestResetTask_ReopensTask(t *testing.T) {
	s, id := agentTestServer(t)
	if _, err := s.coordinator.SetTasks(id, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.coordinator.StartTask(id, 0)
	_, _ = s.coordinator.CompleteTask(id, 0)
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.SetPathValue("id", id)
	req.SetPathValue("index", "0")
	w := httptest.NewRecorder()
	s.handleResetTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	st, _ := s.coordinator.Get(id)
	if st.Tasks[0].Status != "pending" || st.Tasks[0].StartedAt != "" || st.Tasks[0].CompletedAt != "" {
		t.Errorf("task = %+v", st.Tasks[0])
	}
}
