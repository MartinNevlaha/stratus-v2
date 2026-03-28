package db

const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;

CREATE TABLE IF NOT EXISTS schema_versions (
    version INTEGER PRIMARY KEY,
    applied TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Memory events
CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ts         TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    actor      TEXT    NOT NULL DEFAULT 'agent',
    scope      TEXT    NOT NULL DEFAULT 'repo',
    type       TEXT    NOT NULL DEFAULT 'discovery',
    text       TEXT    NOT NULL,
    title      TEXT    NOT NULL DEFAULT '',
    tags       TEXT    NOT NULL DEFAULT '[]',
    refs       TEXT    NOT NULL DEFAULT '{}',
    ttl        TEXT,
    importance REAL    NOT NULL DEFAULT 0.5,
    dedupe_key TEXT    UNIQUE,
    project    TEXT,
    session_id TEXT,
    created_ms INTEGER NOT NULL DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER))
);

CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(
    title, text, tags,
    content='events', content_rowid='id',
    tokenize='porter unicode61'
);

CREATE TRIGGER IF NOT EXISTS events_ai AFTER INSERT ON events BEGIN
    INSERT INTO events_fts(rowid, title, text, tags) VALUES (new.id, new.title, new.text, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS events_au AFTER UPDATE ON events BEGIN
    INSERT INTO events_fts(events_fts, rowid, title, text, tags) VALUES ('delete', old.id, old.title, old.text, old.tags);
    INSERT INTO events_fts(rowid, title, text, tags) VALUES (new.id, new.title, new.text, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS events_ad AFTER DELETE ON events BEGIN
    INSERT INTO events_fts(events_fts, rowid, title, text, tags) VALUES ('delete', old.id, old.title, old.text, old.tags);
END;

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    content_session_id TEXT    NOT NULL,
    project            TEXT    NOT NULL DEFAULT '',
    initial_prompt     TEXT,
    started_at         TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Governance docs (markdown chunks)
CREATE TABLE IF NOT EXISTS docs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path   TEXT    NOT NULL,
    chunk_index INTEGER NOT NULL DEFAULT 0,
    title       TEXT    NOT NULL DEFAULT '',
    content     TEXT    NOT NULL,
    doc_type    TEXT    NOT NULL DEFAULT 'project',
    file_hash   TEXT    NOT NULL,
    project     TEXT    NOT NULL DEFAULT '',
    indexed_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(file_path, chunk_index)
);

CREATE VIRTUAL TABLE IF NOT EXISTS docs_fts USING fts5(
    title, content, doc_type,
    content='docs', content_rowid='id',
    tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS docs_ai AFTER INSERT ON docs BEGIN
    INSERT INTO docs_fts(rowid, title, content, doc_type) VALUES (new.id, new.title, new.content, new.doc_type);
END;
CREATE TRIGGER IF NOT EXISTS docs_au AFTER UPDATE ON docs BEGIN
    INSERT INTO docs_fts(docs_fts, rowid, title, content, doc_type) VALUES ('delete', old.id, old.title, old.content, old.doc_type);
    INSERT INTO docs_fts(rowid, title, content, doc_type) VALUES (new.id, new.title, new.content, new.doc_type);
END;
CREATE TRIGGER IF NOT EXISTS docs_ad AFTER DELETE ON docs BEGIN
    INSERT INTO docs_fts(docs_fts, rowid, title, content, doc_type) VALUES ('delete', old.id, old.title, old.content, old.doc_type);
END;

-- Learning: pattern candidates
CREATE TABLE IF NOT EXISTS candidates (
    id             TEXT PRIMARY KEY,
    detection_type TEXT    NOT NULL,
    count          INTEGER NOT NULL DEFAULT 1,
    confidence     REAL    NOT NULL DEFAULT 0.5,
    files          TEXT    NOT NULL DEFAULT '[]',
    description    TEXT    NOT NULL,
    status         TEXT    NOT NULL DEFAULT 'pending',
    detected_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Learning: proposals
CREATE TABLE IF NOT EXISTS proposals (
    id               TEXT PRIMARY KEY,
    candidate_id     TEXT    NOT NULL REFERENCES candidates(id),
    type             TEXT    NOT NULL DEFAULT 'rule',
    title            TEXT    NOT NULL,
    description      TEXT    NOT NULL,
    proposed_content TEXT    NOT NULL DEFAULT '',
    proposed_path    TEXT,
    confidence       REAL    NOT NULL DEFAULT 0.5,
    status           TEXT    NOT NULL DEFAULT 'pending',
    decision         TEXT,
    decided_at       TEXT,
    session_id       TEXT,
    created_at       TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Orchestration workflows (replaces spec-state.json + bug-state.json)
CREATE TABLE IF NOT EXISTS workflows (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL DEFAULT 'spec',
    phase      TEXT NOT NULL DEFAULT 'plan',
    complexity TEXT NOT NULL DEFAULT 'simple',
    state_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Swarm: Missions (groups of coordinated tickets)
CREATE TABLE IF NOT EXISTS missions (
    id               TEXT PRIMARY KEY,
    workflow_id      TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    title            TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'planning',
    base_branch      TEXT NOT NULL DEFAULT 'main',
    merge_branch     TEXT NOT NULL DEFAULT '',
    strategy         TEXT NOT NULL DEFAULT '',
    strategy_outcome TEXT NOT NULL DEFAULT '{}',
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Swarm: Workers (agent processes with git worktrees)
CREATE TABLE IF NOT EXISTS workers (
    id             TEXT PRIMARY KEY,
    mission_id     TEXT NOT NULL REFERENCES missions(id) ON DELETE CASCADE,
    agent_type     TEXT NOT NULL,
    worktree_path  TEXT NOT NULL DEFAULT '',
    branch_name    TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'pending',
    session_id     TEXT,
    last_heartbeat TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_workers_mission ON workers(mission_id);

-- Swarm: Tickets (atomic work units)
CREATE TABLE IF NOT EXISTS tickets (
    id          TEXT PRIMARY KEY,
    mission_id  TEXT NOT NULL REFERENCES missions(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    domain      TEXT NOT NULL DEFAULT 'general',
    priority    INTEGER NOT NULL DEFAULT 100,
    status      TEXT NOT NULL DEFAULT 'pending',
    worker_id   TEXT REFERENCES workers(id),
    depends_on  TEXT NOT NULL DEFAULT '[]',
    result      TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_tickets_mission ON tickets(mission_id);

-- Swarm: Signals (inter-agent messages)
CREATE TABLE IF NOT EXISTS signals (
    id          TEXT PRIMARY KEY,
    mission_id  TEXT NOT NULL REFERENCES missions(id) ON DELETE CASCADE,
    from_worker TEXT NOT NULL,
    to_worker   TEXT NOT NULL DEFAULT '*',
    type        TEXT NOT NULL,
    payload     TEXT NOT NULL DEFAULT '{}',
    read        INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_signals_to      ON signals(to_worker, read);
CREATE INDEX IF NOT EXISTS idx_signals_mission  ON signals(mission_id);

-- Swarm: File reservations (prevent edit conflicts)
CREATE TABLE IF NOT EXISTS file_reservations (
    id          TEXT PRIMARY KEY,
    mission_id  TEXT NOT NULL REFERENCES missions(id) ON DELETE CASCADE,
    worker_id   TEXT NOT NULL REFERENCES workers(id) ON DELETE CASCADE,
    patterns    TEXT NOT NULL DEFAULT '[]',
    reason      TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_file_reservations_mission ON file_reservations(mission_id);

-- Swarm: Checkpoints (coordinator state snapshots)
CREATE TABLE IF NOT EXISTS swarm_checkpoints (
    id          TEXT PRIMARY KEY,
    mission_id  TEXT NOT NULL REFERENCES missions(id) ON DELETE CASCADE,
    progress    INTEGER NOT NULL DEFAULT 0,
    state_json  TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_swarm_checkpoints_mission ON swarm_checkpoints(mission_id);

-- Swarm: Forge (merge queue entries)
CREATE TABLE IF NOT EXISTS forge_entries (
    id             TEXT PRIMARY KEY,
    mission_id     TEXT NOT NULL REFERENCES missions(id) ON DELETE CASCADE,
    worker_id      TEXT NOT NULL REFERENCES workers(id),
    branch_name    TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'pending',
    conflict_files TEXT NOT NULL DEFAULT '[]',
    merged_at      TEXT,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Insight: State tracking
CREATE TABLE IF NOT EXISTS insight_state (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    last_analysis TEXT NOT NULL,
    next_analysis TEXT NOT NULL,
    patterns_detected INTEGER DEFAULT 0,
    proposals_generated INTEGER DEFAULT 0,
    proposals_accepted INTEGER DEFAULT 0,
    acceptance_rate REAL DEFAULT 0,
    model_version TEXT NOT NULL DEFAULT 'v1',
    config_json TEXT DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Insight: Pattern library
CREATE TABLE IF NOT EXISTS insight_patterns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern_type TEXT NOT NULL,
    pattern_name TEXT NOT NULL,
    description TEXT NOT NULL,
    frequency INTEGER DEFAULT 1,
    confidence REAL NOT NULL,
    examples_json TEXT DEFAULT '[]',
    metadata_json TEXT DEFAULT '{}',
    last_seen TEXT NOT NULL,
    first_seen TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_insight_patterns_type ON insight_patterns(pattern_type);
CREATE INDEX IF NOT EXISTS idx_insight_patterns_confidence ON insight_patterns(confidence DESC);

-- Insight: Proposal feedback
CREATE TABLE IF NOT EXISTS insight_feedback (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    proposal_id TEXT NOT NULL,
    feedback_type TEXT NOT NULL,
    reason TEXT,
    impact_score REAL,
    measured_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (proposal_id) REFERENCES insight_proposals(id)
);

CREATE INDEX IF NOT EXISTS idx_insight_feedback_proposal ON insight_feedback(proposal_id);

-- Insight: Analysis history
CREATE TABLE IF NOT EXISTS insight_analyses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    analysis_type TEXT NOT NULL,
    scope TEXT,
    findings_json TEXT NOT NULL DEFAULT '{}',
    recommendations_json TEXT DEFAULT '{}',
    patterns_found INTEGER DEFAULT 0,
    proposals_created INTEGER DEFAULT 0,
    execution_time_ms INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_insight_analyses_type ON insight_analyses(analysis_type);
CREATE INDEX IF NOT EXISTS idx_insight_analyses_created ON insight_analyses(created_at DESC);

-- Insight: Event log for real-time observability
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
CREATE INDEX IF NOT EXISTS idx_insight_events_source ON insight_events(source);

-- Daily aggregated metrics
CREATE TABLE IF NOT EXISTS daily_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    metric_date TEXT NOT NULL UNIQUE,
    total_workflows INTEGER DEFAULT 0,
    completed_workflows INTEGER DEFAULT 0,
    avg_workflow_duration_ms INTEGER DEFAULT 0,
    total_tasks INTEGER DEFAULT 0,
    completed_tasks INTEGER DEFAULT 0,
    success_rate REAL DEFAULT 0,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_daily_metrics_date ON daily_metrics(metric_date);

-- Insight: Improvement proposals
CREATE TABLE IF NOT EXISTS insight_proposals (
    id                TEXT PRIMARY KEY,
    type              TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'detected',
    title             TEXT NOT NULL,
    description       TEXT NOT NULL,
    confidence        REAL NOT NULL,
    risk_level        TEXT NOT NULL DEFAULT 'medium',
    source_pattern_id TEXT NOT NULL,
    evidence          TEXT NOT NULL DEFAULT '{}',
    recommendation    TEXT NOT NULL DEFAULT '{}',
    decision_reason   TEXT,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_insight_proposals_type ON insight_proposals(type);
CREATE INDEX IF NOT EXISTS idx_insight_proposals_status ON insight_proposals(status);
CREATE INDEX IF NOT EXISTS idx_insight_proposals_pattern ON insight_proposals(source_pattern_id);
CREATE INDEX IF NOT EXISTS idx_insight_proposals_created ON insight_proposals(created_at DESC);

-- Insight: Agent Scorecards
CREATE TABLE IF NOT EXISTS insight_agent_scorecards (
    id TEXT PRIMARY KEY,
    agent_name TEXT NOT NULL,
    window TEXT NOT NULL,
    window_start TEXT NOT NULL,
    window_end TEXT NOT NULL,
    total_runs INTEGER DEFAULT 0,
    success_rate REAL DEFAULT 0,
    failure_rate REAL DEFAULT 0,
    review_pass_rate REAL DEFAULT 0,
    rework_rate REAL DEFAULT 0,
    avg_cycle_time_ms INTEGER DEFAULT 0,
    regression_rate REAL DEFAULT 0,
    confidence_score REAL DEFAULT 0,
    trend TEXT DEFAULT 'stable',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(agent_name, window)
);

CREATE INDEX IF NOT EXISTS idx_agent_scorecards_name ON insight_agent_scorecards(agent_name);
CREATE INDEX IF NOT EXISTS idx_agent_scorecards_window ON insight_agent_scorecards(window);

-- Insight: Workflow Scorecards
CREATE TABLE IF NOT EXISTS insight_workflow_scorecards (
    id TEXT PRIMARY KEY,
    workflow_type TEXT NOT NULL,
    window TEXT NOT NULL,
    window_start TEXT NOT NULL,
    window_end TEXT NOT NULL,
    total_runs INTEGER DEFAULT 0,
    completion_rate REAL DEFAULT 0,
    failure_rate REAL DEFAULT 0,
    review_rejection_rate REAL DEFAULT 0,
    rework_rate REAL DEFAULT 0,
    avg_duration_ms INTEGER DEFAULT 0,
    confidence_score REAL DEFAULT 0,
    trend TEXT DEFAULT 'stable',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(workflow_type, window)
);

CREATE INDEX IF NOT EXISTS idx_workflow_scorecards_type ON insight_workflow_scorecards(workflow_type);
CREATE INDEX IF NOT EXISTS idx_workflow_scorecards_window ON insight_workflow_scorecards(window);

-- Insight: Routing Recommendations
CREATE TABLE IF NOT EXISTS insight_routing_recommendations (
    id TEXT PRIMARY KEY,
    workflow_type TEXT NOT NULL,
    recommendation_type TEXT NOT NULL,
    recommended_agent TEXT,
    current_agent TEXT,
    confidence REAL NOT NULL,
    risk_level TEXT NOT NULL DEFAULT 'medium',
    reason TEXT NOT NULL,
    evidence TEXT NOT NULL DEFAULT '{}',
    observations INTEGER DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_routing_workflow ON insight_routing_recommendations(workflow_type);
CREATE INDEX IF NOT EXISTS idx_routing_type ON insight_routing_recommendations(recommendation_type);
CREATE INDEX IF NOT EXISTS idx_routing_confidence ON insight_routing_recommendations(confidence DESC);
CREATE INDEX IF NOT EXISTS idx_routing_created ON insight_routing_recommendations(created_at DESC);

-- Insight: Workflow Metrics (cached aggregated metrics)
CREATE TABLE IF NOT EXISTS insight_workflow_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id TEXT NOT NULL,
    workflow_type TEXT NOT NULL,
    task_type TEXT,
    agents_used_json TEXT DEFAULT '[]',
    retry_count INTEGER DEFAULT 0,
    cycle_time_ms INTEGER DEFAULT 0,
    success_rate REAL DEFAULT 0,
    review_fail_rate REAL DEFAULT 0,
    analysis_window TEXT NOT NULL,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(workflow_id, analysis_window)
);

CREATE INDEX IF NOT EXISTS idx_workflow_metrics_type ON insight_workflow_metrics(workflow_type);
CREATE INDEX IF NOT EXISTS idx_workflow_metrics_window ON insight_workflow_metrics(analysis_window);
CREATE INDEX IF NOT EXISTS idx_workflow_metrics_computed ON insight_workflow_metrics(computed_at DESC);

-- Insight: Engineering Knowledge Artifacts
CREATE TABLE IF NOT EXISTS insight_artifacts (
    id                 TEXT PRIMARY KEY,
    workflow_id        TEXT NOT NULL,
    task_type          TEXT NOT NULL DEFAULT '',
    workflow_type      TEXT NOT NULL DEFAULT '',
    repo_type          TEXT NOT NULL DEFAULT '',
    problem_class      TEXT NOT NULL DEFAULT '',
    agents_used_json   TEXT NOT NULL DEFAULT '[]',
    root_cause         TEXT NOT NULL DEFAULT '',
    solution_pattern   TEXT NOT NULL DEFAULT '',
    files_changed_json TEXT NOT NULL DEFAULT '[]',
    review_result      TEXT NOT NULL DEFAULT '',
    cycle_time_minutes INTEGER DEFAULT 0,
    success            INTEGER DEFAULT 0,
    metadata_json      TEXT NOT NULL DEFAULT '{}',
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_artifacts_workflow_id ON insight_artifacts(workflow_id);
CREATE INDEX IF NOT EXISTS idx_artifacts_workflow_type ON insight_artifacts(workflow_type);
CREATE INDEX IF NOT EXISTS idx_artifacts_problem_class ON insight_artifacts(problem_class);
CREATE INDEX IF NOT EXISTS idx_artifacts_repo_type ON insight_artifacts(repo_type);
CREATE INDEX IF NOT EXISTS idx_artifacts_success ON insight_artifacts(success);
CREATE INDEX IF NOT EXISTS idx_artifacts_created ON insight_artifacts(created_at DESC);

-- Insight: Solution Patterns (mined from artifacts)
CREATE TABLE IF NOT EXISTS insight_solution_patterns (
    id                      TEXT PRIMARY KEY,
    problem_class           TEXT NOT NULL,
    solution_pattern        TEXT NOT NULL,
    repo_type               TEXT NOT NULL DEFAULT '',
    success_rate            REAL DEFAULT 0,
    occurrence_count        INTEGER DEFAULT 0,
    example_artifacts_json  TEXT NOT NULL DEFAULT '[]',
    confidence              REAL DEFAULT 0,
    first_seen              TEXT NOT NULL,
    last_seen               TEXT NOT NULL,
    created_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(problem_class, solution_pattern, repo_type)
);

CREATE INDEX IF NOT EXISTS idx_solution_patterns_problem ON insight_solution_patterns(problem_class);
CREATE INDEX IF NOT EXISTS idx_solution_patterns_repo ON insight_solution_patterns(repo_type);
CREATE INDEX IF NOT EXISTS idx_solution_patterns_success ON insight_solution_patterns(success_rate DESC);

-- Insight: Problem Statistics (aggregated knowledge)
CREATE TABLE IF NOT EXISTS insight_problem_stats (
    id                  TEXT PRIMARY KEY,
    problem_class       TEXT NOT NULL,
    repo_type           TEXT NOT NULL DEFAULT '',
    best_agent          TEXT NOT NULL DEFAULT '',
    best_workflow       TEXT NOT NULL DEFAULT '',
    success_rate        REAL DEFAULT 0,
    occurrence_count    INTEGER DEFAULT 0,
    avg_cycle_time      INTEGER DEFAULT 0,
    agents_success_json TEXT NOT NULL DEFAULT '{}',
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(problem_class, repo_type)
);

CREATE INDEX IF NOT EXISTS idx_problem_stats_problem ON insight_problem_stats(problem_class);
CREATE INDEX IF NOT EXISTS idx_problem_stats_repo ON insight_problem_stats(repo_type);
CREATE INDEX IF NOT EXISTS idx_problem_stats_success ON insight_problem_stats(success_rate DESC);

-- Insight: Trajectories (complete workflow execution paths)
CREATE TABLE IF NOT EXISTS insight_trajectories (
    id                  TEXT PRIMARY KEY,
    workflow_id         TEXT NOT NULL,
    task_type           TEXT NOT NULL DEFAULT '',
    repo_type           TEXT NOT NULL DEFAULT '',
    workflow_type       TEXT NOT NULL DEFAULT '',
    steps_json          TEXT NOT NULL DEFAULT '[]',
    step_count          INTEGER NOT NULL DEFAULT 0,
    final_result        TEXT NOT NULL DEFAULT '',
    cycle_time_minutes  INTEGER DEFAULT 0,
    started_at          TEXT NOT NULL,
    completed_at        TEXT,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_trajectories_workflow ON insight_trajectories(workflow_id);
CREATE INDEX IF NOT EXISTS idx_trajectories_task_type ON insight_trajectories(task_type);
CREATE INDEX IF NOT EXISTS idx_trajectories_repo_type ON insight_trajectories(repo_type);
CREATE INDEX IF NOT EXISTS idx_trajectories_result ON insight_trajectories(final_result);
CREATE INDEX IF NOT EXISTS idx_trajectories_created ON insight_trajectories(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_trajectories_started ON insight_trajectories(started_at);

-- Insight: Trajectory Patterns (mined optimal agent sequences)
CREATE TABLE IF NOT EXISTS insight_trajectory_patterns (
    id                       TEXT PRIMARY KEY,
    problem_type             TEXT NOT NULL,
    repo_type                TEXT NOT NULL DEFAULT '',
    optimal_agent_sequence_json TEXT NOT NULL DEFAULT '[]',
    success_rate             REAL NOT NULL DEFAULT 0,
    occurrence_count         INTEGER NOT NULL DEFAULT 1,
    avg_cycle_time_minutes   INTEGER DEFAULT 0,
    example_trajectory_ids_json TEXT NOT NULL DEFAULT '[]',
    confidence               REAL NOT NULL DEFAULT 0,
    created_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(problem_type, repo_type)
);

CREATE INDEX IF NOT EXISTS idx_trajectory_patterns_problem ON insight_trajectory_patterns(problem_type);
CREATE INDEX IF NOT EXISTS idx_trajectory_patterns_repo ON insight_trajectory_patterns(repo_type);
CREATE INDEX IF NOT EXISTS idx_trajectory_patterns_success ON insight_trajectory_patterns(success_rate DESC);

-- Insight: Workflow Candidates (synthesized from trajectory patterns)
CREATE TABLE IF NOT EXISTS insight_workflow_candidates (
    id                      TEXT PRIMARY KEY,
    workflow_name           TEXT NOT NULL,
    task_type               TEXT NOT NULL,
    repo_type               TEXT NOT NULL DEFAULT '',
    base_workflow           TEXT NOT NULL,
    steps_json              TEXT NOT NULL DEFAULT '[]',
    phase_transitions_json  TEXT NOT NULL DEFAULT '{}',
    confidence              REAL NOT NULL DEFAULT 0,
    status                  TEXT NOT NULL DEFAULT 'candidate',
    source_pattern_id       TEXT,
    created_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_workflow_candidates_status ON insight_workflow_candidates(status);
CREATE INDEX IF NOT EXISTS idx_workflow_candidates_task_repo ON insight_workflow_candidates(task_type, repo_type);
CREATE INDEX IF NOT EXISTS idx_workflow_candidates_confidence ON insight_workflow_candidates(confidence DESC);

-- Insight: Workflow Experiments (A/B testing with bandit)
CREATE TABLE IF NOT EXISTS insight_workflow_experiments (
    id                TEXT PRIMARY KEY,
    candidate_id      TEXT NOT NULL REFERENCES insight_workflow_candidates(id) ON DELETE CASCADE,
    baseline_workflow TEXT NOT NULL,
    traffic_percent   REAL NOT NULL DEFAULT 10,
    status            TEXT NOT NULL DEFAULT 'running',
    sample_size       INTEGER NOT NULL DEFAULT 100,
    runs_candidate    INTEGER NOT NULL DEFAULT 0,
    runs_baseline     INTEGER NOT NULL DEFAULT 0,
    bandit_state_json TEXT NOT NULL DEFAULT '{}',
    started_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at      TEXT,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_workflow_experiments_status ON insight_workflow_experiments(status);
CREATE INDEX IF NOT EXISTS idx_workflow_experiments_candidate ON insight_workflow_experiments(candidate_id);

-- Insight: Experiment Results (per-run metrics)
CREATE TABLE IF NOT EXISTS insight_experiment_results (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    experiment_id   TEXT NOT NULL REFERENCES insight_workflow_experiments(id) ON DELETE CASCADE,
    workflow_id     TEXT NOT NULL,
    used_candidate  INTEGER NOT NULL DEFAULT 0,
    success         INTEGER NOT NULL DEFAULT 0,
    cycle_time_min  INTEGER NOT NULL DEFAULT 0,
    retry_count     INTEGER NOT NULL DEFAULT 0,
    review_passes   INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_experiment_results_exp ON insight_experiment_results(experiment_id);
CREATE INDEX IF NOT EXISTS idx_experiment_results_workflow ON insight_experiment_results(workflow_id);

-- Insight: Agent Candidates (evolution proposals)
CREATE TABLE IF NOT EXISTS insight_agent_candidates (
    id                TEXT PRIMARY KEY,
    agent_name        TEXT NOT NULL,
    base_agent        TEXT NOT NULL,
    specialization    TEXT NOT NULL,
    reason            TEXT NOT NULL,
    confidence        REAL NOT NULL DEFAULT 0,
    prompt_diff_json  TEXT NOT NULL DEFAULT '{}',
    status            TEXT NOT NULL DEFAULT 'pending',
    evidence_json     TEXT NOT NULL DEFAULT '{}',
    opportunity_type  TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(agent_name)
);

CREATE INDEX IF NOT EXISTS idx_agent_candidates_status ON insight_agent_candidates(status);
CREATE INDEX IF NOT EXISTS idx_agent_candidates_base ON insight_agent_candidates(base_agent);
CREATE INDEX IF NOT EXISTS idx_agent_candidates_specialization ON insight_agent_candidates(specialization);
CREATE INDEX IF NOT EXISTS idx_agent_candidates_confidence ON insight_agent_candidates(confidence DESC);

-- Insight: Agent Experiments (A/B testing for agent evolution)
CREATE TABLE IF NOT EXISTS insight_agent_experiments (
    id                TEXT PRIMARY KEY,
    candidate_id      TEXT NOT NULL REFERENCES insight_agent_candidates(id) ON DELETE CASCADE,
    candidate_agent   TEXT NOT NULL,
    baseline_agent    TEXT NOT NULL,
    traffic_percent   REAL NOT NULL DEFAULT 10,
    status            TEXT NOT NULL DEFAULT 'running',
    sample_size       INTEGER NOT NULL DEFAULT 100,
    runs_candidate    INTEGER NOT NULL DEFAULT 0,
    runs_baseline     INTEGER NOT NULL DEFAULT 0,
    bandit_state_json TEXT NOT NULL DEFAULT '{}',
    started_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at      TEXT,
    winner            TEXT,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_agent_experiments_status ON insight_agent_experiments(status);
CREATE INDEX IF NOT EXISTS idx_agent_experiments_candidate ON insight_agent_experiments(candidate_id);
CREATE INDEX IF NOT EXISTS idx_agent_experiments_baseline ON insight_agent_experiments(baseline_agent);

-- Insight: Agent Experiment Results (per-run metrics)
CREATE TABLE IF NOT EXISTS insight_agent_experiment_results (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    experiment_id   TEXT NOT NULL REFERENCES insight_agent_experiments(id) ON DELETE CASCADE,
    workflow_id     TEXT NOT NULL,
    task_type       TEXT NOT NULL DEFAULT '',
    used_candidate  INTEGER NOT NULL DEFAULT 0,
    success         INTEGER NOT NULL DEFAULT 0,
    cycle_time_ms   INTEGER NOT NULL DEFAULT 0,
    review_passed   INTEGER NOT NULL DEFAULT 0,
    rework_count    INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_agent_exp_results_exp ON insight_agent_experiment_results(experiment_id);
CREATE INDEX IF NOT EXISTS idx_agent_exp_results_workflow ON insight_agent_experiment_results(workflow_id);

-- Product Intelligence: Tracked projects
CREATE TABLE IF NOT EXISTS pi_projects (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    path            TEXT NOT NULL,
    domain          TEXT NOT NULL DEFAULT '',
    domain_confidence REAL DEFAULT 0,
    readme_hash     TEXT,
    last_analyzed   TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_pi_projects_domain ON pi_projects(domain);
CREATE INDEX IF NOT EXISTS idx_pi_projects_path ON pi_projects(path);

-- Product Intelligence: Features detected in projects
CREATE TABLE IF NOT EXISTS pi_project_features (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES pi_projects(id) ON DELETE CASCADE,
    feature_name    TEXT NOT NULL,
    feature_type    TEXT NOT NULL DEFAULT 'capability',
    description     TEXT NOT NULL DEFAULT '',
    evidence_json   TEXT NOT NULL DEFAULT '{}',
    confidence      REAL DEFAULT 0.5,
    source          TEXT NOT NULL DEFAULT 'code_analysis',
    detected_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(project_id, feature_name)
);

CREATE INDEX IF NOT EXISTS idx_pi_features_project ON pi_project_features(project_id);
CREATE INDEX IF NOT EXISTS idx_pi_features_type ON pi_project_features(feature_type);

-- Product Intelligence: Market/competitor features
CREATE TABLE IF NOT EXISTS pi_market_features (
    id              TEXT PRIMARY KEY,
    domain          TEXT NOT NULL,
    feature_name    TEXT NOT NULL,
    feature_type    TEXT NOT NULL DEFAULT 'capability',
    prevalence      REAL DEFAULT 0,
    importance      REAL DEFAULT 0.5,
    sources_json    TEXT NOT NULL DEFAULT '[]',
    discovered_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(domain, feature_name)
);

CREATE INDEX IF NOT EXISTS idx_pi_market_domain ON pi_market_features(domain);
CREATE INDEX IF NOT EXISTS idx_pi_market_prevalence ON pi_market_features(prevalence DESC);

-- Product Intelligence: Feature gap analysis results
CREATE TABLE IF NOT EXISTS pi_feature_gaps (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES pi_projects(id) ON DELETE CASCADE,
    feature_name    TEXT NOT NULL,
    gap_type        TEXT NOT NULL DEFAULT 'missing',
    impact_score    REAL DEFAULT 0,
    complexity_score REAL DEFAULT 0,
    strategic_fit   REAL DEFAULT 0,
    confidence      REAL DEFAULT 0,
    reasoning       TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'identified',
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_pi_gaps_project ON pi_feature_gaps(project_id);
CREATE INDEX IF NOT EXISTS idx_pi_gaps_status ON pi_feature_gaps(status);
CREATE INDEX IF NOT EXISTS idx_pi_gaps_impact ON pi_feature_gaps(impact_score DESC);

-- Product Intelligence: Feature proposals
CREATE TABLE IF NOT EXISTS pi_feature_proposals (
    id                      TEXT PRIMARY KEY,
    project_id              TEXT NOT NULL REFERENCES pi_projects(id) ON DELETE CASCADE,
    gap_id                  TEXT REFERENCES pi_feature_gaps(id),
    feature_name            TEXT NOT NULL,
    title                   TEXT NOT NULL,
    description             TEXT NOT NULL,
    rationale               TEXT NOT NULL,
    impact_score            INTEGER NOT NULL DEFAULT 5,
    complexity_score        INTEGER NOT NULL DEFAULT 5,
    strategic_fit           REAL DEFAULT 0.5,
    confidence              REAL DEFAULT 0,
    evidence_json           TEXT NOT NULL DEFAULT '{}',
    implementation_hints_json TEXT NOT NULL DEFAULT '[]',
    status                  TEXT NOT NULL DEFAULT 'proposed',
    workflow_id             TEXT,
    created_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_pi_proposals_project ON pi_feature_proposals(project_id);
CREATE INDEX IF NOT EXISTS idx_pi_proposals_status ON pi_feature_proposals(status);
CREATE INDEX IF NOT EXISTS idx_pi_proposals_gap ON pi_feature_proposals(gap_id);
CREATE INDEX IF NOT EXISTS idx_pi_proposals_workflow ON pi_feature_proposals(workflow_id);
`
