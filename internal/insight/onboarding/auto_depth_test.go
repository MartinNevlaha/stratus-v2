package onboarding

import (
	"fmt"
	"testing"
)

func TestResolveAutoDepth_SmallProject(t *testing.T) {
	profile := &ProjectProfile{
		Languages:    []LanguageStat{{Language: "Go", FileCount: 10}},
		DirectoryTree: "cmd\nmain.go\n",
	}

	result := ResolveAutoDepth(profile)

	if result.Depth != "shallow" {
		t.Errorf("expected shallow for small project, got %q", result.Depth)
	}
	if result.MaxPages < 3 {
		t.Errorf("expected at least 3 pages, got %d", result.MaxPages)
	}
}

func TestResolveAutoDepth_MediumProject(t *testing.T) {
	profile := &ProjectProfile{
		Languages: []LanguageStat{
			{Language: "Go", FileCount: 80},
			{Language: "TypeScript", FileCount: 40},
		},
		DirectoryTree: "api\nconfig\ndb\nfrontend\ninternal\ncmd\n",
		GitStats:      &GitStats{CommitCount: 150},
	}

	result := ResolveAutoDepth(profile)

	if result.Depth != "standard" {
		t.Errorf("expected standard for medium project, got %q", result.Depth)
	}
	if result.MaxPages < 10 {
		t.Errorf("expected at least 10 pages, got %d", result.MaxPages)
	}
}

func TestResolveAutoDepth_LargeProject(t *testing.T) {
	profile := &ProjectProfile{
		Languages: []LanguageStat{
			{Language: "Go", FileCount: 300},
			{Language: "TypeScript", FileCount: 150},
			{Language: "Python", FileCount: 50},
			{Language: "Shell", FileCount: 20},
			{Language: "Svelte", FileCount: 30},
		},
		DirectoryTree: "api\nconfig\ndb\nfrontend\ninternal\ncmd\norchestration\nswarm\nmcp\nhooks\nterminal\nvexor\ninsight\nguardian\ndocs\n",
		GitStats:      &GitStats{CommitCount: 800},
		DetectedPatterns: []string{"monorepo"},
	}

	result := ResolveAutoDepth(profile)

	if result.Depth != "deep" {
		t.Errorf("expected deep for large project, got %q", result.Depth)
	}
	if result.MaxPages < 30 {
		t.Errorf("expected at least 30 pages for large project, got %d", result.MaxPages)
	}
}

func TestResolveAutoDepth_MaxPagesCappedDefault(t *testing.T) {
	// ResolveAutoDepth (default 200 cap) still caps at 200 for backward compat.
	langs := make([]LanguageStat, 0)
	for i := 0; i < 10; i++ {
		langs = append(langs, LanguageStat{Language: "Go", FileCount: 100})
	}

	var tree string
	for i := 0; i < 100; i++ {
		tree += "module_" + string(rune('a'+i%26)) + "\n"
	}

	profile := &ProjectProfile{
		Languages:        langs,
		DirectoryTree:    tree,
		GitStats:         &GitStats{CommitCount: 1000},
		DetectedPatterns: []string{"monorepo"},
	}

	result := ResolveAutoDepth(profile)

	if result.MaxPages > 200 {
		t.Errorf("expected max pages capped at 200 (default cap), got %d", result.MaxPages)
	}
}

func TestResolveAutoDepth_ReasonNotEmpty(t *testing.T) {
	profile := &ProjectProfile{
		Languages: []LanguageStat{{Language: "Go", FileCount: 10}},
	}

	result := ResolveAutoDepth(profile)

	if result.Reason == "" {
		t.Error("expected non-empty reason")
	}
}

// TestClampPages_MaxCapZero_Unlimited verifies that maxCap=0 returns the raw
// computed value without applying any ceiling.
func TestClampPages_MaxCapZero_Unlimited(t *testing.T) {
	// A value well above the old hard cap of 200.
	got := clampPages(500, 0)
	if got != 500 {
		t.Errorf("clampPages(500, 0) = %d, want 500 (unlimited)", got)
	}
}

// TestClampPages_MaxCapPositive_Clamps verifies that a positive maxCap acts as ceiling.
func TestClampPages_MaxCapPositive_Clamps(t *testing.T) {
	cases := []struct {
		n      int
		maxCap int
		want   int
	}{
		{100, 50, 50},  // clamped to maxCap
		{30, 50, 30},   // below cap, returned as-is
		{50, 50, 50},   // exactly at cap
		{201, 200, 200},// old sentinel still works when explicit
	}
	for _, tc := range cases {
		got := clampPages(tc.n, tc.maxCap)
		if got != tc.want {
			t.Errorf("clampPages(%d, %d) = %d, want %d", tc.n, tc.maxCap, got, tc.want)
		}
	}
}

// TestClampPages_FloorPreserved verifies that the floor of 5 is always preserved.
func TestClampPages_FloorPreserved(t *testing.T) {
	cases := []struct {
		n      int
		maxCap int
	}{
		{0, 0},   // unlimited cap, below floor
		{3, 0},   // unlimited cap, below floor
		{0, 200}, // positive cap, below floor
		{1, 10},  // positive cap, below floor
	}
	for _, tc := range cases {
		got := clampPages(tc.n, tc.maxCap)
		if got < 5 {
			t.Errorf("clampPages(%d, %d) = %d, want >= 5 (floor)", tc.n, tc.maxCap, got)
		}
	}
}

// TestResolveAutoDepth_WithMaxCap_Unlimited verifies that passing maxCap=0 to
// ResolveAutoDepthWithCap lets the computed page count exceed 200.
// The deep formula is modules*4+5; with 60 unique modules that yields 245 pages,
// which would have been clamped to 200 with the old hard cap.
func TestResolveAutoDepth_WithMaxCap_Unlimited(t *testing.T) {
	langs := make([]LanguageStat, 0)
	for i := 0; i < 10; i++ {
		langs = append(langs, LanguageStat{Language: "Go", FileCount: 100})
	}
	// Build 60 unique module names (e.g. mod00..mod59) so that modules*4+5 = 245 > 200.
	var tree string
	for i := 0; i < 60; i++ {
		tree += fmt.Sprintf("mod%02d\n", i)
	}
	profile := &ProjectProfile{
		Languages:        langs,
		DirectoryTree:    tree,
		GitStats:         &GitStats{CommitCount: 1000},
		DetectedPatterns: []string{"monorepo"},
	}

	result := ResolveAutoDepthWithCap(profile, 0)
	if result.MaxPages <= 200 {
		t.Errorf("expected MaxPages > 200 with unlimited cap, got %d", result.MaxPages)
	}
}
