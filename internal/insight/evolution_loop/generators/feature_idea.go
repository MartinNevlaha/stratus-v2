package generators

import (
	"fmt"
	"regexp"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
)

const (
	featureIdeaTODOTitleMaxLen = 80
	featureIdeaWikiStaleness   = 0.5
)

var forwardLookingRe = regexp.MustCompile(`(?i)\b(later|future|someday|todo[: ]?add|consider|would be nice|maybe)\b`)

type featureIdeaGenerator struct{}

func (g *featureIdeaGenerator) Category() string { return "feature_idea" }

func (g *featureIdeaGenerator) Generate(b baseline.Bundle, max int) []scoring.Hypothesis {
	if max <= 0 {
		return nil
	}

	var hyps []scoring.Hypothesis

	// Signal 1: Forward-looking TODOs.
	for _, todo := range b.TODOs {
		if len(hyps) >= max {
			break
		}
		if !forwardLookingRe.MatchString(todo.Text) {
			continue
		}
		title := todo.Text
		if len(title) > featureIdeaTODOTitleMaxLen {
			title = title[:featureIdeaTODOTitleMaxLen]
		}
		hyps = append(hyps, scoring.Hypothesis{
			Category:  "feature_idea",
			Title:     title,
			Rationale: fmt.Sprintf("Forward-looking TODO at %s:%d", todo.Path, todo.Line),
			FileRefs:  []string{todo.Path},
			SignalRefs: []string{
				fmt.Sprintf("todo:%s:%d", todo.Path, todo.Line),
			},
		})
	}

	// Signal 2: Stale wiki pages.
	for _, w := range b.WikiTitles {
		if len(hyps) >= max {
			break
		}
		if w.Staleness <= featureIdeaWikiStaleness {
			continue
		}
		hyps = append(hyps, scoring.Hypothesis{
			Category:  "feature_idea",
			Title:     fmt.Sprintf("Modernize %s", w.Title),
			Rationale: fmt.Sprintf("Wiki page '%s' has staleness %.2f; consider updating or expanding", w.Title, w.Staleness),
			FileRefs:  nil,
			SignalRefs: []string{
				fmt.Sprintf("wiki:%s", w.ID),
			},
		})
	}

	return hyps
}
