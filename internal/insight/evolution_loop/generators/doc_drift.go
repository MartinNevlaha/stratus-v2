package generators

import (
	"fmt"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
)

const docDriftStalenessThreshold = 0.7

type docDriftGenerator struct{}

func (g *docDriftGenerator) Category() string { return "doc_drift" }

func (g *docDriftGenerator) Generate(b baseline.Bundle, max int) []scoring.Hypothesis {
	if max <= 0 {
		return nil
	}

	var hyps []scoring.Hypothesis

	for _, w := range b.WikiTitles {
		if len(hyps) >= max {
			break
		}
		if w.Staleness <= docDriftStalenessThreshold {
			continue
		}
		hyps = append(hyps, scoring.Hypothesis{
			Category:  "doc_drift",
			Title:     fmt.Sprintf("Refresh wiki: %s", w.Title),
			Rationale: fmt.Sprintf("Wiki staleness %.2f; content likely outdated", w.Staleness),
			SignalRefs: []string{
				fmt.Sprintf("wiki:%s:%.4f", w.ID, w.Staleness),
			},
		})
	}

	return hyps
}
