package workflow_synthesis

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type Evaluator struct {
	store  Store
	config Config
}

func NewEvaluator(store Store, config Config) *Evaluator {
	return &Evaluator{
		store:  store,
		config: config,
	}
}

func (e *Evaluator) Evaluate(ctx context.Context, experimentID string) (*db.ExperimentEvaluation, error) {
	candidateMetrics, baselineMetrics, err := e.store.GetExperimentMetrics(ctx, experimentID)
	if err != nil {
		return nil, fmt.Errorf("get experiment metrics: %w", err)
	}

	minSamples := candidateMetrics.SampleSize
	if baselineMetrics.SampleSize < minSamples {
		minSamples = baselineMetrics.SampleSize
	}

	if minSamples < e.config.MinSampleSize {
		return &db.ExperimentEvaluation{
			ExperimentID:     experimentID,
			CandidateMetrics: candidateMetrics,
			BaselineMetrics:  baselineMetrics,
			ShouldPromote:    false,
			PromotionReason:  fmt.Sprintf("insufficient sample size (candidate: %d, baseline: %d, min: %d)", candidateMetrics.SampleSize, baselineMetrics.SampleSize, e.config.MinSampleSize),
		}, nil
	}

	successRateDelta := candidateMetrics.SuccessRate - baselineMetrics.SuccessRate
	cycleTimeDelta := 0.0
	if baselineMetrics.AvgCycleTime > 0 {
		cycleTimeDelta = (baselineMetrics.AvgCycleTime - candidateMetrics.AvgCycleTime) / baselineMetrics.AvgCycleTime
	}

	evaluation := &db.ExperimentEvaluation{
		ExperimentID:     experimentID,
		CandidateMetrics: candidateMetrics,
		BaselineMetrics:  baselineMetrics,
		SuccessRateDelta: successRateDelta,
		CycleTimeDelta:   cycleTimeDelta,
	}

	shouldPromote, reason := e.shouldPromote(candidateMetrics, baselineMetrics, successRateDelta, cycleTimeDelta)
	evaluation.ShouldPromote = shouldPromote
	evaluation.PromotionReason = reason

	return evaluation, nil
}

func (e *Evaluator) shouldPromote(candidate, baseline db.EvaluationMetrics, successRateDelta, cycleTimeDelta float64) (bool, string) {
	if successRateDelta >= e.config.MinSuccessRateDelta {
		return true, fmt.Sprintf("success rate improved by %.1f%% (%.1f%% -> %.1f%%)", successRateDelta*100, baseline.SuccessRate*100, candidate.SuccessRate*100)
	}

	if cycleTimeDelta >= e.config.MinCycleTimeReduction {
		return true, fmt.Sprintf("cycle time reduced by %.1f%% (%.1f min -> %.1f min)", cycleTimeDelta*100, baseline.AvgCycleTime, candidate.AvgCycleTime)
	}

	if successRateDelta > 0.05 && cycleTimeDelta > 0.10 {
		return true, fmt.Sprintf("combined improvement: success rate +%.1f%%, cycle time -%.1f%%", successRateDelta*100, cycleTimeDelta*100)
	}

	reasons := []string{}
	if successRateDelta < 0 {
		reasons = append(reasons, fmt.Sprintf("success rate decreased by %.1f%%", -successRateDelta*100))
	}
	if cycleTimeDelta < 0 {
		reasons = append(reasons, fmt.Sprintf("cycle time increased by %.1f%%", -cycleTimeDelta*100))
	}
	if len(reasons) == 0 {
		reasons = append(reasons, fmt.Sprintf("improvement below threshold (success: +%.1f%% < %.1f%%, cycle: -%.1f%% < %.1f%%)", successRateDelta*100, e.config.MinSuccessRateDelta*100, cycleTimeDelta*100, e.config.MinCycleTimeReduction*100))
	}

	return false, reasons[0]
}

func (e *Evaluator) ShouldAbort(candidate, baseline db.EvaluationMetrics) bool {
	if candidate.SampleSize < e.config.MinSampleSize/2 {
		return false
	}

	if candidate.SuccessRate < baseline.SuccessRate-0.20 {
		slog.Warn("candidate significantly underperforming, recommending abort",
			"candidate_success_rate", candidate.SuccessRate,
			"baseline_success_rate", baseline.SuccessRate)
		return true
	}

	return false
}

func (e *Evaluator) CalculateStatisticalSignificance(candidate, baseline db.EvaluationMetrics) float64 {
	n1 := float64(candidate.SampleSize)
	n2 := float64(baseline.SampleSize)
	p1 := candidate.SuccessRate
	p2 := baseline.SuccessRate

	if n1 == 0 || n2 == 0 {
		return 0
	}

	pooledP := (p1*n1 + p2*n2) / (n1 + n2)
	if pooledP == 0 || pooledP == 1 {
		return 0
	}

	se := math.Sqrt(pooledP * (1 - pooledP) * (1/n1 + 1/n2))
	if se == 0 {
		return 0
	}

	z := (p1 - p2) / se

	pValue := 0.5 * (1 + math.Erf(z/math.Sqrt(2)))
	return pValue
}

func (e *Evaluator) EvaluateAll(ctx context.Context) ([]db.ExperimentEvaluation, error) {
	experiments, err := e.store.ListRunningExperiments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list running experiments: %w", err)
	}

	var evaluations []db.ExperimentEvaluation
	for _, exp := range experiments {
		totalRuns := exp.RunsCandidate + exp.RunsBaseline
		if totalRuns < e.config.MinSampleSize {
			continue
		}

		evaluation, err := e.Evaluate(ctx, exp.ID)
		if err != nil {
			slog.Error("failed to evaluate experiment", "experiment_id", exp.ID, "error", err)
			continue
		}

		evaluations = append(evaluations, *evaluation)
	}

	return evaluations, nil
}
