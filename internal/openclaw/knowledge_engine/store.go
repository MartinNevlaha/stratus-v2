package knowledge_engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type KnowledgeStore interface {
	SaveSolutionPattern(ctx context.Context, pattern SolutionPattern) error
	GetSolutionPatternByID(ctx context.Context, id string) (*SolutionPattern, error)
	ListSolutionPatterns(ctx context.Context, filters SolutionPatternFilters) ([]SolutionPattern, error)
	GetBestSolutionForProblem(ctx context.Context, problemClass, repoType string) (*SolutionPattern, error)

	SaveProblemStats(ctx context.Context, stats ProblemStats) error
	GetProblemStatsByID(ctx context.Context, id string) (*ProblemStats, error)
	GetProblemStatsByClass(ctx context.Context, problemClass string) (*ProblemStats, error)
	GetProblemStatsByClassAndRepo(ctx context.Context, problemClass, repoType string) (*ProblemStats, error)
	ListProblemStats(ctx context.Context, filters ProblemStatsFilters) ([]ProblemStats, error)
	GetBestAgentForProblem(ctx context.Context, problemClass, repoType string) (string, float64, error)

	CountSolutionPatterns(ctx context.Context) (int, error)
	CountProblemStats(ctx context.Context) (int, error)
}

type SolutionPattern struct {
	ID               string    `json:"id"`
	ProblemClass     string    `json:"problem_class"`
	SolutionPattern  string    `json:"solution_pattern"`
	RepoType         string    `json:"repo_type"`
	SuccessRate      float64   `json:"success_rate"`
	OccurrenceCount  int       `json:"occurrence_count"`
	ExampleArtifacts []string  `json:"example_artifacts"`
	Confidence       float64   `json:"confidence"`
	FirstSeen        time.Time `json:"first_seen"`
	LastSeen         time.Time `json:"last_seen"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ProblemStats struct {
	ID              string             `json:"id"`
	ProblemClass    string             `json:"problem_class"`
	RepoType        string             `json:"repo_type"`
	BestAgent       string             `json:"best_agent"`
	BestWorkflow    string             `json:"best_workflow"`
	SuccessRate     float64            `json:"success_rate"`
	OccurrenceCount int                `json:"occurrence_count"`
	AvgCycleTime    int                `json:"avg_cycle_time"`
	AgentsSuccess   map[string]float64 `json:"agents_success"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type SolutionPatternFilters struct {
	ProblemClass   string
	RepoType       string
	MinSuccessRate float64
	Limit          int
}

type ProblemStatsFilters struct {
	ProblemClass string
	RepoType     string
	Limit        int
}

type DBKnowledgeStore struct {
	database *db.DB
}

func NewDBKnowledgeStore(database *db.DB) *DBKnowledgeStore {
	return &DBKnowledgeStore{database: database}
}

func (s *DBKnowledgeStore) SaveSolutionPattern(ctx context.Context, pattern SolutionPattern) error {
	dbPattern := solutionPatternToDB(pattern)
	return s.database.SaveSolutionPattern(dbPattern)
}

func (s *DBKnowledgeStore) GetSolutionPatternByID(ctx context.Context, id string) (*SolutionPattern, error) {
	dbPattern, err := s.database.GetSolutionPatternByID(id)
	if err != nil {
		return nil, fmt.Errorf("get solution pattern: %w", err)
	}
	if dbPattern == nil {
		return nil, nil
	}
	pattern := dbSolutionPatternToModel(*dbPattern)
	return &pattern, nil
}

func (s *DBKnowledgeStore) ListSolutionPatterns(ctx context.Context, filters SolutionPatternFilters) ([]SolutionPattern, error) {
	dbFilters := db.SolutionPatternFilters{
		ProblemClass:   filters.ProblemClass,
		RepoType:       filters.RepoType,
		MinSuccessRate: filters.MinSuccessRate,
		Limit:          filters.Limit,
	}

	dbPatterns, err := s.database.ListSolutionPatterns(dbFilters)
	if err != nil {
		return nil, fmt.Errorf("list solution patterns: %w", err)
	}

	patterns := make([]SolutionPattern, len(dbPatterns))
	for i, p := range dbPatterns {
		patterns[i] = dbSolutionPatternToModel(p)
	}

	return patterns, nil
}

func (s *DBKnowledgeStore) GetBestSolutionForProblem(ctx context.Context, problemClass, repoType string) (*SolutionPattern, error) {
	dbPattern, err := s.database.GetBestSolutionForProblem(problemClass, repoType)
	if err != nil {
		return nil, fmt.Errorf("get best solution: %w", err)
	}
	if dbPattern == nil {
		return nil, nil
	}
	pattern := dbSolutionPatternToModel(*dbPattern)
	return &pattern, nil
}

func (s *DBKnowledgeStore) SaveProblemStats(ctx context.Context, stats ProblemStats) error {
	dbStats := problemStatsToDB(stats)
	return s.database.SaveProblemStats(dbStats)
}

func (s *DBKnowledgeStore) GetProblemStatsByID(ctx context.Context, id string) (*ProblemStats, error) {
	dbStats, err := s.database.GetProblemStatsByID(id)
	if err != nil {
		return nil, fmt.Errorf("get problem stats: %w", err)
	}
	if dbStats == nil {
		return nil, nil
	}
	stats := dbProblemStatsToModel(*dbStats)
	return &stats, nil
}

func (s *DBKnowledgeStore) GetProblemStatsByClass(ctx context.Context, problemClass string) (*ProblemStats, error) {
	dbStats, err := s.database.GetProblemStatsByClass(problemClass)
	if err != nil {
		return nil, fmt.Errorf("get problem stats by class: %w", err)
	}
	if dbStats == nil {
		return nil, nil
	}
	stats := dbProblemStatsToModel(*dbStats)
	return &stats, nil
}

func (s *DBKnowledgeStore) GetProblemStatsByClassAndRepo(ctx context.Context, problemClass, repoType string) (*ProblemStats, error) {
	dbStats, err := s.database.GetProblemStatsByClassAndRepo(problemClass, repoType)
	if err != nil {
		return nil, fmt.Errorf("get problem stats by class and repo: %w", err)
	}
	if dbStats == nil {
		return nil, nil
	}
	stats := dbProblemStatsToModel(*dbStats)
	return &stats, nil
}

func (s *DBKnowledgeStore) ListProblemStats(ctx context.Context, filters ProblemStatsFilters) ([]ProblemStats, error) {
	dbFilters := db.ProblemStatsFilters{
		ProblemClass: filters.ProblemClass,
		RepoType:     filters.RepoType,
		Limit:        filters.Limit,
	}

	dbStats, err := s.database.ListProblemStats(dbFilters)
	if err != nil {
		return nil, fmt.Errorf("list problem stats: %w", err)
	}

	stats := make([]ProblemStats, len(dbStats))
	for i, s := range dbStats {
		stats[i] = dbProblemStatsToModel(s)
	}

	return stats, nil
}

func (s *DBKnowledgeStore) GetBestAgentForProblem(ctx context.Context, problemClass, repoType string) (string, float64, error) {
	return s.database.GetBestAgentForProblem(problemClass, repoType)
}

func (s *DBKnowledgeStore) CountSolutionPatterns(ctx context.Context) (int, error) {
	return s.database.CountSolutionPatterns()
}

func (s *DBKnowledgeStore) CountProblemStats(ctx context.Context) (int, error) {
	return s.database.CountProblemStats()
}

type ArtifactQuery interface {
	GetSuccessfulArtifactsWithSolution(ctx context.Context, limit int) ([]ArtifactData, error)
	ListArtifacts(ctx context.Context, filters ArtifactFilterOptions) ([]ArtifactData, error)
	CountArtifacts(ctx context.Context) (int, error)
	GetProblemClassStats(ctx context.Context) ([]map[string]any, error)
	GetAgentSuccessByProblem(ctx context.Context) ([]map[string]any, error)
}

type ArtifactData struct {
	ID              string
	WorkflowID      string
	TaskType        string
	WorkflowType    string
	RepoType        string
	ProblemClass    string
	AgentsUsed      []string
	RootCause       string
	SolutionPattern string
	FilesChanged    []string
	ReviewResult    string
	CycleTimeMin    int
	Success         bool
	Metadata        map[string]any
	CreatedAt       time.Time
}

type ArtifactFilterOptions struct {
	WorkflowType string
	ProblemClass string
	RepoType     string
	Success      *bool
	Limit        int
	Offset       int
}

type DBArtifactQuery struct {
	database *db.DB
}

func NewDBArtifactQuery(database *db.DB) *DBArtifactQuery {
	return &DBArtifactQuery{database: database}
}

func (q *DBArtifactQuery) GetSuccessfulArtifactsWithSolution(ctx context.Context, limit int) ([]ArtifactData, error) {
	artifacts, err := q.database.GetSuccessfulArtifactsWithSolution(limit)
	if err != nil {
		return nil, fmt.Errorf("get successful artifacts: %w", err)
	}

	result := make([]ArtifactData, len(artifacts))
	for i, a := range artifacts {
		result[i] = dbArtifactToArtifactData(a)
	}

	return result, nil
}

func (q *DBArtifactQuery) ListArtifacts(ctx context.Context, filters ArtifactFilterOptions) ([]ArtifactData, error) {
	dbFilters := db.ArtifactFilters{
		WorkflowType: filters.WorkflowType,
		ProblemClass: filters.ProblemClass,
		RepoType:     filters.RepoType,
		Success:      filters.Success,
		Limit:        filters.Limit,
		Offset:       filters.Offset,
	}

	artifacts, err := q.database.ListArtifacts(dbFilters)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}

	result := make([]ArtifactData, len(artifacts))
	for i, a := range artifacts {
		result[i] = dbArtifactToArtifactData(a)
	}

	return result, nil
}

func (q *DBArtifactQuery) CountArtifacts(ctx context.Context) (int, error) {
	return q.database.CountArtifacts()
}

func (q *DBArtifactQuery) GetProblemClassStats(ctx context.Context) ([]map[string]any, error) {
	return q.database.GetProblemClassStats()
}

func (q *DBArtifactQuery) GetAgentSuccessByProblem(ctx context.Context) ([]map[string]any, error) {
	return q.database.GetAgentSuccessByProblem()
}

func solutionPatternToDB(p SolutionPattern) *db.SolutionPattern {
	return &db.SolutionPattern{
		ID:               p.ID,
		ProblemClass:     p.ProblemClass,
		SolutionPattern:  p.SolutionPattern,
		RepoType:         p.RepoType,
		SuccessRate:      p.SuccessRate,
		OccurrenceCount:  p.OccurrenceCount,
		ExampleArtifacts: p.ExampleArtifacts,
		Confidence:       p.Confidence,
		FirstSeen:        p.FirstSeen,
		LastSeen:         p.LastSeen,
	}
}

func dbSolutionPatternToModel(p db.SolutionPattern) SolutionPattern {
	return SolutionPattern{
		ID:               p.ID,
		ProblemClass:     p.ProblemClass,
		SolutionPattern:  p.SolutionPattern,
		RepoType:         p.RepoType,
		SuccessRate:      p.SuccessRate,
		OccurrenceCount:  p.OccurrenceCount,
		ExampleArtifacts: p.ExampleArtifacts,
		Confidence:       p.Confidence,
		FirstSeen:        p.FirstSeen,
		LastSeen:         p.LastSeen,
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
	}
}

func problemStatsToDB(s ProblemStats) *db.ProblemStats {
	return &db.ProblemStats{
		ID:              s.ID,
		ProblemClass:    s.ProblemClass,
		RepoType:        s.RepoType,
		BestAgent:       s.BestAgent,
		BestWorkflow:    s.BestWorkflow,
		SuccessRate:     s.SuccessRate,
		OccurrenceCount: s.OccurrenceCount,
		AvgCycleTime:    s.AvgCycleTime,
		AgentsSuccess:   s.AgentsSuccess,
	}
}

func dbProblemStatsToModel(s db.ProblemStats) ProblemStats {
	return ProblemStats{
		ID:              s.ID,
		ProblemClass:    s.ProblemClass,
		RepoType:        s.RepoType,
		BestAgent:       s.BestAgent,
		BestWorkflow:    s.BestWorkflow,
		SuccessRate:     s.SuccessRate,
		OccurrenceCount: s.OccurrenceCount,
		AvgCycleTime:    s.AvgCycleTime,
		AgentsSuccess:   s.AgentsSuccess,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

func dbArtifactToArtifactData(a db.Artifact) ArtifactData {
	var createdAt time.Time
	if a.CreatedAt != "" {
		createdAt, _ = time.Parse(time.RFC3339Nano, a.CreatedAt)
	}

	var metadata map[string]any
	if a.Metadata != nil {
		metadata = a.Metadata
	} else {
		metadata = make(map[string]any)
	}

	return ArtifactData{
		ID:              a.ID,
		WorkflowID:      a.WorkflowID,
		TaskType:        a.TaskType,
		WorkflowType:    a.WorkflowType,
		RepoType:        a.RepoType,
		ProblemClass:    a.ProblemClass,
		AgentsUsed:      a.AgentsUsed,
		RootCause:       a.RootCause,
		SolutionPattern: a.SolutionPattern,
		FilesChanged:    a.FilesChanged,
		ReviewResult:    a.ReviewResult,
		CycleTimeMin:    a.CycleTimeMin,
		Success:         a.Success,
		Metadata:        metadata,
		CreatedAt:       createdAt,
	}
}

func parseAgentsSuccess(data any) map[string]float64 {
	result := make(map[string]float64)
	switch v := data.(type) {
	case map[string]float64:
		return v
	case map[string]any:
		for key, val := range v {
			if f, ok := val.(float64); ok {
				result[key] = f
			}
		}
	case string:
		if v != "" {
			json.Unmarshal([]byte(v), &result)
		}
	}
	return result
}
