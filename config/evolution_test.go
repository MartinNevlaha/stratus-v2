package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeEvolutionConfig writes a .stratus.json with the given evolution JSON
// fragment into a temp dir, changes to it, and returns the dir.
func writeEvolutionConfig(t *testing.T, evolutionJSON string) string {
	t.Helper()
	dir := t.TempDir()
	raw := `{"evolution":` + evolutionJSON + `}`
	if err := os.WriteFile(filepath.Join(dir, ".stratus.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Chdir(dir)
	return dir
}

// TestEvolution_ValidConfigWithTokenCap verifies that a valid evolution config
// with MaxTokensPerCycle > 0 loads without error.
func TestEvolution_ValidConfigWithTokenCap(t *testing.T) {
	writeEvolutionConfig(t, `{
		"enabled": true,
		"max_tokens_per_cycle": 1000,
		"scoring_weights": {
			"churn": 0.2, "test_gap": 0.2, "todo": 0.1,
			"staleness": 0.1, "adr_violation": 0.1,
			"llm_impact": 0.15, "llm_effort": 0.05,
			"llm_confidence": 0.05, "llm_novelty": 0.05,
			"max_tokens_per_judge_call": 4000
		}
	}`)
	_, err := LoadAndValidate()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestEvolution_EnabledWithZeroTokenCap verifies that enabling evolution without
// MaxTokensPerCycle returns ErrTokenCapRequired.
func TestEvolution_EnabledWithZeroTokenCap(t *testing.T) {
	writeEvolutionConfig(t, `{"enabled": true, "max_tokens_per_cycle": 0}`)
	_, err := LoadAndValidate()
	if !errors.Is(err, ErrTokenCapRequired) {
		t.Errorf("want ErrTokenCapRequired, got: %v", err)
	}
}

// TestEvolution_ScoringWeightsChurnOutOfRange verifies that a weight > 1 fails.
func TestEvolution_ScoringWeightsChurnOutOfRange(t *testing.T) {
	writeEvolutionConfig(t, `{
		"enabled": true,
		"max_tokens_per_cycle": 1000,
		"scoring_weights": {
			"churn": 1.5, "max_tokens_per_judge_call": 4000
		}
	}`)
	_, err := LoadAndValidate()
	if !errors.Is(err, ErrInvalidScoringWeights) {
		t.Errorf("want ErrInvalidScoringWeights, got: %v", err)
	}
}

// TestEvolution_StaticWeightsSumExceedsOne verifies that static weight sum > 1.0 fails.
func TestEvolution_StaticWeightsSumExceedsOne(t *testing.T) {
	writeEvolutionConfig(t, `{
		"enabled": true,
		"max_tokens_per_cycle": 1000,
		"scoring_weights": {
			"churn": 0.4, "test_gap": 0.4, "todo": 0.4,
			"staleness": 0.0, "adr_violation": 0.0,
			"max_tokens_per_judge_call": 4000
		}
	}`)
	_, err := LoadAndValidate()
	if !errors.Is(err, ErrInvalidScoringWeights) {
		t.Errorf("want ErrInvalidScoringWeights, got: %v", err)
	}
}

// TestEvolution_MaxTokensPerJudgeCallZero verifies that MaxTokensPerJudgeCall=0 fails.
func TestEvolution_MaxTokensPerJudgeCallZero(t *testing.T) {
	writeEvolutionConfig(t, `{
		"enabled": true,
		"max_tokens_per_cycle": 1000,
		"scoring_weights": {
			"churn": 0.2, "test_gap": 0.2,
			"max_tokens_per_judge_call": 0
		}
	}`)
	_, err := LoadAndValidate()
	if !errors.Is(err, ErrInvalidScoringWeights) {
		t.Errorf("want ErrInvalidScoringWeights, got: %v", err)
	}
}

// TestEvolution_UnknownCategory verifies that an unknown category fails.
func TestEvolution_UnknownCategory(t *testing.T) {
	writeEvolutionConfig(t, `{
		"enabled": true,
		"max_tokens_per_cycle": 1000,
		"allowed_evolution_categories": ["bogus_cat"]
	}`)
	_, err := LoadAndValidate()
	if !errors.Is(err, ErrInvalidCategory) {
		t.Errorf("want ErrInvalidCategory, got: %v", err)
	}
}

// TestEvolution_PromptTuningWithoutStratusSelf verifies that prompt_tuning in
// the allowlist without StratusSelfEnabled=true fails.
func TestEvolution_PromptTuningWithoutStratusSelf(t *testing.T) {
	writeEvolutionConfig(t, `{
		"enabled": true,
		"max_tokens_per_cycle": 1000,
		"stratus_self_enabled": false,
		"allowed_evolution_categories": ["refactor_opportunity", "prompt_tuning"]
	}`)
	_, err := LoadAndValidate()
	if !errors.Is(err, ErrInvalidCategory) {
		t.Errorf("want ErrInvalidCategory, got: %v", err)
	}
}

// TestEvolution_PromptTuningWithStratusSelfEnabled verifies that prompt_tuning
// is accepted when StratusSelfEnabled=true.
func TestEvolution_PromptTuningWithStratusSelfEnabled(t *testing.T) {
	writeEvolutionConfig(t, `{
		"enabled": true,
		"max_tokens_per_cycle": 1000,
		"stratus_self_enabled": true,
		"allowed_evolution_categories": ["refactor_opportunity", "prompt_tuning"]
	}`)
	_, err := LoadAndValidate()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestEvolution_NegativeBaselineLimits verifies that negative BaselineLimits fails.
func TestEvolution_NegativeBaselineLimits(t *testing.T) {
	writeEvolutionConfig(t, `{
		"enabled": true,
		"max_tokens_per_cycle": 1000,
		"baseline_limits": {"vexor_top_k": -1}
	}`)
	_, err := LoadAndValidate()
	if !errors.Is(err, ErrInvalidBaselineLimits) {
		t.Errorf("want ErrInvalidBaselineLimits, got: %v", err)
	}
}

// TestEvolution_DefaultsApplied verifies that defaults are applied when fields
// are omitted from JSON (except MaxTokensPerCycle which must be explicit).
func TestEvolution_DefaultsApplied(t *testing.T) {
	// Write config with evolution disabled so no token cap needed.
	writeEvolutionConfig(t, `{"enabled": false}`)
	cfg := Load()
	ev := cfg.Evolution

	// ScoringWeights defaults
	sw := ev.ScoringWeights
	if sw.Churn != 0.2 {
		t.Errorf("ScoringWeights.Churn = %v, want 0.2", sw.Churn)
	}
	if sw.TestGap != 0.2 {
		t.Errorf("ScoringWeights.TestGap = %v, want 0.2", sw.TestGap)
	}
	if sw.TODO != 0.1 {
		t.Errorf("ScoringWeights.TODO = %v, want 0.1", sw.TODO)
	}
	if sw.Staleness != 0.1 {
		t.Errorf("ScoringWeights.Staleness = %v, want 0.1", sw.Staleness)
	}
	if sw.ADRViolation != 0.1 {
		t.Errorf("ScoringWeights.ADRViolation = %v, want 0.1", sw.ADRViolation)
	}
	if sw.LLMImpact != 0.15 {
		t.Errorf("ScoringWeights.LLMImpact = %v, want 0.15", sw.LLMImpact)
	}
	if sw.LLMEffort != 0.05 {
		t.Errorf("ScoringWeights.LLMEffort = %v, want 0.05", sw.LLMEffort)
	}
	if sw.LLMConfidence != 0.05 {
		t.Errorf("ScoringWeights.LLMConfidence = %v, want 0.05", sw.LLMConfidence)
	}
	if sw.LLMNovelty != 0.05 {
		t.Errorf("ScoringWeights.LLMNovelty = %v, want 0.05", sw.LLMNovelty)
	}
	if sw.MaxTokensPerJudgeCall != 4000 {
		t.Errorf("ScoringWeights.MaxTokensPerJudgeCall = %v, want 4000", sw.MaxTokensPerJudgeCall)
	}

	// BaselineLimits defaults
	bl := ev.BaselineLimits
	if bl.VexorTopK != 30 {
		t.Errorf("BaselineLimits.VexorTopK = %v, want 30", bl.VexorTopK)
	}
	if bl.GitLogCommits != 200 {
		t.Errorf("BaselineLimits.GitLogCommits = %v, want 200", bl.GitLogCommits)
	}
	if bl.TODOMax != 50 {
		t.Errorf("BaselineLimits.TODOMax = %v, want 50", bl.TODOMax)
	}

	// AllowedEvolutionCategories defaults
	wantCats := []string{
		"refactor_opportunity", "test_gap", "architecture_drift",
		"feature_idea", "dx_improvement", "doc_drift",
	}
	if len(ev.AllowedEvolutionCategories) != len(wantCats) {
		t.Errorf("AllowedEvolutionCategories len = %v, want %v", len(ev.AllowedEvolutionCategories), len(wantCats))
	} else {
		catSet := make(map[string]bool)
		for _, c := range ev.AllowedEvolutionCategories {
			catSet[c] = true
		}
		for _, w := range wantCats {
			if !catSet[w] {
				t.Errorf("AllowedEvolutionCategories missing %q", w)
			}
		}
	}

	// MaxTokensPerCycle must remain 0 (not defaulted — user must set explicitly).
	if ev.MaxTokensPerCycle != 0 {
		t.Errorf("MaxTokensPerCycle = %v, want 0 (must be set explicitly)", ev.MaxTokensPerCycle)
	}
}

// TestEvolution_JSONRoundTrip verifies that new fields survive a marshal/unmarshal cycle.
func TestEvolution_JSONRoundTrip(t *testing.T) {
	cfg := Default()
	cfg.Evolution.MaxTokensPerCycle = 5000
	cfg.Evolution.StratusSelfEnabled = true
	cfg.Evolution.ScoringWeights.Churn = 0.3

	data, err := json.Marshal(cfg.Evolution)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got EvolutionConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.MaxTokensPerCycle != 5000 {
		t.Errorf("MaxTokensPerCycle = %v, want 5000", got.MaxTokensPerCycle)
	}
	if !got.StratusSelfEnabled {
		t.Errorf("StratusSelfEnabled = false, want true")
	}
	if got.ScoringWeights.Churn != 0.3 {
		t.Errorf("ScoringWeights.Churn = %v, want 0.3", got.ScoringWeights.Churn)
	}
}
