package db

import (
	"fmt"
	"github.com/MartinNevlaha/stratus-v2/projectidentity"
)

// UpsertPeerInProject verifies a filesystem root; display names never establish
// identity. A session may move between verified worktrees but not projects.
func (d *DB) UpsertPeerInProject(key, runtime, project, root string) (*Peer, error) {
	id, canonical, err := projectidentity.Resolve(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProjectRoot, err)
	}
	return upsertPeer(d.sql, key, runtime, project, id, canonical)
}

// GetPeerBySession accepts only the exact caller identity, never its public handle.
func (d *DB) GetPeerBySession(key string) (*Peer, error) { return findPeerByKey(d.sql, key) }

func senderProject(q querier, key string) (string, error) {
	if key == UserKey {
		return "", nil
	}
	p, err := findPeerByKey(q, key)
	if err != nil {
		return "", err
	}
	if p.ProjectID == "" {
		return "", ErrProjectScope
	}
	return p.ProjectID, nil
}

func checkTaskProject(q querier, key string, id int64) error {
	t, err := getTask(q, id)
	if err != nil {
		return err
	}
	if key == UserKey {
		return nil
	}
	projectID, err := senderProject(q, key)
	if err != nil {
		return err
	}
	if projectID != t.ProjectID {
		return ErrProjectScope
	}
	return nil
}

// ListPeersForSession scopes discovery using the registered caller, not caller-
// supplied project names. Unknown legacy sessions may see only themselves.
func (d *DB) ListPeersForSession(key string, activeOnly bool) ([]Peer, error) {
	self, err := findPeerByKey(d.sql, key)
	if err != nil {
		return nil, err
	}
	where, args := ` WHERE session_key = ?`, []any{key}
	if self.ProjectID != "" {
		where, args = ` WHERE project_id = ?`, []any{self.ProjectID}
	}
	if activeOnly {
		where += ` AND ended_at IS NULL AND last_seen_at >= ?`
		args = append(args, activeCutoff())
	}
	rows, err := d.sql.Query(`SELECT `+peerCols+` FROM peers`+where+` ORDER BY last_seen_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Peer{}
	for rows.Next() {
		p, e := scanPeer(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (d *DB) ListSharedTasksForSession(key, status string) ([]SharedTask, error) {
	projectID, err := senderProject(d.sql, key)
	if err != nil {
		return nil, err
	}
	if key == UserKey {
		return nil, ErrPeerNotFound
	}
	where, args := ` WHERE t.project_id = ?`, []any{projectID}
	if status != "" {
		where += ` AND t.status = ?`
		args = append(args, status)
	}
	rows, err := d.sql.Query(taskSelect+where+` ORDER BY t.id DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SharedTask{}
	for rows.Next() {
		task, e := scanTask(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *task)
	}
	return out, rows.Err()
}

// CreateSharedTaskInProject gives an unassigned dashboard task the server's
// verified project. Agent callers cannot override their registered project.
func (d *DB) CreateSharedTaskInProject(createdBy, title, description, assignee, root string) (*SharedTask, error) {
	id, _, err := projectidentity.Resolve(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProjectRoot, err)
	}
	return d.createSharedTask(createdBy, title, description, assignee, id)
}

func (d *DB) ListMessagesForSession(key string, limit int) ([]Message, error) {
	if _, err := findPeerByKey(d.sql, key); err != nil {
		return nil, err
	}
	return d.ListMessages(key, limit)
}
