package workflow_synthesis

import (
	"context"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type Store interface {
	SaveCandidate(ctx context.Context, c *db.WorkflowCandidate) error
	GetCandidateByID(ctx context.Context, id string) (*db.WorkflowCandidate, error)
	GetCandidatesByTaskType(ctx context.Context, taskType, repoType string) ([]db.WorkflowCandidate, error)
	GetCandidateForTask(ctx context.Context, taskType, repoType string) (*db.WorkflowCandidate, error)
	GetExperimentForTask(ctx context.Context, taskType, repoType string) (*db.WorkflowExperiment, *db.WorkflowCandidate, error)
	UpdateCandidateStatus(ctx context.Context, id, status string) error
	ListCandidates(ctx context.Context, status string, limit int) ([]db.WorkflowCandidate, error)

	SaveExperiment(ctx context.Context, e *db.WorkflowExperiment) error
	GetExperimentByID(ctx context.Context, id string) (*db.WorkflowExperiment, error)
	ListRunningExperiments(ctx context.Context) ([]db.WorkflowExperiment, error)
	UpdateExperimentBandit(ctx context.Context, id string, bandit db.BanditState, runsCandidate, runsBaseline int) error
	UpdateExperimentStatus(ctx context.Context, id, status string) error

	SaveExperimentResult(ctx context.Context, r *db.ExperimentResult) error
	GetExperimentResults(ctx context.Context, experimentID string) ([]db.ExperimentResult, error)
	GetExperimentMetrics(ctx context.Context, experimentID string) (candidate, baseline db.EvaluationMetrics, err error)

	GetTrajectoryPatterns(ctx context.Context, limit int) ([]db.TrajectoryPattern, error)
}

type DBStore struct {
	db *db.DB
}

func NewDBStore(database *db.DB) *DBStore {
	return &DBStore{db: database}
}

func (s *DBStore) SaveCandidate(ctx context.Context, c *db.WorkflowCandidate) error {
	return s.db.SaveWorkflowCandidate(c)
}

func (s *DBStore) GetCandidateByID(ctx context.Context, id string) (*db.WorkflowCandidate, error) {
	return s.db.GetWorkflowCandidateByID(id)
}

func (s *DBStore) GetCandidatesByTaskType(ctx context.Context, taskType, repoType string) ([]db.WorkflowCandidate, error) {
	return s.db.GetWorkflowCandidatesByTaskType(taskType, repoType)
}

func (s *DBStore) GetCandidateForTask(ctx context.Context, taskType, repoType string) (*db.WorkflowCandidate, error) {
	return s.db.GetCandidateWorkflowForTask(taskType, repoType)
}

func (s *DBStore) GetExperimentForTask(ctx context.Context, taskType, repoType string) (*db.WorkflowExperiment, *db.WorkflowCandidate, error) {
	return s.db.GetExperimentForTask(taskType, repoType)
}

func (s *DBStore) UpdateCandidateStatus(ctx context.Context, id, status string) error {
	return s.db.UpdateWorkflowCandidateStatus(id, status)
}

func (s *DBStore) ListCandidates(ctx context.Context, status string, limit int) ([]db.WorkflowCandidate, error) {
	return s.db.ListWorkflowCandidates(status, limit)
}

func (s *DBStore) SaveExperiment(ctx context.Context, e *db.WorkflowExperiment) error {
	e.StartedAt = time.Now()
	return s.db.SaveWorkflowExperiment(e)
}

func (s *DBStore) GetExperimentByID(ctx context.Context, id string) (*db.WorkflowExperiment, error) {
	return s.db.GetWorkflowExperimentByID(id)
}

func (s *DBStore) ListRunningExperiments(ctx context.Context) ([]db.WorkflowExperiment, error) {
	return s.db.ListRunningExperiments()
}

func (s *DBStore) UpdateExperimentBandit(ctx context.Context, id string, bandit db.BanditState, runsCandidate, runsBaseline int) error {
	return s.db.UpdateExperimentBandit(id, bandit, runsCandidate, runsBaseline)
}

func (s *DBStore) UpdateExperimentStatus(ctx context.Context, id, status string) error {
	return s.db.UpdateExperimentStatus(id, status)
}

func (s *DBStore) SaveExperimentResult(ctx context.Context, r *db.ExperimentResult) error {
	return s.db.SaveExperimentResult(r)
}

func (s *DBStore) GetExperimentResults(ctx context.Context, experimentID string) ([]db.ExperimentResult, error) {
	return s.db.GetExperimentResults(experimentID)
}

func (s *DBStore) GetExperimentMetrics(ctx context.Context, experimentID string) (candidate, baseline db.EvaluationMetrics, err error) {
	return s.db.GetExperimentMetrics(experimentID)
}

func (s *DBStore) GetTrajectoryPatterns(ctx context.Context, limit int) ([]db.TrajectoryPattern, error) {
	return s.db.ListTrajectoryPatterns(limit)
}
