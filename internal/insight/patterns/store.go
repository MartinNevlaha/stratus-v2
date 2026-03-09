package patterns

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type PatternStore interface {
	SavePattern(ctx context.Context, pattern Pattern) error
	GetRecentPatterns(ctx context.Context, limit int) ([]Pattern, error)
	GetPatternsByType(ctx context.Context, patternType PatternType, limit int) ([]Pattern, error)
	FindPatternByName(ctx context.Context, patternName string) (*Pattern, error)
	UpdatePattern(ctx context.Context, pattern Pattern) error
}

type DBPatternStore struct {
	database *db.DB
}

func NewDBPatternStore(database *db.DB) *DBPatternStore {
	return &DBPatternStore{database: database}
}

func (s *DBPatternStore) SavePattern(ctx context.Context, pattern Pattern) error {
	dbPattern := patternToDB(pattern)
	return s.database.SaveInsightPattern(dbPattern)
}

func (s *DBPatternStore) GetRecentPatterns(ctx context.Context, limit int) ([]Pattern, error) {
	dbPatterns, err := s.database.ListInsightPatterns("", "", 0, limit)
	if err != nil {
		return nil, fmt.Errorf("list patterns: %w", err)
	}

	patterns := make([]Pattern, len(dbPatterns))
	for i, p := range dbPatterns {
		patterns[i] = dbPatternToModel(p)
	}
	return patterns, nil
}

func (s *DBPatternStore) GetPatternsByType(ctx context.Context, patternType PatternType, limit int) ([]Pattern, error) {
	dbPatterns, err := s.database.ListInsightPatterns(string(patternType), "", 0, limit)
	if err != nil {
		return nil, fmt.Errorf("list patterns by type: %w", err)
	}

	patterns := make([]Pattern, len(dbPatterns))
	for i, p := range dbPatterns {
		patterns[i] = dbPatternToModel(p)
	}
	return patterns, nil
}

func (s *DBPatternStore) FindPatternByName(ctx context.Context, patternName string) (*Pattern, error) {
	dbPattern, err := s.database.FindPatternByName(patternName)
	if err != nil {
		return nil, fmt.Errorf("find pattern: %w", err)
	}
	if dbPattern == nil {
		return nil, nil
	}

	pattern := dbPatternToModel(*dbPattern)
	return &pattern, nil
}

func (s *DBPatternStore) UpdatePattern(ctx context.Context, pattern Pattern) error {
	dbPattern := patternToDB(pattern)
	return s.database.UpdateInsightPattern(dbPattern)
}

func patternToDB(p Pattern) *db.InsightPattern {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	firstSeen := p.FirstSeen.Format(time.RFC3339Nano)
	if firstSeen == "" || p.FirstSeen.IsZero() {
		firstSeen = now
	}

	return &db.InsightPattern{
		ID:          parseID(p.ID),
		PatternType: string(p.Type),
		PatternName: string(p.Type),
		Description: p.Description,
		Frequency:   p.Frequency,
		Confidence:  p.Confidence,
		Severity:    string(p.Severity),
		Evidence:    p.Evidence,
		LastSeen:    p.LastSeen.Format(time.RFC3339Nano),
		FirstSeen:   firstSeen,
	}
}

func dbPatternToModel(p db.InsightPattern) Pattern {
	var firstSeen, lastSeen time.Time
	if p.FirstSeen != "" {
		firstSeen, _ = time.Parse(time.RFC3339Nano, p.FirstSeen)
	}
	if p.LastSeen != "" {
		lastSeen, _ = time.Parse(time.RFC3339Nano, p.LastSeen)
	}

	return Pattern{
		ID:          fmt.Sprintf("%d", p.ID),
		Type:        PatternType(p.PatternType),
		Timestamp:   lastSeen,
		Severity:    SeverityLevel(p.Severity),
		Description: p.Description,
		Evidence:    p.Evidence,
		Frequency:   p.Frequency,
		Confidence:  p.Confidence,
		FirstSeen:   firstSeen,
		LastSeen:    lastSeen,
	}
}

func parseID(id string) int {
	var result int
	fmt.Sscanf(id, "%d", &result)
	return result
}

type EventQuery interface {
	GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]EventForDetection, error)
}

type EventForDetection struct {
	ID        string
	Type      string
	Timestamp time.Time
	Source    string
	Payload   map[string]any
}

type DBEventQuery struct {
	db *sql.DB
}

func NewDBEventQuery(db *sql.DB) *DBEventQuery {
	return &DBEventQuery{db: db}
}

func (q *DBEventQuery) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]EventForDetection, error) {
	if limit <= 0 {
		limit = 1000
	}
	if len(eventTypes) == 0 {
		return []EventForDetection{}, nil
	}

	query := `
		SELECT id, type, timestamp, source, payload
		FROM insight_events
		WHERE type IN (` + placeholders(len(eventTypes)) + `) AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT ?`

	args := make([]any, len(eventTypes)+3)
	for i, et := range eventTypes {
		args[i] = et
	}
	args[len(eventTypes)] = start.Format(time.RFC3339Nano)
	args[len(eventTypes)+1] = end.Format(time.RFC3339Nano)
	args[len(eventTypes)+2] = limit

	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []EventForDetection
	for rows.Next() {
		var e EventForDetection
		var timestamp, payloadStr string
		if err := rows.Scan(&e.ID, &e.Type, &timestamp, &e.Source, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, timestamp)
		if err := json.Unmarshal([]byte(payloadStr), &e.Payload); err != nil {
			e.Payload = make(map[string]any)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	result := "?"
	for i := 1; i < n; i++ {
		result += ",?"
	}
	return result
}
