package generators

import (
	"testing"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

func TestPromptTuningGenerator_Category(t *testing.T) {
	g := &promptTuningGenerator{}
	if g.Category() != "prompt_tuning" {
		t.Errorf("expected category prompt_tuning, got %q", g.Category())
	}
}

func TestPromptTuningGenerator_EmptyBundle(t *testing.T) {
	g := &promptTuningGenerator{}
	hyps := g.Generate(baseline.Bundle{}, 10)
	// Should always emit exactly 1 hypothesis regardless of bundle content.
	if len(hyps) != 1 {
		t.Errorf("expected 1 hypothesis from prompt_tuning, got %d", len(hyps))
	}
	if hyps[0].Category != "prompt_tuning" {
		t.Errorf("expected category prompt_tuning, got %q", hyps[0].Category)
	}
}

func TestPromptTuningGenerator_MaxZero(t *testing.T) {
	g := &promptTuningGenerator{}
	hyps := g.Generate(baseline.Bundle{}, 0)
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses when max=0, got %d", len(hyps))
	}
}

func TestPromptTuningGenerator_RealisticBundle(t *testing.T) {
	b := baseline.Bundle{
		TODOs: []baseline.TODOItem{
			{Path: "foo.go", Line: 1, Text: "TODO: something", Kind: "TODO"},
		},
	}
	g := &promptTuningGenerator{}
	hyps := g.Generate(b, 5)
	if len(hyps) != 1 {
		t.Errorf("expected exactly 1 hypothesis, got %d", len(hyps))
	}
}
