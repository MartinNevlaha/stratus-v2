package generators

import (
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

func TestArchitectureDriftGenerator_EmptyBundle(t *testing.T) {
	g := &architectureDriftGenerator{}
	hyps := g.Generate(baseline.Bundle{}, 10)
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses for empty bundle, got %d", len(hyps))
	}
}

func TestArchitectureDriftGenerator_Category(t *testing.T) {
	g := &architectureDriftGenerator{}
	if g.Category() != "architecture_drift" {
		t.Errorf("expected category architecture_drift, got %q", g.Category())
	}
}

func TestArchitectureDriftGenerator_HappyPath(t *testing.T) {
	now := time.Now()
	b := baseline.Bundle{
		GovernanceRefs: []baseline.GovernanceRef{
			{ID: "adr-001", Title: "Authentication strategy for API", Kind: "adr"},
			{ID: "rule-001", Title: "Error handling policy", Kind: "rule"},
		},
		GitCommits: []baseline.GitCommit{
			// "authentication" matches token "authentication" from ADR title.
			{Hash: "abc1", Subject: "refactor authentication middleware", Files: []string{"middleware/auth.go"}, At: now},
			{Hash: "abc2", Subject: "update error handling in handler", Files: []string{"api/handler.go"}, At: now},
		},
	}
	g := &architectureDriftGenerator{}
	hyps := g.Generate(b, 10)
	if len(hyps) == 0 {
		t.Fatal("expected at least 1 hypothesis for matching tokens, got 0")
	}
	for _, h := range hyps {
		if h.Category != "architecture_drift" {
			t.Errorf("expected category architecture_drift, got %q", h.Category)
		}
	}
}

func TestArchitectureDriftGenerator_NoMatch(t *testing.T) {
	now := time.Now()
	b := baseline.Bundle{
		GovernanceRefs: []baseline.GovernanceRef{
			{ID: "adr-002", Title: "Database selection", Kind: "adr"},
		},
		GitCommits: []baseline.GitCommit{
			{Hash: "xyz1", Subject: "fix typo in readme", Files: []string{"README.md"}, At: now},
		},
	}
	g := &architectureDriftGenerator{}
	hyps := g.Generate(b, 10)
	// "database" and "selection" should not match "fix typo in readme"
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses when no tokens match, got %d", len(hyps))
	}
}

func TestArchitectureDriftGenerator_MaxEnforcement(t *testing.T) {
	now := time.Now()
	// 8 ADRs, each with a unique keyword that matches a commit.
	refs := make([]baseline.GovernanceRef, 8)
	commits := make([]baseline.GitCommit, 8)
	for i := 0; i < 8; i++ {
		word := "keyword" + string(rune('a'+i)) + "stuff"
		refs[i] = baseline.GovernanceRef{
			ID:    "adr-" + string(rune('0'+i)),
			Title: word + " architecture decision",
			Kind:  "adr",
		}
		commits[i] = baseline.GitCommit{
			Hash:    "h" + string(rune('0'+i)),
			Subject: "update " + word + " module",
			Files:   []string{"pkg/module.go"},
			At:      now,
		}
	}
	b := baseline.Bundle{GovernanceRefs: refs, GitCommits: commits}
	g := &architectureDriftGenerator{}
	hyps := g.Generate(b, 3)
	if len(hyps) != 3 {
		t.Errorf("expected exactly 3 hypotheses (max enforcement), got %d", len(hyps))
	}
}
