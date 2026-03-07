package agent_evolution

import (
	"time"

	"github.com/google/uuid"
)

type OpportunityType string

const (
	OpportunitySpecialization OpportunityType = "specialization"
	OpportunityPromptImprove  OpportunityType = "prompt_improvement"
	OpportunityDeprecation    OpportunityType = "deprecation"
	OpportunityConsolidation  OpportunityType = "consolidation"
)

type CandidateStatus string

const (
	CandidatePending    CandidateStatus = "pending"
	CandidateExperiment CandidateStatus = "experimenting"
	CandidatePromoted   CandidateStatus = "promoted"
	CandidateRejected   CandidateStatus = "rejected"
	CandidateDeprecated CandidateStatus = "deprecated"
)

type ExperimentStatus string

const (
	ExperimentRunning   ExperimentStatus = "running"
	ExperimentCompleted ExperimentStatus = "completed"
	ExperimentCancelled ExperimentStatus = "cancelled"
)

type ExperimentWinner string

const (
	WinnerCandidate    ExperimentWinner = "candidate"
	WinnerBaseline     ExperimentWinner = "baseline"
	WinnerInconclusive ExperimentWinner = "inconclusive"
)

type AgentPerformanceProfile struct {
	AgentName          string         `json:"agent_name"`
	TotalRuns          int            `json:"total_runs"`
	SuccessRate        float64        `json:"success_rate"`
	FailureRate        float64        `json:"failure_rate"`
	ReviewPassRate     float64        `json:"review_pass_rate"`
	ReworkRate         float64        `json:"rework_rate"`
	AvgCycleTimeMs     int64          `json:"avg_cycle_time_ms"`
	Trend              string         `json:"trend"`
	TaskTypeFrequency  map[string]int `json:"task_type_frequency"`
	RepoTypeFrequency  map[string]int `json:"repo_type_frequency"`
	ProblemFrequency   map[string]int `json:"problem_frequency"`
	CommonFailureModes []string       `json:"common_failure_modes"`
	ConfidenceScore    float64        `json:"confidence_score"`
	WindowStart        time.Time      `json:"window_start"`
	WindowEnd          time.Time      `json:"window_end"`
}

type EvolutionOpportunity struct {
	ID              string          `json:"id"`
	AgentName       string          `json:"agent_name"`
	OpportunityType OpportunityType `json:"opportunity_type"`
	Specialization  string          `json:"specialization,omitempty"`
	Reason          string          `json:"reason"`
	Confidence      float64         `json:"confidence"`
	Evidence        map[string]any  `json:"evidence"`
	Frequency       int             `json:"frequency"`
	CreatedAt       time.Time       `json:"created_at"`
}

type PromptDiff struct {
	Additions     []string `json:"additions"`
	Modifications []string `json:"modifications"`
	NewFocus      string   `json:"new_focus,omitempty"`
}

type CandidateAgent struct {
	ID              string          `json:"id"`
	AgentName       string          `json:"agent_name"`
	BaseAgent       string          `json:"base_agent"`
	Specialization  string          `json:"specialization"`
	Reason          string          `json:"reason"`
	Confidence      float64         `json:"confidence"`
	PromptDiff      PromptDiff      `json:"prompt_diff"`
	Status          CandidateStatus `json:"status"`
	OpportunityType OpportunityType `json:"opportunity_type"`
	Evidence        map[string]any  `json:"evidence"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type BanditState struct {
	CandidateAlpha float64 `json:"candidate_alpha"`
	CandidateBeta  float64 `json:"candidate_beta"`
	BaselineAlpha  float64 `json:"baseline_alpha"`
	BaselineBeta   float64 `json:"baseline_beta"`
}

type AgentExperiment struct {
	ID             string           `json:"id"`
	CandidateID    string           `json:"candidate_id"`
	CandidateAgent string           `json:"candidate_agent"`
	BaselineAgent  string           `json:"baseline_agent"`
	TrafficPercent float64          `json:"traffic_percent"`
	Status         ExperimentStatus `json:"status"`
	SampleSize     int              `json:"sample_size"`
	RunsCandidate  int              `json:"runs_candidate"`
	RunsBaseline   int              `json:"runs_baseline"`
	BanditState    BanditState      `json:"bandit_state"`
	StartedAt      time.Time        `json:"started_at"`
	CompletedAt    *time.Time       `json:"completed_at,omitempty"`
	Winner         ExperimentWinner `json:"winner,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

type ExperimentResult struct {
	ID            int       `json:"id"`
	ExperimentID  string    `json:"experiment_id"`
	WorkflowID    string    `json:"workflow_id"`
	TaskType      string    `json:"task_type"`
	UsedCandidate bool      `json:"used_candidate"`
	Success       bool      `json:"success"`
	CycleTimeMs   int64     `json:"cycle_time_ms"`
	ReviewPassed  bool      `json:"review_passed"`
	ReworkCount   int       `json:"rework_count"`
	CreatedAt     time.Time `json:"created_at"`
}

type ExperimentMetrics struct {
	CandidateSuccessRate float64 `json:"candidate_success_rate"`
	BaselineSuccessRate  float64 `json:"baseline_success_rate"`
	CandidateCycleTime   int64   `json:"candidate_cycle_time_ms"`
	BaselineCycleTime    int64   `json:"baseline_cycle_time_ms"`
	CandidateReviewRate  float64 `json:"candidate_review_pass_rate"`
	BaselineReviewRate   float64 `json:"baseline_review_pass_rate"`
	CandidateReworkRate  float64 `json:"candidate_rework_rate"`
	BaselineReworkRate   float64 `json:"baseline_rework_rate"`
	RunsCandidate        int     `json:"runs_candidate"`
	RunsBaseline         int     `json:"runs_baseline"`
	SuccessRateDelta     float64 `json:"success_rate_delta"`
	CycleTimeDelta       int64   `json:"cycle_time_delta_ms"`
}

type Config struct {
	MinRunsForAnalysis        int     `json:"min_runs_for_analysis"`
	DeprecationThreshold      float64 `json:"deprecation_threshold"`
	SpecializationThreshold   float64 `json:"specialization_threshold"`
	MinConfidenceForCandidate float64 `json:"min_confidence_for_candidate"`
	ExperimentTrafficPercent  float64 `json:"experiment_traffic_percent"`
	ExperimentSampleSize      int     `json:"experiment_sample_size"`
	MinSuccessRateDelta       float64 `json:"min_success_rate_delta"`
	MinCycleTimeReduction     float64 `json:"min_cycle_time_reduction"`
	MaxCandidatesPerRun       int     `json:"max_candidates_per_run"`
}

func DefaultConfig() Config {
	return Config{
		MinRunsForAnalysis:        10,
		DeprecationThreshold:      0.30,
		SpecializationThreshold:   0.60,
		MinConfidenceForCandidate: 0.65,
		ExperimentTrafficPercent:  10.0,
		ExperimentSampleSize:      100,
		MinSuccessRateDelta:       0.10,
		MinCycleTimeReduction:     0.15,
		MaxCandidatesPerRun:       5,
	}
}

func NewCandidateAgent(
	agentName string,
	baseAgent string,
	specialization string,
	opportunityType OpportunityType,
	reason string,
	confidence float64,
) CandidateAgent {
	now := time.Now().UTC()
	return CandidateAgent{
		ID:              uuid.New().String(),
		AgentName:       agentName,
		BaseAgent:       baseAgent,
		Specialization:  specialization,
		OpportunityType: opportunityType,
		Reason:          reason,
		Confidence:      confidence,
		Status:          CandidatePending,
		PromptDiff:      PromptDiff{},
		Evidence:        make(map[string]any),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func NewAgentExperiment(candidateID, candidateAgent, baselineAgent string, trafficPercent float64, sampleSize int) AgentExperiment {
	now := time.Now().UTC()
	return AgentExperiment{
		ID:             uuid.New().String(),
		CandidateID:    candidateID,
		CandidateAgent: candidateAgent,
		BaselineAgent:  baselineAgent,
		TrafficPercent: trafficPercent,
		Status:         ExperimentRunning,
		SampleSize:     sampleSize,
		RunsCandidate:  0,
		RunsBaseline:   0,
		BanditState:    BanditState{CandidateAlpha: 1, CandidateBeta: 1, BaselineAlpha: 1, BaselineBeta: 1},
		StartedAt:      now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func NewEvolutionOpportunity(
	agentName string,
	opportunityType OpportunityType,
	specialization string,
	reason string,
	confidence float64,
	frequency int,
) EvolutionOpportunity {
	return EvolutionOpportunity{
		ID:              uuid.New().String(),
		AgentName:       agentName,
		OpportunityType: opportunityType,
		Specialization:  specialization,
		Reason:          reason,
		Confidence:      confidence,
		Frequency:       frequency,
		Evidence:        make(map[string]any),
		CreatedAt:       time.Now().UTC(),
	}
}

func (m *ExperimentMetrics) IsSignificant() bool {
	if m.BaselineCycleTime > 0 {
		cycleTimeImprovement := float64(-m.CycleTimeDelta) / float64(m.BaselineCycleTime)
		return m.SuccessRateDelta >= 0.10 || cycleTimeImprovement >= 0.15
	}
	return m.SuccessRateDelta >= 0.10
}

func (m *ExperimentMetrics) DetermineWinner() ExperimentWinner {
	if !m.IsSignificant() {
		return WinnerInconclusive
	}
	if m.BaselineCycleTime > 0 {
		cycleTimeImprovement := float64(-m.CycleTimeDelta) / float64(m.BaselineCycleTime)
		if m.SuccessRateDelta > 0.05 || cycleTimeImprovement > 0.10 {
			return WinnerCandidate
		}
	} else if m.SuccessRateDelta > 0.05 {
		return WinnerCandidate
	}
	return WinnerBaseline
}
