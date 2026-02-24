package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
)

// ANSI escape codes matching better-claude colors exactly.
const (
	ansiReset       = "\x1b[0m"
	ansiDim         = "\x1b[2m"
	ansiMagenta     = "\x1b[35m"
	ansiCyan        = "\x1b[36m"
	ansiGreen       = "\x1b[32m"
	ansiYellow      = "\x1b[33m"
	ansiBlue        = "\x1b[34m"
	ansiBrightWhite = "\x1b[97m"
	ansiRed         = "\x1b[31m"
	nbsp            = "\u00a0" // non-breaking space prevents terminal trimming
)

// slInput is the JSON Claude Code sends on stdin to the statusline command.
type slInput struct {
	Workspace struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	Model struct {
		DisplayName string `json:"display_name"`
		ID          string `json:"id"`
	} `json:"model"`
	Cost struct {
		TotalCostUSD    float64 `json:"total_cost_usd"`
		TotalDurationMS float64 `json:"total_duration_ms"`
	} `json:"cost"`
	ContextWindow struct {
		ContextWindowSize int `json:"context_window_size"`
		CurrentUsage      struct {
			InputTokens               int `json:"input_tokens"`
			CacheReadInputTokens      int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens  int `json:"cache_creation_input_tokens"`
		} `json:"current_usage"`
	} `json:"context_window"`
}

// slDashboard is the subset of /api/dashboard/state we care about.
type slDashboard struct {
	Workflows []*slWorkflow `json:"workflows"`
}

// slWorkflow mirrors orchestration.WorkflowState JSON fields.
type slWorkflow struct {
	ID         string              `json:"id"`
	Type       string              `json:"type"`
	Phase      string              `json:"phase"`
	Title      string              `json:"title"`
	Tasks      []slTask            `json:"tasks"`
	TotalTasks int                 `json:"total_tasks"`
	Aborted    bool                `json:"aborted"`
	Delegated  map[string][]string `json:"delegated_agents"`
}

// slTask mirrors orchestration.Task JSON fields.
type slTask struct {
	Status string `json:"status"`
}

// cmdStatusline reads session metrics from stdin, fetches stratus workflow
// state, and prints an ANSI-colored status line to stdout.
func cmdStatusline() {
	var in slInput
	_ = json.NewDecoder(os.Stdin).Decode(&in)

	cfg := config.Load()
	state := fetchStratusState(fmt.Sprintf("http://127.0.0.1:%d", cfg.Port))

	fmt.Print(formatStatusline(in, state))
}

// fetchStratusState calls the dashboard state endpoint and returns the result,
// or nil if the server is unreachable or returns invalid JSON.
func fetchStratusState(base string) *slDashboard {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(base + "/api/dashboard/state")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var s slDashboard
	if json.NewDecoder(resp.Body).Decode(&s) != nil {
		return nil
	}
	return &s
}

// formatStatusline assembles the full status line from all segments.
func formatStatusline(in slInput, state *slDashboard) string {
	cwd := in.Workspace.CurrentDir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	sep := ansiDim + " | " + ansiReset
	var parts []string
	for _, s := range []string{
		fmtGit(cwd),
		fmtModel(in),
		fmtCost(in),
		fmtDuration(in),
		fmtContext(in),
		fmtStratus(state),
	} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	out := ansiReset + strings.Join(parts, sep)
	// Replace regular spaces with non-breaking spaces to prevent terminal trimming.
	return strings.ReplaceAll(out, " ", nbsp)
}

// fmtGit returns the current git branch in magenta, or "" if not in a repo.
func fmtGit(cwd string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return ""
	}
	return ansiMagenta + "⎇ " + branch + ansiReset
}

// fmtModel returns the model display name in cyan, or "" if not set.
func fmtModel(in slInput) string {
	name := in.Model.DisplayName
	if name == "" {
		return ""
	}
	return ansiCyan + name + ansiReset
}

// fmtCost returns the formatted session cost in green, or "" if zero.
func fmtCost(in slInput) string {
	if in.Cost.TotalCostUSD == 0 {
		return ""
	}
	return ansiGreen + fmt.Sprintf("$%.2f", in.Cost.TotalCostUSD) + ansiReset
}

// fmtDuration returns the session duration in yellow, or "" if zero.
func fmtDuration(in slInput) string {
	if in.Cost.TotalDurationMS == 0 {
		return ""
	}
	totalMins := int(in.Cost.TotalDurationMS / 1000 / 60)
	if totalMins == 0 {
		return ""
	}
	var dur string
	if totalMins >= 60 {
		hrs := totalMins / 60
		mins := totalMins % 60
		dur = fmt.Sprintf("%dhr %dm", hrs, mins)
	} else {
		dur = fmt.Sprintf("%dm", totalMins)
	}
	return ansiYellow + dur + ansiReset
}

// fmtContext returns the context window usage percentage in blue, or "" if not set.
func fmtContext(in slInput) string {
	size := in.ContextWindow.ContextWindowSize
	if size == 0 {
		return ""
	}
	used := in.ContextWindow.CurrentUsage.InputTokens +
		in.ContextWindow.CurrentUsage.CacheReadInputTokens +
		in.ContextWindow.CurrentUsage.CacheCreationInputTokens
	pct := float64(used) / float64(size) * 100
	return ansiBlue + fmt.Sprintf("Ctx: %.1f%%", pct) + ansiReset
}

// fmtStratus returns the stratus workflow status segment.
func fmtStratus(state *slDashboard) string {
	icon := ansiGreen + "◈" + ansiReset

	if state == nil {
		return ansiRed + "◈ offline" + ansiReset
	}

	// Find the first active (non-aborted, non-complete) workflow.
	var active *slWorkflow
	for _, wf := range state.Workflows {
		if !wf.Aborted && wf.Phase != "complete" {
			active = wf
			break
		}
	}

	if active == nil {
		return icon + " " + ansiBrightWhite + "idle v" + Version + ansiReset
	}

	// Build slug from title (max 12 chars) or first 8 chars of ID.
	slug := active.Title
	if slug == "" {
		if len(active.ID) > 8 {
			slug = active.ID[:8]
		} else {
			slug = active.ID
		}
	} else if len([]rune(slug)) > 12 {
		slug = string([]rune(slug)[:12]) + "…"
	}

	// Count completed tasks.
	done := 0
	for _, t := range active.Tasks {
		if t.Status == "done" {
			done++
		}
	}
	total := active.TotalTasks

	text := fmt.Sprintf("%s (%s) %d/%d", active.Phase, slug, done, total)

	// Count delegated agents in the current phase.
	if agents, ok := active.Delegated[active.Phase]; ok && len(agents) > 0 {
		text += fmt.Sprintf(" [%d agents]", len(agents))
	}

	return icon + " " + ansiBrightWhite + text + ansiReset
}
