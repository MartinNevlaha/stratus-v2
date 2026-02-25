package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Event represents a memory event.
type Event struct {
	ID        int64            `json:"id"`
	Ts        string           `json:"ts"`
	Actor     string           `json:"actor"`
	Scope     string           `json:"scope"`
	Type      string           `json:"type"`
	Text      string           `json:"text"`
	Title     string           `json:"title"`
	Tags      []string         `json:"tags"`
	Refs      map[string]any   `json:"refs"`
	TTL       *string          `json:"ttl,omitempty"`
	Importance float64         `json:"importance"`
	DedupeKey *string          `json:"dedupe_key,omitempty"`
	Project   *string          `json:"project,omitempty"`
	SessionID *string          `json:"session_id,omitempty"`
	CreatedMs int64            `json:"created_ms"`
}

// SaveEventInput is the input for SaveEvent.
type SaveEventInput struct {
	Actor      string         `json:"actor"`
	Scope      string         `json:"scope"`
	Type       string         `json:"type"`
	Text       string         `json:"text"`
	Title      string         `json:"title"`
	Tags       []string       `json:"tags"`
	Refs       map[string]any `json:"refs"`
	TTL        *string        `json:"ttl,omitempty"`
	Importance float64        `json:"importance"`
	DedupeKey  *string        `json:"dedupe_key,omitempty"`
	Project    *string        `json:"project,omitempty"`
	SessionID  *string        `json:"session_id,omitempty"`
}

// SaveEvent saves an event, returning the ID. Handles deduplication.
func (d *DB) SaveEvent(in SaveEventInput) (int64, error) {
	// Dedupe by explicit key
	if in.DedupeKey != nil && *in.DedupeKey != "" {
		var existing int64
		err := d.sql.QueryRow(`SELECT id FROM events WHERE dedupe_key = ?`, *in.DedupeKey).Scan(&existing)
		if err == nil {
			return existing, nil
		}
	}

	tags, _ := json.Marshal(in.Tags)
	refs, _ := json.Marshal(in.Refs)
	if in.Tags == nil {
		tags = []byte("[]")
	}
	if in.Refs == nil {
		refs = []byte("{}")
	}
	if in.Actor == "" {
		in.Actor = "agent"
	}
	if in.Scope == "" {
		in.Scope = "repo"
	}
	if in.Type == "" {
		in.Type = "discovery"
	}
	if in.Importance == 0 {
		in.Importance = 0.5
	}

	res, err := d.sql.Exec(`
		INSERT INTO events (actor, scope, type, text, title, tags, refs, ttl, importance, dedupe_key, project, session_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Actor, in.Scope, in.Type, in.Text, in.Title,
		string(tags), string(refs), in.TTL, in.Importance,
		in.DedupeKey, in.Project, in.SessionID,
	)
	if err != nil {
		// Unique constraint on dedupe_key — return existing
		if strings.Contains(err.Error(), "UNIQUE") {
			var existing int64
			_ = d.sql.QueryRow(`SELECT id FROM events WHERE dedupe_key = ?`, *in.DedupeKey).Scan(&existing)
			return existing, nil
		}
		return 0, fmt.Errorf("insert event: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// SearchEventsInput is the input for SearchEvents.
type SearchEventsInput struct {
	Query     string
	Type      string
	Scope     string
	Project   string
	DateStart string
	DateEnd   string
	Limit     int
	Offset    int
}

// buildFTS5Query converts a raw user query into a safe FTS5 MATCH expression.
// Each whitespace-separated term is quoted (escaping internal quotes) and given
// a trailing '*' so that partial-word / prefix matching works.
// E.g. "auth error" → `"auth"* "error"*`
func buildFTS5Query(raw string) string {
	terms := strings.Fields(strings.TrimSpace(raw))
	if len(terms) == 0 {
		return ""
	}
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.ReplaceAll(t, `"`, `""`) // escape internal double-quotes
		parts = append(parts, `"`+t+`"*`)
	}
	return strings.Join(parts, " ")
}

// SearchEvents searches events using FTS5.
func (d *DB) SearchEvents(in SearchEventsInput) ([]Event, error) {
	if in.Limit <= 0 {
		in.Limit = 20
	}

	var rows *sql.Rows
	var err error

	if in.Query != "" {
		ftsQuery := buildFTS5Query(in.Query)
		// FTS5 search
		rows, err = d.sql.Query(`
			SELECT e.id, e.ts, e.actor, e.scope, e.type, e.text, e.title,
			       e.tags, e.refs, e.ttl, e.importance, e.dedupe_key, e.project, e.session_id, e.created_ms
			FROM events_fts f
			JOIN events e ON e.id = f.rowid
			WHERE events_fts MATCH ?
			  AND (? = '' OR e.type = ?)
			  AND (? = '' OR e.scope = ?)
			  AND (? = '' OR e.project = ?)
			  AND (? = '' OR e.ts >= ?)
			  AND (? = '' OR e.ts <= ?)
			ORDER BY rank
			LIMIT ? OFFSET ?`,
			ftsQuery,
			in.Type, in.Type,
			in.Scope, in.Scope,
			in.Project, in.Project,
			in.DateStart, in.DateStart,
			in.DateEnd, in.DateEnd,
			in.Limit, in.Offset,
		)
	} else {
		rows, err = d.sql.Query(`
			SELECT id, ts, actor, scope, type, text, title,
			       tags, refs, ttl, importance, dedupe_key, project, session_id, created_ms
			FROM events
			WHERE (? = '' OR type = ?)
			  AND (? = '' OR scope = ?)
			  AND (? = '' OR project = ?)
			  AND (? = '' OR ts >= ?)
			  AND (? = '' OR ts <= ?)
			ORDER BY created_ms DESC
			LIMIT ? OFFSET ?`,
			in.Type, in.Type,
			in.Scope, in.Scope,
			in.Project, in.Project,
			in.DateStart, in.DateStart,
			in.DateEnd, in.DateEnd,
			in.Limit, in.Offset,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("search events: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// GetEventsByIDs returns events by IDs (batch).
func (d *DB) GetEventsByIDs(ids []int64) ([]Event, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := d.sql.Query(`
		SELECT id, ts, actor, scope, type, text, title,
		       tags, refs, ttl, importance, dedupe_key, project, session_id, created_ms
		FROM events WHERE id IN (`+placeholders+`) ORDER BY created_ms DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("get events by ids: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// GetTimeline returns events around an anchor event.
func (d *DB) GetTimeline(anchorID int64, depthBefore, depthAfter int) ([]Event, error) {
	if depthBefore <= 0 {
		depthBefore = 10
	}
	if depthAfter <= 0 {
		depthAfter = 10
	}
	// Use CTEs to get exactly N events before and N events after by ID,
	// avoiding ID-arithmetic gaps that BETWEEN would produce.
	rows, err := d.sql.Query(`
		WITH
		  before AS (
		    SELECT id, ts, actor, scope, type, text, title,
		           tags, refs, ttl, importance, dedupe_key, project, session_id, created_ms
		    FROM events WHERE id <= ? ORDER BY id DESC LIMIT ?
		  ),
		  after AS (
		    SELECT id, ts, actor, scope, type, text, title,
		           tags, refs, ttl, importance, dedupe_key, project, session_id, created_ms
		    FROM events WHERE id > ? ORDER BY id ASC LIMIT ?
		  )
		SELECT * FROM before
		UNION ALL
		SELECT * FROM after
		ORDER BY id`,
		anchorID, depthBefore,
		anchorID, depthAfter,
	)
	if err != nil {
		return nil, fmt.Errorf("get timeline: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var events []Event
	for rows.Next() {
		var e Event
		var tagsJSON, refsJSON string
		var ttl, dedupeKey, project, sessionID sql.NullString
		if err := rows.Scan(
			&e.ID, &e.Ts, &e.Actor, &e.Scope, &e.Type, &e.Text, &e.Title,
			&tagsJSON, &refsJSON, &ttl, &e.Importance,
			&dedupeKey, &project, &sessionID, &e.CreatedMs,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &e.Tags)
		_ = json.Unmarshal([]byte(refsJSON), &e.Refs)
		if ttl.Valid {
			e.TTL = &ttl.String
		}
		if dedupeKey.Valid {
			e.DedupeKey = &dedupeKey.String
		}
		if project.Valid {
			e.Project = &project.String
		}
		if sessionID.Valid {
			e.SessionID = &sessionID.String
		}
		if e.Tags == nil {
			e.Tags = []string{}
		}
		if e.Refs == nil {
			e.Refs = map[string]any{}
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// Session represents a Claude Code session.
type Session struct {
	ID               int64   `json:"id"`
	ContentSessionID string  `json:"content_session_id"`
	Project          string  `json:"project"`
	InitialPrompt    *string `json:"initial_prompt,omitempty"`
	StartedAt        string  `json:"started_at"`
}

// SaveSession creates or returns an existing session.
func (d *DB) SaveSession(contentSessionID, project string, initialPrompt *string) (*Session, error) {
	var s Session
	var ip sql.NullString
	err := d.sql.QueryRow(`SELECT id, content_session_id, project, initial_prompt, started_at FROM sessions WHERE content_session_id = ?`, contentSessionID).
		Scan(&s.ID, &s.ContentSessionID, &s.Project, &ip, &s.StartedAt)
	if err == nil {
		if ip.Valid {
			s.InitialPrompt = &ip.String
		}
		return &s, nil
	}
	res, err := d.sql.Exec(`INSERT INTO sessions (content_session_id, project, initial_prompt) VALUES (?, ?, ?)`,
		contentSessionID, project, initialPrompt)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Session{
		ID:               id,
		ContentSessionID: contentSessionID,
		Project:          project,
		InitialPrompt:    initialPrompt,
		StartedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

// ListSessions returns all sessions.
func (d *DB) ListSessions(limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.sql.Query(`SELECT id, content_session_id, project, initial_prompt, started_at FROM sessions ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var s Session
		var ip sql.NullString
		if err := rows.Scan(&s.ID, &s.ContentSessionID, &s.Project, &ip, &s.StartedAt); err != nil {
			return nil, err
		}
		if ip.Valid {
			s.InitialPrompt = &ip.String
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}
