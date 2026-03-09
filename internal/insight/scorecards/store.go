package scorecards

import (
	"context"
	"fmt"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type ScorecardStore interface {
	SaveAgentScorecard(ctx context.Context, scorecard AgentScorecard) error
	SaveWorkflowScorecard(ctx context.Context, scorecard WorkflowScorecard) error
	GetAgentScorecards(ctx context.Context, window Window, sortBy string, sortDir string, limit int) ([]AgentScorecard, error)
	GetWorkflowScorecards(ctx context.Context, window Window, sortBy string, sortDir string, limit int) ([]WorkflowScorecard, error)
	GetAgentScorecardByName(ctx context.Context, agentName string, window Window) (*AgentScorecard, error)
	GetWorkflowScorecardByType(ctx context.Context, workflowType string, window Window) (*WorkflowScorecard, error)
	GetScorecardHighlights(ctx context.Context, window Window) (map[string]interface{}, error)
}

type DBScorecardStore struct {
	database *db.DB
}

func NewDBScorecardStore(database *db.DB) *DBScorecardStore {
	return &DBScorecardStore{database: database}
}

func (s *DBScorecardStore) SaveAgentScorecard(ctx context.Context, scorecard AgentScorecard) error {
	dbCard := agentScorecardToDB(scorecard)
	return s.database.SaveAgentScorecard(dbCard)
}

func (s *DBScorecardStore) SaveWorkflowScorecard(ctx context.Context, scorecard WorkflowScorecard) error {
	dbCard := workflowScorecardToDB(scorecard)
	return s.database.SaveWorkflowScorecard(dbCard)
}

func (s *DBScorecardStore) GetAgentScorecards(ctx context.Context, window Window, sortBy string, sortDir string, limit int) ([]AgentScorecard, error) {
	dbCards, err := s.database.ListAgentScorecards(string(window), sortBy, sortDir, limit)
	if err != nil {
		return nil, fmt.Errorf("list agent scorecards: %w", err)
	}

	cards := make([]AgentScorecard, len(dbCards))
	for i, c := range dbCards {
		cards[i] = dbAgentScorecardToModel(c)
	}
	return cards, nil
}

func (s *DBScorecardStore) GetWorkflowScorecards(ctx context.Context, window Window, sortBy string, sortDir string, limit int) ([]WorkflowScorecard, error) {
	dbCards, err := s.database.ListWorkflowScorecards(string(window), sortBy, sortDir, limit)
	if err != nil {
		return nil, fmt.Errorf("list workflow scorecards: %w", err)
	}

	cards := make([]WorkflowScorecard, len(dbCards))
	for i, c := range dbCards {
		cards[i] = dbWorkflowScorecardToModel(c)
	}
	return cards, nil
}

func (s *DBScorecardStore) GetAgentScorecardByName(ctx context.Context, agentName string, window Window) (*AgentScorecard, error) {
	dbCard, err := s.database.GetAgentScorecardByName(agentName, string(window))
	if err != nil {
		return nil, fmt.Errorf("get agent scorecard: %w", err)
	}
	if dbCard == nil {
		return nil, nil
	}

	card := dbAgentScorecardToModel(*dbCard)
	return &card, nil
}

func (s *DBScorecardStore) GetWorkflowScorecardByType(ctx context.Context, workflowType string, window Window) (*WorkflowScorecard, error) {
	dbCard, err := s.database.GetWorkflowScorecardByType(workflowType, string(window))
	if err != nil {
		return nil, fmt.Errorf("get workflow scorecard: %w", err)
	}
	if dbCard == nil {
		return nil, nil
	}

	card := dbWorkflowScorecardToModel(*dbCard)
	return &card, nil
}

func (s *DBScorecardStore) GetScorecardHighlights(ctx context.Context, window Window) (map[string]interface{}, error) {
	return s.database.GetScorecardHighlights(string(window))
}

func agentScorecardToDB(c AgentScorecard) *db.AgentScorecard {
	return &db.AgentScorecard{
		ID:              c.ID,
		AgentName:       c.AgentName,
		Window:          string(c.Window),
		WindowStart:     c.WindowStart.Format(time.RFC3339Nano),
		WindowEnd:       c.WindowEnd.Format(time.RFC3339Nano),
		TotalRuns:       c.TotalRuns,
		SuccessRate:     c.SuccessRate,
		FailureRate:     c.FailureRate,
		ReviewPassRate:  c.ReviewPassRate,
		ReworkRate:      c.ReworkRate,
		AvgCycleTimeMs:  c.AvgCycleTimeMs,
		RegressionRate:  c.RegressionRate,
		ConfidenceScore: c.ConfidenceScore,
		Trend:           string(c.Trend),
		UpdatedAt:       c.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func dbAgentScorecardToModel(c db.AgentScorecard) AgentScorecard {
	var windowStart, windowEnd, updatedAt time.Time
	if c.WindowStart != "" {
		windowStart, _ = time.Parse(time.RFC3339Nano, c.WindowStart)
	}
	if c.WindowEnd != "" {
		windowEnd, _ = time.Parse(time.RFC3339Nano, c.WindowEnd)
	}
	if c.UpdatedAt != "" {
		updatedAt, _ = time.Parse(time.RFC3339Nano, c.UpdatedAt)
	}

	return AgentScorecard{
		ID:              c.ID,
		AgentName:       c.AgentName,
		Window:          Window(c.Window),
		WindowStart:     windowStart,
		WindowEnd:       windowEnd,
		TotalRuns:       c.TotalRuns,
		SuccessRate:     c.SuccessRate,
		FailureRate:     c.FailureRate,
		ReviewPassRate:  c.ReviewPassRate,
		ReworkRate:      c.ReworkRate,
		AvgCycleTimeMs:  c.AvgCycleTimeMs,
		RegressionRate:  c.RegressionRate,
		ConfidenceScore: c.ConfidenceScore,
		Trend:           Trend(c.Trend),
		UpdatedAt:       updatedAt,
	}
}

func workflowScorecardToDB(c WorkflowScorecard) *db.WorkflowScorecard {
	return &db.WorkflowScorecard{
		ID:                  c.ID,
		WorkflowType:        c.WorkflowType,
		Window:              string(c.Window),
		WindowStart:         c.WindowStart.Format(time.RFC3339Nano),
		WindowEnd:           c.WindowEnd.Format(time.RFC3339Nano),
		TotalRuns:           c.TotalRuns,
		CompletionRate:      c.CompletionRate,
		FailureRate:         c.FailureRate,
		ReviewRejectionRate: c.ReviewRejectionRate,
		ReworkRate:          c.ReworkRate,
		AvgDurationMs:       c.AvgDurationMs,
		ConfidenceScore:     c.ConfidenceScore,
		Trend:               string(c.Trend),
		UpdatedAt:           c.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func dbWorkflowScorecardToModel(c db.WorkflowScorecard) WorkflowScorecard {
	var windowStart, windowEnd, updatedAt time.Time
	if c.WindowStart != "" {
		windowStart, _ = time.Parse(time.RFC3339Nano, c.WindowStart)
	}
	if c.WindowEnd != "" {
		windowEnd, _ = time.Parse(time.RFC3339Nano, c.WindowEnd)
	}
	if c.UpdatedAt != "" {
		updatedAt, _ = time.Parse(time.RFC3339Nano, c.UpdatedAt)
	}

	return WorkflowScorecard{
		ID:                  c.ID,
		WorkflowType:        c.WorkflowType,
		Window:              Window(c.Window),
		WindowStart:         windowStart,
		WindowEnd:           windowEnd,
		TotalRuns:           c.TotalRuns,
		CompletionRate:      c.CompletionRate,
		FailureRate:         c.FailureRate,
		ReviewRejectionRate: c.ReviewRejectionRate,
		ReworkRate:          c.ReworkRate,
		AvgDurationMs:       c.AvgDurationMs,
		ConfidenceScore:     c.ConfidenceScore,
		Trend:               Trend(c.Trend),
		UpdatedAt:           updatedAt,
	}
}
