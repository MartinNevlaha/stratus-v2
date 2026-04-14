package generators

import (
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

func TestDXImprovementGenerator_EmptyBundle(t *testing.T) {
	g := &dxImprovementGenerator{}
	hyps := g.Generate(baseline.Bundle{}, 10)
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses for empty bundle, got %d", len(hyps))
	}
}

func TestDXImprovementGenerator_Category(t *testing.T) {
	g := &dxImprovementGenerator{}
	if g.Category() != "dx_improvement" {
		t.Errorf("expected category dx_improvement, got %q", g.Category())
	}
}

func TestDXImprovementGenerator_RepeatedCommits(t *testing.T) {
	now := time.Now()
	// Same subject repeated 3 times — signals painful manual process.
	b := baseline.Bundle{
		GitCommits: []baseline.GitCommit{
			{Hash: "a1", Subject: "bump version manually", Files: []string{"version.go"}, At: now},
			{Hash: "a2", Subject: "bump version manually", Files: []string{"version.go"}, At: now},
			{Hash: "a3", Subject: "bump version manually", Files: []string{"version.go"}, At: now},
		},
	}
	g := &dxImprovementGenerator{}
	hyps := g.Generate(b, 10)
	if len(hyps) == 0 {
		t.Fatal("expected at least 1 hypothesis from repeated commits, got 0")
	}
	if hyps[0].Category != "dx_improvement" {
		t.Errorf("expected category dx_improvement, got %q", hyps[0].Category)
	}
}

func TestDXImprovementGenerator_DXTODOs(t *testing.T) {
	b := baseline.Bundle{
		TODOs: []baseline.TODOItem{
			{Path: "Makefile", Line: 5, Text: "TODO: this build step is slow", Kind: "TODO"},
			{Path: "ci/deploy.sh", Line: 10, Text: "TODO: flaky test workaround", Kind: "TODO"},
			{Path: "api/handler.go", Line: 20, Text: "TODO: add validation", Kind: "TODO"}, // no DX keyword
		},
	}
	g := &dxImprovementGenerator{}
	hyps := g.Generate(b, 10)
	if len(hyps) < 1 {
		t.Errorf("expected at least 1 hypothesis from DX TODOs, got %d", len(hyps))
	}
}

func TestDXImprovementGenerator_MaxEnforcement(t *testing.T) {
	now := time.Now()
	// 8 distinct repeated commit subjects.
	var commits []baseline.GitCommit
	for i := 0; i < 8; i++ {
		subj := "rebuild ci pipeline step " + string(rune('a'+i))
		for j := 0; j < 3; j++ {
			commits = append(commits, baseline.GitCommit{
				Hash:    "h" + string(rune('a'+i)) + string(rune('0'+j)),
				Subject: subj,
				Files:   []string{"Makefile"},
				At:      now,
			})
		}
	}
	b := baseline.Bundle{GitCommits: commits}
	g := &dxImprovementGenerator{}
	hyps := g.Generate(b, 3)
	if len(hyps) != 3 {
		t.Errorf("expected exactly 3 hypotheses (max enforcement), got %d", len(hyps))
	}
}
