package orchestration

import (
	"context"
	"sync"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/openclaw/workflow_synthesis"
)

type SynthesisRouter struct {
	store       workflow_synthesis.Store
	banditCache map[string]*workflow_synthesis.ThompsonBandit
	banditMu    sync.RWMutex
}

func NewSynthesisRouter(store workflow_synthesis.Store) *SynthesisRouter {
	return &SynthesisRouter{
		store:       store,
		banditCache: make(map[string]*workflow_synthesis.ThompsonBandit),
	}
}

type RoutingDecision struct {
	UseCandidate     bool
	Candidate        *db.WorkflowCandidate
	Experiment       *db.WorkflowExperiment
	BaselineWorkflow string
}

func (r *SynthesisRouter) Route(ctx context.Context, taskType, repoType string) (*RoutingDecision, error) {
	candidate, err := r.store.GetCandidateForTask(ctx, taskType, repoType)
	if err != nil {
		return nil, err
	}

	if candidate != nil && candidate.Status == "promoted" {
		return &RoutingDecision{
			UseCandidate:     true,
			Candidate:        candidate,
			BaselineWorkflow: candidate.BaseWorkflow,
		}, nil
	}

	experiment, expCandidate, err := r.store.GetExperimentForTask(ctx, taskType, repoType)
	if err != nil {
		return nil, err
	}

	if experiment == nil || expCandidate == nil {
		return &RoutingDecision{
			UseCandidate:     false,
			BaselineWorkflow: r.getDefaultBaseline(taskType),
		}, nil
	}

	bandit := r.getOrCreateBandit(experiment.ID, &experiment.BanditState)
	useCandidate := bandit.Select()

	return &RoutingDecision{
		UseCandidate:     useCandidate,
		Candidate:        expCandidate,
		Experiment:       experiment,
		BaselineWorkflow: experiment.BaselineWorkflow,
	}, nil
}

func (r *SynthesisRouter) RecordResult(
	ctx context.Context,
	experimentID string,
	workflowID string,
	useCandidate bool,
	success bool,
	cycleTimeMin int,
	retryCount int,
	reviewPasses int,
) error {
	experiment, err := r.store.GetExperimentByID(ctx, experimentID)
	if err != nil {
		return err
	}
	if experiment == nil {
		return nil
	}

	result := &db.ExperimentResult{
		ExperimentID:  experimentID,
		WorkflowID:    workflowID,
		UsedCandidate: useCandidate,
		Success:       success,
		CycleTimeMin:  cycleTimeMin,
		RetryCount:    retryCount,
		ReviewPasses:  reviewPasses,
	}

	if err := r.store.SaveExperimentResult(ctx, result); err != nil {
		return err
	}

	bandit := r.getOrCreateBandit(experimentID, &experiment.BanditState)
	bandit.Update(useCandidate, success)

	runsCandidate := experiment.RunsCandidate
	runsBaseline := experiment.RunsBaseline
	if useCandidate {
		runsCandidate++
	} else {
		runsBaseline++
	}

	if err := r.store.UpdateExperimentBandit(ctx, experimentID, db.BanditState{
		CandidatePulls:  bandit.CandidatePulls,
		BaselinePulls:   bandit.BaselinePulls,
		CandidateReward: bandit.CandidateReward,
		BaselineReward:  bandit.BaselineReward,
	}, runsCandidate, runsBaseline); err != nil {
		return err
	}

	return nil
}

func (r *SynthesisRouter) GetPromotedWorkflow(ctx context.Context, taskType, repoType string) (*db.WorkflowCandidate, error) {
	candidate, err := r.store.GetCandidateForTask(ctx, taskType, repoType)
	if err != nil {
		return nil, err
	}
	if candidate != nil && candidate.Status == "promoted" {
		return candidate, nil
	}
	return nil, nil
}

func (r *SynthesisRouter) GetActiveExperiment(ctx context.Context, taskType, repoType string) (*db.WorkflowExperiment, *db.WorkflowCandidate, error) {
	return r.store.GetExperimentForTask(ctx, taskType, repoType)
}

func (r *SynthesisRouter) getOrCreateBandit(experimentID string, state *db.BanditState) *workflow_synthesis.ThompsonBandit {
	r.banditMu.RLock()
	bandit, ok := r.banditCache[experimentID]
	r.banditMu.RUnlock()

	if ok {
		return bandit
	}

	r.banditMu.Lock()
	defer r.banditMu.Unlock()

	if bandit, ok := r.banditCache[experimentID]; ok {
		return bandit
	}

	bandit = &workflow_synthesis.ThompsonBandit{
		CandidatePulls:  state.CandidatePulls,
		BaselinePulls:   state.BaselinePulls,
		CandidateReward: state.CandidateReward,
		BaselineReward:  state.BaselineReward,
	}

	r.banditCache[experimentID] = bandit
	return bandit
}

func (r *SynthesisRouter) getDefaultBaseline(taskType string) string {
	switch taskType {
	case "bug_fix", "hotfix":
		return "bug"
	case "e2e", "test":
		return "e2e"
	default:
		return "spec"
	}
}

func (r *SynthesisRouter) GetWorkflowSteps(candidate *db.WorkflowCandidate) []Phase {
	if candidate == nil || len(candidate.Steps) == 0 {
		return nil
	}

	phases := make([]Phase, 0, len(candidate.Steps))
	for _, step := range candidate.Steps {
		phases = append(phases, Phase(step.Phase))
	}
	return phases
}

func (r *SynthesisRouter) GetWorkflowTransitions(candidate *db.WorkflowCandidate) map[Phase][]Phase {
	if candidate == nil {
		return nil
	}

	transitions := make(map[Phase][]Phase)
	for _, step := range candidate.Steps {
		from := Phase(step.Phase)
		toPhases := make([]Phase, 0, len(step.NextPhases))
		for _, to := range step.NextPhases {
			toPhases = append(toPhases, Phase(to))
		}
		if len(toPhases) > 0 {
			transitions[from] = toPhases
		}
	}
	return transitions
}
