package generators

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
)

const (
	adrMinTokenLen    = 4
	adrMaxFileRefs    = 5
	adrMaxCommitHashes = 3
)

type architectureDriftGenerator struct{}

func (g *architectureDriftGenerator) Category() string { return "architecture_drift" }

func (g *architectureDriftGenerator) Generate(b baseline.Bundle, max int) []scoring.Hypothesis {
	if max <= 0 {
		return nil
	}

	var hyps []scoring.Hypothesis

	for _, ref := range b.GovernanceRefs {
		if ref.Kind != "adr" && ref.Kind != "rule" {
			continue
		}

		tokens := extractTokens(ref.Title)
		if len(tokens) == 0 {
			continue
		}

		// Find commits whose subject contains at least one token.
		var matchedCommits []baseline.GitCommit
		for _, c := range b.GitCommits {
			subjectLower := strings.ToLower(c.Subject)
			for _, tok := range tokens {
				if strings.Contains(subjectLower, tok) {
					matchedCommits = append(matchedCommits, c)
					break
				}
			}
		}

		if len(matchedCommits) == 0 {
			continue
		}

		// Collect unique files (capped at adrMaxFileRefs).
		fileSet := make(map[string]bool)
		var fileRefs []string
		for _, c := range matchedCommits {
			for _, f := range c.Files {
				if !fileSet[f] {
					fileSet[f] = true
					fileRefs = append(fileRefs, f)
				}
			}
		}
		sort.Strings(fileRefs)
		if len(fileRefs) > adrMaxFileRefs {
			fileRefs = fileRefs[:adrMaxFileRefs]
		}

		// Collect hashes (max adrMaxCommitHashes).
		hashes := make([]string, 0, adrMaxCommitHashes)
		for i, c := range matchedCommits {
			if i >= adrMaxCommitHashes {
				break
			}
			hashes = append(hashes, c.Hash)
		}
		hashStr := strings.Join(hashes, ",")

		hyps = append(hyps, scoring.Hypothesis{
			Category:  "architecture_drift",
			Title:     fmt.Sprintf("Possible ADR drift: %s", ref.Title),
			Rationale: fmt.Sprintf("Recent commits reference terms from '%s'; verify alignment", ref.Title),
			FileRefs:  fileRefs,
			SignalRefs: []string{
				fmt.Sprintf("adr:%s", ref.ID),
				fmt.Sprintf("commits:%s", hashStr),
			},
		})

		if len(hyps) >= max {
			break
		}
	}

	return hyps
}

// extractTokens splits a governance title into lowercase tokens of length >= adrMinTokenLen.
func extractTokens(title string) []string {
	words := strings.Fields(strings.ToLower(title))
	var tokens []string
	seen := make(map[string]bool)
	for _, w := range words {
		// Strip punctuation from edges.
		w = strings.Trim(w, ".,;:!?\"'()")
		if len(w) >= adrMinTokenLen && !seen[w] {
			seen[w] = true
			tokens = append(tokens, w)
		}
	}
	return tokens
}
