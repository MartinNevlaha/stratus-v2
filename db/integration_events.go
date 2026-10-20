package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/MartinNevlaha/stratus-v2/integrations"
	"time"
)

type IntegrationActivity struct {
	Verdict                                           ReviewVerdict
	Event                                             integrations.Event
	EventKey, SessionKey, AgentKey, WorkflowID, Phase string
	TaskIndex                                         *int
}
type IntegrationResult struct {
	Recorded, Duplicate bool
	AgentID             string
	DetailEventID       int64
}

func (d *DB) ApplyIntegrationActivity(ctx context.Context, in IntegrationActivity) (out IntegrationResult, err error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if in.EventKey != "" {
		r, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO integration_event_receipts(runtime,event_key) VALUES('codex',?)`, in.EventKey)
		if e != nil {
			return out, e
		}
		n, _ := r.RowsAffected()
		if n == 0 {
			out.Duplicate = true
			return out, tx.Commit()
		}
	} else {
		if _, e := tx.ExecContext(ctx, `UPDATE integration_event_receipts SET event_key=event_key WHERE 0`); e != nil {
			return out, e
		}
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT state_json FROM workflows WHERE id=?`, in.WorkflowID).Scan(&raw); err != nil {
		return out, err
	}
	var owner struct {
		Runtime string `json:"runtime"`
		Session string `json:"session_id"`
	}
	if e := json.Unmarshal([]byte(raw), &owner); e != nil {
		return out, e
	}
	if owner.Runtime != "codex" || owner.Session != in.SessionKey || in.SessionKey != "codex:"+in.Event.NativeSessionID {
		return out, fmt.Errorf("workflow runtime ownership conflict")
	}
	ts := nowTS()
	kind := in.Event.Kind
	id := in.AgentKey
	if id != "" {
		var existingWorkflow string
		e := tx.QueryRowContext(ctx, `SELECT workflow_id FROM workflow_agents WHERE id=?`, id).Scan(&existingWorkflow)
		if e != nil && e != sql.ErrNoRows {
			return out, e
		}
		if existingWorkflow != "" && existingWorkflow != in.WorkflowID {
			return out, fmt.Errorf("native run already belongs to another workflow")
		}
	}
	if in.Event.NativeAgentID != "" {
		var role string
		e := tx.QueryRowContext(ctx, `SELECT delivery_role FROM codex_agent_roles WHERE workflow_id=? AND session_id=? AND native_agent_id=?`, in.WorkflowID, in.SessionKey, in.Event.NativeAgentID).Scan(&role)
		if e != nil && e != sql.ErrNoRows {
			return out, e
		}
		if kind == "agent_role" {
			var other string
			e := tx.QueryRowContext(ctx, `SELECT workflow_id FROM codex_agent_roles WHERE session_id=? AND native_agent_id=? AND workflow_id!=? LIMIT 1`, in.SessionKey, in.Event.NativeAgentID, in.WorkflowID).Scan(&other)
			if e != nil && e != sql.ErrNoRows {
				return out, e
			}
			if other != "" {
				return out, fmt.Errorf("native worker already belongs to another workflow")
			}
			if role != "" && role != in.Event.DeliveryRole {
				return out, fmt.Errorf("conflicting explicit agent role")
			}
			role = in.Event.DeliveryRole
			if _, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO codex_agent_roles(workflow_id,session_id,native_agent_id,delivery_role) VALUES(?,?,?,?)`, in.WorkflowID, in.SessionKey, in.Event.NativeAgentID, role); e != nil {
				return out, e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE workflow_agents SET agent_type=?, native_agent_id=? WHERE workflow_id=? AND session_id=? AND (native_agent_id=? OR id=?)`, role, in.Event.NativeAgentID, in.WorkflowID, in.SessionKey, in.Event.NativeAgentID, id); e != nil {
				return out, e
			}
		}
		if role != "" {
			in.Event.AgentType = role
		}
	}
	if id != "" {
		status := "running"
		if kind == "agent_stop" || kind == "turn_stop" {
			status = "done"
		}
		if kind == "agent_start" || kind == "agent_stop" || in.Event.MainAgent || kind == "tool" && in.Event.NativeAgentID != "" && in.Event.TurnID != "" && in.EventKey != "" {
			_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO workflow_agents(id,workflow_id,session_id,agent_type,phase,task_index,status,started_at,last_activity,ended_at,verdict,must_fix,should_fix,suggestion,native_agent_id,turn_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, in.WorkflowID, in.SessionKey, in.Event.AgentType, in.Phase, in.TaskIndex, status, ts, ts, func() string {
				if status == "done" {
					return ts
				}
				return ""
			}(), in.Verdict.Verdict, in.Verdict.MustFix, in.Verdict.ShouldFix, in.Verdict.Suggestion, in.Event.NativeAgentID, in.Event.TurnID)
			if err != nil {
				return out, err
			}
		}
		if kind == "tool" {
			var files string
			if e := tx.QueryRowContext(ctx, `SELECT files FROM workflow_agents WHERE id=? AND workflow_id=?`, id, in.WorkflowID).Scan(&files); e == nil {
				merged := mergeFiles(files, in.Event.Files)
				b, _ := json.Marshal(merged)
				count := 0
				if in.EventKey != "" {
					count = 1
				}
				_, err = tx.ExecContext(ctx, `UPDATE workflow_agents SET tool_calls=tool_calls+?,last_tool=?,last_activity=?,files=? WHERE id=? AND workflow_id=?`, count, in.Event.ToolName, ts, string(b), id, in.WorkflowID)
				if err != nil {
					return out, err
				}
			}
		}
		if kind == "agent_stop" || kind == "turn_stop" {
			_, err = tx.ExecContext(ctx, `UPDATE workflow_agents SET status='done',ended_at=?,last_activity=?,verdict=?,must_fix=?,should_fix=?,suggestion=? WHERE id=? AND workflow_id=? AND status='running'`, ts, ts, in.Verdict.Verdict, in.Verdict.MustFix, in.Verdict.ShouldFix, in.Verdict.Suggestion, id, in.WorkflowID)
			if err != nil {
				return out, err
			}
		}
	}
	if kind == "tool" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO workflow_logs(workflow_id,session_id,ts,tool_name,summary,created_ms) VALUES(?,?,?,?,?,?)`, in.WorkflowID, in.SessionKey, ts, in.Event.ToolName, in.Event.Summary, time.Now().UnixMilli()); err != nil {
			return out, err
		}
	}
	text, _ := json.Marshal(in.Event)
	refs, _ := json.Marshal(map[string]string{"runtime": "codex", "workflow_id": in.WorkflowID, "agent_id": id, "native_agent_id": in.Event.NativeAgentID, "native_session_id": in.Event.NativeSessionID})
	r, err := tx.ExecContext(ctx, `INSERT INTO events(actor,scope,type,text,title,tags,refs,importance,session_id) VALUES('hook','repo','codex_activity',?,?,'[]',?,0.1,?)`, string(text), kind, string(refs), in.SessionKey)
	if err != nil {
		return out, err
	}
	out.DetailEventID, _ = r.LastInsertId()
	out.AgentID = id
	out.Recorded = true
	return out, tx.Commit()
}
func (d *DB) ListIntegrationSteps(ctx context.Context, wf, agent string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 40
	}
	rows, e := d.sql.QueryContext(ctx, `SELECT id,ts,actor,scope,type,text,title,tags,refs,ttl,importance,dedupe_key,project,session_id,created_ms FROM events WHERE type='codex_activity' AND json_extract(refs,'$.workflow_id')=? AND json_extract(refs,'$.agent_id')=? ORDER BY id DESC LIMIT ?`, wf, agent, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items, e := scanEvents(rows)
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, e
}

// CodexAgentRole exposes only bindings whose persisted workflow still has that owner.
func (d *DB) CodexAgentRole(session, agent, workflow string) (role, id string, err error) {
	rows, err := d.sql.Query(`SELECT r.delivery_role,r.workflow_id FROM codex_agent_roles r JOIN workflows w ON w.id=r.workflow_id WHERE r.session_id=? AND r.native_agent_id=? AND (?='' OR r.workflow_id=?) AND json_extract(w.state_json,'$.runtime')='codex' AND json_extract(w.state_json,'$.session_id')=r.session_id`, session, agent, workflow, workflow)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	for rows.Next() {
		if id != "" {
			return "", "", fmt.Errorf("ambiguous agent role binding")
		}
		if err = rows.Scan(&role, &id); err != nil {
			return "", "", err
		}
	}
	return role, id, rows.Err()
}
