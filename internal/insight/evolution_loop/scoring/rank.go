package scoring

import "github.com/MartinNevlaha/stratus-v2/config"

// Blend produces a final [0,1] score by combining static and LLM-judge signals
// using the weights defined in config.ScoringWeights.
//
// Effort inversion: LLMEffort weight is applied to (1 - l.Effort). This means
// a hypothesis that requires HIGH effort (l.Effort close to 1) contributes LESS
// to the final score — cheaper/quicker wins are ranked higher all else being equal.
//
// Formula:
//
//	static = w.Churn*s.Churn + w.TestGap*s.TestGap + w.TODO*s.TODO +
//	         w.Staleness*s.Staleness + w.ADRViolation*s.ADRViolation
//	llm    = w.LLMImpact*l.Impact + w.LLMEffort*(1-l.Effort) +
//	         w.LLMConfidence*l.Confidence + w.LLMNovelty*l.Novelty
//	final  = clamp01(static + llm)
func Blend(s StaticScores, l LLMScores, w config.ScoringWeights) Blended {
	churnContrib := w.Churn * s.Churn
	testGapContrib := w.TestGap * s.TestGap
	todoContrib := w.TODO * s.TODO
	stalenessContrib := w.Staleness * s.Staleness
	adrContrib := w.ADRViolation * s.ADRViolation

	impactContrib := w.LLMImpact * l.Impact
	// Effort is inverted: cheaper = higher score.
	effortInvContrib := w.LLMEffort * (1 - l.Effort)
	confidenceContrib := w.LLMConfidence * l.Confidence
	noveltyContrib := w.LLMNovelty * l.Novelty

	static := churnContrib + testGapContrib + todoContrib + stalenessContrib + adrContrib
	llm := impactContrib + effortInvContrib + confidenceContrib + noveltyContrib
	final := clamp01(static + llm)

	return Blended{
		Final:  final,
		Static: s,
		LLM:    l,
		Breakdown: map[string]float64{
			"churn":          churnContrib,
			"test_gap":       testGapContrib,
			"todo":           todoContrib,
			"staleness":      stalenessContrib,
			"adr_violation":  adrContrib,
			"llm_impact":     impactContrib,
			"llm_effort_inv": effortInvContrib,
			"llm_confidence": confidenceContrib,
			"llm_novelty":    noveltyContrib,
		},
	}
}
