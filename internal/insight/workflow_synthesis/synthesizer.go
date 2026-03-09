package workflow_synthesis

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type Synthesizer struct {
	store     Store
	generator *CandidateGenerator
	evaluator *Evaluator
	config    Config
}

func NewSynthesizer(store Store, config Config) *Synthesizer {
	return &Synthesizer{
		store:     store,
		generator: NewCandidateGenerator(store, config),
		evaluator: NewEvaluator(store, config),
		config:    config,
	}
}

type SynthesisResult struct {
	CandidatesGenerated  int      `json:"candidates_generated"`
	ExperimentsStarted   int      `json:"experiments_started"`
	ExperimentsEvaluated int      `json:"experiments_evaluated"`
	WorkflowsPromoted    int      `json:"workflows_promoted"`
	WorkflowsRejected    int      `json:"workflows_rejected"`
	PromotedCandidateIDs []string `json:"promoted_candidate_ids"`
	RejectedCandidateIDs []string `json:"rejected_candidate_ids"`
}

func (s *Synthesizer) Run(ctx context.Context) (*SynthesisResult, error) {
	result := &SynthesisResult{
		PromotedCandidateIDs: []string{},
		RejectedCandidateIDs: []string{},
	}

	genResult, err := s.generator.Generate(ctx)
	if err != nil {
		return nil, fmt.Errorf("generate candidates: %w", err)
	}
	result.CandidatesGenerated = genResult.CandidatesGenerated

	if len(genResult.CandidateIDs) > 0 {
		slog.Info("workflow synthesis: generated new candidates",
			"count", len(genResult.CandidateIDs),
			"ids", genResult.CandidateIDs)
	}

	for _, candidateID := range genResult.CandidateIDs {
		candidate, err := s.store.GetCandidateByID(ctx, candidateID)
		if err != nil {
			slog.Error("failed to get candidate", "id", candidateID, "error", err)
			continue
		}

		if candidate.Confidence >= s.config.MinConfidence {
			if err := s.startExperiment(ctx, candidate); err != nil {
				slog.Error("failed to start experiment", "candidate_id", candidateID, "error", err)
				continue
			}
			result.ExperimentsStarted++
		}
	}

	experiments, err := s.store.ListRunningExperiments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list running experiments: %w", err)
	}

	for _, exp := range experiments {
		totalRuns := exp.RunsCandidate + exp.RunsBaseline
		halfSample := exp.SampleSize / 2

		if totalRuns < halfSample {
			continue
		}

		evaluation, err := s.evaluator.Evaluate(ctx, exp.ID)
		if err != nil {
			slog.Error("failed to evaluate experiment", "experiment_id", exp.ID, "error", err)
			continue
		}

		result.ExperimentsEvaluated++

		if evaluation.ShouldPromote {
			if err := s.promote(ctx, exp.CandidateID, exp.ID, evaluation.PromotionReason); err != nil {
				slog.Error("failed to promote workflow", "candidate_id", exp.CandidateID, "error", err)
				continue
			}
			result.WorkflowsPromoted++
			result.PromotedCandidateIDs = append(result.PromotedCandidateIDs, exp.CandidateID)
			slog.Info("workflow synthesis: promoted candidate workflow",
				"candidate_id", exp.CandidateID,
				"reason", evaluation.PromotionReason)
		} else if s.shouldReject(evaluation) {
			if err := s.reject(ctx, exp.CandidateID, exp.ID, evaluation.PromotionReason); err != nil {
				slog.Error("failed to reject workflow", "candidate_id", exp.CandidateID, "error", err)
				continue
			}
			result.WorkflowsRejected++
			result.RejectedCandidateIDs = append(result.RejectedCandidateIDs, exp.CandidateID)
			slog.Info("workflow synthesis: rejected candidate workflow",
				"candidate_id", exp.CandidateID,
				"reason", evaluation.PromotionReason)
		}
	}

	return result, nil
}

func (s *Synthesizer) startExperiment(ctx context.Context, candidate *db.WorkflowCandidate) error {
	experiment := &db.WorkflowExperiment{
		CandidateID:      candidate.ID,
		BaselineWorkflow: candidate.BaseWorkflow,
		TrafficPercent:   10.0,
		Status:           "running",
		SampleSize:       s.config.DefaultExperimentSize,
		RunsCandidate:    0,
		RunsBaseline:     0,
		BanditState: db.BanditState{
			CandidatePulls:  1,
			BaselinePulls:   1,
			CandidateReward: 0.5,
			BaselineReward:  0.5,
		},
	}

	if err := s.store.SaveExperiment(ctx, experiment); err != nil {
		return err
	}

	if err := s.store.UpdateCandidateStatus(ctx, candidate.ID, "experiment"); err != nil {
		return err
	}

	slog.Info("workflow synthesis: started experiment",
		"experiment_id", experiment.ID,
		"candidate_id", candidate.ID,
		"task_type", candidate.TaskType,
		"repo_type", candidate.RepoType)

	return nil
}

func (s *Synthesizer) promote(ctx context.Context, candidateID, experimentID, reason string) error {
	if err := s.store.UpdateCandidateStatus(ctx, candidateID, "promoted"); err != nil {
		return err
	}

	if err := s.store.UpdateExperimentStatus(ctx, experimentID, "completed"); err != nil {
		return err
	}

	return nil
}

func (s *Synthesizer) reject(ctx context.Context, candidateID, experimentID, reason string) error {
	if err := s.store.UpdateCandidateStatus(ctx, candidateID, "rejected"); err != nil {
		return err
	}

	if err := s.store.UpdateExperimentStatus(ctx, experimentID, "completed"); err != nil {
		return err
	}

	return nil
}

func (s *Synthesizer) shouldReject(evaluation *db.ExperimentEvaluation) bool {
	if evaluation.SuccessRateDelta < -0.10 {
		return true
	}
	if evaluation.CycleTimeDelta < -0.25 {
		return true
	}

	minSamples := evaluation.CandidateMetrics.SampleSize
	if evaluation.BaselineMetrics.SampleSize < minSamples {
		minSamples = evaluation.BaselineMetrics.SampleSize
	}

	if minSamples >= s.config.DefaultExperimentSize {
		return !evaluation.ShouldPromote
	}

	return false
}

func (s *Synthesizer) SelectWorkflow(ctx context.Context, taskType, repoType string) (*db.WorkflowCandidate, bool, error) {
	candidate, err := s.store.GetCandidateForTask(ctx, taskType, repoType)
	if err != nil {
		return nil, false, err
	}
	if candidate != nil {
		return candidate, true, nil
	}

	experiment, expCandidate, err := s.store.GetExperimentForTask(ctx, taskType, repoType)
	if err != nil {
		return nil, false, err
	}
	if experiment == nil || expCandidate == nil {
		return nil, false, nil
	}

	bandit := &ThompsonBandit{
		CandidatePulls:  experiment.BanditState.CandidatePulls,
		BaselinePulls:   experiment.BanditState.BaselinePulls,
		CandidateReward: experiment.BanditState.CandidateReward,
		BaselineReward:  experiment.BanditState.BaselineReward,
	}

	useCandidate := bandit.Select()

	return expCandidate, useCandidate, nil
}

func (s *Synthesizer) RecordResult(ctx context.Context, taskType, repoType, workflowID string, useCandidate bool, success bool, cycleTimeMin, retryCount, reviewPasses int) error {
	experiment, _, err := s.store.GetExperimentForTask(ctx, taskType, repoType)
	if err != nil {
		return err
	}
	if experiment == nil {
		return nil
	}

	result := &db.ExperimentResult{
		ExperimentID:  experiment.ID,
		WorkflowID:    workflowID,
		UsedCandidate: useCandidate,
		Success:       success,
		CycleTimeMin:  cycleTimeMin,
		RetryCount:    retryCount,
		ReviewPasses:  reviewPasses,
	}

	if err := s.store.SaveExperimentResult(ctx, result); err != nil {
		return err
	}

	bandit := &ThompsonBandit{
		CandidatePulls:  experiment.BanditState.CandidatePulls,
		BaselinePulls:   experiment.BanditState.BaselinePulls,
		CandidateReward: experiment.BanditState.CandidateReward,
		BaselineReward:  experiment.BanditState.BaselineReward,
	}
	bandit.Update(useCandidate, success)

	runsCandidate := experiment.RunsCandidate
	runsBaseline := experiment.RunsBaseline
	if useCandidate {
		runsCandidate++
	} else {
		runsBaseline++
	}

	if err := s.store.UpdateExperimentBandit(ctx, experiment.ID, db.BanditState{
		CandidatePulls:  bandit.CandidatePulls,
		BaselinePulls:   bandit.BaselinePulls,
		CandidateReward: bandit.CandidateReward,
		BaselineReward:  bandit.BaselineReward,
	}, runsCandidate, runsBaseline); err != nil {
		return err
	}

	return nil
}

func (s *Synthesizer) GetExperimentStats(ctx context.Context) (map[string]interface{}, error) {
	experiments, err := s.store.ListRunningExperiments(ctx)
	if err != nil {
		return nil, err
	}

	candidates, err := s.store.ListCandidates(ctx, "", 100)
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"running_experiments":  len(experiments),
		"total_candidates":     len(candidates),
		"candidates_by_status": make(map[string]int),
	}

	statusCounts := stats["candidates_by_status"].(map[string]int)
	for _, c := range candidates {
		statusCounts[c.Status]++
	}

	return stats, nil
}

func (s *Synthesizer) RunEvaluationOnly(ctx context.Context) ([]db.ExperimentEvaluation, error) {
	return s.evaluator.EvaluateAll(ctx)
}

func (s *Synthesizer) GenerateCandidatesOnly(ctx context.Context) (*GenerationResult, error) {
	return s.generator.Generate(ctx)
}

func (s *Synthesizer) GetCandidateForTask(ctx context.Context, taskType, repoType string) (*db.WorkflowCandidate, error) {
	return s.store.GetCandidateForTask(ctx, taskType, repoType)
}

func (s *Synthesizer) GetExperimentForTask(ctx context.Context, taskType, repoType string) (*db.WorkflowExperiment, *db.WorkflowCandidate, error) {
	return s.store.GetExperimentForTask(ctx, taskType, repoType)
}

func (s *Synthesizer) ListCandidates(ctx context.Context, status string, limit int) ([]db.WorkflowCandidate, error) {
	return s.store.ListCandidates(ctx, status, limit)
}

func (s *Synthesizer) ListRunningExperiments(ctx context.Context) ([]db.WorkflowExperiment, error) {
	return s.store.ListRunningExperiments(ctx)
}

func (s *Synthesizer) PromoteCandidate(ctx context.Context, candidateID string) error {
	candidate, err := s.store.GetCandidateByID(ctx, candidateID)
	if err != nil {
		return err
	}
	if candidate == nil {
		return fmt.Errorf("candidate not found: %s", candidateID)
	}

	return s.store.UpdateCandidateStatus(ctx, candidateID, "promoted")
}

func (s *Synthesizer) RollbackCandidate(ctx context.Context, candidateID string) error {
	candidate, err := s.store.GetCandidateByID(ctx, candidateID)
	if err != nil {
		return err
	}
	if candidate == nil {
		return fmt.Errorf("candidate not found: %s", candidateID)
	}

	if candidate.Status != "promoted" {
		return fmt.Errorf("can only rollback promoted candidates, current status: %s", candidate.Status)
	}

	return s.store.UpdateCandidateStatus(ctx, candidateID, "rollback")
}

func (s *Synthesizer) StartExperimentForCandidate(ctx context.Context, candidateID string) error {
	candidate, err := s.store.GetCandidateByID(ctx, candidateID)
	if err != nil {
		return err
	}
	if candidate == nil {
		return fmt.Errorf("candidate not found: %s", candidateID)
	}

	if candidate.Status != "candidate" {
		return fmt.Errorf("can only start experiment for candidates, current status: %s", candidate.Status)
	}

	return s.startExperiment(ctx, candidate)
}

func (s *Synthesizer) AbortExperiment(ctx context.Context, experimentID string) error {
	experiment, err := s.store.GetExperimentByID(ctx, experimentID)
	if err != nil {
		return err
	}
	if experiment == nil {
		return fmt.Errorf("experiment not found: %s", experimentID)
	}

	if err := s.store.UpdateCandidateStatus(ctx, experiment.CandidateID, "rejected"); err != nil {
		return err
	}

	return s.store.UpdateExperimentStatus(ctx, experimentID, "aborted")
}

func (s *Synthesizer) ForceEvaluate(ctx context.Context, experimentID string) (*db.ExperimentEvaluation, error) {
	evaluation, err := s.evaluator.Evaluate(ctx, experimentID)
	if err != nil {
		return nil, err
	}

	if evaluation.ShouldPromote {
		experiment, err := s.store.GetExperimentByID(ctx, experimentID)
		if err != nil {
			return nil, err
		}
		if experiment != nil {
			if err := s.promote(ctx, experiment.CandidateID, experimentID, evaluation.PromotionReason); err != nil {
				return nil, err
			}
		}
	}

	return evaluation, nil
}

func (s *Synthesizer) GetMetrics(ctx context.Context, experimentID string) (candidate, baseline db.EvaluationMetrics, err error) {
	return s.store.GetExperimentMetrics(ctx, experimentID)
}
