package generators

import (
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
)

// Generator is the shared interface for all hypothesis generators.
// Implementations MUST be pure: no I/O, no LLM calls.
type Generator interface {
	// Category returns the hypothesis category this generator produces.
	Category() string
	// Generate produces up to max hypotheses from the provided bundle.
	// Returns min(candidates, max) results.
	Generate(b baseline.Bundle, max int) []scoring.Hypothesis
}

// allGenerators is the ordered list of all known generators (excluding prompt_tuning,
// which is conditional).
func allGenerators() []Generator {
	return []Generator{
		&refactorOpportunityGenerator{},
		&testGapGenerator{},
		&architectureDriftGenerator{},
		&featureIdeaGenerator{},
		&dxImprovementGenerator{},
		&docDriftGenerator{},
	}
}

// Registry returns all generators configured for the given allowed categories.
// Unknown categories are silently dropped. The prompt_tuning generator is only
// included when both "prompt_tuning" is in allowedCategories AND stratusSelfEnabled
// is true.
func Registry(allowedCategories []string, stratusSelfEnabled bool) []Generator {
	allowed := make(map[string]bool, len(allowedCategories))
	for _, c := range allowedCategories {
		allowed[c] = true
	}

	var result []Generator
	for _, g := range allGenerators() {
		if allowed[g.Category()] {
			result = append(result, g)
		}
	}

	if allowed["prompt_tuning"] && stratusSelfEnabled {
		result = append(result, &promptTuningGenerator{})
	}

	return result
}
