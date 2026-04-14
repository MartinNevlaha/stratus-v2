package scoring

import (
	"path/filepath"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

// StaticScorer computes deterministic, pure signal scores from a Hypothesis
// and a pre-built baseline.Bundle. All outputs are clamped to [0, 1].
// No I/O, no randomness.
type StaticScorer interface {
	Score(h Hypothesis, bundle baseline.Bundle) StaticScores
}

type staticScorer struct{}

// NewStaticScorer returns the default StaticScorer implementation.
func NewStaticScorer() StaticScorer {
	return &staticScorer{}
}

func (sc *staticScorer) Score(h Hypothesis, bundle baseline.Bundle) StaticScores {
	return StaticScores{
		Churn:        scoreChurn(h, bundle),
		TestGap:      scoreTestGap(h, bundle),
		TODO:         scoreTODO(h, bundle),
		Staleness:    scoreStaleness(h, bundle),
		ADRViolation: scoreADRViolation(h, bundle),
	}
}

// scoreChurn returns a 0..1 score based on how frequently files in h.FileRefs
// appear in bundle.GitCommits. 20 commits touching a file in 30d = max churn.
func scoreChurn(h Hypothesis, bundle baseline.Bundle) float64 {
	if len(h.FileRefs) == 0 {
		return 0
	}
	// Build lowercase set of FileRefs for O(1) lookup.
	refSet := make(map[string]struct{}, len(h.FileRefs))
	for _, f := range h.FileRefs {
		refSet[strings.ToLower(f)] = struct{}{}
	}
	// Count commits touching each file.
	counts := make(map[string]int, len(h.FileRefs))
	for _, c := range bundle.GitCommits {
		for _, f := range c.Files {
			lf := strings.ToLower(f)
			if _, ok := refSet[lf]; ok {
				counts[lf]++
			}
		}
	}
	if len(counts) == 0 {
		return 0
	}
	var maxCount int
	for _, n := range counts {
		if n > maxCount {
			maxCount = n
		}
	}
	return clamp01(float64(maxCount) / 20.0)
}

// scoreTestGap returns a 0..1 score where 1 means no tests at all.
// For each file in h.FileRefs it looks up the TestRatio for that file's
// top-level directory. The minimum ratio drives the score so the worst-covered
// area determines the gap.
func scoreTestGap(h Hypothesis, bundle baseline.Bundle) float64 {
	if len(h.FileRefs) == 0 {
		if h.Category == "test_gap" {
			return 0.5
		}
		return 0
	}
	// Build lowercase dir → ratio map.
	ratioMap := make(map[string]float64, len(bundle.TestRatios))
	for _, tr := range bundle.TestRatios {
		ratioMap[strings.ToLower(tr.Dir)] = tr.Ratio
	}
	minRatio := 1.0
	for _, f := range h.FileRefs {
		topDir := topLevelDir(f)
		ratio, ok := ratioMap[strings.ToLower(topDir)]
		if !ok {
			ratio = 0 // missing dir treated as 0 coverage
		}
		if ratio < minRatio {
			minRatio = ratio
		}
	}
	gap := 1.0 - minRatio
	if gap < 0 {
		gap = 0
	}
	return gap
}

// scoreTODO counts TODO items in bundle.TODOs that reference any file in h.FileRefs.
// 5 matching TODOs = score 1.0. A feature_idea hypothesis gets a +0.2 bonus if any
// TODOs reference the same file.
func scoreTODO(h Hypothesis, bundle baseline.Bundle) float64 {
	if len(h.FileRefs) == 0 {
		return 0
	}
	refSet := make(map[string]struct{}, len(h.FileRefs))
	for _, f := range h.FileRefs {
		refSet[strings.ToLower(f)] = struct{}{}
	}
	var count int
	for _, todo := range bundle.TODOs {
		if _, ok := refSet[strings.ToLower(todo.Path)]; ok {
			count++
		}
	}
	score := clamp01(float64(count) / 5.0)
	if h.Category == "feature_idea" && count > 0 {
		score = clamp01(score + 0.2)
	}
	return score
}

// scoreStaleness returns a 0..1 score based on how stale wiki pages that match
// any of h.SymbolRefs or h.Title keywords are. Falls back to half the global
// maximum staleness when no pages match.
func scoreStaleness(h Hypothesis, bundle baseline.Bundle) float64 {
	if len(bundle.WikiTitles) == 0 {
		return 0
	}
	// Build a set of lowercase keywords from SymbolRefs and title words.
	keywords := make(map[string]struct{})
	for _, sym := range h.SymbolRefs {
		kw := strings.ToLower(sym)
		if kw != "" {
			keywords[kw] = struct{}{}
		}
	}
	for _, word := range tokenize(h.Title) {
		if word != "" {
			keywords[word] = struct{}{}
		}
	}
	var maxMatched float64
	matched := false
	for _, w := range bundle.WikiTitles {
		wTitle := strings.ToLower(w.Title)
		for kw := range keywords {
			if strings.Contains(wTitle, kw) {
				if w.Staleness > maxMatched {
					maxMatched = w.Staleness
				}
				matched = true
				break
			}
		}
	}
	if matched {
		return clamp01(maxMatched)
	}
	// Global fallback: max staleness × 0.5.
	var globalTop float64
	for _, w := range bundle.WikiTitles {
		if w.Staleness > globalTop {
			globalTop = w.Staleness
		}
	}
	return clamp01(globalTop * 0.5)
}

// scoreADRViolation counts governance refs whose Title shares keywords (min length 4)
// with h.Title + h.Rationale. Capped at 3 matches; score = count/3.
func scoreADRViolation(h Hypothesis, bundle baseline.Bundle) float64 {
	if len(bundle.GovernanceRefs) == 0 {
		return 0
	}
	// Collect meaningful keywords (length >= 4) from title + rationale.
	combined := strings.ToLower(h.Title + " " + h.Rationale)
	words := tokenize(combined)
	keywords := make(map[string]struct{})
	for _, w := range words {
		if len(w) >= 4 {
			keywords[w] = struct{}{}
		}
	}
	if len(keywords) == 0 {
		return 0
	}
	var matchCount int
	for _, g := range bundle.GovernanceRefs {
		gTitle := strings.ToLower(g.Title)
		for kw := range keywords {
			if strings.Contains(gTitle, kw) {
				matchCount++
				break // one match per ref is enough
			}
		}
	}
	if matchCount > 3 {
		matchCount = 3
	}
	return float64(matchCount) / 3.0
}

// topLevelDir extracts the first directory component of a file path.
// "pkg/foo/bar.go" → "pkg"; "bar.go" → "."; "a/b.go" → "a".
func topLevelDir(filePath string) string {
	clean := filepath.Clean(filePath)
	parts := strings.SplitN(clean, string(filepath.Separator), 2)
	if len(parts) == 1 {
		return "."
	}
	return parts[0]
}

// tokenize splits a string into lowercase words on non-alphanumeric boundaries.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !('a' <= r && r <= 'z') && !('0' <= r && r <= '9')
	})
}
