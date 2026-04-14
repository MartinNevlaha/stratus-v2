package code_analyst

import (
	"math"
	"sort"
)

const (
	defaultMaxFiles        = 10
	defaultMinChurnScore   = 0.1
	defaultCoverageDefault = 0.5
	defaultComplexityCap   = 3.0
)

// RankerConfig holds parameters for file ranking.
type RankerConfig struct {
	MaxFiles        int     // top-K to return (default: 10)
	MinChurnScore   float64 // skip files below this threshold (default: 0.1)
	CoverageDefault float64 // used when coverage is unknown (default: 0.5)
	ComplexityCap   float64 // max complexity proxy value (default: 3.0)
}

// Ranker scores and ranks files by composite risk score.
type Ranker struct {
	config RankerConfig
}

// NewRanker constructs a Ranker with the given config, applying sensible defaults
// for any zero or invalid values.
func NewRanker(config RankerConfig) *Ranker {
	if config.MaxFiles <= 0 {
		config.MaxFiles = defaultMaxFiles
	}
	if config.MinChurnScore <= 0 {
		config.MinChurnScore = defaultMinChurnScore
	}
	if config.CoverageDefault <= 0 {
		config.CoverageDefault = defaultCoverageDefault
	}
	if config.ComplexityCap <= 0 {
		config.ComplexityCap = defaultComplexityCap
	}
	return &Ranker{config: config}
}

// Rank scores the given file signals and returns the top-K files sorted by
// composite score descending.
// coverageMap maps file paths to their test coverage (0.0–1.0). Missing entries
// use config.CoverageDefault.
func (r *Ranker) Rank(signals []FileSignals, coverageMap map[string]float64) []FileScore {
	if len(signals) == 0 {
		return []FileScore{}
	}

	// Step 1: filter out test files and find max commit count.
	nonTest := make([]FileSignals, 0, len(signals))
	maxCommits := 0
	for _, s := range signals {
		if s.TestFile {
			continue
		}
		nonTest = append(nonTest, s)
		if s.CommitCount > maxCommits {
			maxCommits = s.CommitCount
		}
	}

	if len(nonTest) == 0 {
		return []FileScore{}
	}

	// Steps 2–5: compute scores.
	scored := make([]FileScore, 0, len(nonTest))
	for _, s := range nonTest {
		// Step 2: normalize churn.
		churnRate := 0.0
		if maxCommits > 0 {
			churnRate = float64(s.CommitCount) / float64(maxCommits)
		}

		// Step 3: lookup coverage.
		coverage := r.config.CoverageDefault
		if coverageMap != nil {
			if cov, ok := coverageMap[s.FilePath]; ok {
				coverage = cov
			}
		}

		// Step 4: compute complexity proxy.
		complexityProxy := math.Min(float64(s.LineCount)/100.0, r.config.ComplexityCap)

		// Step 5: compute composite score.
		composite := churnRate * (1.0 - coverage) * complexityProxy

		scored = append(scored, FileScore{
			FilePath:        s.FilePath,
			ChurnRate:       churnRate,
			Coverage:        coverage,
			ComplexityProxy: complexityProxy,
			CompositeScore:  composite,
			CommitCount:     s.CommitCount,
			LineCount:       s.LineCount,
			TechDebtMarkers: s.TechDebtMarkers,
		})
	}

	// Step 6: filter by MinChurnScore.
	filtered := scored[:0]
	for _, fs := range scored {
		if fs.CompositeScore >= r.config.MinChurnScore {
			filtered = append(filtered, fs)
		}
	}

	if len(filtered) == 0 {
		return []FileScore{}
	}

	// Step 7: sort descending by composite score.
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CompositeScore > filtered[j].CompositeScore
	})

	// Step 8: return top MaxFiles entries.
	if len(filtered) > r.config.MaxFiles {
		filtered = filtered[:r.config.MaxFiles]
	}

	return filtered
}
