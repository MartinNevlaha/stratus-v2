package generators

import (
	"testing"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

func TestTestGapGenerator_EmptyBundle(t *testing.T) {
	g := &testGapGenerator{}
	hyps := g.Generate(baseline.Bundle{}, 10)
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses for empty bundle, got %d", len(hyps))
	}
}

func TestTestGapGenerator_Category(t *testing.T) {
	g := &testGapGenerator{}
	if g.Category() != "test_gap" {
		t.Errorf("expected category test_gap, got %q", g.Category())
	}
}

func TestTestGapGenerator_HappyPath(t *testing.T) {
	b := baseline.Bundle{
		TestRatios: []baseline.TestRatio{
			{Dir: "api", SourceFiles: 10, TestFiles: 1, Ratio: 0.1},
			{Dir: "db", SourceFiles: 5, TestFiles: 4, Ratio: 0.8},
			{Dir: "cmd", SourceFiles: 3, TestFiles: 0, Ratio: 0.0},
		},
	}
	g := &testGapGenerator{}
	hyps := g.Generate(b, 10)
	// Should emit for api (0.1 < 0.3) and cmd (0.0 < 0.3), not db (0.8).
	if len(hyps) != 2 {
		t.Errorf("expected 2 hypotheses, got %d", len(hyps))
	}
	for _, h := range hyps {
		if h.Category != "test_gap" {
			t.Errorf("expected category test_gap, got %q", h.Category)
		}
	}
}

func TestTestGapGenerator_SkipsZeroSourceFiles(t *testing.T) {
	b := baseline.Bundle{
		TestRatios: []baseline.TestRatio{
			{Dir: "empty", SourceFiles: 0, TestFiles: 0, Ratio: 0.0},
		},
	}
	g := &testGapGenerator{}
	hyps := g.Generate(b, 10)
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses when SourceFiles=0, got %d", len(hyps))
	}
}

func TestTestGapGenerator_MaxEnforcement(t *testing.T) {
	ratios := make([]baseline.TestRatio, 8)
	for i := range ratios {
		ratios[i] = baseline.TestRatio{
			Dir:         "dir" + string(rune('a'+i)),
			SourceFiles: 10,
			TestFiles:   0,
			Ratio:       0.0,
		}
	}
	b := baseline.Bundle{TestRatios: ratios}
	g := &testGapGenerator{}
	hyps := g.Generate(b, 3)
	if len(hyps) != 3 {
		t.Errorf("expected exactly 3 hypotheses (max enforcement), got %d", len(hyps))
	}
}
