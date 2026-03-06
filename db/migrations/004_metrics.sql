-- Migration 004: Analytics & Metrics Foundation
-- Created: 2026-03-06
-- Purpose: Track workflow efficiency, agent performance, and basic metrics

-- Raw metrics events (inserted on every workflow action)
CREATE TABLE IF NOT EXISTS workflow_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id TEXT NOT NULL,
    metric_type TEXT NOT NULL,  -- 'workflow' or 'agent'
    metric_name TEXT NOT NULL,  -- 'started', 'phase_duration', 'task_completed', 'completed'
    metric_value REAL NOT NULL,
    metadata TEXT,  -- JSON for context (phase names, agent IDs, etc.)
    recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_metrics_type ON workflow_metrics(metric_type);
CREATE INDEX IF NOT EXISTS idx_metrics_name ON workflow_metrics(metric_name);
CREATE INDEX IF NOT EXISTS idx_metrics_workflow ON workflow_metrics(workflow_id);
CREATE INDEX IF NOT EXISTS idx_metrics_recorded ON workflow_metrics(recorded_at);

-- Daily aggregated metrics (computed by aggregation job)
CREATE TABLE IF NOT EXISTS daily_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    metric_date TEXT NOT NULL UNIQUE,  -- YYYY-MM-DD
    total_workflows INTEGER DEFAULT 0,
    completed_workflows INTEGER DEFAULT 0,
    avg_workflow_duration_ms INTEGER DEFAULT 0,
    total_tasks INTEGER DEFAULT 0,
    completed_tasks INTEGER DEFAULT 0,
    success_rate REAL DEFAULT 0,
    metrics_json TEXT,  -- Additional detailed metrics as JSON
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_daily_metrics_date ON daily_metrics(metric_date);
