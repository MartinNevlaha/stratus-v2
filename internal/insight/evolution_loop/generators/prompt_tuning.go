package generators

import (
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
)

// promptTuningGenerator is a Stratus-self opt-in generator. It always emits exactly
// one hypothesis suggesting a review of the insight engine prompt style. It is
// included in Registry only when stratusSelfEnabled=true.
type promptTuningGenerator struct{}

func (g *promptTuningGenerator) Category() string { return "prompt_tuning" }

func (g *promptTuningGenerator) Generate(_ baseline.Bundle, max int) []scoring.Hypothesis {
	if max <= 0 {
		return nil
	}
	return []scoring.Hypothesis{
		{
			Category:  "prompt_tuning",
			Title:     "Review insight engine prompt style",
			Rationale: "Periodic review of the evolution engine's prompt phrasing can improve hypothesis quality and reduce noise.",
			SignalRefs: []string{"self:prompt_review"},
		},
	}
}
