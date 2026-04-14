package generators

import (
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

func TestRefactorOpportunityGenerator_EmptyBundle(t *testing.T) {
	g := &refactorOpportunityGenerator{}
	hyps := g.Generate(baseline.Bundle{}, 10)
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses for empty bundle, got %d", len(hyps))
	}
}

func TestRefactorOpportunityGenerator_Category(t *testing.T) {
	g := &refactorOpportunityGenerator{}
	if g.Category() != "refactor_opportunity" {
		t.Errorf("expected category refactor_opportunity, got %q", g.Category())
	}
}

func TestRefactorOpportunityGenerator_HappyPath(t *testing.T) {
	now := time.Now()
	// Build commits: file "hot.go" touched 5 times (above min threshold of 3).
	commits := make([]baseline.GitCommit, 5)
	for i := range commits {
		commits[i] = baseline.GitCommit{
			Hash:    "abc123",
			Subject: "fix something",
			Files:   []string{"hot.go"},
			At:      now,
		}
	}
	todos := []baseline.TODOItem{
		{Path: "hot.go", Line: 10, Text: "TODO: refactor this", Kind: "TODO"},
		{Path: "hot.go", Line: 20, Text: "TODO: clean up", Kind: "TODO"},
	}
	b := baseline.Bundle{
		GitCommits: commits,
		TODOs:      todos,
	}
	g := &refactorOpportunityGenerator{}
	hyps := g.Generate(b, 10)
	if len(hyps) == 0 {
		t.Fatal("expected at least 1 hypothesis, got 0")
	}
	if hyps[0].Category != "refactor_opportunity" {
		t.Errorf("expected category refactor_opportunity, got %q", hyps[0].Category)
	}
	if len(hyps[0].FileRefs) == 0 {
		t.Error("expected FileRefs to be non-empty")
	}
}

func TestRefactorOpportunityGenerator_BelowThreshold(t *testing.T) {
	now := time.Now()
	// Only 2 commits touching a file — below the min threshold of 3.
	commits := []baseline.GitCommit{
		{Hash: "a1", Subject: "fix", Files: []string{"cold.go"}, At: now},
		{Hash: "a2", Subject: "fix", Files: []string{"cold.go"}, At: now},
	}
	b := baseline.Bundle{GitCommits: commits}
	g := &refactorOpportunityGenerator{}
	hyps := g.Generate(b, 10)
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses for files below threshold, got %d", len(hyps))
	}
}

func TestRefactorOpportunityGenerator_MaxEnforcement(t *testing.T) {
	now := time.Now()
	// Create 8 hot files, each touched 4 times.
	var commits []baseline.GitCommit
	for i := 0; i < 8; i++ {
		file := "file" + string(rune('a'+i)) + ".go"
		for j := 0; j < 4; j++ {
			commits = append(commits, baseline.GitCommit{
				Hash:    "h" + string(rune('a'+i)) + string(rune('0'+j)),
				Subject: "fix",
				Files:   []string{file},
				At:      now,
			})
		}
	}
	b := baseline.Bundle{GitCommits: commits}
	g := &refactorOpportunityGenerator{}
	hyps := g.Generate(b, 3)
	if len(hyps) != 3 {
		t.Errorf("expected exactly 3 hypotheses (max enforcement), got %d", len(hyps))
	}
}
