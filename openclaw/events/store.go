package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

type Store interface {
	SaveEvent(ctx context.Context, event Event) error
	GetRecentEvents(ctx context.Context, limit int) ([]Event, error)
	GetEventsByType(ctx context.Context, eventType EventType, limit int) ([]Event, error)
	GetEventsInTimeRange(ctx context.Context, start, end time.Time, limit int) ([]Event, error)
	GetEventsByTypes(ctx context.Context, eventTypes []EventType, limit int) ([]Event, error)
	GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []EventType, start, end time.Time, limit int) ([]Event, error)
}

type DBStore struct {
	db *sql.DB
}

func NewDBStore(db *sql.DB) *DBStore {
	return &DBStore{db: db}
}

func (s *DBStore) SaveEvent(ctx context.Context, event Event) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO openclaw_events (id, type, timestamp, source, payload)
		VALUES (?, ?, ?, ?, ?)`,
		event.ID,
		string(event.Type),
		event.Timestamp.Format(time.RFC3339Nano),
		event.Source,
		string(payload),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

func (s *DBStore) GetRecentEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, timestamp, source, payload
		FROM openclaw_events
		ORDER BY timestamp DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	return s.scanEvents(rows)
}

func (s *DBStore) GetEventsByType(ctx context.Context, eventType EventType, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, timestamp, source, payload
		FROM openclaw_events
		WHERE type = ?
		ORDER BY timestamp DESC
		LIMIT ?`,
		string(eventType),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	return s.scanEvents(rows)
}

func (s *DBStore) scanEvents(rows *sql.Rows) ([]Event, error) {
	var events []Event
	for rows.Next() {
		var e Event
		var timestamp, payload string
		if err := rows.Scan(&e.ID, &e.Type, &timestamp, &e.Source, &payload); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		var parseErr error
		e.Timestamp, parseErr = time.Parse(time.RFC3339Nano, timestamp)
		if parseErr != nil {
			slog.Warn("failed to parse event timestamp", "error", parseErr, "timestamp", timestamp)
			e.Timestamp = time.Time{}
		}
		if err := json.Unmarshal([]byte(payload), &e.Payload); err != nil {
			e.Payload = make(map[string]any)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *DBStore) GetEventsInTimeRange(ctx context.Context, start, end time.Time, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 1000
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, timestamp, source, payload
		FROM openclaw_events
		WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT ?`,
		start.Format(time.RFC3339Nano),
		end.Format(time.RFC3339Nano),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query events in time range: %w", err)
	}
	defer rows.Close()

	return s.scanEvents(rows)
}

func (s *DBStore) GetEventsByTypes(ctx context.Context, eventTypes []EventType, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	if len(eventTypes) == 0 {
		return s.GetRecentEvents(ctx, limit)
	}

	query := `
		SELECT id, type, timestamp, source, payload
		FROM openclaw_events
		WHERE type IN (` + placeholders(len(eventTypes)) + `)
		ORDER BY timestamp DESC
		LIMIT ?`

	args := make([]any, len(eventTypes)+1)
	for i, et := range eventTypes {
		args[i] = string(et)
	}
	args[len(eventTypes)] = limit

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events by types: %w", err)
	}
	defer rows.Close()

	return s.scanEvents(rows)
}

func (s *DBStore) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []EventType, start, end time.Time, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 1000
	}
	if len(eventTypes) == 0 {
		return s.GetEventsInTimeRange(ctx, start, end, limit)
	}

	query := `
		SELECT id, type, timestamp, source, payload
		FROM openclaw_events
		WHERE type IN (` + placeholders(len(eventTypes)) + `) AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT ?`

	args := make([]any, len(eventTypes)+3)
	for i, et := range eventTypes {
		args[i] = string(et)
	}
	args[len(eventTypes)] = start.Format(time.RFC3339Nano)
	args[len(eventTypes)+1] = end.Format(time.RFC3339Nano)
	args[len(eventTypes)+2] = limit

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events by types in time range: %w", err)
	}
	defer rows.Close()

	return s.scanEvents(rows)
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
