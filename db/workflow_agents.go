package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// WorkflowAgent is a subagent run observed by hooks inside a normal (non-swarm) workflow.
type WorkflowAgent struct {
	NativeAgentID  string   `json:"native_agent_id,omitempty"`
	TurnID         string   `json:"turn_id,omitempty"`
	Runtime        string   `json:"runtime,omitempty"`
	UsageAvailable *bool    `json:"usage_available,omitempty"`
	ID             string   `json:"id"`
	WorkflowID     string   `json:"workflow_id"`
	SessionID      string   `json:"session_id"`
	AgentType      string   `json:"agent_type"`
	Phase          string   `json:"phase"`
	TaskIndex      *int     `json:"task_index,omitempty"`
	Status         string   `json:"status"` // running | done | failed
	StartedAt      string   `json:"started_at"`
	EndedAt        string   `json:"ended_at,omitempty"`
	LastActivity   string   `json:"last_activity"`
	ToolCalls      int      `json:"tool_calls"`
	LastTool       string   `json:"last_tool,omitempty"`
	Files          []string `json:"files"`
	TranscriptPath string   `json:"transcript_path,omitempty"`
	Verdict        string   `json:"verdict,omitempty"`
	MustFix        int      `json:"must_fix"`
	ShouldFix      int      `json:"should_fix"`
	Suggestion     int      `json:"suggestion"`
	// TokensIn counts input plus cache tokens, TokensOut output tokens; filled when the agent stops.
	TokensIn  int `json:"tokens_in"`
	TokensOut int `json:"tokens_out"`
}

// ReviewVerdict is the structured outcome parsed from a reviewer agent's final message.
type ReviewVerdict struct {
	Verdict    string // PASS | FAIL | ""
	MustFix    int
	ShouldFix  int
	Suggestion int
}

const wfAgentCols = `id, workflow_id, session_id, agent_type, phase, task_index, status, started_at,
	ended_at, last_activity, tool_calls, last_tool, files, transcript_path, verdict, must_fix, should_fix, suggestion,
	tokens_in, tokens_out, native_agent_id, turn_id`

func nowTS() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// StartWorkflowAgent records a running agent. A repeated start for the same id is ignored.
func (d *DB) StartWorkflowAgent(a WorkflowAgent) error {
	ts := nowTS()
	var task any
	if a.TaskIndex != nil {
		task = *a.TaskIndex
	}
	_, err := d.sql.Exec(
		`INSERT OR IGNORE INTO workflow_agents
		 (id, workflow_id, session_id, agent_type, phase, task_index, status, started_at, last_activity, transcript_path)
		 VALUES (?, ?, ?, ?, ?, ?, 'running', ?, ?, ?)`,
		a.ID, a.WorkflowID, a.SessionID, a.AgentType, a.Phase, task, ts, ts, a.TranscriptPath,
	)
	if err != nil {
		return fmt.Errorf("start workflow agent: %w", err)
	}
	return nil
}

// TouchWorkflowAgent counts one tool call for the agent and merges the touched files.
// An unknown agent id is a no-op.
func (d *DB) TouchWorkflowAgent(id, tool string, files []string) error {
	var raw string
	err := d.sql.QueryRow(`SELECT files FROM workflow_agents WHERE id = ?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("touch workflow agent: read files: %w", err)
	}
	merged := mergeFiles(raw, files)
	b, _ := json.Marshal(merged)
	_, err = d.sql.Exec(
		`UPDATE workflow_agents SET tool_calls = tool_calls + 1, last_tool = ?, files = ?, last_activity = ? WHERE id = ?`,
		tool, string(b), nowTS(), id,
	)
	if err != nil {
		return fmt.Errorf("touch workflow agent: %w", err)
	}
	return nil
}

func mergeFiles(raw string, add []string) []string {
	var cur []string
	_ = json.Unmarshal([]byte(raw), &cur)
	seen := make(map[string]bool, len(cur))
	for _, f := range cur {
		seen[f] = true
	}
	for _, f := range add {
		if f != "" && !seen[f] {
			seen[f] = true
			cur = append(cur, f)
		}
	}
	if cur == nil {
		cur = []string{}
	}
	return cur
}

// StopWorkflowAgent closes the agent with a final status and the parsed review verdict.
func (d *DB) StopWorkflowAgent(id, status string, v ReviewVerdict) error {
	ts := nowTS()
	_, err := d.sql.Exec(
		`UPDATE workflow_agents SET status = ?, ended_at = ?, last_activity = ?,
		 verdict = ?, must_fix = ?, should_fix = ?, suggestion = ? WHERE id = ?`,
		status, ts, ts, v.Verdict, v.MustFix, v.ShouldFix, v.Suggestion, id,
	)
	if err != nil {
		return fmt.Errorf("stop workflow agent: %w", err)
	}
	return nil
}

// SetWorkflowAgentUsage stores the token totals read from the agent's transcript.
func (d *DB) SetWorkflowAgentUsage(id string, tokensIn, tokensOut int) error {
	if _, err := d.sql.Exec(`UPDATE workflow_agents SET tokens_in = ?, tokens_out = ? WHERE id = ?`, tokensIn, tokensOut, id); err != nil {
		return fmt.Errorf("set workflow agent usage: %w", err)
	}
	return nil
}

// SumWorkflowAgentTokens returns the token total (in + out) per workflow.
func (d *DB) SumWorkflowAgentTokens() (map[string]int, error) {
	rows, err := d.sql.Query(`SELECT workflow_id, SUM(tokens_in + tokens_out) FROM workflow_agents GROUP BY workflow_id`)
	if err != nil {
		return nil, fmt.Errorf("sum workflow agent tokens: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan workflow token sum: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}

// GetWorkflowAgent returns one agent of a workflow, or sql.ErrNoRows.
func (d *DB) GetWorkflowAgent(workflowID, id string) (WorkflowAgent, error) {
	rows, err := d.sql.Query(`SELECT `+wfAgentCols+` FROM workflow_agents WHERE workflow_id = ? AND id = ?`, workflowID, id)
	if err != nil {
		return WorkflowAgent{}, fmt.Errorf("get workflow agent: %w", err)
	}
	defer rows.Close()
	list, err := scanWorkflowAgents(rows)
	if err != nil {
		return WorkflowAgent{}, err
	}
	if len(list) == 0 {
		return WorkflowAgent{}, sql.ErrNoRows
	}
	return list[0], nil
}

// FindWorkflowAgent looks an agent up by id alone (found is false when it does not exist).
func (d *DB) FindWorkflowAgent(id string) (WorkflowAgent, bool, error) {
	rows, err := d.sql.Query(`SELECT `+wfAgentCols+` FROM workflow_agents WHERE id = ?`, id)
	if err != nil {
		return WorkflowAgent{}, false, fmt.Errorf("find workflow agent: %w", err)
	}
	defer rows.Close()
	list, err := scanWorkflowAgents(rows)
	if err != nil || len(list) == 0 {
		return WorkflowAgent{}, false, err
	}
	return list[0], true, nil
}

// ListWorkflowAgents returns the agents of a workflow, oldest first.
func (d *DB) ListWorkflowAgents(workflowID string) ([]WorkflowAgent, error) {
	rows, err := d.sql.Query(`SELECT `+wfAgentCols+` FROM workflow_agents WHERE workflow_id = ? ORDER BY started_at, id`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("list workflow agents: %w", err)
	}
	defer rows.Close()
	return scanWorkflowAgents(rows)
}

// ListStaleWorkflowAgents returns running agents with no activity for longer than threshold.
func (d *DB) ListStaleWorkflowAgents(threshold time.Duration) ([]WorkflowAgent, error) {
	cutoff := time.Now().UTC().Add(-threshold).Format("2006-01-02T15:04:05.000Z")
	rows, err := d.sql.Query(`SELECT `+wfAgentCols+` FROM workflow_agents WHERE status = 'running' AND last_activity < ?`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list stale workflow agents: %w", err)
	}
	defer rows.Close()
	return scanWorkflowAgents(rows)
}

func scanWorkflowAgents(rows *sql.Rows) ([]WorkflowAgent, error) {
	out := []WorkflowAgent{}
	for rows.Next() {
		var a WorkflowAgent
		var task sql.NullInt64
		var files string
		if err := rows.Scan(&a.ID, &a.WorkflowID, &a.SessionID, &a.AgentType, &a.Phase, &task, &a.Status,
			&a.StartedAt, &a.EndedAt, &a.LastActivity, &a.ToolCalls, &a.LastTool, &files, &a.TranscriptPath,
			&a.Verdict, &a.MustFix, &a.ShouldFix, &a.Suggestion, &a.TokensIn, &a.TokensOut, &a.NativeAgentID, &a.TurnID); err != nil {
			return nil, fmt.Errorf("scan workflow agent: %w", err)
		}
		if task.Valid {
			n := int(task.Int64)
			a.TaskIndex = &n
		}
		if err := json.Unmarshal([]byte(files), &a.Files); err != nil || a.Files == nil {
			a.Files = []string{}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
