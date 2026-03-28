package db

import "time"

// WorkflowLog is a single tool-call entry streamed from a hook during a workflow run.
type WorkflowLog struct {
	ID         int64  `json:"id"`
	WorkflowID string `json:"workflow_id"`
	SessionID  string `json:"session_id"`
	Ts         string `json:"ts"`
	ToolName   string `json:"tool_name"`
	Summary    string `json:"summary"`
	CreatedMs  int64  `json:"created_ms"`
}

// SaveWorkflowLog inserts a log entry and returns the saved row.
func (d *DB) SaveWorkflowLog(workflowID, sessionID, toolName, summary string) (WorkflowLog, error) {
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.999Z")
	createdMs := time.Now().UnixMilli()
	res, err := d.sql.Exec(
		`INSERT INTO workflow_logs (workflow_id, session_id, ts, tool_name, summary, created_ms)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		workflowID, sessionID, ts, toolName, summary, createdMs,
	)
	if err != nil {
		return WorkflowLog{}, err
	}
	id, _ := res.LastInsertId()
	return WorkflowLog{
		ID:         id,
		WorkflowID: workflowID,
		SessionID:  sessionID,
		Ts:         ts,
		ToolName:   toolName,
		Summary:    summary,
		CreatedMs:  createdMs,
	}, nil
}

// GetWorkflowLogs returns the most recent log entries for a workflow (newest last).
func (d *DB) GetWorkflowLogs(workflowID string, limit int) ([]WorkflowLog, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.sql.Query(
		`SELECT id, workflow_id, session_id, ts, tool_name, summary, created_ms
		 FROM workflow_logs WHERE workflow_id = ?
		 ORDER BY id DESC LIMIT ?`,
		workflowID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkflowLogs(rows, true)
}

// GetWorkflowLogsBySession returns the most recent log entries for a session (newest last).
func (d *DB) GetWorkflowLogsBySession(sessionID string, limit int) ([]WorkflowLog, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.sql.Query(
		`SELECT id, workflow_id, session_id, ts, tool_name, summary, created_ms
		 FROM workflow_logs WHERE session_id = ?
		 ORDER BY id DESC LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkflowLogs(rows, true)
}

func scanWorkflowLogs(rows interface {
	Next() bool
	Scan(...any) error
	Close() error
}, reverse bool) ([]WorkflowLog, error) {
	var logs []WorkflowLog
	for rows.Next() {
		var l WorkflowLog
		if err := rows.Scan(&l.ID, &l.WorkflowID, &l.SessionID, &l.Ts, &l.ToolName, &l.Summary, &l.CreatedMs); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	if reverse {
		for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
			logs[i], logs[j] = logs[j], logs[i]
		}
	}
	return logs, nil
}
