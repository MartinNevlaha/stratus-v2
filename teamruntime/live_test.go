package teamruntime

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teams"
	"github.com/google/uuid"
)

// Explicitly gated, billable acceptance test. At most two tiny model turns per
// runtime, in a fresh empty project. No retries, fallback, or permission grants.
func TestLiveNativeTwoTurns(t *testing.T) {
	if os.Getenv("STRATUS_TEAM_LIVE") != "1" {
		t.Skip("billable; requires STRATUS_TEAM_LIVE=1 and explicit authorization")
	}
	for _, runtime := range []string{"codex", "claude-code", "opencode"} {
		t.Run(runtime, func(t *testing.T) {
			root := t.TempDir()
			if out, err := exec.Command("git", "init", "--quiet", root).CombinedOutput(); err != nil {
				t.Fatalf("empty test project: %s %v", out, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 170*time.Second)
			defer cancel()
			a := Adapters()[runtime]
			ready := a.Preflight(ctx, root)
			if !ready.Available {
				t.Fatalf("preflight unavailable: %s", ready.Reason)
			}
			profile, ok := cheapProfile(runtime, ready.Models)
			if !ok {
				t.Skip("no explicitly recognized cheap authenticated model; no fallback or model call")
			}
			t.Logf("requested runtime=%s model=%s effort=%s", runtime, profile.Model, profile.Effort)
			s, err := a.Open(ctx, teams.SessionSpec{ProjectRoot: root, TeamID: "isolated-smoke", MemberID: "smoke", Profile: profile, Instructions: "This is an isolated transport test. Do not call any tools. Reply only with the requested short text."}, func(e teams.Event) {
				if e.Kind == "approval" {
					cancel()
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			nonce := "team-" + uuid.NewString()[:8]
			first, err := s.Submit(ctx, teams.Input{ID: uuid.NewString(), Text: "Remember the token " + nonce + " for the next message. Reply only READY. Do not use tools."})
			if err != nil {
				t.Fatalf("first turn: %v", err)
			}
			if first.NativeID == "" {
				t.Fatal("no native identity")
			}
			t.Logf("first observed_model=%s observed_effort=%s input_tokens=%v output_tokens=%v", first.Model, first.Effort, tokenCount(first.InputTokens), tokenCount(first.OutputTokens))
			second, err := s.Submit(ctx, teams.Input{ID: uuid.NewString(), Text: "What exact token did I ask you to remember? Reply only that token. Do not use tools."})
			if err != nil {
				t.Fatalf("second turn: %v", err)
			}
			if second.NativeID != first.NativeID || !strings.Contains(second.Text, nonce) {
				t.Fatalf("session/context continuity failed; same_identity=%t result=%q", second.NativeID == first.NativeID, second.Text)
			}
			t.Logf("second observed_model=%s observed_effort=%s input_tokens=%v output_tokens=%v; same identity and remembered token verified", second.Model, second.Effort, tokenCount(second.InputTokens), tokenCount(second.OutputTokens))
		})
	}
}

func cheapProfile(runtime string, models []teams.Model) (teams.Profile, bool) {
	candidates := []string{}
	switch runtime {
	case "codex":
		candidates = []string{"gpt-6-luna"}
	case "claude-code":
		candidates = []string{"haiku", "claude-haiku-5-5", "claude-haiku-4-5-20251001"}
	case "opencode":
		candidates = []string{"openai/gpt-6-luna", "anthropic/claude-haiku-5-5", "anthropic/claude-haiku-4-5-20251001"}
	}
	for _, id := range candidates {
		for _, m := range models {
			if m.ID != id {
				continue
			}
			effort := ""
			if slices.Contains(m.Efforts, "medium") {
				effort = "medium"
			} else if slices.Contains(m.Efforts, "low") {
				effort = "low"
			} else if len(m.Efforts) > 0 {
				continue
			}
			return teams.Profile{Runtime: runtime, Model: id, Effort: effort}, true
		}
	}
	return teams.Profile{}, false
}
func tokenCount(n *int64) any {
	if n == nil {
		return "unknown"
	}
	return *n
}
