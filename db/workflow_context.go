package db

import (
	"fmt"
	"time"
)

const workflowEventColumns = `id,ts,actor,scope,type,text,title,tags,refs,ttl,importance,dedupe_key,project,session_id,created_ms`

// LatestWorkflowEvent returns the newest event of eventType whose refs carry
// workflow_id (and, when phase is non-empty, that phase). It returns nil, nil
// when there is none.
func (d *DB) LatestWorkflowEvent(eventType, workflowID, phase string) (*Event, error) {
	rows, err := d.sql.Query(`SELECT `+workflowEventColumns+` FROM events
		WHERE type = ? AND json_extract(refs,'$.workflow_id') = ? AND (? = '' OR json_extract(refs,'$.phase') = ?)
		ORDER BY id DESC LIMIT 1`, eventType, workflowID, phase, phase)
	if err != nil {
		return nil, fmt.Errorf("query latest workflow event: %w", err)
	}
	defer rows.Close()
	evs, err := scanEvents(rows)
	if err != nil {
		return nil, fmt.Errorf("scan latest workflow event: %w", err)
	}
	if len(evs) == 0 {
		return nil, nil
	}
	return &evs[0], nil
}

// RecentWorkflowDecisions returns up to limit decision events tied to the
// workflow (refs.workflow_id or a tag equal to the workflow id), newest first.
// A non-zero since keeps only decisions created strictly after it.
func (d *DB) RecentWorkflowDecisions(workflowID string, limit int, since time.Time) ([]Event, error) {
	var sinceMs int64
	if !since.IsZero() {
		sinceMs = since.UnixMilli()
	}
	rows, err := d.sql.Query(`SELECT `+workflowEventColumns+` FROM events
		WHERE type = 'decision' AND (json_extract(refs,'$.workflow_id') = ?
			OR EXISTS (SELECT 1 FROM json_each(events.tags) WHERE json_each.value = ?))
			AND created_ms > ?
		ORDER BY id DESC LIMIT ?`, workflowID, workflowID, sinceMs, limit)
	if err != nil {
		return nil, fmt.Errorf("query workflow decisions: %w", err)
	}
	defer rows.Close()
	evs, err := scanEvents(rows)
	if err != nil {
		return nil, fmt.Errorf("scan workflow decisions: %w", err)
	}
	return evs, nil
}
