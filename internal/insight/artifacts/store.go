package artifacts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type ArtifactStore interface {
	SaveArtifact(ctx context.Context, artifact Artifact) error
	GetArtifactByWorkflowID(ctx context.Context, workflowID string) (*Artifact, error)
	GetArtifactByID(ctx context.Context, id string) (*Artifact, error)
	ListArtifacts(ctx context.Context, filters ArtifactFilterOptions) ([]Artifact, error)
	GetArtifactsByProblemClass(ctx context.Context, problemClass ProblemClass, limit int) ([]Artifact, error)
	GetSuccessfulArtifactsWithSolution(ctx context.Context, limit int) ([]Artifact, error)
	CountArtifacts(ctx context.Context) (int, error)
}

type ArtifactFilterOptions struct {
	WorkflowType string
	ProblemClass string
	RepoType     string
	Success      *bool
	Limit        int
	Offset       int
}

type DBArtifactStore struct {
	database *db.DB
}

func NewDBArtifactStore(database *db.DB) *DBArtifactStore {
	return &DBArtifactStore{database: database}
}

func (s *DBArtifactStore) SaveArtifact(ctx context.Context, artifact Artifact) error {
	dbArtifact := artifactToDB(artifact)
	return s.database.SaveArtifact(dbArtifact)
}

func (s *DBArtifactStore) GetArtifactByWorkflowID(ctx context.Context, workflowID string) (*Artifact, error) {
	dbArtifact, err := s.database.GetArtifactByWorkflowID(workflowID)
	if err != nil {
		return nil, fmt.Errorf("get artifact by workflow: %w", err)
	}
	if dbArtifact == nil {
		return nil, nil
	}
	artifact := dbArtifactToArtifact(*dbArtifact)
	return &artifact, nil
}

func (s *DBArtifactStore) GetArtifactByID(ctx context.Context, id string) (*Artifact, error) {
	dbArtifact, err := s.database.GetArtifactByID(id)
	if err != nil {
		return nil, fmt.Errorf("get artifact: %w", err)
	}
	if dbArtifact == nil {
		return nil, nil
	}
	artifact := dbArtifactToArtifact(*dbArtifact)
	return &artifact, nil
}

func (s *DBArtifactStore) ListArtifacts(ctx context.Context, filters ArtifactFilterOptions) ([]Artifact, error) {
	dbFilters := db.ArtifactFilters{
		WorkflowType: filters.WorkflowType,
		ProblemClass: filters.ProblemClass,
		RepoType:     filters.RepoType,
		Success:      filters.Success,
		Limit:        filters.Limit,
		Offset:       filters.Offset,
	}

	dbArtifacts, err := s.database.ListArtifacts(dbFilters)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}

	artifacts := make([]Artifact, len(dbArtifacts))
	for i, a := range dbArtifacts {
		artifacts[i] = dbArtifactToArtifact(a)
	}

	return artifacts, nil
}

func (s *DBArtifactStore) GetArtifactsByProblemClass(ctx context.Context, problemClass ProblemClass, limit int) ([]Artifact, error) {
	dbArtifacts, err := s.database.GetArtifactsByProblemClass(string(problemClass), limit)
	if err != nil {
		return nil, fmt.Errorf("get artifacts by problem: %w", err)
	}

	artifacts := make([]Artifact, len(dbArtifacts))
	for i, a := range dbArtifacts {
		artifacts[i] = dbArtifactToArtifact(a)
	}

	return artifacts, nil
}

func (s *DBArtifactStore) GetSuccessfulArtifactsWithSolution(ctx context.Context, limit int) ([]Artifact, error) {
	dbArtifacts, err := s.database.GetSuccessfulArtifactsWithSolution(limit)
	if err != nil {
		return nil, fmt.Errorf("get successful artifacts: %w", err)
	}

	artifacts := make([]Artifact, len(dbArtifacts))
	for i, a := range dbArtifacts {
		artifacts[i] = dbArtifactToArtifact(a)
	}

	return artifacts, nil
}

func (s *DBArtifactStore) CountArtifacts(ctx context.Context) (int, error) {
	return s.database.CountArtifacts()
}

type EventQuery interface {
	GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]EventForArtifact, error)
	GetEventsByWorkflowID(ctx context.Context, workflowID string) ([]EventForArtifact, error)
	GetCompletedWorkflowIDs(ctx context.Context, since time.Time, limit int) ([]string, error)
}

func artifactToDB(a Artifact) *db.Artifact {
	return &db.Artifact{
		ID:              a.ID,
		WorkflowID:      a.WorkflowID,
		TaskType:        string(a.TaskType),
		WorkflowType:    a.WorkflowType,
		RepoType:        string(a.RepoType),
		ProblemClass:    string(a.ProblemClass),
		AgentsUsed:      a.AgentsUsed,
		RootCause:       a.RootCause,
		SolutionPattern: a.SolutionPattern,
		FilesChanged:    a.FilesChanged,
		ReviewResult:    string(a.ReviewResult),
		CycleTimeMin:    a.CycleTimeMin,
		Success:         a.Success,
		Metadata:        a.Metadata,
		CreatedAt:       a.CreatedAt.Format(time.RFC3339Nano),
	}
}

func dbArtifactToArtifact(a db.Artifact) Artifact {
	var createdAt time.Time
	if a.CreatedAt != "" {
		createdAt, _ = time.Parse(time.RFC3339Nano, a.CreatedAt)
	}

	return Artifact{
		ID:              a.ID,
		WorkflowID:      a.WorkflowID,
		TaskType:        TaskType(a.TaskType),
		WorkflowType:    a.WorkflowType,
		RepoType:        RepoType(a.RepoType),
		ProblemClass:    ProblemClass(a.ProblemClass),
		AgentsUsed:      a.AgentsUsed,
		RootCause:       a.RootCause,
		SolutionPattern: a.SolutionPattern,
		FilesChanged:    a.FilesChanged,
		ReviewResult:    ReviewResult(a.ReviewResult),
		CycleTimeMin:    a.CycleTimeMin,
		Success:         a.Success,
		Metadata:        a.Metadata,
		CreatedAt:       createdAt,
	}
}

type DBEventQuery struct {
	db SQLExecutor
}

type SQLExecutor interface {
	QueryContext(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) Row
}

type Rows interface {
	Close()
	Next() bool
	Scan(dest ...any) error
	Err() error
}

type Row interface {
	Scan(dest ...any) error
}

func NewDBEventQuery(db SQLExecutor) *DBEventQuery {
	return &DBEventQuery{db: db}
}

func (q *DBEventQuery) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]EventForArtifact, error) {
	if limit <= 0 {
		limit = 10000
	}
	if len(eventTypes) == 0 {
		return []EventForArtifact{}, nil
	}

	placeholders := ""
	for i := range eventTypes {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}

	query := `SELECT id, type, timestamp, source, payload
		FROM insight_events
		WHERE type IN (` + placeholders + `) AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT ?`

	args := make([]any, 0, len(eventTypes)+3)
	for _, et := range eventTypes {
		args = append(args, et)
	}
	args = append(args, start.Format(time.RFC3339Nano))
	args = append(args, end.Format(time.RFC3339Nano))
	args = append(args, limit)

	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	return scanEventsForArtifact(rows)
}

func (q *DBEventQuery) GetEventsByWorkflowID(ctx context.Context, workflowID string) ([]EventForArtifact, error) {
	query := `SELECT id, type, timestamp, source, payload
		FROM insight_events
		WHERE json_extract(payload, '$.workflow_id') = ?
		ORDER BY timestamp ASC`

	rows, err := q.db.QueryContext(ctx, query, workflowID)
	if err != nil {
		return nil, fmt.Errorf("query events by workflow: %w", err)
	}
	defer rows.Close()

	return scanEventsForArtifact(rows)
}

func (q *DBEventQuery) GetCompletedWorkflowIDs(ctx context.Context, since time.Time, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 1000
	}

	query := `SELECT DISTINCT json_extract(payload, '$.workflow_id')
		FROM insight_events
		WHERE type = 'workflow.completed'
		  AND timestamp >= ?
		  AND json_extract(payload, '$.workflow_id') IS NOT NULL
		ORDER BY timestamp DESC
		LIMIT ?`

	rows, err := q.db.QueryContext(ctx, query, since.Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, fmt.Errorf("query completed workflows: %w", err)
	}
	defer rows.Close()

	var workflowIDs []string
	for rows.Next() {
		var id *string
		if err := rows.Scan(&id); err != nil {
			slog.Warn("failed to scan workflow id", "error", err)
			continue
		}
		if id != nil && *id != "" {
			workflowIDs = append(workflowIDs, *id)
		}
	}

	return workflowIDs, rows.Err()
}

func scanEventsForArtifact(rows Rows) ([]EventForArtifact, error) {
	var events []EventForArtifact
	for rows.Next() {
		var e EventForArtifact
		var timestamp, payloadStr string
		if err := rows.Scan(&e.ID, &e.Type, &timestamp, &e.Source, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, timestamp)
		if payloadStr != "" {
			if err := json.Unmarshal([]byte(payloadStr), &e.Payload); err != nil {
				slog.Warn("failed to parse event payload", "event_id", e.ID, "error", err)
				e.Payload = make(map[string]any)
			}
		} else {
			e.Payload = make(map[string]any)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
