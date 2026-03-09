package agent_evolution

import (
	"context"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type Store interface {
	SaveCandidate(ctx context.Context, c *CandidateAgent) error
	GetCandidateByID(ctx context.Context, id string) (*CandidateAgent, error)
	GetCandidateByName(ctx context.Context, agentName string) (*CandidateAgent, error)
	ListCandidates(ctx context.Context, status string, limit int) ([]CandidateAgent, error)
	UpdateCandidateStatus(ctx context.Context, id string, status CandidateStatus) error
	DeleteCandidate(ctx context.Context, id string) error

	SaveExperiment(ctx context.Context, e *AgentExperiment) error
	GetExperimentByID(ctx context.Context, id string) (*AgentExperiment, error)
	GetExperimentByCandidateID(ctx context.Context, candidateID string) (*AgentExperiment, error)
	ListRunningExperiments(ctx context.Context) ([]AgentExperiment, error)
	ListExperiments(ctx context.Context, status string, limit int) ([]AgentExperiment, error)
	UpdateExperimentBandit(ctx context.Context, id string, bandit BanditState, runsCandidate, runsBaseline int) error
	UpdateExperimentStatus(ctx context.Context, id string, status ExperimentStatus, winner ExperimentWinner) error
	DeleteExperiment(ctx context.Context, id string) error

	SaveExperimentResult(ctx context.Context, r *ExperimentResult) error
	GetExperimentResults(ctx context.Context, experimentID string) ([]ExperimentResult, error)
	GetExperimentMetrics(ctx context.Context, experimentID string) (ExperimentMetrics, error)
}

type DBStore struct {
	db *db.DB
}

func NewDBStore(database *db.DB) *DBStore {
	return &DBStore{db: database}
}

func (s *DBStore) SaveCandidate(ctx context.Context, c *CandidateAgent) error {
	dbCandidate := candidateToDB(c)
	return s.db.SaveAgentCandidate(dbCandidate)
}

func (s *DBStore) GetCandidateByID(ctx context.Context, id string) (*CandidateAgent, error) {
	dbCandidate, err := s.db.GetAgentCandidateByID(id)
	if err != nil {
		return nil, err
	}
	if dbCandidate == nil {
		return nil, nil
	}
	return dbCandidateToModel(dbCandidate), nil
}

func (s *DBStore) GetCandidateByName(ctx context.Context, agentName string) (*CandidateAgent, error) {
	dbCandidate, err := s.db.GetAgentCandidateByName(agentName)
	if err != nil {
		return nil, err
	}
	if dbCandidate == nil {
		return nil, nil
	}
	return dbCandidateToModel(dbCandidate), nil
}

func (s *DBStore) ListCandidates(ctx context.Context, status string, limit int) ([]CandidateAgent, error) {
	statusStr := ""
	if status != "" {
		statusStr = string(status)
	}
	dbCandidates, err := s.db.ListAgentCandidates(statusStr, limit)
	if err != nil {
		return nil, err
	}
	candidates := make([]CandidateAgent, len(dbCandidates))
	for i, c := range dbCandidates {
		candidates[i] = *dbCandidateToModel(&c)
	}
	return candidates, nil
}

func (s *DBStore) UpdateCandidateStatus(ctx context.Context, id string, status CandidateStatus) error {
	return s.db.UpdateAgentCandidateStatus(id, string(status))
}

func (s *DBStore) DeleteCandidate(ctx context.Context, id string) error {
	return s.db.DeleteAgentCandidate(id)
}

func (s *DBStore) SaveExperiment(ctx context.Context, e *AgentExperiment) error {
	dbExperiment := experimentToDB(e)
	return s.db.SaveAgentExperiment(dbExperiment)
}

func (s *DBStore) GetExperimentByID(ctx context.Context, id string) (*AgentExperiment, error) {
	dbExperiment, err := s.db.GetAgentExperimentByID(id)
	if err != nil {
		return nil, err
	}
	if dbExperiment == nil {
		return nil, nil
	}
	return dbExperimentToModel(dbExperiment), nil
}

func (s *DBStore) GetExperimentByCandidateID(ctx context.Context, candidateID string) (*AgentExperiment, error) {
	dbExperiment, err := s.db.GetAgentExperimentByCandidateID(candidateID)
	if err != nil {
		return nil, err
	}
	if dbExperiment == nil {
		return nil, nil
	}
	return dbExperimentToModel(dbExperiment), nil
}

func (s *DBStore) ListRunningExperiments(ctx context.Context) ([]AgentExperiment, error) {
	dbExperiments, err := s.db.ListRunningAgentExperiments()
	if err != nil {
		return nil, err
	}
	experiments := make([]AgentExperiment, len(dbExperiments))
	for i, e := range dbExperiments {
		experiments[i] = *dbExperimentToModel(&e)
	}
	return experiments, nil
}

func (s *DBStore) ListExperiments(ctx context.Context, status string, limit int) ([]AgentExperiment, error) {
	dbExperiments, err := s.db.ListAgentExperiments(status, limit)
	if err != nil {
		return nil, err
	}
	experiments := make([]AgentExperiment, len(dbExperiments))
	for i, e := range dbExperiments {
		experiments[i] = *dbExperimentToModel(&e)
	}
	return experiments, nil
}

func (s *DBStore) UpdateExperimentBandit(ctx context.Context, id string, bandit BanditState, runsCandidate, runsBaseline int) error {
	dbBandit := db.AgentBanditState{
		CandidateAlpha: bandit.CandidateAlpha,
		CandidateBeta:  bandit.CandidateBeta,
		BaselineAlpha:  bandit.BaselineAlpha,
		BaselineBeta:   bandit.BaselineBeta,
	}
	return s.db.UpdateAgentExperimentBandit(id, dbBandit, runsCandidate, runsBaseline)
}

func (s *DBStore) UpdateExperimentStatus(ctx context.Context, id string, status ExperimentStatus, winner ExperimentWinner) error {
	return s.db.UpdateAgentExperimentStatus(id, string(status), string(winner))
}

func (s *DBStore) DeleteExperiment(ctx context.Context, id string) error {
	return s.db.DeleteAgentExperiment(id)
}

func (s *DBStore) SaveExperimentResult(ctx context.Context, r *ExperimentResult) error {
	dbResult := &db.AgentExperimentResult{
		ExperimentID:  r.ExperimentID,
		WorkflowID:    r.WorkflowID,
		TaskType:      r.TaskType,
		UsedCandidate: r.UsedCandidate,
		Success:       r.Success,
		CycleTimeMs:   r.CycleTimeMs,
		ReviewPassed:  r.ReviewPassed,
		ReworkCount:   r.ReworkCount,
	}
	err := s.db.SaveAgentExperimentResult(dbResult)
	if err == nil {
		r.ID = int(dbResult.ID)
	}
	return err
}

func (s *DBStore) GetExperimentResults(ctx context.Context, experimentID string) ([]ExperimentResult, error) {
	dbResults, err := s.db.GetAgentExperimentResults(experimentID)
	if err != nil {
		return nil, err
	}
	results := make([]ExperimentResult, len(dbResults))
	for i, r := range dbResults {
		results[i] = ExperimentResult{
			ID:            int(r.ID),
			ExperimentID:  r.ExperimentID,
			WorkflowID:    r.WorkflowID,
			TaskType:      r.TaskType,
			UsedCandidate: r.UsedCandidate,
			Success:       r.Success,
			CycleTimeMs:   r.CycleTimeMs,
			ReviewPassed:  r.ReviewPassed,
			ReworkCount:   r.ReworkCount,
		}
	}
	return results, nil
}

func (s *DBStore) GetExperimentMetrics(ctx context.Context, experimentID string) (ExperimentMetrics, error) {
	dbCand, dbBase, err := s.db.GetAgentExperimentMetrics(experimentID)
	if err != nil {
		return ExperimentMetrics{}, err
	}
	metrics := ExperimentMetrics{
		CandidateSuccessRate: dbCand.SuccessRate,
		BaselineSuccessRate:  dbBase.SuccessRate,
		CandidateCycleTime:   int64(dbCand.AvgCycleTimeMs),
		BaselineCycleTime:    int64(dbBase.AvgCycleTimeMs),
		CandidateReviewRate:  dbCand.ReviewPassRate,
		BaselineReviewRate:   dbBase.ReviewPassRate,
		CandidateReworkRate:  dbCand.ReworkRate,
		BaselineReworkRate:   dbBase.ReworkRate,
		RunsCandidate:        dbCand.SampleSize,
		RunsBaseline:         dbBase.SampleSize,
	}
	if dbCand.SampleSize > 0 && dbBase.SampleSize > 0 {
		metrics.SuccessRateDelta = dbCand.SuccessRate - dbBase.SuccessRate
		metrics.CycleTimeDelta = int64(dbCand.AvgCycleTimeMs - dbBase.AvgCycleTimeMs)
	}
	return metrics, nil
}

func candidateToDB(c *CandidateAgent) *db.AgentCandidate {
	return &db.AgentCandidate{
		ID:              c.ID,
		AgentName:       c.AgentName,
		BaseAgent:       c.BaseAgent,
		Specialization:  c.Specialization,
		Reason:          c.Reason,
		Confidence:      c.Confidence,
		PromptDiff:      promptDiffToMap(c.PromptDiff),
		Status:          string(c.Status),
		Evidence:        c.Evidence,
		OpportunityType: string(c.OpportunityType),
		CreatedAt:       c.CreatedAt.Format("2006-01-02T15:04:05.999Z"),
		UpdatedAt:       c.UpdatedAt.Format("2006-01-02T15:04:05.999Z"),
	}
}

func dbCandidateToModel(c *db.AgentCandidate) *CandidateAgent {
	return &CandidateAgent{
		ID:              c.ID,
		AgentName:       c.AgentName,
		BaseAgent:       c.BaseAgent,
		Specialization:  c.Specialization,
		Reason:          c.Reason,
		Confidence:      c.Confidence,
		PromptDiff:      mapToPromptDiff(c.PromptDiff),
		Status:          CandidateStatus(c.Status),
		OpportunityType: OpportunityType(c.OpportunityType),
		Evidence:        c.Evidence,
	}
}

func promptDiffToMap(p PromptDiff) map[string]interface{} {
	return map[string]interface{}{
		"additions":     p.Additions,
		"modifications": p.Modifications,
		"new_focus":     p.NewFocus,
	}
}

func mapToPromptDiff(m map[string]interface{}) PromptDiff {
	p := PromptDiff{}
	if v, ok := m["additions"]; ok {
		if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					p.Additions = append(p.Additions, s)
				}
			}
		}
	}
	if v, ok := m["modifications"]; ok {
		if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					p.Modifications = append(p.Modifications, s)
				}
			}
		}
	}
	if v, ok := m["new_focus"].(string); ok {
		p.NewFocus = v
	}
	return p
}

func experimentToDB(e *AgentExperiment) *db.AgentExperiment {
	return &db.AgentExperiment{
		ID:             e.ID,
		CandidateID:    e.CandidateID,
		CandidateAgent: e.CandidateAgent,
		BaselineAgent:  e.BaselineAgent,
		TrafficPercent: e.TrafficPercent,
		Status:         string(e.Status),
		SampleSize:     e.SampleSize,
		RunsCandidate:  e.RunsCandidate,
		RunsBaseline:   e.RunsBaseline,
		BanditState: db.AgentBanditState{
			CandidateAlpha: e.BanditState.CandidateAlpha,
			CandidateBeta:  e.BanditState.CandidateBeta,
			BaselineAlpha:  e.BanditState.BaselineAlpha,
			BaselineBeta:   e.BanditState.BaselineBeta,
		},
		StartedAt:   e.StartedAt,
		CompletedAt: e.CompletedAt,
		Winner:      string(e.Winner),
	}
}

func dbExperimentToModel(e *db.AgentExperiment) *AgentExperiment {
	return &AgentExperiment{
		ID:             e.ID,
		CandidateID:    e.CandidateID,
		CandidateAgent: e.CandidateAgent,
		BaselineAgent:  e.BaselineAgent,
		TrafficPercent: e.TrafficPercent,
		Status:         ExperimentStatus(e.Status),
		SampleSize:     e.SampleSize,
		RunsCandidate:  e.RunsCandidate,
		RunsBaseline:   e.RunsBaseline,
		BanditState: BanditState{
			CandidateAlpha: e.BanditState.CandidateAlpha,
			CandidateBeta:  e.BanditState.CandidateBeta,
			BaselineAlpha:  e.BanditState.BaselineAlpha,
			BaselineBeta:   e.BanditState.BaselineBeta,
		},
		StartedAt:   e.StartedAt,
		CompletedAt: e.CompletedAt,
		Winner:      ExperimentWinner(e.Winner),
	}
}
