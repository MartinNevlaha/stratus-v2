package scoring

import (
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

// makeBundle is a helper to build a minimal baseline.Bundle for tests.
func makeBundle(commits []baseline.GitCommit, todos []baseline.TODOItem, wikis []baseline.WikiTitle, govs []baseline.GovernanceRef, ratios []baseline.TestRatio) baseline.Bundle {
	return baseline.Bundle{
		GitCommits:     commits,
		TODOs:          todos,
		WikiTitles:     wikis,
		GovernanceRefs: govs,
		TestRatios:     ratios,
		GeneratedAt:    time.Now(),
	}
}

// commits returns N GitCommit entries that all touch the given file path.
func commits(n int, file string) []baseline.GitCommit {
	cs := make([]baseline.GitCommit, n)
	for i := range cs {
		cs[i] = baseline.GitCommit{
			Hash:  "abc",
			Files: []string{file},
			At:    time.Now(),
		}
	}
	return cs
}

func TestStaticScorer_Churn_TenCommits_HalfScore(t *testing.T) {
	scorer := NewStaticScorer()
	h := Hypothesis{FileRefs: []string{"pkg/foo.go"}}
	bundle := makeBundle(commits(10, "pkg/foo.go"), nil, nil, nil, nil)
	s := scorer.Score(h, bundle)
	want := 0.5 // 10/20
	if s.Churn != want {
		t.Errorf("Churn: got %v, want %v", s.Churn, want)
	}
}

func TestStaticScorer_Churn_TwentyFiveCommits_Capped(t *testing.T) {
	scorer := NewStaticScorer()
	h := Hypothesis{FileRefs: []string{"pkg/foo.go"}}
	bundle := makeBundle(commits(25, "pkg/foo.go"), nil, nil, nil, nil)
	s := scorer.Score(h, bundle)
	want := 1.0 // capped at 1.0
	if s.Churn != want {
		t.Errorf("Churn: got %v, want %v", s.Churn, want)
	}
}

func TestStaticScorer_Churn_NoFileRefs_Zero(t *testing.T) {
	scorer := NewStaticScorer()
	h := Hypothesis{FileRefs: nil}
	bundle := makeBundle(commits(10, "pkg/foo.go"), nil, nil, nil, nil)
	s := scorer.Score(h, bundle)
	if s.Churn != 0 {
		t.Errorf("Churn: got %v, want 0", s.Churn)
	}
}

func TestStaticScorer_TestGap_LowRatio(t *testing.T) {
	scorer := NewStaticScorer()
	// File in "pkg" dir. TestRatio for "pkg" = 0.3 → gap = 0.7
	h := Hypothesis{FileRefs: []string{"pkg/foo.go"}}
	bundle := makeBundle(nil, nil, nil, nil, []baseline.TestRatio{
		{Dir: "pkg", Ratio: 0.3},
	})
	s := scorer.Score(h, bundle)
	want := 0.7
	if abs(s.TestGap-want) > 1e-9 {
		t.Errorf("TestGap: got %v, want %v", s.TestGap, want)
	}
}

func TestStaticScorer_TestGap_MissingDir_FullGap(t *testing.T) {
	scorer := NewStaticScorer()
	// File in "unknown" dir — not in ratios → treat as 0 → gap = 1.0
	h := Hypothesis{FileRefs: []string{"unknown/foo.go"}}
	bundle := makeBundle(nil, nil, nil, nil, []baseline.TestRatio{
		{Dir: "pkg", Ratio: 0.8},
	})
	s := scorer.Score(h, bundle)
	want := 1.0
	if s.TestGap != want {
		t.Errorf("TestGap: got %v, want %v", s.TestGap, want)
	}
}

func TestStaticScorer_TestGap_CategoryFallback(t *testing.T) {
	scorer := NewStaticScorer()
	// No FileRefs, category = "test_gap" → fallback 0.5
	h := Hypothesis{Category: "test_gap", FileRefs: nil}
	bundle := makeBundle(nil, nil, nil, nil, nil)
	s := scorer.Score(h, bundle)
	want := 0.5
	if s.TestGap != want {
		t.Errorf("TestGap fallback: got %v, want %v", s.TestGap, want)
	}
}

func TestStaticScorer_TODO_ThreeItems(t *testing.T) {
	scorer := NewStaticScorer()
	h := Hypothesis{FileRefs: []string{"pkg/foo.go"}}
	todos := []baseline.TODOItem{
		{Path: "pkg/foo.go", Text: "TODO: fix this"},
		{Path: "pkg/foo.go", Text: "TODO: and this"},
		{Path: "pkg/foo.go", Text: "TODO: and that"},
		{Path: "other/bar.go", Text: "TODO: unrelated"},
	}
	bundle := makeBundle(nil, todos, nil, nil, nil)
	s := scorer.Score(h, bundle)
	want := 0.6 // 3/5
	if abs(s.TODO-want) > 1e-9 {
		t.Errorf("TODO: got %v, want %v", s.TODO, want)
	}
}

func TestStaticScorer_TODO_FeatureIdeaBonus(t *testing.T) {
	scorer := NewStaticScorer()
	// feature_idea + 3 TODOs on same file → base 0.6 + 0.2 bonus = 0.8
	h := Hypothesis{Category: "feature_idea", FileRefs: []string{"pkg/foo.go"}}
	todos := []baseline.TODOItem{
		{Path: "pkg/foo.go", Text: "TODO: fix this"},
		{Path: "pkg/foo.go", Text: "TODO: and this"},
		{Path: "pkg/foo.go", Text: "TODO: and that"},
	}
	bundle := makeBundle(nil, todos, nil, nil, nil)
	s := scorer.Score(h, bundle)
	want := 0.8 // 0.6 + 0.2 bonus
	if abs(s.TODO-want) > 1e-9 {
		t.Errorf("TODO feature_idea bonus: got %v, want %v", s.TODO, want)
	}
}

func TestStaticScorer_Staleness_MatchingWikiPage(t *testing.T) {
	scorer := NewStaticScorer()
	h := Hypothesis{Title: "authentication flow refactor", SymbolRefs: []string{"AuthHandler"}}
	wikis := []baseline.WikiTitle{
		{ID: "w1", Title: "Authentication Flow", Staleness: 0.8},
		{ID: "w2", Title: "Deployment Guide", Staleness: 0.9},
	}
	bundle := makeBundle(nil, nil, wikis, nil, nil)
	s := scorer.Score(h, bundle)
	want := 0.8
	if abs(s.Staleness-want) > 1e-9 {
		t.Errorf("Staleness: got %v, want %v", s.Staleness, want)
	}
}

func TestStaticScorer_Staleness_NoMatchGlobalFallback(t *testing.T) {
	scorer := NewStaticScorer()
	// No keyword match → take global top staleness × 0.5
	h := Hypothesis{Title: "completely unrelated xyz", SymbolRefs: nil}
	wikis := []baseline.WikiTitle{
		{ID: "w1", Title: "Authentication Flow", Staleness: 0.9},
		{ID: "w2", Title: "Deployment Guide", Staleness: 0.7},
	}
	bundle := makeBundle(nil, nil, wikis, nil, nil)
	s := scorer.Score(h, bundle)
	want := 0.45 // 0.9 × 0.5
	if abs(s.Staleness-want) > 1e-9 {
		t.Errorf("Staleness global fallback: got %v, want %v", s.Staleness, want)
	}
}

func TestStaticScorer_ADRViolation_TwoMatches(t *testing.T) {
	scorer := NewStaticScorer()
	// Title contains "database" and "migration" → 2 matches in governance refs → 2/3
	h := Hypothesis{
		Title:     "database migration strategy",
		Rationale: "we need better database handling",
	}
	govs := []baseline.GovernanceRef{
		{ID: "g1", Title: "Database Selection ADR"},
		{ID: "g2", Title: "Migration Policy"},
		{ID: "g3", Title: "Unrelated CI Rules"},
	}
	bundle := makeBundle(nil, nil, nil, govs, nil)
	s := scorer.Score(h, bundle)
	want := 2.0 / 3.0
	if abs(s.ADRViolation-want) > 1e-9 {
		t.Errorf("ADRViolation: got %v, want %v", s.ADRViolation, want)
	}
}

func TestStaticScorer_Determinism(t *testing.T) {
	scorer := NewStaticScorer()
	h := Hypothesis{
		Category:   "refactor_opportunity",
		Title:      "database migration strategy",
		Rationale:  "we need better database handling",
		FileRefs:   []string{"pkg/foo.go"},
		SymbolRefs: []string{"AuthHandler"},
	}
	bundle := makeBundle(
		commits(10, "pkg/foo.go"),
		[]baseline.TODOItem{{Path: "pkg/foo.go", Text: "TODO: fix"}},
		[]baseline.WikiTitle{{ID: "w1", Title: "Authentication Flow", Staleness: 0.8}},
		[]baseline.GovernanceRef{{ID: "g1", Title: "Database Selection ADR"}},
		[]baseline.TestRatio{{Dir: "pkg", Ratio: 0.3}},
	)
	s1 := scorer.Score(h, bundle)
	s2 := scorer.Score(h, bundle)
	s3 := scorer.Score(h, bundle)
	if s1 != s2 || s2 != s3 {
		t.Errorf("Determinism: got different results: %v, %v, %v", s1, s2, s3)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
