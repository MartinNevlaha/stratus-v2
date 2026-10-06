package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

func slInputOf(t *testing.T, raw string) slInput {
	t.Helper()
	var in slInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	return in
}

func TestFmtModel_ShowsEffortLevel(t *testing.T) {
	in := slInputOf(t, `{"model": {"display_name": "Opus 5.5"}, "effort": {"level": "high"}}`)
	if got := plain(fmtModel(in)); got != "[Opus 5.5 · high]" {
		t.Fatalf("fmtModel = %q", got)
	}
	in = slInputOf(t, `{"model": {"display_name": "Opus 5.5"}}`)
	if got := plain(fmtModel(in)); got != "[Opus 5.5]" {
		t.Fatalf("fmtModel without effort = %q", got)
	}
}

func TestFmtWorktree(t *testing.T) {
	in := slInputOf(t, `{"worktree": {"name": "my-feature", "path": "/p/.claude/worktrees/my-feature"}}`)
	if got := plain(fmtWorktree(in)); got != "🌳 my-feature" {
		t.Fatalf("fmtWorktree = %q", got)
	}
	if got := fmtWorktree(slInputOf(t, `{}`)); got != "" {
		t.Fatalf("fmtWorktree outside a worktree = %q, want empty", got)
	}
}

func TestFmtRateLimit_FiveHourWindow(t *testing.T) {
	cases := map[string]string{
		`{"rate_limits": {"five_hour": {"used_percentage": 42.4}}}`: "5h 42%",
		`{"rate_limits": {"five_hour": {"used_percentage": 0}}}`:    "5h 0%",
		`{}`: "",
	}
	for raw, want := range cases {
		if got := plain(fmtRateLimit(slInputOf(t, raw))); got != want {
			t.Errorf("fmtRateLimit(%s) = %q, want %q", raw, got, want)
		}
	}
	if got := fmtRateLimit(slInputOf(t, `{"rate_limits": {"five_hour": {"used_percentage": 95}}}`)); !strings.Contains(got, ansiRed) {
		t.Errorf("a nearly spent window should be red: %q", got)
	}
}

func TestFormatStatusline_PlacesNewSegments(t *testing.T) {
	in := slInputOf(t, `{"model": {"display_name": "Opus 5.5"}, "effort": {"level": "high"},
		"worktree": {"name": "wt1"}, "rate_limits": {"five_hour": {"used_percentage": 10}},
		"context_window": {"used_percentage": 20}}`)
	lines := strings.Split(plain(formatStatusline(in, nil)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want two lines, got %q", lines)
	}
	if !strings.Contains(lines[0], "[Opus 5.5 · high]") || !strings.Contains(lines[0], "🌳 wt1") {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.Contains(lines[1], "5h 10%") {
		t.Errorf("second line = %q", lines[1])
	}
}
