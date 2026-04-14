package scoring

import (
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
)

func zeroWeights() config.ScoringWeights {
	return config.ScoringWeights{}
}

func allOneWeights() config.ScoringWeights {
	return config.ScoringWeights{
		Churn:         1,
		TestGap:       1,
		TODO:          1,
		Staleness:     1,
		ADRViolation:  1,
		LLMImpact:     1,
		LLMEffort:     1,
		LLMConfidence: 1,
		LLMNovelty:    1,
	}
}

func TestBlend_ZeroWeights_FinalZero(t *testing.T) {
	s := StaticScores{Churn: 0.5, TestGap: 0.8, TODO: 0.6, Staleness: 0.7, ADRViolation: 0.3}
	l := LLMScores{Impact: 0.9, Effort: 0.5, Confidence: 0.8, Novelty: 0.7}
	b := Blend(s, l, zeroWeights())
	if b.Final != 0 {
		t.Errorf("ZeroWeights: got Final=%v, want 0", b.Final)
	}
}

func TestBlend_AllOneWeights_ClampedSum(t *testing.T) {
	s := StaticScores{Churn: 0.5, TestGap: 0.5, TODO: 0.5, Staleness: 0.5, ADRViolation: 0.5}
	l := LLMScores{Impact: 0.5, Effort: 0.0, Confidence: 0.5, Novelty: 0.5}
	b := Blend(s, l, allOneWeights())
	// static = 2.5, llm = 0.5+1.0+0.5+0.5 = 2.5, total = 5.0 → clamped to 1.0
	if b.Final != 1.0 {
		t.Errorf("AllOneWeights: got Final=%v, want 1.0", b.Final)
	}
}

func TestBlend_EffortInversion_ZeroEffort_FullContribution(t *testing.T) {
	w := config.ScoringWeights{LLMEffort: 0.5}
	s := StaticScores{}
	l := LLMScores{Effort: 0.0} // Effort=0 → contribution = 0.5*(1-0)=0.5
	b := Blend(s, l, w)
	want := 0.5
	if abs(b.Final-want) > 1e-9 {
		t.Errorf("EffortInversion Effort=0: got %v, want %v", b.Final, want)
	}
}

func TestBlend_EffortInversion_FullEffort_ZeroContribution(t *testing.T) {
	w := config.ScoringWeights{LLMEffort: 0.5}
	s := StaticScores{}
	l := LLMScores{Effort: 1.0} // Effort=1 → contribution = 0.5*(1-1)=0
	b := Blend(s, l, w)
	if b.Final != 0 {
		t.Errorf("EffortInversion Effort=1: got %v, want 0", b.Final)
	}
}

func TestBlend_BreakdownKeysPresent(t *testing.T) {
	b := Blend(StaticScores{}, LLMScores{}, zeroWeights())
	expectedKeys := []string{
		"churn", "test_gap", "todo", "staleness", "adr_violation",
		"llm_impact", "llm_effort_inv", "llm_confidence", "llm_novelty",
	}
	for _, k := range expectedKeys {
		if _, ok := b.Breakdown[k]; !ok {
			t.Errorf("Breakdown missing key %q", k)
		}
	}
}

func TestBlend_Monotonicity_IncreasingImpact(t *testing.T) {
	w := config.ScoringWeights{LLMImpact: 0.3}
	s := StaticScores{}
	low := Blend(s, LLMScores{Impact: 0.2}, w)
	high := Blend(s, LLMScores{Impact: 0.8}, w)
	if high.Final <= low.Final {
		t.Errorf("Monotonicity: increasing Impact should increase Final; low=%v high=%v", low.Final, high.Final)
	}
}

func TestBlend_Clamping_SumExceedsOne(t *testing.T) {
	// All signals high, all weights 1 → sum >> 1 → must clamp to 1.0
	s := StaticScores{Churn: 1, TestGap: 1, TODO: 1, Staleness: 1, ADRViolation: 1}
	l := LLMScores{Impact: 1, Effort: 0, Confidence: 1, Novelty: 1}
	b := Blend(s, l, allOneWeights())
	if b.Final != 1.0 {
		t.Errorf("Clamping: got %v, want 1.0", b.Final)
	}
}
