package orchestration

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// ErrWorkflowNotFound is returned when a workflow ID does not exist in the database.
var ErrWorkflowNotFound = errors.New("workflow not found")

// ChangeSummary holds the structural and semantic summary of changes made during a workflow.
type ChangeSummary struct {
	CapabilitiesAdded    []string `json:"capabilities_added"`
	CapabilitiesModified []string `json:"capabilities_modified"`
	CapabilitiesRemoved  []string `json:"capabilities_removed"`
	DownstreamRisks      []string `json:"downstream_risks"`
	GovernanceCompliance []string `json:"governance_compliance"`
	TestCoverageDelta    string   `json:"test_coverage_delta"`
	FilesChanged         int      `json:"files_changed"`
	LinesAdded           int      `json:"lines_added"`
	LinesRemoved         int      `json:"lines_removed"`
	GovernanceDocs       []string `json:"governance_docs_matched,omitempty"` // raw FTS matches for agent context
	VexorExcerpts        []string `json:"vexor_excerpts,omitempty"`          // raw similarity results for agent context
	GeneratedAt          string   `json:"generated_at"`
}

// WorkflowState is the full state of a workflow.
type WorkflowState struct {
	ID            string              `json:"id"`
	Type          WorkflowType        `json:"type"`
	Phase         Phase               `json:"phase"`
	Complexity    Complexity          `json:"complexity"`
	Delegated     map[string][]string `json:"delegated_agents"` // phase → agent list
	Tasks         []Task              `json:"tasks"`
	CurrentTask   *int                `json:"current_task,omitempty"`
	TotalTasks    int                 `json:"total_tasks"`
	Aborted       bool                `json:"aborted"`
	Title         string              `json:"title"`
	SessionID     string              `json:"session_id,omitempty"` // Claude Code session that owns this workflow
	PlanContent   string              `json:"plan_content,omitempty"`
	DesignContent string              `json:"design_content,omitempty"`
	BaseCommit    string              `json:"base_commit,omitempty"`    // git HEAD at workflow creation
	ChangeSummary *ChangeSummary      `json:"change_summary,omitempty"` // populated on complete
	CreatedAt     string              `json:"created_at"`
	UpdatedAt     string              `json:"updated_at"`
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
	return &Coordinator{
		db: db,
	}
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
	if err := c.save(state); err != nil {
		return nil, err
	}
	return state, nil
}

// Get retrieves a workflow by ID.
func (c *Coordinator) Get(id string) (*WorkflowState, error) {
	var stateJSON, wtype, phase, complexity string
	var createdAt, updatedAt string
	err := c.db.SQL().QueryRow(`SELECT type, phase, complexity, state_json, created_at, updated_at FROM workflows WHERE id = ?`, id).
		Scan(&wtype, &phase, &complexity, &stateJSON, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workflow %q: %w", id, ErrWorkflowNotFound)
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

func validatePhaseReadiness(state *WorkflowState, to Phase) []string {
	var warnings []string

	switch state.Type {
	case WorkflowSpec:
		warnings = append(warnings, validateSpecPhaseReadiness(state, to)...)
	case WorkflowBug:
		warnings = append(warnings, validateBugPhaseReadiness(state, to)...)
	case WorkflowE2E:
		warnings = append(warnings, validateE2EPhaseReadiness(state, to)...)
	}

	return warnings
}

func validateSpecPhaseReadiness(state *WorkflowState, to Phase) []string {
	var warnings []string

	switch state.Phase {
	case PhasePlan:
		if to == PhaseImplement {
			if len(state.Tasks) == 0 {
				warnings = append(warnings,
					"tasks not defined — use /api/workflows/<id>/tasks to set tasks before implementing")
			}
			if state.PlanContent == "" {
				warnings = append(warnings,
					"plan not defined — write plan to docs/plans/<slug>.md and set via /api/workflows/<id>/plan")
			}
		}

	case PhaseImplement:
		if to == PhaseVerify {
			var incompleteTasks []string
			for i, task := range state.Tasks {
				if task.Status != "done" {
					incompleteTasks = append(incompleteTasks,
						fmt.Sprintf("task %d: %s", i+1, task.Title))
				}
			}
			if len(incompleteTasks) > 0 {
				warnings = append(warnings,
					fmt.Sprintf("transitioning to verify with incomplete tasks: %s",
						strings.Join(incompleteTasks, ", ")))
			}
		}

	case PhaseVerify:
		if to == PhaseLearn {
			phaseDelegations := state.Delegated[string(state.Phase)]
			if phaseDelegations == nil {
				phaseDelegations = []string{}
			}
			hasReviewer := false
			for _, agent := range phaseDelegations {
				if agent == "delivery-code-reviewer" {
					hasReviewer = true
					break
				}
			}
			if !hasReviewer {
				warnings = append(warnings,
					"transitioning to learn without code review delegation")
			}
		}

	case PhaseLearn:
		if to == PhaseComplete {
			// TODO: Add validation for learning artifacts when implemented
		}
	}

	return warnings
}

func validateBugPhaseReadiness(state *WorkflowState, to Phase) []string {
	var warnings []string

	switch state.Phase {
	case PhaseAnalyze:
		if to == PhaseFix {
			if len(state.Tasks) == 0 {
				warnings = append(warnings,
					"tasks not defined — use /api/workflows/<id>/tasks to set fix tasks before transitioning to fix")
			}
		}

	case PhaseFix:
		if to == PhaseReview {
			var incompleteTasks []string
			for i, task := range state.Tasks {
				if task.Status != "done" {
					incompleteTasks = append(incompleteTasks,
						fmt.Sprintf("task %d: %s", i+1, task.Title))
				}
			}
			if len(incompleteTasks) > 0 {
				warnings = append(warnings,
					fmt.Sprintf("transitioning to review with incomplete fixes: %s",
						strings.Join(incompleteTasks, ", ")))
			}
		}

	case PhaseReview:
		if to == PhaseComplete {
			phaseDelegations := state.Delegated[string(state.Phase)]
			if phaseDelegations == nil {
				phaseDelegations = []string{}
			}
			hasReviewer := false
			for _, agent := range phaseDelegations {
				if agent == "delivery-code-reviewer" {
					hasReviewer = true
					break
				}
			}
			if !hasReviewer {
				warnings = append(warnings,
					"transitioning to complete without code review delegation")
			}
		}
	}

	return warnings
}

func validateE2EPhaseReadiness(state *WorkflowState, to Phase) []string {
	var warnings []string

	switch state.Phase {
	case PhaseSetup:
		if to == PhasePlan {
			// TODO: Validate setup completion
		}

	case PhasePlan:
		if to == PhaseGenerate {
			if state.PlanContent == "" {
				warnings = append(warnings,
					"transitioning to generate without test plan")
			}
		}

	case PhaseGenerate:
		if to == PhaseHeal {
			if len(state.Tasks) == 0 {
				warnings = append(warnings,
					"transitioning to heal with no tests generated")
			}
		}

	case PhaseHeal:
		if to == PhaseComplete {
			var incompleteTasks []string
			for i, task := range state.Tasks {
				if task.Status != "done" {
					incompleteTasks = append(incompleteTasks,
						fmt.Sprintf("test %d: %s", i+1, task.Title))
				}
			}
			if len(incompleteTasks) > 0 {
				warnings = append(warnings,
					fmt.Sprintf("transitioning to complete with failing tests: %s",
						strings.Join(incompleteTasks, ", ")))
			}
		}
	}

	return warnings
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

	for _, w := range validatePhaseReadiness(state, to) {
		log.Printf("warning: workflow %s phase transition: %s", id, w)
	}

	state.Phase = to
	if err := c.save(state); err != nil {
		return nil, err
	}

	return state, nil
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
	if err := c.save(state); err != nil {
		return nil, err
	}
	return state, nil
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
	if err := c.save(state); err != nil {
		return nil, err
	}
	return state, nil
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

// SetPlanContent stores the plan markdown content in the workflow state.
func (c *Coordinator) SetPlanContent(id, content string) (*WorkflowState, error) {
	state, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	state.PlanContent = content
	return state, c.save(state)
}

// SetDesignContent stores the design document markdown content in the workflow state.
func (c *Coordinator) SetDesignContent(id, content string) (*WorkflowState, error) {
	state, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	state.DesignContent = content
	return state, c.save(state)
}

// ListActive returns all non-completed, non-aborted workflows.
func (c *Coordinator) ListActive() ([]*WorkflowState, error) {
	rows, err := c.db.SQL().Query(`SELECT id, type, phase, complexity, state_json, created_at, updated_at FROM workflows WHERE JSON_EXTRACT(state_json, '$.aborted') IS NOT 1 AND phase != 'complete' ORDER BY updated_at DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []*WorkflowState
	for rows.Next() {
		var id, wtype, phase, complexity, stateJSON, createdAt, updatedAt string
		if err := rows.Scan(&id, &wtype, &phase, &complexity, &stateJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var state WorkflowState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			continue
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
		states = append(states, &state)
	}
	return states, rows.Err()
}

// SetSessionID records the Claude Code session ID for a workflow.
// First-writer-wins: no-op if a session ID is already set.
// Used when a workflow is first created via POST /api/workflows.
func (c *Coordinator) SetSessionID(id, sessionID string) error {
	state, err := c.Get(id)
	if err != nil {
		return err
	}
	if state.SessionID != "" {
		return nil // already captured — don't overwrite
	}
	state.SessionID = sessionID
	return c.save(state)
}

// UpdateSessionID sets (or replaces) the Claude Code session ID for a workflow.
// Unlike SetSessionID, this always overwrites the existing value.
// Used by the /resume skill via PATCH /api/workflows/{id}/session.
func (c *Coordinator) UpdateSessionID(id, sessionID string) (*WorkflowState, error) {
	state, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	state.SessionID = sessionID
	return state, c.save(state)
}

// SetBaseCommit records the git HEAD SHA at workflow creation time.
func (c *Coordinator) SetBaseCommit(id, commit string) error {
	state, err := c.Get(id)
	if err != nil {
		return err
	}
	if state.BaseCommit != "" {
		return nil // already captured
	}
	state.BaseCommit = commit
	return c.save(state)
}

// SetChangeSummary stores (or replaces) the change summary for a workflow.
// Computed fields (FilesChanged, LinesAdded, LinesRemoved, GovernanceDocs, VexorExcerpts, GeneratedAt)
// are preserved when the incoming summary has zero values for them.
func (c *Coordinator) SetChangeSummary(id string, incoming *ChangeSummary) (*WorkflowState, error) {
	state, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	if state.ChangeSummary == nil {
		state.ChangeSummary = incoming
	} else {
		// Merge: preserve computed fields, overwrite semantic fields from incoming
		existing := state.ChangeSummary
		if incoming.FilesChanged != 0 {
			existing.FilesChanged = incoming.FilesChanged
		}
		if incoming.LinesAdded != 0 {
			existing.LinesAdded = incoming.LinesAdded
		}
		if incoming.LinesRemoved != 0 {
			existing.LinesRemoved = incoming.LinesRemoved
		}
		if len(incoming.GovernanceDocs) > 0 {
			existing.GovernanceDocs = incoming.GovernanceDocs
		}
		if len(incoming.VexorExcerpts) > 0 {
			existing.VexorExcerpts = incoming.VexorExcerpts
		}
		if incoming.GeneratedAt != "" {
			existing.GeneratedAt = incoming.GeneratedAt
		}
		// Semantic fields: only overwrite when the incoming value is non-empty,
		// so the async generateChangeSummary goroutine (which starts with empty slices)
		// cannot erase data that an agent already submitted via PUT /summary.
		if len(incoming.CapabilitiesAdded) > 0 {
			existing.CapabilitiesAdded = incoming.CapabilitiesAdded
		}
		if len(incoming.CapabilitiesModified) > 0 {
			existing.CapabilitiesModified = incoming.CapabilitiesModified
		}
		if len(incoming.CapabilitiesRemoved) > 0 {
			existing.CapabilitiesRemoved = incoming.CapabilitiesRemoved
		}
		if len(incoming.DownstreamRisks) > 0 {
			existing.DownstreamRisks = incoming.DownstreamRisks
		}
		if len(incoming.GovernanceCompliance) > 0 {
			existing.GovernanceCompliance = incoming.GovernanceCompliance
		}
		if incoming.TestCoverageDelta != "" {
			existing.TestCoverageDelta = incoming.TestCoverageDelta
		}
	}
	return state, c.save(state)
}

// ListAll returns all workflows (including completed and aborted), newest first.
func (c *Coordinator) ListAll() ([]*WorkflowState, error) {
	rows, err := c.db.SQL().Query(`SELECT id, type, phase, complexity, state_json, created_at, updated_at FROM workflows ORDER BY updated_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []*WorkflowState
	for rows.Next() {
		var id, wtype, phase, complexity, stateJSON, createdAt, updatedAt string
		if err := rows.Scan(&id, &wtype, &phase, &complexity, &stateJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var state WorkflowState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			continue
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
		states = append(states, &state)
	}
	return states, rows.Err()
}

// CountPastWorkflows returns the total number of completed or aborted workflows.
func (c *Coordinator) CountPastWorkflows() (int, error) {
	var count int
	err := c.db.SQL().QueryRow(`SELECT COUNT(*) FROM workflows WHERE phase = 'complete' OR JSON_EXTRACT(state_json, '$.aborted') = 1`).Scan(&count)
	return count, err
}

// ListPastWorkflows returns completed or aborted workflows with offset/limit pagination.
func (c *Coordinator) ListPastWorkflows(offset, limit int) ([]*WorkflowState, error) {
	rows, err := c.db.SQL().Query(`SELECT id, type, phase, complexity, state_json, created_at, updated_at FROM workflows WHERE phase = 'complete' OR JSON_EXTRACT(state_json, '$.aborted') = 1 ORDER BY updated_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []*WorkflowState
	for rows.Next() {
		var id, wtype, phase, complexity, stateJSON, createdAt, updatedAt string
		if err := rows.Scan(&id, &wtype, &phase, &complexity, &stateJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var state WorkflowState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			continue
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
		states = append(states, &state)
	}
	return states, rows.Err()
}

// Delete permanently removes a workflow from the database.
func (c *Coordinator) Delete(id string) error {
	res, err := c.db.SQL().Exec(`DELETE FROM workflows WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("workflow %q: %w", id, ErrWorkflowNotFound)
	}
	return nil
}

// WorkflowHistorySummary holds aggregated historical data used for risk scoring.
type WorkflowHistorySummary struct {
	TotalCompleted   int
	AbortedCount     int
	AbortRate        float64
	AvgDurationMin   float64
	SimilarWorkflows []SimilarWorkflow
}

// SimilarWorkflow is a past workflow used as a reference in risk analysis.
type SimilarWorkflow struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Complexity  string `json:"complexity"`
	DurationMin int    `json:"duration_min"`
	Aborted     bool   `json:"aborted"`
}

// WorkflowHistory returns aggregate stats and similar past workflows for risk scoring.
// wtype filters similar workflows by type; pass empty string to skip similarity lookup.
func (c *Coordinator) WorkflowHistory(wtype string) (*WorkflowHistorySummary, error) {
	var summary WorkflowHistorySummary

	// Aggregate stats filtered by type (or all types if wtype is empty).
	// COALESCE on SUM prevents NULL scan errors when the table is empty.
	row := c.db.SQL().QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN JSON_EXTRACT(state_json, '$.aborted') = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(CAST((julianday(updated_at) - julianday(created_at)) * 1440 AS INTEGER)), 0)
		FROM workflows
		WHERE (phase = 'complete' OR JSON_EXTRACT(state_json, '$.aborted') = 1)
		  AND (? = '' OR type = ?)`, wtype, wtype)
	var total, aborted int
	var avgDur float64
	if err := row.Scan(&total, &aborted, &avgDur); err != nil {
		return nil, fmt.Errorf("workflow history stats: %w", err)
	}
	summary.TotalCompleted = total
	summary.AbortedCount = aborted
	summary.AvgDurationMin = avgDur
	if total > 0 {
		summary.AbortRate = float64(aborted) / float64(total)
	}

	// Similar past workflows by type
	if wtype != "" {
		rows, err := c.db.SQL().Query(`
			SELECT
				id,
				type,
				complexity,
				COALESCE(JSON_EXTRACT(state_json, '$.title'), ''),
				COALESCE(JSON_EXTRACT(state_json, '$.aborted'), 0),
				CAST((julianday(updated_at) - julianday(created_at)) * 1440 AS INTEGER)
			FROM workflows
			WHERE type = ?
			  AND (phase = 'complete' OR JSON_EXTRACT(state_json, '$.aborted') = 1)
			ORDER BY updated_at DESC
			LIMIT 5`, wtype)
		if err != nil {
			return &summary, nil // non-fatal
		}
		defer rows.Close()
		for rows.Next() {
			var sw SimilarWorkflow
			var abortedFlag int
			if err := rows.Scan(&sw.ID, &sw.Type, &sw.Complexity, &sw.Title, &abortedFlag, &sw.DurationMin); err != nil {
				continue
			}
			sw.Aborted = abortedFlag == 1
			summary.SimilarWorkflows = append(summary.SimilarWorkflows, sw)
		}
		_ = rows.Err() // non-fatal; similar workflows are best-effort
	}

	return &summary, nil
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

// SetEventBus is a no-op stub so the Insight event bus can be wired in without
// requiring the orchestration package to import insight/events.
func (c *Coordinator) SetEventBus(_ interface{}) {}
