package orchestration

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// WorkflowState is the full state of a workflow.
type WorkflowState struct {
	ID         string            `json:"id"`
	Type       WorkflowType      `json:"type"`
	Phase      Phase             `json:"phase"`
	Complexity Complexity        `json:"complexity"`
	Delegated  map[string][]string `json:"delegated_agents"` // phase → agent list
	Tasks      []Task            `json:"tasks"`
	CurrentTask *int             `json:"current_task,omitempty"`
	TotalTasks  int              `json:"total_tasks"`
	Aborted    bool              `json:"aborted"`
	Title      string            `json:"title"`
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at"`
}

// Task is a single work item within a workflow.
type Task struct {
	Index  int    `json:"index"`
	Title  string `json:"title"`
	Status string `json:"status"` // pending | in_progress | done
}

// Coordinator manages workflow state persistence.
type Coordinator struct {
	db *db.DB
}

// NewCoordinator creates a new coordinator.
func NewCoordinator(db *db.DB) *Coordinator {
	return &Coordinator{db: db}
}

// Start creates a new workflow or returns an existing one with the same ID.
func (c *Coordinator) Start(id string, wtype WorkflowType, complexity Complexity, title string) (*WorkflowState, error) {
	existing, err := c.Get(id)
	if err == nil {
		return existing, nil
	}

	state := &WorkflowState{
		ID:         id,
		Type:       wtype,
		Phase:      InitialPhase(wtype),
		Complexity: complexity,
		Delegated:  map[string][]string{},
		Tasks:      []Task{},
		Title:      title,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	return state, c.save(state)
}

// Get retrieves a workflow by ID.
func (c *Coordinator) Get(id string) (*WorkflowState, error) {
	var stateJSON, wtype, phase, complexity string
	var createdAt, updatedAt string
	err := c.db.SQL().QueryRow(`SELECT type, phase, complexity, state_json, created_at, updated_at FROM workflows WHERE id = ?`, id).
		Scan(&wtype, &phase, &complexity, &stateJSON, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}

	var state WorkflowState
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, fmt.Errorf("decode workflow state: %w", err)
	}
	state.ID = id
	state.Type = WorkflowType(wtype)
	state.Phase = Phase(phase)
	state.Complexity = Complexity(complexity)
	state.CreatedAt = createdAt
	state.UpdatedAt = updatedAt
	if state.Delegated == nil {
		state.Delegated = map[string][]string{}
	}
	if state.Tasks == nil {
		state.Tasks = []Task{}
	}
	return &state, nil
}

// Transition moves a workflow to a new phase.
func (c *Coordinator) Transition(id string, to Phase) (*WorkflowState, error) {
	state, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	if err := ValidateTransition(state.Type, state.Phase, to); err != nil {
		return nil, err
	}
	state.Phase = to
	return state, c.save(state)
}

// RecordDelegation records an agent delegation for the current phase.
func (c *Coordinator) RecordDelegation(id, agentID string) (*WorkflowState, error) {
	state, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	phase := string(state.Phase)
	for _, a := range state.Delegated[phase] {
		if a == agentID {
			return state, nil // already recorded
		}
	}
	state.Delegated[phase] = append(state.Delegated[phase], agentID)
	return state, c.save(state)
}

// SetTasks sets the task list for a workflow.
func (c *Coordinator) SetTasks(id string, titles []string) (*WorkflowState, error) {
	state, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	state.Tasks = make([]Task, len(titles))
	for i, t := range titles {
		state.Tasks[i] = Task{Index: i, Title: t, Status: "pending"}
	}
	state.TotalTasks = len(titles)
	return state, c.save(state)
}

// StartTask marks a task as in_progress.
func (c *Coordinator) StartTask(id string, index int) (*WorkflowState, error) {
	state, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(state.Tasks) {
		return nil, fmt.Errorf("task index %d out of range (total: %d)", index, len(state.Tasks))
	}
	state.Tasks[index].Status = "in_progress"
	state.CurrentTask = &index
	return state, c.save(state)
}

// CompleteTask marks a task as done.
func (c *Coordinator) CompleteTask(id string, index int) (*WorkflowState, error) {
	state, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(state.Tasks) {
		return nil, fmt.Errorf("task index %d out of range", index)
	}
	state.Tasks[index].Status = "done"
	state.CurrentTask = nil
	return state, c.save(state)
}

// Abort marks a workflow as aborted.
func (c *Coordinator) Abort(id string) (*WorkflowState, error) {
	state, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	state.Aborted = true
	return state, c.save(state)
}

// ListActive returns all non-completed, non-aborted workflows.
func (c *Coordinator) ListActive() ([]*WorkflowState, error) {
	rows, err := c.db.SQL().Query(`SELECT id FROM workflows WHERE JSON_EXTRACT(state_json, '$.aborted') IS NOT 1 AND phase != 'complete' ORDER BY updated_at DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []*WorkflowState
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		s, err := c.Get(id)
		if err == nil {
			states = append(states, s)
		}
	}
	return states, rows.Err()
}

func (c *Coordinator) save(state *WorkflowState) error {
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode workflow state: %w", err)
	}
	_, err = c.db.SQL().Exec(`
		INSERT INTO workflows (id, type, phase, complexity, state_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			phase = excluded.phase,
			complexity = excluded.complexity,
			state_json = excluded.state_json,
			updated_at = excluded.updated_at`,
		state.ID, state.Type, state.Phase, state.Complexity, string(data), state.CreatedAt, state.UpdatedAt,
	)
	return err
}
