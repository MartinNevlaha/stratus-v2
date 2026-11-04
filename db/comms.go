package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Comms: messages and shared tasks between agent sessions of different runtimes
// (Claude Code, OpenCode, Codex). A peer is one session, keyed by its session key:
// the raw Claude Code session id, "codex:<native>" or "opencode:<id>".

// UserKey is the sender/recipient key of the human using the dashboard.
const UserKey = "user"

// PeerActiveWindow is how long after its last hook call a session still counts as active.
const PeerActiveWindow = 6 * time.Hour

const commsTSLayout = "2006-01-02T15:04:05.000Z"

// Size limits: messages are injected verbatim into other sessions' model context.
const (
	MaxMessageBody     = 8000
	MaxTaskTitle       = 200
	MaxTaskDescription = 4000
)

// heartbeatInterval throttles last_seen writes from the inbox hot path.
const heartbeatInterval = time.Minute

var (
	ErrPeerNotFound       = errors.New("peer not found")
	ErrInvalidHandle      = errors.New("invalid handle")
	ErrHandleTaken        = errors.New("handle already taken")
	ErrUnknownRecipient   = errors.New("unknown recipient")
	ErrNoRecipients       = errors.New("no active recipients")
	ErrInvalidKind        = errors.New("invalid message kind")
	ErrEmptyBody          = errors.New("message body is empty")
	ErrTaskNotFound       = errors.New("shared task not found")
	ErrInvalidTaskStatus  = errors.New("invalid shared task status")
	ErrEmptyTitle         = errors.New("shared task title is empty")
	ErrTooLong            = errors.New("text too long")
	ErrTaskTaken          = errors.New("shared task is already assigned to another session")
	ErrProjectScope       = errors.New("session and target must belong to the same verified project")
	ErrInvalidProjectRoot = errors.New("invalid project root")
)

// MessageKinds lists the accepted message kinds.
var MessageKinds = []string{"text", "done", "question", "task_assigned", "task_update", "task_done"}

// SharedTaskStatuses lists the accepted shared task statuses.
var SharedTaskStatuses = []string{"open", "claimed", "done", "cancelled"}

var runtimeAliases = map[string]string{
	"claude-code": "claude-code", "claude": "claude-code",
	"opencode": "opencode", "codex": "codex",
}

var handlePrefix = map[string]string{"claude-code": "claude", "opencode": "opencode", "codex": "codex"}

var handleRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)

type Peer struct {
	SessionKey  string  `json:"session_key"`
	Runtime     string  `json:"runtime"`
	Handle      string  `json:"handle"`
	Project     string  `json:"project"`
	ProjectID   string  `json:"project_id"`
	ProjectRoot string  `json:"project_root"`
	StartedAt   string  `json:"started_at"`
	LastSeenAt  string  `json:"last_seen_at"`
	EndedAt     *string `json:"ended_at,omitempty"`
	Active      bool    `json:"active"`
}

type Message struct {
	ProjectID   string   `json:"project_id"`
	ID          int64    `json:"id"`
	FromKey     string   `json:"from_key"`
	FromHandle  string   `json:"from_handle"`
	FromRuntime string   `json:"from_runtime"`
	ToSpec      string   `json:"to"`
	Kind        string   `json:"kind"`
	Body        string   `json:"body"`
	TaskID      *int64   `json:"task_id,omitempty"`
	CreatedAt   string   `json:"created_at"`
	Recipients  []string `json:"recipients,omitempty"`
}

type SharedTask struct {
	ProjectID       string `json:"project_id"`
	ID              int64  `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	Status          string `json:"status"`
	CreatedBy       string `json:"created_by"`
	CreatedByHandle string `json:"created_by_handle"`
	Assignee        string `json:"assignee,omitempty"`
	AssigneeHandle  string `json:"assignee_handle,omitempty"`
	Result          string `json:"result"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// InboxFilter narrows ClaimInbox; From matches a sender handle, session key or runtime.
type InboxFilter struct {
	From string
	Kind string
}

// SharedTaskUpdate holds the fields to change; nil fields are left as they are.
type SharedTaskUpdate struct {
	Status   *string
	Result   *string
	Assignee *string
}

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

func commsNow() string { return time.Now().UTC().Format(commsTSLayout) }

func activeCutoff() string { return time.Now().UTC().Add(-PeerActiveWindow).Format(commsTSLayout) }

// RuntimeForKey infers a runtime from the session-key convention.
func RuntimeForKey(key string) string {
	switch {
	case strings.HasPrefix(key, "codex:"):
		return "codex"
	case strings.HasPrefix(key, "opencode:"):
		return "opencode"
	default:
		return "claude-code"
	}
}

// NormalizeRuntime maps a runtime name or alias to its canonical name, or "" if unknown.
func NormalizeRuntime(r string) string { return runtimeAliases[strings.ToLower(strings.TrimSpace(r))] }

func randomHandle(runtime string) string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return handlePrefix[runtime] + "-" + hex.EncodeToString(b)
}

const peerCols = `session_key, runtime, handle, project, started_at, last_seen_at, ended_at, project_id, project_root`

func scanPeer(row interface{ Scan(...any) error }) (*Peer, error) {
	var p Peer
	var ended sql.NullString
	if err := row.Scan(&p.SessionKey, &p.Runtime, &p.Handle, &p.Project, &p.StartedAt, &p.LastSeenAt, &ended, &p.ProjectID, &p.ProjectRoot); err != nil {
		return nil, err
	}
	if ended.Valid {
		p.EndedAt = &ended.String
	}
	p.Active = !ended.Valid && p.LastSeenAt >= activeCutoff()
	return &p, nil
}

// findPeer resolves a session key or handle to a peer.
func findPeer(q querier, ref string) (*Peer, error) {
	p, err := scanPeer(q.QueryRow(`SELECT `+peerCols+` FROM peers WHERE session_key = ? OR handle = ? ORDER BY session_key = ? DESC LIMIT 1`, ref, ref, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPeerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find peer: %w", err)
	}
	return p, nil
}

// findPeerByKey resolves a caller's own identity. Only the exact session key counts:
// handles are public, so accepting one would let any agent act as that session.
func findPeerByKey(q querier, key string) (*Peer, error) {
	p, err := scanPeer(q.QueryRow(`SELECT `+peerCols+` FROM peers WHERE session_key = ?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPeerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find peer by key: %w", err)
	}
	return p, nil
}

func upsertPeer(q querier, key, runtime, project string, scope ...string) (*Peer, error) {
	projectID, projectRoot := "", ""
	if len(scope) == 2 {
		projectID, projectRoot = scope[0], scope[1]
	}
	key = strings.TrimSpace(key)
	if key == "" || key == UserKey {
		return nil, fmt.Errorf("upsert peer: %w", ErrInvalidHandle)
	}
	runtime = NormalizeRuntime(runtime)
	if runtime == "" {
		runtime = RuntimeForKey(key)
	}
	var isHandle bool
	if err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM peers WHERE handle = ? AND session_key != ?)`, key, key).Scan(&isHandle); err != nil {
		return nil, fmt.Errorf("check peer key: %w", err)
	}
	if isHandle {
		return nil, fmt.Errorf("%w: %q is another session's handle, not a session id", ErrPeerNotFound, key)
	}
	now := commsNow()
	var err error
	for range 8 { // retry only on a random handle collision
		var result sql.Result
		result, err = q.Exec(`INSERT INTO peers (`+peerCols+`) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?)
			ON CONFLICT(session_key) DO UPDATE SET last_seen_at = excluded.last_seen_at, ended_at = NULL,
				project = CASE WHEN excluded.project = '' THEN peers.project ELSE excluded.project END,
				project_id = CASE WHEN excluded.project_id = '' THEN peers.project_id ELSE excluded.project_id END,
				project_root = CASE WHEN excluded.project_root = '' THEN peers.project_root ELSE excluded.project_root END
			WHERE excluded.project_id = '' OR peers.project_id = '' OR peers.project_id = excluded.project_id`,
			key, runtime, randomHandle(runtime), project, now, now, projectID, projectRoot)
		if err == nil {
			if n, _ := result.RowsAffected(); n == 0 {
				return nil, ErrProjectScope
			}
		}
		if err == nil || !strings.Contains(err.Error(), "peers.handle") {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("upsert peer: %w", err)
	}
	return findPeerByKey(q, key)
}

// UpsertPeer registers a session or refreshes it (last seen, un-ended). An empty
// runtime is inferred from the key. The handle is kept across re-registrations.
func (d *DB) UpsertPeer(key, runtime, project string) (*Peer, error) {
	return upsertPeer(d.sql, key, runtime, project)
}

// GetPeer returns a peer by session key or handle.
func (d *DB) GetPeer(ref string) (*Peer, error) { return findPeer(d.sql, ref) }

// EndPeer marks a session as ended.
func (d *DB) EndPeer(key string) error {
	p, err := findPeerByKey(d.sql, key)
	if err != nil {
		return err
	}
	if _, err := d.sql.Exec(`UPDATE peers SET ended_at = ? WHERE session_key = ?`, commsNow(), p.SessionKey); err != nil {
		return fmt.Errorf("end peer: %w", err)
	}
	return nil
}

// RenamePeer sets a session's handle.
func (d *DB) RenamePeer(key, handle string) (*Peer, error) {
	handle = strings.TrimSpace(handle)
	if !handleRe.MatchString(handle) || NormalizeRuntime(handle) != "" || handle == UserKey {
		return nil, fmt.Errorf("%w: use 2-32 chars of a-z, 0-9 and '-', not a runtime name", ErrInvalidHandle)
	}
	p, err := findPeerByKey(d.sql, key)
	if err != nil {
		return nil, err
	}
	if _, err := d.sql.Exec(`UPDATE peers SET handle = ? WHERE session_key = ?`, handle, p.SessionKey); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrHandleTaken
		}
		return nil, fmt.Errorf("rename peer: %w", err)
	}
	return findPeerByKey(d.sql, p.SessionKey)
}

// ListPeers returns peers, most recently seen first.
func (d *DB) ListPeers(activeOnly bool) ([]Peer, error) {
	query := `SELECT ` + peerCols + ` FROM peers`
	var args []any
	if activeOnly {
		query += ` WHERE ended_at IS NULL AND last_seen_at >= ?`
		args = append(args, activeCutoff())
	}
	rows, err := d.sql.Query(query+` ORDER BY last_seen_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	defer rows.Close()
	peers := []Peer{}
	for rows.Next() {
		p, err := scanPeer(rows)
		if err != nil {
			return nil, fmt.Errorf("scan peer: %w", err)
		}
		peers = append(peers, *p)
	}
	return peers, rows.Err()
}

// requireSender checks that a sender/actor is the dashboard user or a registered
// session, so a wrong session_id fails loudly instead of creating a ghost peer.
func requireSender(q querier, key string) error {
	if key == UserKey {
		return nil
	}
	if _, err := findPeerByKey(q, key); err != nil {
		if errors.Is(err, ErrPeerNotFound) {
			return fmt.Errorf("%w: unknown sender session %q — use the session_id from your Stratus comms context", ErrPeerNotFound, key)
		}
		return err
	}
	return nil
}

// resolveRecipients turns an address into session keys: "*" (all active but the
// sender), a runtime (its active peers but the sender), "user" (nobody — the
// dashboard sees every message), or a handle / session key.
func resolveRecipients(q querier, fromKey, to string) ([]string, error) {
	to = strings.TrimSpace(to)
	if to == UserKey {
		return nil, nil
	}
	projectID, err := senderProject(q, fromKey)
	if err != nil {
		return nil, err
	}
	if to == "*" || NormalizeRuntime(to) != "" {
		query := `SELECT session_key FROM peers WHERE ended_at IS NULL AND last_seen_at >= ? AND session_key != ?`
		args := []any{activeCutoff(), fromKey}
		if fromKey != UserKey {
			query += ` AND project_id = ?`
			args = append(args, projectID)
		}
		if to != "*" {
			query += ` AND runtime = ?`
			args = append(args, NormalizeRuntime(to))
		}
		rows, err := q.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("resolve recipients: %w", err)
		}
		defer rows.Close()
		var keys []string
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				return nil, fmt.Errorf("scan recipient: %w", err)
			}
			keys = append(keys, k)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("resolve recipients: %w", err)
		}
		if len(keys) == 0 {
			return nil, fmt.Errorf("%w for %q", ErrNoRecipients, to)
		}
		return keys, nil
	}
	p, err := findPeer(q, to)
	if errors.Is(err, ErrPeerNotFound) {
		return nil, fmt.Errorf("%w %q", ErrUnknownRecipient, to)
	}
	if err != nil {
		return nil, err
	}
	if fromKey != UserKey && p.ProjectID != projectID {
		return nil, ErrProjectScope
	}
	return []string{p.SessionKey}, nil
}

func sendMessage(q querier, fromKey, to, kind, body string, taskID *int64) (*Message, []string, error) {
	if kind == "" {
		kind = "text"
	}
	if !slices.Contains(MessageKinds, kind) {
		return nil, nil, fmt.Errorf("%w %q (allowed: %s)", ErrInvalidKind, kind, strings.Join(MessageKinds, ", "))
	}
	if strings.TrimSpace(body) == "" {
		return nil, nil, ErrEmptyBody
	}
	if len(body) > MaxMessageBody {
		return nil, nil, fmt.Errorf("%w: message body exceeds %d bytes", ErrTooLong, MaxMessageBody)
	}
	if err := requireSender(q, fromKey); err != nil {
		return nil, nil, err
	}
	if taskID != nil {
		if err := checkTaskProject(q, fromKey, *taskID); err != nil {
			return nil, nil, err
		}
	}
	recipients, err := resolveRecipients(q, fromKey, to)
	if err != nil {
		return nil, nil, err
	}
	now := commsNow()
	projectID := ""
	if fromKey != UserKey {
		p, e := findPeerByKey(q, fromKey)
		if e != nil {
			return nil, nil, e
		}
		projectID = p.ProjectID
	} else if len(recipients) == 1 {
		p, e := findPeerByKey(q, recipients[0])
		if e != nil {
			return nil, nil, e
		}
		projectID = p.ProjectID
	}
	res, err := q.Exec(`INSERT INTO messages (from_key, to_spec, kind, body, task_id, created_at, project_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		fromKey, strings.TrimSpace(to), kind, body, taskID, now, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("insert message: %w", err)
	}
	id, _ := res.LastInsertId()
	for _, k := range recipients {
		if _, err := q.Exec(`INSERT INTO message_recipients (message_id, session_key) VALUES (?, ?)`, id, k); err != nil {
			return nil, nil, fmt.Errorf("insert recipient: %w", err)
		}
	}
	msgs, err := queryMessages(q, `WHERE m.id = ?`, []any{id}, 1)
	if err != nil || len(msgs) == 0 {
		return nil, nil, fmt.Errorf("reload message: %w", err)
	}
	return &msgs[0], recipients, nil
}

// SendMessage stores a message and fans it out to its recipients' inboxes. It
// returns the recipients' session keys; an unregistered sender is registered.
func (d *DB) SendMessage(fromKey, to, kind, body string, taskID *int64) (*Message, []string, error) {
	tx, err := d.sql.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	m, recips, err := sendMessage(tx, fromKey, to, kind, body, taskID)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit message: %w", err)
	}
	return m, recips, nil
}

const messageSelect = `SELECT m.id, m.from_key, COALESCE(p.handle, m.from_key), COALESCE(p.runtime, ''), COALESCE((SELECT handle FROM peers WHERE session_key = m.to_spec), m.to_spec), m.kind, m.body, m.task_id, m.created_at,
	(SELECT group_concat(COALESCE(rp.handle, r.session_key), ',') FROM message_recipients r LEFT JOIN peers rp ON rp.session_key = r.session_key WHERE r.message_id = m.id), m.project_id
	FROM messages m LEFT JOIN peers p ON p.session_key = m.from_key `

func queryMessages(q querier, where string, args []any, limit int) ([]Message, error) {
	rows, err := q.Query(messageSelect+where+` ORDER BY m.id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()
	msgs := []Message{}
	for rows.Next() {
		var m Message
		var task sql.NullInt64
		var recips sql.NullString
		if err := rows.Scan(&m.ID, &m.FromKey, &m.FromHandle, &m.FromRuntime, &m.ToSpec, &m.Kind, &m.Body, &task, &m.CreatedAt, &recips, &m.ProjectID); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if task.Valid {
			m.TaskID = &task.Int64
		}
		if recips.String != "" {
			m.Recipients = strings.Split(recips.String, ",")
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (f InboxFilter) matches(m Message) bool {
	if f.Kind != "" && m.Kind != f.Kind {
		return false
	}
	if f.From == "" {
		return true
	}
	if rt := NormalizeRuntime(f.From); rt != "" {
		return m.FromRuntime == rt
	}
	return m.FromHandle == f.From || m.FromKey == f.From
}

// ValidateInboxFilter rejects filters that could never match: an unknown kind, or a
// from that is neither a runtime, the user, nor a known peer.
func (d *DB) ValidateInboxFilter(f InboxFilter) error {
	if f.Kind != "" && !slices.Contains(MessageKinds, f.Kind) {
		return fmt.Errorf("%w %q (allowed: %s)", ErrInvalidKind, f.Kind, strings.Join(MessageKinds, ", "))
	}
	if f.From == "" || f.From == UserKey || NormalizeRuntime(f.From) != "" {
		return nil
	}
	if _, err := findPeer(d.sql, f.From); err != nil {
		return fmt.Errorf("from %q: %w", f.From, err)
	}
	return nil
}

// ClaimInbox returns the undelivered messages for a session that match the filter,
// oldest first, and marks them delivered. With register, a missing or ended peer is
// (re)registered — hooks use this; tool calls do not, so a wrong id is an error.
// The common case (nothing pending) only reads, apart from a throttled heartbeat.
func (d *DB) ClaimInbox(ctx context.Context, key string, f InboxFilter, limit int, register bool) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	p, err := findPeerByKey(d.sql, key)
	switch {
	case errors.Is(err, ErrPeerNotFound) && register, err == nil && register && p.EndedAt != nil:
		if p, err = upsertPeer(d.sql, key, "", ""); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case p.EndedAt == nil && p.LastSeenAt < time.Now().UTC().Add(-heartbeatInterval).Format(commsTSLayout):
		if _, err := d.sql.Exec(`UPDATE peers SET last_seen_at = ? WHERE session_key = ?`, commsNow(), p.SessionKey); err != nil {
			return nil, fmt.Errorf("heartbeat peer: %w", err)
		}
	}
	key = p.SessionKey

	var pending bool
	if err := d.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM message_recipients WHERE session_key = ? AND delivered_at IS NULL)`, key).Scan(&pending); err != nil {
		return nil, fmt.Errorf("probe inbox: %w", err)
	}
	if !pending {
		return []Message{}, nil
	}

	pendingMsgs, err := queryMessages(d.sql, `WHERE m.id IN (SELECT message_id FROM message_recipients WHERE session_key = ? AND delivered_at IS NULL)`, []any{key}, 500)
	if err != nil {
		return nil, err
	}
	// The tx starts with a write, so SQLite's busy_timeout applies (a read-then-write
	// tx fails with SQLITE_BUSY under contention). BeginTx(ctx): a client that gave up
	// (hook timeout, cancelled wait) rolls back instead of losing its messages.
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	claimed := []Message{}
	now := commsNow()
	for i := len(pendingMsgs) - 1; i >= 0 && len(claimed) < limit; i-- { // oldest first
		m := pendingMsgs[i]
		if !f.matches(m) {
			continue
		}
		res, err := tx.Exec(`UPDATE message_recipients SET delivered_at = ? WHERE message_id = ? AND session_key = ? AND delivered_at IS NULL`, now, m.ID, key)
		if err != nil {
			return nil, fmt.Errorf("mark delivered: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 1 { // 0: a concurrent claim already took it
			claimed = append(claimed, m)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit inbox claim: %w", err)
	}
	return claimed, nil
}

// ListMessages returns recent messages, newest first; a non-empty peer (handle or
// key) limits them to those it sent or received.
func (d *DB) ListMessages(peer string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if peer == "" {
		return queryMessages(d.sql, ``, nil, limit)
	}
	key := peer
	if p, err := findPeer(d.sql, peer); err == nil {
		key = p.SessionKey
	}
	return queryMessages(d.sql, `WHERE m.from_key = ? OR m.id IN (SELECT message_id FROM message_recipients WHERE session_key = ?)`, []any{key, key}, limit)
}

const taskSelect = `SELECT t.id, t.title, t.description, t.status, t.created_by, COALESCE(c.handle, t.created_by), COALESCE(t.assignee, ''), COALESCE(a.handle, t.assignee, ''), t.result, t.created_at, t.updated_at, t.project_id
	FROM shared_tasks t LEFT JOIN peers c ON c.session_key = t.created_by LEFT JOIN peers a ON a.session_key = t.assignee `

func scanTask(row interface{ Scan(...any) error }) (*SharedTask, error) {
	var t SharedTask
	if err := row.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.CreatedBy, &t.CreatedByHandle, &t.Assignee, &t.AssigneeHandle, &t.Result, &t.CreatedAt, &t.UpdatedAt, &t.ProjectID); err != nil {
		return nil, err
	}
	return &t, nil
}

func getTask(q querier, id int64) (*SharedTask, error) {
	t, err := scanTask(q.QueryRow(taskSelect+`WHERE t.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get shared task: %w", err)
	}
	return t, nil
}

// resolveAssignee maps a handle or key to a session key; "" stays unassigned.
func resolveAssignee(q querier, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	p, err := findPeer(q, ref)
	if errors.Is(err, ErrPeerNotFound) {
		return "", fmt.Errorf("%w %q", ErrUnknownRecipient, ref)
	}
	if err != nil {
		return "", err
	}
	return p.SessionKey, nil
}

// CreateSharedTask adds a task to the shared board; an assignee gets a task_assigned message.
func (d *DB) CreateSharedTask(createdBy, title, description, assignee string) (*SharedTask, error) {
	return d.createSharedTask(createdBy, title, description, assignee, "")
}

func (d *DB) createSharedTask(createdBy, title, description, assignee, requiredProject string) (*SharedTask, error) {
	if strings.TrimSpace(title) == "" {
		return nil, ErrEmptyTitle
	}
	if len(title) > MaxTaskTitle || len(description) > MaxTaskDescription {
		return nil, fmt.Errorf("%w: title max %d, description max %d bytes", ErrTooLong, MaxTaskTitle, MaxTaskDescription)
	}
	tx, err := d.sql.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := requireSender(tx, createdBy); err != nil {
		return nil, err
	}
	assigneeKey, err := resolveAssignee(tx, assignee)
	if err != nil {
		return nil, err
	}
	projectID, err := senderProject(tx, createdBy)
	if err != nil {
		return nil, err
	}
	if requiredProject != "" {
		if createdBy != UserKey && requiredProject != projectID {
			return nil, ErrProjectScope
		}
		projectID = requiredProject
	}
	if assigneeKey != "" {
		p, e := findPeerByKey(tx, assigneeKey)
		if e != nil {
			return nil, e
		}
		if createdBy == UserKey && projectID == "" {
			projectID = p.ProjectID
		}
		if projectID == "" || p.ProjectID != projectID {
			return nil, ErrProjectScope
		}
	}
	now := commsNow()
	res, err := tx.Exec(`INSERT INTO shared_tasks (title, description, status, created_by, assignee, created_at, updated_at, project_id) VALUES (?, ?, 'open', ?, NULLIF(?, ''), ?, ?, ?)`,
		strings.TrimSpace(title), description, createdBy, assigneeKey, now, now, projectID)
	if err != nil {
		return nil, fmt.Errorf("insert shared task: %w", err)
	}
	id, _ := res.LastInsertId()
	if assigneeKey != "" && assigneeKey != createdBy {
		body := fmt.Sprintf("Task #%d assigned to you: %s", id, strings.TrimSpace(title))
		if description != "" {
			body += "\n" + description
		}
		if _, _, err := sendMessage(tx, createdBy, assigneeKey, "task_assigned", body, &id); err != nil {
			return nil, fmt.Errorf("notify assignee: %w", err)
		}
	}
	t, err := getTask(tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit shared task: %w", err)
	}
	return t, nil
}

// ListSharedTasks returns tasks, newest first, optionally filtered by status.
func (d *DB) ListSharedTasks(status string) ([]SharedTask, error) {
	where, args := ``, []any{}
	if status != "" {
		where, args = `WHERE t.status = ? `, []any{status}
	}
	rows, err := d.sql.Query(taskSelect+where+`ORDER BY t.id DESC LIMIT 500`, args...)
	if err != nil {
		return nil, fmt.Errorf("list shared tasks: %w", err)
	}
	defer rows.Close()
	tasks := []SharedTask{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan shared task: %w", err)
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

// UpdateSharedTask changes a task on behalf of actor. Claiming without an assignee
// assigns the actor. A new assignee gets task_assigned; the creator is told about
// status changes (task_done for done/cancelled, task_update otherwise).
func (d *DB) UpdateSharedTask(id int64, actor string, u SharedTaskUpdate) (*SharedTask, error) {
	if u.Status != nil && !slices.Contains(SharedTaskStatuses, *u.Status) {
		return nil, fmt.Errorf("%w %q (allowed: %s)", ErrInvalidTaskStatus, *u.Status, strings.Join(SharedTaskStatuses, ", "))
	}
	tx, err := d.sql.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	// Acquire the SQLite writer before reading ownership so simultaneous claims
	// serialize instead of both observing an unassigned task.
	if _, err := tx.Exec(`UPDATE shared_tasks SET updated_at = updated_at WHERE id = ?`, id); err != nil {
		return nil, fmt.Errorf("lock shared task: %w", err)
	}
	t, err := getTask(tx, id)
	if err != nil {
		return nil, err
	}
	if err := requireSender(tx, actor); err != nil {
		return nil, err
	}
	if err := checkTaskProject(tx, actor, id); err != nil {
		return nil, err
	}
	if u.Result != nil && len(*u.Result) > MaxTaskDescription {
		return nil, fmt.Errorf("%w: result exceeds %d bytes", ErrTooLong, MaxTaskDescription)
	}
	assignee := t.Assignee
	if u.Assignee != nil {
		if assignee, err = resolveAssignee(tx, *u.Assignee); err != nil {
			return nil, err
		}
		if assignee != "" {
			p, e := findPeerByKey(tx, assignee)
			if e != nil {
				return nil, e
			}
			if p.ProjectID == "" || p.ProjectID != t.ProjectID {
				return nil, ErrProjectScope
			}
		}
	} else if u.Status != nil && *u.Status == "claimed" && actor != UserKey {
		if assignee != "" && assignee != actor {
			return nil, fmt.Errorf("%w (task #%d belongs to %s)", ErrTaskTaken, id, t.AssigneeHandle)
		}
		assignee = actor
	}
	if actor != UserKey && actor != t.CreatedBy && actor != t.Assignee {
		claimingOpen := t.Status == "open" && t.Assignee == "" && u.Status != nil && *u.Status == "claimed" && assignee == actor
		if !claimingOpen {
			return nil, ErrTaskTaken
		}
	}
	status, result := t.Status, t.Result
	if u.Status != nil {
		status = *u.Status
	}
	if u.Result != nil {
		result = *u.Result
	}
	if _, err := tx.Exec(`UPDATE shared_tasks SET status = ?, result = ?, assignee = NULLIF(?, ''), updated_at = ? WHERE id = ?`,
		status, result, assignee, commsNow(), id); err != nil {
		return nil, fmt.Errorf("update shared task: %w", err)
	}

	notify := func(to, kind, body string) error {
		if to == "" || to == actor {
			return nil
		}
		if _, _, err := sendMessage(tx, actor, to, kind, body, &id); err != nil && !errors.Is(err, ErrUnknownRecipient) {
			return fmt.Errorf("notify %s: %w", to, err)
		}
		return nil
	}
	if assignee != t.Assignee {
		if err := notify(t.Assignee, "task_update", fmt.Sprintf("Task #%d %q was reassigned away from you", id, t.Title)); err != nil {
			return nil, err
		}
		if err := notify(assignee, "task_assigned", fmt.Sprintf("Task #%d assigned to you: %s", id, t.Title)); err != nil {
			return nil, err
		}
	}
	if status != t.Status {
		body := fmt.Sprintf("Task #%d %q is now %s", id, t.Title, status)
		if result != "" {
			body += ": " + result
		}
		kind := "task_update"
		if status == "done" || status == "cancelled" {
			kind = "task_done"
		}
		if err := notify(t.CreatedBy, kind, body); err != nil {
			return nil, err
		}
		// The assignee keeps working unless told; a new assignee already got task_assigned.
		if assignee == t.Assignee && assignee != t.CreatedBy {
			if err := notify(assignee, "task_update", body); err != nil {
				return nil, err
			}
		}
	}
	updated, err := getTask(tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit shared task: %w", err)
	}
	return updated, nil
}
