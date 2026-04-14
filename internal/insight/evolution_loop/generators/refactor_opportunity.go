package generators

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
)

const refactorChurnThreshold = 3

type refactorOpportunityGenerator struct{}

func (g *refactorOpportunityGenerator) Category() string { return "refactor_opportunity" }

func (g *refactorOpportunityGenerator) Generate(b baseline.Bundle, max int) []scoring.Hypothesis {
	if max <= 0 {
		return nil
	}

	// Count commits per file.
	churn := make(map[string]int)
	for _, c := range b.GitCommits {
		for _, f := range c.Files {
			churn[f]++
		}
	}

	// Count TODOs per file.
	todoCounts := make(map[string]int)
	for _, t := range b.TODOs {
		todoCounts[t.Path]++
	}

	// Collect files above threshold.
	type candidate struct {
		file  string
		count int
	}
	var candidates []candidate
	for f, n := range churn {
		if n >= refactorChurnThreshold {
			candidates = append(candidates, candidate{file: f, count: n})
		}
	}

	// Sort by commit count descending, then by file name for stability.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].count != candidates[j].count {
			return candidates[i].count > candidates[j].count
		}
		return candidates[i].file < candidates[j].file
	})

	if len(candidates) > max {
		candidates = candidates[:max]
	}

	hyps := make([]scoring.Hypothesis, 0, len(candidates))
	for _, c := range candidates {
		todoN := todoCounts[c.file]
		signals := []string{
			fmt.Sprintf("churn:%s", c.file),
			fmt.Sprintf("todo_count:%d", todoN),
		}
		sort.Strings(signals)

		hyps = append(hyps, scoring.Hypothesis{
			Category:  "refactor_opportunity",
			Title:     fmt.Sprintf("Refactor hotspot: %s", filepath.Base(c.file)),
			Rationale: fmt.Sprintf("File touched by %d commits in last 30 days; %d TODOs present", c.count, todoN),
			FileRefs:  []string{c.file},
			SignalRefs: signals,
		})
	}
	return hyps
}
