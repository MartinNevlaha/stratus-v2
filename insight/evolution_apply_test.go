package insight

import (
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

func TestApplyHypothesisToConfig_AutoApplyThreshold(t *testing.T) {
	cfg := config.Default()
	cfg.Evolution.AutoApplyThreshold = 0.85

	h := &db.EvolutionHypothesis{
		Category:      "threshold_adjustment",
		Metric:        "auto_apply_accuracy",
		ProposedValue: "0.80",
	}

	if err := applyHypothesisToConfig(&cfg, h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Evolution.AutoApplyThreshold != 0.80 {
		t.Errorf("expected AutoApplyThreshold=0.80, got %f", cfg.Evolution.AutoApplyThreshold)
	}
}

func TestApplyHypothesisToConfig_MinSampleSize(t *testing.T) {
	cfg := config.Default()
	cfg.Evolution.MinSampleSize = 10

	h := &db.EvolutionHypothesis{
		Category:      "threshold_adjustment",
		Metric:        "decision_reliability",
		ProposedValue: "15",
	}

	if err := applyHypothesisToConfig(&cfg, h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Evolution.MinSampleSize != 15 {
		t.Errorf("expected MinSampleSize=15, got %d", cfg.Evolution.MinSampleSize)
	}
}

func TestApplyHypothesisToConfig_RoutingAccuracy(t *testing.T) {
	cfg := config.Default()
	cfg.Insight.MinConfidence = 0.50

	h := &db.EvolutionHypothesis{
		Category:      "workflow_routing",
		Metric:        "routing_accuracy",
		ProposedValue: "0.75",
	}

	if err := applyHypothesisToConfig(&cfg, h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Insight.MinConfidence != 0.75 {
		t.Errorf("expected MinConfidence=0.75, got %f", cfg.Insight.MinConfidence)
	}
}

func TestApplyHypothesisToConfig_AgentSelection_NoError(t *testing.T) {
	cfg := config.Default()

	h := &db.EvolutionHypothesis{
		Category:      "agent_selection",
		Metric:        "task_success_rate",
		ProposedValue: "specialist_above_3_files",
	}

	// agent_selection has no config mapping yet — should not return error.
	if err := applyHypothesisToConfig(&cfg, h); err != nil {
		t.Errorf("expected no error for agent_selection, got: %v", err)
	}
}

func TestApplyHypothesisToConfig_UnknownCategory_ReturnsError(t *testing.T) {
	cfg := config.Default()

	h := &db.EvolutionHypothesis{
		Category:      "unknown_category",
		Metric:        "some_metric",
		ProposedValue: "42",
	}

	if err := applyHypothesisToConfig(&cfg, h); err == nil {
		t.Error("expected error for unknown category, got nil")
	}
}

func TestApplyHypothesisToConfig_InvalidFloat_ReturnsError(t *testing.T) {
	cfg := config.Default()

	h := &db.EvolutionHypothesis{
		Category:      "threshold_adjustment",
		Metric:        "auto_apply_accuracy",
		ProposedValue: "not_a_number",
	}

	if err := applyHypothesisToConfig(&cfg, h); err == nil {
		t.Error("expected error for invalid float value, got nil")
	}
}

func TestApplyHypothesisToConfig_ThresholdOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name     string
		category string
		metric   string
		value    string
	}{
		{"auto_apply_too_high", "threshold_adjustment", "auto_apply_accuracy", "1.5"},
		{"auto_apply_negative", "threshold_adjustment", "auto_apply_accuracy", "-0.1"},
		{"routing_too_high", "workflow_routing", "routing_accuracy", "2.0"},
		{"routing_negative", "workflow_routing", "routing_accuracy", "-0.5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			h := &db.EvolutionHypothesis{
				Category:      tc.category,
				Metric:        tc.metric,
				ProposedValue: tc.value,
			}
			if err := applyHypothesisToConfig(&cfg, h); err == nil {
				t.Errorf("expected error for out-of-range value %s, got nil", tc.value)
			}
		})
	}
}

func TestApplyHypothesisToConfig_MinSampleSizeTooLow(t *testing.T) {
	for _, val := range []string{"0", "-1", "-10"} {
		t.Run(val, func(t *testing.T) {
			cfg := config.Default()
			h := &db.EvolutionHypothesis{
				Category:      "threshold_adjustment",
				Metric:        "decision_reliability",
				ProposedValue: val,
			}
			if err := applyHypothesisToConfig(&cfg, h); err == nil {
				t.Errorf("expected error for MinSampleSize=%s, got nil", val)
			}
		})
	}
}

func TestFormatHypothesisForWiki(t *testing.T) {
	h := &db.EvolutionHypothesis{
		ID:              "hyp-123",
		Category:        "threshold_adjustment",
		Description:     "Reduce auto-apply threshold from 0.85 to 0.80",
		BaselineValue:   "0.85",
		ProposedValue:   "0.80",
		Metric:          "auto_apply_accuracy",
		BaselineMetric:  0.85,
		ExperimentMetric: 0.95,
		Confidence:      0.72,
		Decision:        "proposal_created",
		DecisionReason:  "confidence meets proposal threshold",
	}

	content := formatHypothesisForWiki(h)

	if content == "" {
		t.Fatal("expected non-empty wiki content")
	}
	// Check key sections are present.
	for _, want := range []string{"threshold_adjustment", "0.85", "0.80", "auto_apply_accuracy", "0.72"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected wiki content to contain %q", want)
		}
	}
}
