package generators

import (
	"testing"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

func TestDocDriftGenerator_EmptyBundle(t *testing.T) {
	g := &docDriftGenerator{}
	hyps := g.Generate(baseline.Bundle{}, 10)
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses for empty bundle, got %d", len(hyps))
	}
}

func TestDocDriftGenerator_Category(t *testing.T) {
	g := &docDriftGenerator{}
	if g.Category() != "doc_drift" {
		t.Errorf("expected category doc_drift, got %q", g.Category())
	}
}

func TestDocDriftGenerator_HappyPath(t *testing.T) {
	b := baseline.Bundle{
		WikiTitles: []baseline.WikiTitle{
			{ID: "w1", Title: "API Overview", Staleness: 0.8},
			{ID: "w2", Title: "Getting Started", Staleness: 0.75},
			{ID: "w3", Title: "Changelog", Staleness: 0.5}, // below 0.7
		},
	}
	g := &docDriftGenerator{}
	hyps := g.Generate(b, 10)
	if len(hyps) != 2 {
		t.Errorf("expected 2 hypotheses (staleness > 0.7), got %d", len(hyps))
	}
	for _, h := range hyps {
		if h.Category != "doc_drift" {
			t.Errorf("expected category doc_drift, got %q", h.Category)
		}
		if len(h.SignalRefs) == 0 {
			t.Error("expected SignalRefs to be non-empty")
		}
	}
}

func TestDocDriftGenerator_BelowThreshold(t *testing.T) {
	b := baseline.Bundle{
		WikiTitles: []baseline.WikiTitle{
			{ID: "w1", Title: "Fresh Doc", Staleness: 0.3},
		},
	}
	g := &docDriftGenerator{}
	hyps := g.Generate(b, 10)
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses when all staleness < 0.7, got %d", len(hyps))
	}
}

func TestDocDriftGenerator_MaxEnforcement(t *testing.T) {
	wikis := make([]baseline.WikiTitle, 8)
	for i := range wikis {
		wikis[i] = baseline.WikiTitle{
			ID:        "w" + string(rune('0'+i)),
			Title:     "Page " + string(rune('A'+i)),
			Staleness: 0.9,
		}
	}
	b := baseline.Bundle{WikiTitles: wikis}
	g := &docDriftGenerator{}
	hyps := g.Generate(b, 3)
	if len(hyps) != 3 {
		t.Errorf("expected exactly 3 hypotheses (max enforcement), got %d", len(hyps))
	}
}
