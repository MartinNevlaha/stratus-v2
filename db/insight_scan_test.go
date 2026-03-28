package db

import (
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()

	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})

	return database
}

func TestTrajectoryScanParsesTextTimestamps(t *testing.T) {
	database := openTestDB(t)

	startedAt := time.Now().UTC().Add(-20 * time.Minute).Round(time.Microsecond)
	completedAt := startedAt.Add(12 * time.Minute)

	traj := &Trajectory{
		WorkflowID:   "wf-traj-1",
		TaskType:     "bug_fix",
		RepoType:     "go",
		WorkflowType: "bug",
		Steps: []TrajectoryStep{
			{
				StepNumber: 1,
				AgentName:  "planner",
				ActionType: "analyze",
				Success:    true,
				Timestamp:  startedAt,
			},
		},
		FinalResult:  "success",
		CycleTimeMin: 12,
		StartedAt:    startedAt,
		CompletedAt:  &completedAt,
	}

	if err := database.SaveTrajectory(traj); err != nil {
		t.Fatalf("save trajectory: %v", err)
	}

	got, err := database.GetTrajectoryByID(traj.ID)
	if err != nil {
		t.Fatalf("get trajectory: %v", err)
	}
	if got == nil {
		t.Fatal("expected trajectory, got nil")
	}
	if !got.StartedAt.Equal(startedAt) {
		t.Fatalf("started_at mismatch: got %s want %s", got.StartedAt.Format(time.RFC3339Nano), startedAt.Format(time.RFC3339Nano))
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
		t.Fatalf("completed_at mismatch: got %v want %s", got.CompletedAt, completedAt.Format(time.RFC3339Nano))
	}

	list, err := database.ListTrajectories(TrajectoryFilters{Limit: 10})
	if err != nil {
		t.Fatalf("list trajectories: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 trajectory in list, got %d", len(list))
	}
}

func TestWorkflowExperimentTaskScanParsesTextAndJSON(t *testing.T) {
	database := openTestDB(t)

	candidate := &WorkflowCandidate{
		WorkflowName: "candidate-bug-workflow",
		TaskType:     "bug_fix",
		RepoType:     "go",
		BaseWorkflow: "bug",
		Steps: []WorkflowStep{
			{Phase: "analysis", NextPhases: []string{"implement"}, AgentHint: "planner"},
		},
		PhaseTransitions: map[string]string{"analysis": "implement"},
		Confidence:       0.88,
		Status:           "candidate",
	}
	if err := database.SaveWorkflowCandidate(candidate); err != nil {
		t.Fatalf("save workflow candidate: %v", err)
	}

	startedAt := time.Now().UTC().Add(-30 * time.Minute).Round(time.Microsecond)
	experiment := &WorkflowExperiment{
		CandidateID:      candidate.ID,
		BaselineWorkflow: "bug",
		TrafficPercent:   20,
		Status:           "running",
		SampleSize:       50,
		RunsCandidate:    7,
		RunsBaseline:     6,
		BanditState: BanditState{
			CandidatePulls:  7,
			BaselinePulls:   6,
			CandidateReward: 5.5,
			BaselineReward:  3.5,
		},
		StartedAt: startedAt,
	}
	if err := database.SaveWorkflowExperiment(experiment); err != nil {
		t.Fatalf("save workflow experiment: %v", err)
	}

	gotExp, gotCand, err := database.GetExperimentForTask("bug_fix", "go")
	if err != nil {
		t.Fatalf("get experiment for task: %v", err)
	}
	if gotExp == nil || gotCand == nil {
		t.Fatal("expected experiment and candidate, got nil")
	}
	if !gotExp.StartedAt.Equal(startedAt) {
		t.Fatalf("experiment started_at mismatch: got %s want %s", gotExp.StartedAt.Format(time.RFC3339Nano), startedAt.Format(time.RFC3339Nano))
	}
	if len(gotCand.Steps) != 1 || gotCand.Steps[0].Phase != "analysis" {
		t.Fatalf("candidate steps were not decoded correctly: %+v", gotCand.Steps)
	}
	if gotCand.PhaseTransitions["analysis"] != "implement" {
		t.Fatalf("candidate phase transitions were not decoded correctly: %+v", gotCand.PhaseTransitions)
	}
}

func TestAgentExperimentScanParsesTextTimestamps(t *testing.T) {
	database := openTestDB(t)

	candidate := &AgentCandidate{
		AgentName:       "agent-candidate-1",
		BaseAgent:       "base-agent",
		Specialization:  "backend",
		Reason:          "performance optimization",
		Confidence:      0.79,
		Status:          "pending",
		OpportunityType: "agent.performance_drop",
	}
	if err := database.SaveAgentCandidate(candidate); err != nil {
		t.Fatalf("save agent candidate: %v", err)
	}

	startedAt := time.Now().UTC().Add(-15 * time.Minute).Round(time.Microsecond)
	completedAt := startedAt.Add(10 * time.Minute)
	exp := &AgentExperiment{
		CandidateID:    candidate.ID,
		CandidateAgent: candidate.AgentName,
		BaselineAgent:  candidate.BaseAgent,
		TrafficPercent: 25,
		Status:         "completed",
		SampleSize:     40,
		RunsCandidate:  20,
		RunsBaseline:   20,
		BanditState: AgentBanditState{
			CandidateAlpha: 5,
			CandidateBeta:  2,
			BaselineAlpha:  3,
			BaselineBeta:   4,
		},
		StartedAt:   startedAt,
		CompletedAt: &completedAt,
		Winner:      "candidate",
	}
	if err := database.SaveAgentExperiment(exp); err != nil {
		t.Fatalf("save agent experiment: %v", err)
	}

	got, err := database.GetAgentExperimentByID(exp.ID)
	if err != nil {
		t.Fatalf("get agent experiment: %v", err)
	}
	if got == nil {
		t.Fatal("expected agent experiment, got nil")
	}
	if !got.StartedAt.Equal(startedAt) {
		t.Fatalf("agent experiment started_at mismatch: got %s want %s", got.StartedAt.Format(time.RFC3339Nano), startedAt.Format(time.RFC3339Nano))
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
		t.Fatalf("agent experiment completed_at mismatch: got %v want %s", got.CompletedAt, completedAt.Format(time.RFC3339Nano))
	}

	list, err := database.ListAgentExperiments("", 10)
	if err != nil {
		t.Fatalf("list agent experiments: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 listed agent experiment, got %d", len(list))
	}
}
