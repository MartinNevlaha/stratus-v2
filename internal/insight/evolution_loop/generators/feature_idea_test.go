package generators

import (
	"testing"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

func TestFeatureIdeaGenerator_EmptyBundle(t *testing.T) {
	g := &featureIdeaGenerator{}
	hyps := g.Generate(baseline.Bundle{}, 10)
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses for empty bundle, got %d", len(hyps))
	}
}

func TestFeatureIdeaGenerator_Category(t *testing.T) {
	g := &featureIdeaGenerator{}
	if g.Category() != "feature_idea" {
		t.Errorf("expected category feature_idea, got %q", g.Category())
	}
}

func TestFeatureIdeaGenerator_ForwardLookingTODOs(t *testing.T) {
	b := baseline.Bundle{
		TODOs: []baseline.TODOItem{
			{Path: "api/handler.go", Line: 10, Text: "TODO: maybe add rate limiting later", Kind: "TODO"},
			{Path: "api/handler.go", Line: 20, Text: "TODO: consider adding retry logic", Kind: "TODO"},
			{Path: "db/store.go", Line: 5, Text: "TODO: fix null pointer bug", Kind: "TODO"}, // no forward-looking keyword
		},
	}
	g := &featureIdeaGenerator{}
	hyps := g.Generate(b, 10)
	// Should match "maybe" and "later" in first, "consider" in second, not third.
	if len(hyps) < 2 {
		t.Errorf("expected at least 2 hypotheses from forward-looking TODOs, got %d", len(hyps))
	}
	for _, h := range hyps {
		if h.Category != "feature_idea" {
			t.Errorf("expected category feature_idea, got %q", h.Category)
		}
	}
}

func TestFeatureIdeaGenerator_StaleWiki(t *testing.T) {
	b := baseline.Bundle{
		WikiTitles: []baseline.WikiTitle{
			{ID: "wiki-1", Title: "API Design Guide", Staleness: 0.6},
			{ID: "wiki-2", Title: "Getting Started", Staleness: 0.3}, // below 0.5
		},
	}
	g := &featureIdeaGenerator{}
	hyps := g.Generate(b, 10)
	// Only wiki-1 has staleness > 0.5.
	if len(hyps) != 1 {
		t.Errorf("expected 1 hypothesis from stale wiki, got %d", len(hyps))
	}
	if hyps[0].Category != "feature_idea" {
		t.Errorf("expected category feature_idea, got %q", hyps[0].Category)
	}
}

func TestFeatureIdeaGenerator_MaxEnforcement(t *testing.T) {
	todos := make([]baseline.TODOItem, 10)
	for i := range todos {
		todos[i] = baseline.TODOItem{
			Path: "file.go",
			Line: i + 1,
			Text: "TODO: someday add feature " + string(rune('a'+i)),
			Kind: "TODO",
		}
	}
	b := baseline.Bundle{TODOs: todos}
	g := &featureIdeaGenerator{}
	hyps := g.Generate(b, 3)
	if len(hyps) != 3 {
		t.Errorf("expected exactly 3 hypotheses (max enforcement), got %d", len(hyps))
	}
}
