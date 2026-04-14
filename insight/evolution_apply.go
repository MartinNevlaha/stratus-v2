package insight

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

// applyHypothesisToConfig maps a hypothesis's proposed value to the
// corresponding config field. It modifies cfg in place.
func applyHypothesisToConfig(cfg *config.Config, h *db.EvolutionHypothesis) error {
	switch h.Category {
	case "threshold_adjustment":
		return applyThresholdAdjustment(cfg, h)
	case "workflow_routing":
		return applyWorkflowRouting(cfg, h)
	case "agent_selection":
		// No config mapping yet — record in log, no error.
		return nil
	default:
		return fmt.Errorf("apply hypothesis: unknown category %q", h.Category)
	}
}

func applyThresholdAdjustment(cfg *config.Config, h *db.EvolutionHypothesis) error {
	switch h.Metric {
	case "auto_apply_accuracy":
		v, err := strconv.ParseFloat(h.ProposedValue, 64)
		if err != nil {
			return fmt.Errorf("apply threshold_adjustment: parse auto_apply_accuracy %q: %w", h.ProposedValue, err)
		}
		if v < 0 || v > 1 {
			return fmt.Errorf("apply threshold_adjustment: auto_apply_accuracy %f out of range [0, 1]", v)
		}
		cfg.Evolution.AutoApplyThreshold = v
		return nil
	case "decision_reliability":
		v, err := strconv.Atoi(h.ProposedValue)
		if err != nil {
			return fmt.Errorf("apply threshold_adjustment: parse decision_reliability %q: %w", h.ProposedValue, err)
		}
		if v < 1 {
			return fmt.Errorf("apply threshold_adjustment: decision_reliability %d must be >= 1", v)
		}
		cfg.Evolution.MinSampleSize = v
		return nil
	default:
		return fmt.Errorf("apply threshold_adjustment: unknown metric %q", h.Metric)
	}
}

func applyWorkflowRouting(cfg *config.Config, h *db.EvolutionHypothesis) error {
	switch h.Metric {
	case "routing_accuracy":
		v, err := strconv.ParseFloat(h.ProposedValue, 64)
		if err != nil {
			return fmt.Errorf("apply workflow_routing: parse routing_accuracy %q: %w", h.ProposedValue, err)
		}
		if v < 0 || v > 1 {
			return fmt.Errorf("apply workflow_routing: routing_accuracy %f out of range [0, 1]", v)
		}
		cfg.Insight.MinConfidence = v
		return nil
	default:
		return fmt.Errorf("apply workflow_routing: unknown metric %q", h.Metric)
	}
}

// formatHypothesisForWiki produces markdown content for a wiki page
// summarising an evolution experiment finding.
func formatHypothesisForWiki(h *db.EvolutionHypothesis) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Evolution Finding: %s\n\n", h.Description)
	sb.WriteString("## Experiment Details\n\n")
	sb.WriteString("| Field | Value |\n")
	sb.WriteString("|-------|-------|\n")
	fmt.Fprintf(&sb, "| Category | `%s` |\n", h.Category)
	fmt.Fprintf(&sb, "| Metric | `%s` |\n", h.Metric)
	fmt.Fprintf(&sb, "| Baseline Value | %s |\n", h.BaselineValue)
	fmt.Fprintf(&sb, "| Proposed Value | %s |\n", h.ProposedValue)
	fmt.Fprintf(&sb, "| Baseline Metric | %.3f |\n", h.BaselineMetric)
	fmt.Fprintf(&sb, "| Experiment Metric | %.3f |\n", h.ExperimentMetric)
	fmt.Fprintf(&sb, "| Confidence | %.2f |\n", h.Confidence)
	fmt.Fprintf(&sb, "| Decision | %s |\n", h.Decision)
	fmt.Fprintf(&sb, "| Reason | %s |\n\n", h.DecisionReason)
	sb.WriteString("## Recommendation\n\n")
	fmt.Fprintf(&sb, "The experiment suggests changing `%s` from **%s** to **%s**. ",
		h.Metric, h.BaselineValue, h.ProposedValue)
	fmt.Fprintf(&sb, "Confidence level: **%.0f%%**. ", h.Confidence*100)
	sb.WriteString("Review this finding and apply manually if appropriate.\n")
	return sb.String()
}
