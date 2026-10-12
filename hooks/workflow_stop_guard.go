package hooks

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// workflowStopNudgeMarker identifies a nudge this hook already delivered. It comes
// back inside the coordinator's transcript as an injected user message, which is
// how the hook tells that its last nudge was ignored and must not be repeated.
const workflowStopNudgeMarker = "stratus: workflow still open."

// workflowStopPhaseComplete is the terminal phase of every workflow type.
const workflowStopPhaseComplete = "complete"

// WorkflowStopGuard fires when the coordinator ends its turn.
//
// The failure it exists to catch: a spec/bug/e2e/swarm coordinator finishes a step
// (a phase transition, an agent result, a task) and ends its turn with a status
// summary instead of starting the next step. The workflow sits mid-phase until a
// human types "continue". While this session owns an unfinished workflow and the
// model is not waiting for the user, the model is redirected to keep going.
//
// FAIL-OPEN by design, like TeammateIdle: a check that cannot read its input must
// never wedge a live agent. It is also deliberately narrow — it only judges
// workflows registered for THIS session, never the single-workflow fallback that
// PhaseGuard uses for write guards, because nudging an unrelated session into a
// foreign workflow is worse than not nudging.
func WorkflowStopGuard(event HookEvent) Decision {
	allow := Decision{Continue: true}

	if event.SessionID == "" || event.StopHookActive {
		return allow
	}

	state, err := fetchDashboardStateStrict()
	if err != nil || state == nil {
		return allow
	}

	var open []map[string]any
	for _, wf := range workflowsForSession(state.Workflows, event.SessionID) {
		if phase, _ := wf["phase"].(string); phase != workflowStopPhaseComplete {
			open = append(open, wf)
		}
	}
	if len(open) == 0 {
		return allow
	}

	scan, err := scanStopTranscript(event.TranscriptPath)
	if err != nil {
		// No transcript is fine (older Claude Code, tests); an unreadable one is not
		// evidence of anything.
		scan = stopScan{}
	}

	lastMessage := strings.TrimSpace(event.LastAssistantMessage)
	if lastMessage == "" {
		lastMessage = strings.TrimSpace(scan.lastAssistantText)
	}
	// The model is asking the user something: that is a real stop.
	if strings.HasSuffix(lastMessage, "?") {
		return allow
	}

	// Nudged already and the model answered with text only: another nudge is spam.
	if scan.nudged && scan.toolCallsSinceNudge == 0 {
		return allow
	}

	for _, wf := range open {
		id, _ := wf["id"].(string)
		running, err := workflowHasRunningAgents(id)
		if err != nil || running {
			// Waiting for a background agent's notification is legitimate.
			return allow
		}
	}

	return Decision{Nudge: true, Reason: workflowStopNudgeReason(open)}
}

func workflowStopNudgeReason(open []map[string]any) string {
	parts := make([]string, 0, len(open))
	for _, wf := range open {
		id, _ := wf["id"].(string)
		phase, _ := wf["phase"].(string)
		parts = append(parts, fmt.Sprintf("%s (phase %s)", id, phase))
	}
	return workflowStopNudgeMarker + " " + strings.Join(parts, ", ") + " is not complete. Continue with the " +
		"next step of the workflow now, following its skill — do not end your turn with a status " +
		"summary. Stop only when you need a decision from the user (ask with AskUserQuestion) or a " +
		"step is blocked and you cannot make progress; say exactly what is needed."
}

// workflowHasRunningAgents reports whether any subagent of the workflow is running.
func workflowHasRunningAgents(workflowID string) (bool, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:" + getPort() + "/api/workflows/" + url.PathEscape(workflowID) + "/agents")
	if err != nil {
		return false, fmt.Errorf("list workflow agents: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("list workflow agents: status %d", resp.StatusCode)
	}

	var body struct {
		Agents []struct {
			Status string `json:"status"`
		} `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, fmt.Errorf("decode workflow agents: %w", err)
	}
	for _, a := range body.Agents {
		if a.Status == "running" {
			return true, nil
		}
	}
	return false, nil
}

// stopScan summarises the coordinator transcript for the Stop decision.
type stopScan struct {
	// nudged is true once a real injected message carried the nudge marker;
	// toolCallsSinceNudge counts the model's tool calls after the latest one.
	nudged              bool
	toolCallsSinceNudge int
	lastAssistantText   string
}

// scanStopTranscript walks the coordinator's JSONL transcript. Only an injected
// message (string content or a text block) counts as a nudge: the marker also
// shows up inside tool results whenever the model reads this very file.
func scanStopTranscript(path string) (stopScan, error) {
	if path == "" {
		return stopScan{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return stopScan{}, fmt.Errorf("open transcript: %w", err)
	}
	defer file.Close()

	var scan stopScan
	scanner := bufio.NewScanner(file)
	// Lines carry whole tool results; the 64KB default would end the scan early.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}

		switch entry.Type {
		case "user":
			if strings.Contains(injectedText(entry.Message.Content), workflowStopNudgeMarker) {
				scan.nudged = true
				scan.toolCallsSinceNudge = 0
			}
		case "assistant":
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(entry.Message.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				switch b.Type {
				case "tool_use":
					scan.toolCallsSinceNudge++
				case "text":
					scan.lastAssistantText = b.Text
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return stopScan{}, fmt.Errorf("scan transcript: %w", err)
	}
	return scan, nil
}

// injectedText returns the text of a user entry that is a message rather than a
// tool result: a plain string, or the text blocks of a content array.
func injectedText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}
