package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection with helper methods.
type DB struct {
	sql *sql.DB
}

// Open opens (or creates) the stratus SQLite database at the given path.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	conn, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// WAL mode supports concurrent readers; allow a small pool so long-running
	// operations (e.g. IndexGovernance on startup) don't starve HTTP handlers.
	// busy_timeout above ensures writers retry instead of returning SQLITE_BUSY.
	conn.SetMaxOpenConns(4)
	db := &DB{sql: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// Close closes the underlying SQL connection.
func (d *DB) Close() error { return d.sql.Close() }

// SQL returns the raw *sql.DB for advanced usage.
func (d *DB) SQL() *sql.DB { return d.sql }

func (d *DB) migrate() error {
	if _, err := d.sql.Exec(schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	// Add columns that may be missing on existing databases.
	// SQLite doesn't support ADD COLUMN IF NOT EXISTS, so we ignore
	// "duplicate column" errors from ALTER TABLE.
	for _, stmt := range migrations {
		if _, err := d.sql.Exec(stmt); err != nil {
			if !isDuplicateColumn(err) {
				return fmt.Errorf("migration: %w", err)
			}
		}
	}
	return nil
}

// migrations contains ALTER TABLE statements for columns added after the initial schema.
// Each runs on every startup; duplicate-column errors are silently ignored.
var migrations = []string{
	`ALTER TABLE missions ADD COLUMN strategy TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE missions ADD COLUMN strategy_outcome TEXT NOT NULL DEFAULT '{}'`,
	`
CREATE TABLE IF NOT EXISTS workflow_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id TEXT NOT NULL,
    metric_type TEXT NOT NULL,
    metric_name TEXT NOT NULL,
    metric_value REAL NOT NULL,
    metadata TEXT,
    recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE
)`,
	`CREATE INDEX IF NOT EXISTS idx_metrics_type ON workflow_metrics(metric_type)`,
	`CREATE INDEX IF NOT EXISTS idx_metrics_name ON workflow_metrics(metric_name)`,
	`CREATE INDEX IF NOT EXISTS idx_metrics_workflow ON workflow_metrics(workflow_id)`,
	`CREATE INDEX IF NOT EXISTS idx_metrics_recorded ON workflow_metrics(recorded_at)`,
	`
CREATE TABLE IF NOT EXISTS daily_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    metric_date TEXT NOT NULL UNIQUE,
    total_workflows INTEGER DEFAULT 0,
    completed_workflows INTEGER DEFAULT 0,
    avg_workflow_duration_ms INTEGER DEFAULT 0,
    total_tasks INTEGER DEFAULT 0,
    completed_tasks INTEGER DEFAULT 0,
    success_rate REAL DEFAULT 0,
    metrics_json TEXT,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
)`,
	`CREATE INDEX IF NOT EXISTS idx_daily_metrics_date ON daily_metrics(metric_date)`,
	`
CREATE TABLE IF NOT EXISTS openclaw_state (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    last_analysis TEXT NOT NULL,
    next_analysis TEXT NOT NULL,
    patterns_detected INTEGER DEFAULT 0,
    proposals_generated INTEGER DEFAULT 0,
    proposals_accepted INTEGER DEFAULT 0,
    acceptance_rate REAL DEFAULT 0,
    model_version TEXT NOT NULL DEFAULT 'v1',
    config_json TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M-%fZ', 'now'))
)`,
	`
CREATE TABLE IF NOT EXISTS openclaw_patterns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern_type TEXT NOT NULL,
    pattern_name TEXT NOT NULL,
    description TEXT NOT NULL,
    frequency INTEGER DEFAULT 1,
    confidence REAL NOT NULL,
    examples_json TEXT,
    metadata_json TEXT,
    last_seen TEXT NOT NULL,
    first_seen TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
)`,
	`CREATE INDEX IF NOT EXISTS idx_openclaw_patterns_type ON openclaw_patterns(pattern_type)`,
	`CREATE INDEX IF NOT EXISTS idx_openclaw_patterns_confidence ON openclaw_patterns(confidence DESC)`,
	`
CREATE TABLE IF NOT EXISTS openclaw_feedback (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    proposal_id TEXT NOT NULL,
    feedback_type TEXT NOT NULL,
    reason TEXT,
    impact_score REAL,
    measured_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (proposal_id) REFERENCES proposals(id)
)`,
	`CREATE INDEX IF NOT EXISTS idx_openclaw_feedback_proposal ON openclaw_feedback(proposal_id)`,
	`
CREATE TABLE IF NOT EXISTS openclaw_analyses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    analysis_type TEXT NOT NULL,
    scope TEXT,
    findings_json TEXT NOT NULL,
    recommendations_json TEXT,
    patterns_found INTEGER DEFAULT 0,
    proposals_created INTEGER DEFAULT 0,
    execution_time_ms INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
)`,
	`CREATE INDEX IF NOT EXISTS idx_openclaw_analyses_type ON openclaw_analyses(analysis_type)`,
	`CREATE INDEX IF NOT EXISTS idx_openclaw_analyses_created ON openclaw_analyses(created_at DESC)`,
}

func isDuplicateColumn(err error) bool {
	// modernc.org/sqlite returns "duplicate column name: <col>" for ALTER TABLE ADD COLUMN
	// when the column already exists.
	msg := err.Error()
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}
