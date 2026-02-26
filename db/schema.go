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
    tokenize='porter unicode61'
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
    id           TEXT PRIMARY KEY,
    workflow_id  TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    title        TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'planning',
    base_branch  TEXT NOT NULL DEFAULT 'main',
    merge_branch TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
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
`
