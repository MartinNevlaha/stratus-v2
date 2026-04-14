package generators

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
)

const dxRepeatThreshold = 2

var dxTODORe = regexp.MustCompile(`(?i)\b(build|ci|makefile|dx|dev[-_ ]?experience|slow|flaky)\b`)

type dxImprovementGenerator struct{}

func (g *dxImprovementGenerator) Category() string { return "dx_improvement" }

func (g *dxImprovementGenerator) Generate(b baseline.Bundle, max int) []scoring.Hypothesis {
	if max <= 0 {
		return nil
	}

	var hyps []scoring.Hypothesis

	// Signal 1: Repeated identical commit subjects.
	subjectCounts := make(map[string]int)
	subjectFiles := make(map[string]map[string]bool)
	for _, c := range b.GitCommits {
		subjectCounts[c.Subject]++
		if subjectFiles[c.Subject] == nil {
			subjectFiles[c.Subject] = make(map[string]bool)
		}
		for _, f := range c.Files {
			subjectFiles[c.Subject][f] = true
		}
	}

	// Collect repeated subjects (count > dxRepeatThreshold), sorted for stability.
	type subjectCandidate struct {
		subject string
		count   int
	}
	var repeated []subjectCandidate
	for subj, n := range subjectCounts {
		if n > dxRepeatThreshold {
			repeated = append(repeated, subjectCandidate{subject: subj, count: n})
		}
	}
	sort.Slice(repeated, func(i, j int) bool {
		if repeated[i].count != repeated[j].count {
			return repeated[i].count > repeated[j].count
		}
		return repeated[i].subject < repeated[j].subject
	})

	for _, rc := range repeated {
		if len(hyps) >= max {
			break
		}
		var files []string
		for f := range subjectFiles[rc.subject] {
			files = append(files, f)
		}
		sort.Strings(files)
		hyps = append(hyps, scoring.Hypothesis{
			Category:  "dx_improvement",
			Title:     fmt.Sprintf("Automate repetitive task: %q", rc.subject),
			Rationale: fmt.Sprintf("Commit subject %q appeared %d times — likely a painful manual process", rc.subject, rc.count),
			FileRefs:  files,
			SignalRefs: []string{
				fmt.Sprintf("repeated_commit:%s:%d", rc.subject, rc.count),
			},
		})
	}

	// Signal 2: TODOs mentioning build/CI/dev keywords.
	for _, todo := range b.TODOs {
		if len(hyps) >= max {
			break
		}
		if !dxTODORe.MatchString(todo.Text) {
			continue
		}
		hyps = append(hyps, scoring.Hypothesis{
			Category:  "dx_improvement",
			Title:     fmt.Sprintf("DX issue: %s", truncate(todo.Text, 80)),
			Rationale: fmt.Sprintf("TODO at %s:%d references build/CI/dev experience concern", todo.Path, todo.Line),
			FileRefs:  []string{todo.Path},
			SignalRefs: []string{
				fmt.Sprintf("dx_todo:%s:%d", todo.Path, todo.Line),
			},
		})
	}

	return hyps
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
