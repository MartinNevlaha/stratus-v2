package trajectory_engine

import (
	"time"
)

type TrajectoryStep struct {
	StepNumber    int            `json:"step"`
	AgentName     string         `json:"agent"`
	ActionType    string         `json:"action"`
	Phase         string         `json:"phase,omitempty"`
	InputContext  string         `json:"input,omitempty"`
	OutputSummary string         `json:"output,omitempty"`
	Success       bool           `json:"success"`
	DurationMs    int64          `json:"duration_ms,omitempty"`
	Timestamp     time.Time      `json:"timestamp"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type Trajectory struct {
	ID           string           `json:"id"`
	WorkflowID   string           `json:"workflow_id"`
	TaskType     string           `json:"task_type"`
	RepoType     string           `json:"repo_type"`
	WorkflowType string           `json:"workflow_type"`
	Steps        []TrajectoryStep `json:"steps"`
	StepCount    int              `json:"step_count"`
	FinalResult  string           `json:"final_result"`
	CycleTimeMin int              `json:"cycle_time_minutes"`
	StartedAt    time.Time        `json:"started_at"`
	CompletedAt  *time.Time       `json:"completed_at,omitempty"`
}

type TrajectoryPattern struct {
	ID                   string   `json:"id"`
	ProblemType          string   `json:"problem_type"`
	RepoType             string   `json:"repo_type"`
	OptimalAgentSequence []string `json:"optimal_agent_sequence"`
	SuccessRate          float64  `json:"success_rate"`
	OccurrenceCount      int      `json:"occurrence_count"`
	AvgCycleTimeMin      int      `json:"avg_cycle_time_minutes"`
	ExampleTrajectoryIDs []string `json:"example_trajectory_ids"`
	Confidence           float64  `json:"confidence"`
}

type FailurePoint struct {
	StepNumber    int      `json:"step_number"`
	AgentName     string   `json:"agent_name"`
	ActionType    string   `json:"action_type"`
	FailureRate   float64  `json:"failure_rate"`
	CommonReasons []string `json:"common_reasons"`
	Occurrences   int      `json:"occurrences"`
}

type AgentStepPattern struct {
	Sequence    []string `json:"sequence"`
	SequenceKey string   `json:"sequence_key"`
	SuccessRate float64  `json:"success_rate"`
	AvgDuration int64    `json:"avg_duration_ms"`
	Occurrences int      `json:"occurrences"`
	TaskTypes   []string `json:"task_types,omitempty"`
}

type WorkflowInefficiency struct {
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Impact      string  `json:"impact"`
	Suggestion  string  `json:"suggestion"`
	Confidence  float64 `json:"confidence"`
	Occurrences int     `json:"occurrences"`
	AvgTimeLost int64   `json:"avg_time_lost_ms,omitempty"`
}

type AnalysisResult struct {
	FailurePoints        []FailurePoint         `json:"failure_points"`
	SuccessfulPaths      []AgentStepPattern     `json:"successful_paths"`
	AgentStepPatterns    []AgentStepPattern     `json:"agent_step_patterns"`
	Inefficiencies       []WorkflowInefficiency `json:"inefficiencies"`
	TrajectoriesAnalyzed int                    `json:"trajectories_analyzed"`
	PatternsExtracted    int                    `json:"patterns_extracted"`
}
