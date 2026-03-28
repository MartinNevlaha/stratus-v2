package agent_evolution

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"
)

var (
	globalRand   *rand.Rand
	globalRandMu sync.Mutex
)

func init() {
	globalRand = rand.New(rand.NewSource(time.Now().UnixNano()))
}

type AgentExperimentRunner struct {
	store  Store
	config Config
}

func NewAgentExperimentRunner(store Store, config Config) *AgentExperimentRunner {
	return &AgentExperimentRunner{
		store:  store,
		config: config,
	}
}

func (r *AgentExperimentRunner) StartExperiment(ctx context.Context, candidateID string) (*AgentExperiment, error) {
	existing, _ := r.store.GetExperimentByCandidateID(ctx, candidateID)
	if existing != nil && existing.Status == ExperimentRunning {
		return existing, nil
	}

	candidate, err := r.store.GetCandidateByID(ctx, candidateID)
	if err != nil {
		return nil, err
	}
	if candidate == nil {
		return nil, nil
	}

	experiment := NewAgentExperiment(
		candidateID,
		candidate.AgentName,
		candidate.BaseAgent,
		r.config.ExperimentTrafficPercent,
		r.config.ExperimentSampleSize,
	)

	if err := r.store.SaveExperiment(ctx, &experiment); err != nil {
		return nil, err
	}

	if err := r.store.UpdateCandidateStatus(ctx, candidateID, CandidateExperiment); err != nil {
		return nil, err
	}

	return &experiment, nil
}

func (r *AgentExperimentRunner) ShouldUseCandidate(ctx context.Context, experimentID string) (bool, error) {
	experiment, err := r.store.GetExperimentByID(ctx, experimentID)
	if err != nil {
		return false, err
	}
	if experiment == nil || experiment.Status != ExperimentRunning {
		return false, nil
	}

	return r.selectUsingBandit(experiment.BanditState), nil
}

func (r *AgentExperimentRunner) RecordResult(ctx context.Context, experimentID string, result ExperimentResult) error {
	experiment, err := r.store.GetExperimentByID(ctx, experimentID)
	if err != nil {
		return err
	}
	if experiment == nil {
		return nil
	}

	result.ExperimentID = experimentID
	result.CreatedAt = time.Now().UTC()

	if err := r.store.SaveExperimentResult(ctx, &result); err != nil {
		return err
	}

	newBandit := r.updateBandit(experiment.BanditState, result.UsedCandidate, result.Success)

	var runsCandidate, runsBaseline int
	if result.UsedCandidate {
		runsCandidate = experiment.RunsCandidate + 1
		runsBaseline = experiment.RunsBaseline
	} else {
		runsCandidate = experiment.RunsCandidate
		runsBaseline = experiment.RunsBaseline + 1
	}

	if err := r.store.UpdateExperimentBandit(ctx, experimentID, newBandit, runsCandidate, runsBaseline); err != nil {
		return err
	}

	totalRuns := runsCandidate + runsBaseline
	if totalRuns >= experiment.SampleSize {
		return r.evaluateAndComplete(ctx, experimentID)
	}

	return nil
}

func (r *AgentExperimentRunner) evaluateAndComplete(ctx context.Context, experimentID string) error {
	metrics, err := r.store.GetExperimentMetrics(ctx, experimentID)
	if err != nil {
		return err
	}

	winner := metrics.DetermineWinner()

	experiment, _ := r.store.GetExperimentByID(ctx, experimentID)
	if experiment != nil {
		if err := r.store.UpdateExperimentStatus(ctx, experimentID, ExperimentCompleted, winner); err != nil {
			return err
		}

		if winner == WinnerCandidate {
			if err := r.store.UpdateCandidateStatus(ctx, experiment.CandidateID, CandidatePromoted); err != nil {
				return err
			}
		} else if winner == WinnerBaseline {
			if err := r.store.UpdateCandidateStatus(ctx, experiment.CandidateID, CandidateRejected); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *AgentExperimentRunner) EvaluateExperiment(ctx context.Context, experimentID string) (*ExperimentMetrics, error) {
	metrics, err := r.store.GetExperimentMetrics(ctx, experimentID)
	if err != nil {
		return nil, err
	}

	return &metrics, nil
}

func (r *AgentExperimentRunner) selectUsingBandit(bandit BanditState) bool {
	candidateAlpha := bandit.CandidateAlpha
	candidateBeta := bandit.CandidateBeta
	baselineAlpha := bandit.BaselineAlpha
	baselineBeta := bandit.BaselineBeta

	candidateSample := betaSample(candidateAlpha, candidateBeta)
	baselineSample := betaSample(baselineAlpha, baselineBeta)

	return candidateSample > baselineSample
}

func (r *AgentExperimentRunner) updateBandit(bandit BanditState, wasCandidate bool, success bool) BanditState {
	if wasCandidate {
		bandit.CandidateAlpha += 1
		if !success {
			bandit.CandidateBeta += 1
		}
	} else {
		bandit.BaselineAlpha += 1
		if !success {
			bandit.BaselineBeta += 1
		}
	}
	return bandit
}

func (r *AgentExperimentRunner) GetActiveExperimentForAgent(ctx context.Context, baselineAgent string) (*AgentExperiment, error) {
	experiments, err := r.store.ListRunningExperiments(ctx)
	if err != nil {
		return nil, err
	}

	for _, exp := range experiments {
		if exp.BaselineAgent == baselineAgent {
			return &exp, nil
		}
	}

	return nil, nil
}

func (r *AgentExperimentRunner) CancelExperiment(ctx context.Context, experimentID string) error {
	return r.store.UpdateExperimentStatus(ctx, experimentID, ExperimentCancelled, WinnerInconclusive)
}

func betaSample(alpha, beta float64) float64 {
	x := gammaSample(alpha, 1.0)
	y := gammaSample(beta, 1.0)
	return x / (x + y)
}

func gammaSample(shape, rate float64) float64 {
	if shape < 1 {
		u := randFloat64()
		return gammaSample(shape+1, rate) * math.Pow(u, 1.0/shape)
	}

	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9.0*d)

	for i := 0; i < 1000; i++ {
		x := randNormFloat64()
		v := 1.0 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := randFloat64()
		if u < 1.0-0.0331*(x*x)*(x*x) {
			return d * v / rate
		}
		if math.Log(u) < 0.5*x*x+d*(math.Log(v)-math.Log(d*v-1+1)) {
			return d * v / rate
		}
	}

	return d / rate
}

func randFloat64() float64 {
	globalRandMu.Lock()
	defer globalRandMu.Unlock()
	return globalRand.Float64()
}

func randNormFloat64() float64 {
	globalRandMu.Lock()
	defer globalRandMu.Unlock()
	return globalRand.NormFloat64()
}
