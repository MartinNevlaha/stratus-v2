package events

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestStore(t *testing.T) *DBStore {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "stratus-events-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "test.db")
	conn, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	_, err = conn.Exec(`
		CREATE TABLE IF NOT EXISTS insight_events (
			id         TEXT PRIMARY KEY,
			type       TEXT NOT NULL,
			timestamp  TEXT NOT NULL,
			source     TEXT NOT NULL,
			payload    TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		);
		CREATE INDEX IF NOT EXISTS idx_insight_events_type ON insight_events(type);
		CREATE INDEX IF NOT EXISTS idx_insight_events_timestamp ON insight_events(timestamp DESC);
	`)
	if err != nil {
		t.Fatalf("Failed to create schema: %v", err)
	}

	return NewDBStore(conn)
}

func TestStoreSaveEvent(t *testing.T) {
	store := setupTestStore(t)

	evt := NewEvent(EventWorkflowStarted, "test", map[string]any{
		"workflow_id": "wf-123",
		"title":       "Test Workflow",
	})

	if err := store.SaveEvent(context.Background(), evt); err != nil {
		t.Fatalf("SaveEvent failed: %v", err)
	}
}

func TestStoreGetRecentEvents(t *testing.T) {
	store := setupTestStore(t)

	now := time.Now().UTC()
	events := []Event{
		{ID: "1", Type: EventWorkflowStarted, Timestamp: now.Add(-2 * time.Hour), Source: "test", Payload: map[string]any{"i": 1}},
		{ID: "2", Type: EventWorkflowCompleted, Timestamp: now.Add(-1 * time.Hour), Source: "test", Payload: map[string]any{"i": 2}},
		{ID: "3", Type: EventProposalCreated, Timestamp: now, Source: "test", Payload: map[string]any{"i": 3}},
	}

	for _, evt := range events {
		if err := store.SaveEvent(context.Background(), evt); err != nil {
			t.Fatalf("SaveEvent failed: %v", err)
		}
	}

	retrieved, err := store.GetRecentEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetRecentEvents failed: %v", err)
	}

	if len(retrieved) != 3 {
		t.Errorf("expected 3 events, got %d", len(retrieved))
	}

	if len(retrieved) >= 3 {
		if retrieved[0].ID != "3" {
			t.Errorf("expected most recent event first, got ID %s", retrieved[0].ID)
		}
		if retrieved[2].ID != "1" {
			t.Errorf("expected oldest event last, got ID %s", retrieved[2].ID)
		}
	}
}

func TestStoreGetEventsByType(t *testing.T) {
	store := setupTestStore(t)

	now := time.Now().UTC()
	events := []Event{
		{ID: "1", Type: EventWorkflowStarted, Timestamp: now.Add(-2 * time.Hour), Source: "test", Payload: map[string]any{}},
		{ID: "2", Type: EventWorkflowCompleted, Timestamp: now.Add(-1 * time.Hour), Source: "test", Payload: map[string]any{}},
		{ID: "3", Type: EventWorkflowStarted, Timestamp: now, Source: "test", Payload: map[string]any{}},
	}

	for _, evt := range events {
		if err := store.SaveEvent(context.Background(), evt); err != nil {
			t.Fatalf("SaveEvent failed: %v", err)
		}
	}

	retrieved, err := store.GetEventsByType(context.Background(), EventWorkflowStarted, 10)
	if err != nil {
		t.Fatalf("GetEventsByType failed: %v", err)
	}

	if len(retrieved) != 2 {
		t.Errorf("expected 2 workflow.started events, got %d", len(retrieved))
	}

	for _, evt := range retrieved {
		if evt.Type != EventWorkflowStarted {
			t.Errorf("expected type %s, got %s", EventWorkflowStarted, evt.Type)
		}
	}
}

func TestStoreLimit(t *testing.T) {
	store := setupTestStore(t)

	for i := 0; i < 10; i++ {
		evt := NewEvent(EventWorkflowStarted, "test", map[string]any{"index": i})
		if err := store.SaveEvent(context.Background(), evt); err != nil {
			t.Fatalf("SaveEvent failed: %v", err)
		}
	}

	retrieved, err := store.GetRecentEvents(context.Background(), 5)
	if err != nil {
		t.Fatalf("GetRecentEvents failed: %v", err)
	}

	if len(retrieved) != 5 {
		t.Errorf("expected 5 events with limit, got %d", len(retrieved))
	}
}
