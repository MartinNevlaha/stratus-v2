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
)

// slInput is the JSON Claude Code sends on stdin to the statusline command.
type slInput struct {
	Workspace struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	CWD   string `json:"cwd"`
	Model struct {
		DisplayName string `json:"display_name"`
		ID          string `json:"id"`
	} `json:"model"`
	Cost struct {
		TotalCostUSD    float64 `json:"total_cost_usd"`
		TotalDurationMS float64 `json:"total_duration_ms"`
	} `json:"cost"`
	ContextWindow struct {
		ContextWindowSize int     `json:"context_window_size"`
		UsedPercentage    float64 `json:"used_percentage"`
		CurrentUsage      struct {
			InputTokens              int `json:"input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
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
	cwd := statusCWD(in)
	sep := ansiDim + " | " + ansiReset

	var first []string
	for _, s := range []string{fmtModel(in), fmtDir(cwd), fmtGit(cwd)} {
		if s != "" {
			first = append(first, s)
		}
	}

	var second []string
	for _, s := range []string{fmtContext(in), fmtCost(in), fmtDuration(in), fmtStratus(state)} {
		if s != "" {
			second = append(second, s)
		}
	}

	if len(first) == 0 && len(second) == 0 {
		return ""
	}
	if len(second) == 0 {
		return ansiReset + strings.Join(first, sep)
	}
	if len(first) == 0 {
		return ansiReset + strings.Join(second, sep)
	}
	return ansiReset + strings.Join(first, sep) + "\n" + strings.Join(second, sep)
}

func statusCWD(in slInput) string {
	if in.Workspace.CurrentDir != "" {
		return in.Workspace.CurrentDir
	}
	if in.CWD != "" {
		return in.CWD
	}
	cwd, _ := os.Getwd()
	return cwd
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
	return ansiMagenta + "🌿 " + branch + ansiReset
}

// fmtModel returns the model display name in cyan, or "" if not set.
func fmtModel(in slInput) string {
	name := in.Model.DisplayName
	if name == "" {
		return ""
	}
	return ansiCyan + "[" + name + "]" + ansiReset
}

// fmtDir returns the current directory basename in the shape used by the Claude docs.
func fmtDir(cwd string) string {
	dir := strings.TrimRight(cwd, string(os.PathSeparator))
	if dir == "" {
		return ""
	}
	base := dir
	if idx := strings.LastIndex(dir, string(os.PathSeparator)); idx >= 0 && idx+1 < len(dir) {
		base = dir[idx+1:]
	}
	return ansiBrightWhite + "📁 " + base + ansiReset
}

// fmtCost returns the formatted session cost in green, or "" if zero.
func fmtCost(in slInput) string {
	if in.Cost.TotalCostUSD == 0 {
		return ""
	}
	return ansiYellow + fmt.Sprintf("$%.2f", in.Cost.TotalCostUSD) + ansiReset
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
	return ansiDim + "⏱️ " + dur + ansiReset
}

// fmtContext returns a Claude-docs-style context usage bar.
func fmtContext(in slInput) string {
	pct := in.ContextWindow.UsedPercentage
	if pct == 0 && in.ContextWindow.ContextWindowSize > 0 {
		used := in.ContextWindow.CurrentUsage.InputTokens +
			in.ContextWindow.CurrentUsage.CacheReadInputTokens +
			in.ContextWindow.CurrentUsage.CacheCreationInputTokens
		pct = float64(used) / float64(in.ContextWindow.ContextWindowSize) * 100
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	barColor := ansiGreen
	if pct >= 90 {
		barColor = ansiRed
	} else if pct >= 70 {
		barColor = ansiYellow
	}

	filled := int(pct) / 10
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
	return barColor + bar + ansiReset + fmt.Sprintf(" %.0f%% context", pct)
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
