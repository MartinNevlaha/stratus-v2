package generators

import (
	"fmt"
	"sort"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
)

const testGapRatioThreshold = 0.3

type testGapGenerator struct{}

func (g *testGapGenerator) Category() string { return "test_gap" }

func (g *testGapGenerator) Generate(b baseline.Bundle, max int) []scoring.Hypothesis {
	if max <= 0 {
		return nil
	}

	type candidate struct {
		ratio baseline.TestRatio
	}
	var candidates []candidate
	for _, r := range b.TestRatios {
		if r.SourceFiles == 0 {
			continue
		}
		if r.Ratio < testGapRatioThreshold {
			candidates = append(candidates, candidate{ratio: r})
		}
	}

	// Sort by ratio ascending (worst coverage first), then dir for stability.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ratio.Ratio != candidates[j].ratio.Ratio {
			return candidates[i].ratio.Ratio < candidates[j].ratio.Ratio
		}
		return candidates[i].ratio.Dir < candidates[j].ratio.Dir
	})

	if len(candidates) > max {
		candidates = candidates[:max]
	}

	hyps := make([]scoring.Hypothesis, 0, len(candidates))
	for _, c := range candidates {
		hyps = append(hyps, scoring.Hypothesis{
			Category:  "test_gap",
			Title:     fmt.Sprintf("Low test coverage: %s", c.ratio.Dir),
			Rationale: fmt.Sprintf("Directory %q has %.0f%% test ratio (%d source / %d test files)", c.ratio.Dir, c.ratio.Ratio*100, c.ratio.SourceFiles, c.ratio.TestFiles),
			FileRefs:  []string{c.ratio.Dir},
			SignalRefs: []string{
				fmt.Sprintf("test_ratio:%s:%.4f", c.ratio.Dir, c.ratio.Ratio),
			},
		})
	}
	return hyps
}
