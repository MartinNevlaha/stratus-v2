package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TranscriptStep is one visible action of a subagent: a tool call or a text message.
type TranscriptStep struct {
	Kind    string `json:"kind"` // tool | text
	Name    string `json:"name,omitempty"`
	Summary string `json:"summary,omitempty"`
	Text    string `json:"text,omitempty"`
}

// TranscriptUsage sums the token usage reported across a transcript.
type TranscriptUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens"`
}

var agentIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

const (
	maxTranscriptBytes = 8 << 20
	maxStepText        = 400
	maxStepSummary     = 200
)

// subagentTranscriptPath derives where Claude Code keeps a subagent's own transcript:
// <session transcript without .jsonl>/subagents/agent-<id>.jsonl. It returns "" for an
// empty session path or an id that is not a plain token.
func subagentTranscriptPath(sessionTranscript, agentID string) string {
	if sessionTranscript == "" || !agentIDRe.MatchString(agentID) {
		return ""
	}
	base := strings.TrimSuffix(sessionTranscript, ".jsonl")
	return filepath.Join(base, "subagents", "agent-"+agentID+".jsonl")
}

// readAgentTranscript parses an agent JSONL transcript into its last `limit` steps (all when
// limit <= 0) and the summed token usage. Unparseable lines are skipped.
func readAgentTranscript(path string, limit int) ([]TranscriptStep, TranscriptUsage, error) {
	var usage TranscriptUsage
	f, err := os.Open(path)
	if err != nil {
		return nil, usage, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > maxTranscriptBytes {
		if _, err := f.Seek(st.Size()-maxTranscriptBytes, 0); err != nil {
			return nil, usage, fmt.Errorf("seek transcript: %w", err)
		}
	}

	steps := []TranscriptStep{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxTranscriptBytes)
	for sc.Scan() {
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Usage struct {
					Input         int `json:"input_tokens"`
					Output        int `json:"output_tokens"`
					CacheRead     int `json:"cache_read_input_tokens"`
					CacheCreation int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "assistant" {
			continue
		}
		u := line.Message.Usage
		usage.InputTokens += u.Input
		usage.OutputTokens += u.Output
		usage.CacheReadTokens += u.CacheRead
		usage.CacheCreationTokens += u.CacheCreation

		var blocks []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		}
		if json.Unmarshal(line.Message.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			switch b.Type {
			case "text":
				if t := strings.TrimSpace(b.Text); t != "" {
					steps = append(steps, TranscriptStep{Kind: "text", Text: clip(t, maxStepText)})
				}
			case "tool_use":
				steps = append(steps, TranscriptStep{Kind: "tool", Name: b.Name, Summary: clip(toolInputSummary(b.Input), maxStepSummary)})
			}
		}
	}
	if limit > 0 && len(steps) > limit {
		steps = steps[len(steps)-limit:]
	}
	return steps, usage, nil
}

func toolInputSummary(in map[string]any) string {
	for _, key := range []string{"file_path", "notebook_path", "command", "pattern", "path", "query", "url", "message", "description"} {
		if v, _ := in[key].(string); v != "" {
			return v
		}
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
