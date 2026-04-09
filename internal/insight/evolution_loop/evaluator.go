package evolution_loop

import (
	"math"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

// internalCategories are categories eligible for auto-apply without human review.
var internalCategories = map[string]bool{
	"workflow_routing":      true,
	"agent_selection":       true,
	"threshold_adjustment":  true,
}

// Evaluator decides the outcome of a hypothesis experiment.
type Evaluator struct {
	configFn func() config.EvolutionConfig
}

// NewEvaluator constructs an Evaluator.
func NewEvaluator(configFn func() config.EvolutionConfig) *Evaluator {
	return &Evaluator{configFn: configFn}
}

// Evaluate computes confidence and returns (decision, reason, confidence).
//
// Confidence formula:
//
//	sampleFactor = min(1.0, sqrt(sampleSize / minSampleSize))
//	effectRatio  = (experimentMetric - baselineMetric) / max(baselineMetric, 0.001)
//	confidence   = sampleFactor * effectRatio   (clamped to [0, 1])
//
// Decision rules (evaluated in order):
//  1. Sample too small                   → "inconclusive"
//  2. confidence >= AutoApplyThreshold
//     AND internal category              → "auto_applied"
//  3. confidence >= ProposalThreshold    → "proposal_created"
//  4. confidence > 0                     → "rejected"
//  5. otherwise                          → "rejected"
func (e *Evaluator) Evaluate(
	hypothesis *db.EvolutionHypothesis,
	result *ExperimentResult,
	cfg config.EvolutionConfig,
) (decision, reason string, confidence float64) {
	minSample := cfg.MinSampleSize
	if minSample <= 0 {
		minSample = 1
	}

	// Insufficient sample — report inconclusive before computing confidence.
	if result.SampleSize < minSample {
		return "inconclusive",
			"insufficient sample size: experiment did not collect enough data",
			0
	}

	sampleFactor := math.Min(1.0, math.Sqrt(float64(result.SampleSize)/float64(minSample)))

	baseline := hypothesis.BaselineMetric
	denom := math.Max(baseline, 0.001)
	effectRatio := (result.Metric - baseline) / denom

	confidence = sampleFactor * effectRatio
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}

	switch {
	case confidence >= cfg.AutoApplyThreshold && internalCategories[hypothesis.Category]:
		return "auto_applied",
			"confidence meets auto-apply threshold for internal category",
			confidence

	case confidence >= cfg.ProposalThreshold:
		return "proposal_created",
			"confidence meets proposal threshold; wiki page will be created",
			confidence

	default:
		return "rejected",
			"confidence below proposal threshold",
			confidence
	}
}
