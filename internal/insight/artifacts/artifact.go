package artifacts

import "time"

type TaskType string

const (
	TaskTypeBugFix   TaskType = "bug_fix"
	TaskTypeFeature  TaskType = "feature"
	TaskTypeRefactor TaskType = "refactor"
	TaskTypeTest     TaskType = "test"
	TaskTypeDocs     TaskType = "docs"
	TaskTypeUnknown  TaskType = "unknown"
)

type ProblemClass string

const (
	ProblemRaceCondition  ProblemClass = "race_condition"
	ProblemNullPointer    ProblemClass = "null_pointer"
	ProblemTypeError      ProblemClass = "type_error"
	ProblemAuthIssue      ProblemClass = "auth_issue"
	ProblemDatabaseTx     ProblemClass = "database_transaction"
	ProblemAPIIntegration ProblemClass = "api_integration"
	ProblemConfiguration  ProblemClass = "configuration"
	ProblemTestFailure    ProblemClass = "test_failure"
	ProblemPerformance    ProblemClass = "performance"
	ProblemMemoryLeak     ProblemClass = "memory_leak"
	ProblemConcurrency    ProblemClass = "concurrency"
	ProblemValidation     ProblemClass = "validation"
	ProblemBuildError     ProblemClass = "build_error"
	ProblemDependency     ProblemClass = "dependency"
	ProblemUnknown        ProblemClass = "unknown"
)

type RepoType string

const (
	RepoTypeGolang     RepoType = "golang"
	RepoTypeNodeJS     RepoType = "nodejs"
	RepoTypeNestJS     RepoType = "nestjs"
	RepoTypeReact      RepoType = "react"
	RepoTypeVue        RepoType = "vue"
	RepoTypePython     RepoType = "python"
	RepoTypeRust       RepoType = "rust"
	RepoTypeTypeScript RepoType = "typescript"
	RepoTypeJava       RepoType = "java"
	RepoTypeUnknown    RepoType = "unknown"
)

type ReviewResult string

const (
	ReviewPass    ReviewResult = "pass"
	ReviewFail    ReviewResult = "fail"
	ReviewPending ReviewResult = "pending"
	ReviewNone    ReviewResult = "none"
)

type Artifact struct {
	ID              string         `json:"id"`
	WorkflowID      string         `json:"workflow_id"`
	TaskType        TaskType       `json:"task_type"`
	WorkflowType    string         `json:"workflow_type"`
	RepoType        RepoType       `json:"repo_type"`
	ProblemClass    ProblemClass   `json:"problem_class"`
	AgentsUsed      []string       `json:"agents_used"`
	RootCause       string         `json:"root_cause"`
	SolutionPattern string         `json:"solution_pattern"`
	FilesChanged    []string       `json:"files_changed"`
	ReviewResult    ReviewResult   `json:"review_result"`
	CycleTimeMin    int            `json:"cycle_time_minutes"`
	Success         bool           `json:"success"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       time.Time      `json:"created_at"`
}

type ArtifactConfig struct {
	MinEventsForBuild       int     `json:"min_events_for_build"`
	MaxFilesToTrack         int     `json:"max_files_to_track"`
	MaxAgentsToTrack        int     `json:"max_agents_to_track"`
	MinConfidenceForPattern float64 `json:"min_confidence_for_pattern"`
}

func DefaultArtifactConfig() ArtifactConfig {
	return ArtifactConfig{
		MinEventsForBuild:       2,
		MaxFilesToTrack:         50,
		MaxAgentsToTrack:        10,
		MinConfidenceForPattern: 0.7,
	}
}

type EventForArtifact struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Source    string         `json:"source"`
	Payload   map[string]any `json:"payload"`
}

type ArtifactStats struct {
	TotalArtifacts     int            `json:"total_artifacts"`
	ByProblemClass     map[string]int `json:"by_problem_class"`
	ByRepoType         map[string]int `json:"by_repo_type"`
	ByWorkflowType     map[string]int `json:"by_workflow_type"`
	OverallSuccessRate float64        `json:"overall_success_rate"`
	AvgCycleTimeMin    float64        `json:"avg_cycle_time_min"`
}
