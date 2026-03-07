package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

type TrajectoryStep struct {
	StepNumber    int            `json:"step"`
	AgentName     string         `json:"agent"`
	ActionType    string         `json:"action"`
	Phase         string         `json:"phase,omitempty"`
	InputContext  string         `json:"input,omitempty"`
	OutputSummary string         `json:"output,omitempty"`
	Success       bool           `json:"success"`
	DurationMs    int64          `json:"duration_ms,omitempty"`
	Timestamp     time.Time      `json:"timestamp"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type Trajectory struct {
	ID           string           `json:"id"`
	WorkflowID   string           `json:"workflow_id"`
	TaskType     string           `json:"task_type"`
	RepoType     string           `json:"repo_type"`
	WorkflowType string           `json:"workflow_type"`
	Steps        []TrajectoryStep `json:"steps"`
	StepCount    int              `json:"step_count"`
	FinalResult  string           `json:"final_result"`
	CycleTimeMin int              `json:"cycle_time_minutes"`
	StartedAt    time.Time        `json:"started_at"`
	CompletedAt  *time.Time       `json:"completed_at,omitempty"`
	CreatedAt    string           `json:"created_at"`
}

type TrajectoryPattern struct {
	ID                   string   `json:"id"`
	ProblemType          string   `json:"problem_type"`
	RepoType             string   `json:"repo_type"`
	OptimalAgentSequence []string `json:"optimal_agent_sequence"`
	SuccessRate          float64  `json:"success_rate"`
	OccurrenceCount      int      `json:"occurrence_count"`
	AvgCycleTimeMin      int      `json:"avg_cycle_time_minutes"`
	ExampleTrajectoryIDs []string `json:"example_trajectory_ids"`
	Confidence           float64  `json:"confidence"`
	CreatedAt            string   `json:"created_at"`
	UpdatedAt            string   `json:"updated_at"`
}

type TrajectoryFilters struct {
	TaskType     string
	RepoType     string
	WorkflowType string
	FinalResult  string
	Limit        int
	Offset       int
}

type AgentSequenceStats struct {
	Sequence    []string `json:"sequence"`
	SuccessRate float64  `json:"success_rate"`
	Count       int      `json:"count"`
	AvgDuration int      `json:"avg_duration_minutes"`
}

func (d *DB) SaveTrajectory(t *Trajectory) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}

	stepsBytes, err := json.Marshal(t.Steps)
	if err != nil {
		return fmt.Errorf("marshal steps: %w", err)
	}
	if t.Steps == nil {
		stepsBytes = []byte("[]")
	}

	t.StepCount = len(t.Steps)

	var completedAt sql.NullString
	if t.CompletedAt != nil {
		completedAt = sql.NullString{String: t.CompletedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.sql.Exec(`
		INSERT INTO openclaw_trajectories 
		(id, workflow_id, task_type, repo_type, workflow_type, steps_json, step_count,
		 final_result, cycle_time_minutes, started_at, completed_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			task_type = excluded.task_type,
			repo_type = excluded.repo_type,
			workflow_type = excluded.workflow_type,
			steps_json = excluded.steps_json,
			step_count = excluded.step_count,
			final_result = excluded.final_result,
			cycle_time_minutes = excluded.cycle_time_minutes,
			completed_at = excluded.completed_at
	`,
		t.ID, t.WorkflowID, t.TaskType, t.RepoType, t.WorkflowType,
		string(stepsBytes), t.StepCount, t.FinalResult, t.CycleTimeMin,
		t.StartedAt.UTC().Format(time.RFC3339Nano), completedAt, now,
	)

	return err
}

func (d *DB) GetTrajectoryByID(id string) (*Trajectory, error) {
	row := d.sql.QueryRow(`
		SELECT id, workflow_id, task_type, repo_type, workflow_type, steps_json, step_count,
		       final_result, cycle_time_minutes, started_at, completed_at, created_at
		FROM openclaw_trajectories
		WHERE id = ?
	`, id)

	return scanTrajectory(row)
}

func (d *DB) GetTrajectoryByWorkflowID(workflowID string) (*Trajectory, error) {
	row := d.sql.QueryRow(`
		SELECT id, workflow_id, task_type, repo_type, workflow_type, steps_json, step_count,
		       final_result, cycle_time_minutes, started_at, completed_at, created_at
		FROM openclaw_trajectories
		WHERE workflow_id = ?
	`, workflowID)

	return scanTrajectory(row)
}

func (d *DB) ListTrajectories(filters TrajectoryFilters) ([]Trajectory, error) {
	query := `SELECT id, workflow_id, task_type, repo_type, workflow_type, steps_json, step_count,
	          final_result, cycle_time_minutes, started_at, completed_at, created_at
	          FROM openclaw_trajectories WHERE 1=1`
	args := []any{}

	if filters.TaskType != "" {
		query += " AND task_type = ?"
		args = append(args, filters.TaskType)
	}
	if filters.RepoType != "" {
		query += " AND repo_type = ?"
		args = append(args, filters.RepoType)
	}
	if filters.WorkflowType != "" {
		query += " AND workflow_type = ?"
		args = append(args, filters.WorkflowType)
	}
	if filters.FinalResult != "" {
		query += " AND final_result = ?"
		args = append(args, filters.FinalResult)
	}

	query += " ORDER BY created_at DESC"

	limit := filters.Limit
	if limit <= 0 {
		limit = 100
	}
	query += " LIMIT ? OFFSET ?"
	args = append(args, limit, filters.Offset)

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list trajectories: %w", err)
	}
	defer rows.Close()

	return scanTrajectories(rows)
}

func (d *DB) GetTrajectoriesInTimeRange(start, end time.Time, limit int) ([]Trajectory, error) {
	if limit <= 0 {
		limit = 500
	}

	rows, err := d.sql.Query(`
		SELECT id, workflow_id, task_type, repo_type, workflow_type, steps_json, step_count,
		       final_result, cycle_time_minutes, started_at, completed_at, created_at
		FROM openclaw_trajectories
		WHERE started_at >= ? AND started_at <= ?
		ORDER BY started_at DESC
		LIMIT ?
	`, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, fmt.Errorf("get trajectories in time range: %w", err)
	}
	defer rows.Close()

	return scanTrajectories(rows)
}

func (d *DB) DeleteTrajectory(id string) error {
	_, err := d.sql.Exec("DELETE FROM openclaw_trajectories WHERE id = ?", id)
	return err
}

func (d *DB) SaveTrajectoryPattern(p *TrajectoryPattern) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}

	seqBytes, err := json.Marshal(p.OptimalAgentSequence)
	if err != nil {
		return fmt.Errorf("marshal sequence: %w", err)
	}
	if p.OptimalAgentSequence == nil {
		seqBytes = []byte("[]")
	}

	exampleBytes, err := json.Marshal(p.ExampleTrajectoryIDs)
	if err != nil {
		return fmt.Errorf("marshal examples: %w", err)
	}
	if p.ExampleTrajectoryIDs == nil {
		exampleBytes = []byte("[]")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.sql.Exec(`
		INSERT INTO openclaw_trajectory_patterns 
		(id, problem_type, repo_type, optimal_agent_sequence_json, success_rate,
		 occurrence_count, avg_cycle_time_minutes, example_trajectory_ids_json,
		 confidence, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(problem_type, repo_type) DO UPDATE SET
			optimal_agent_sequence_json = excluded.optimal_agent_sequence_json,
			success_rate = excluded.success_rate,
			occurrence_count = excluded.occurrence_count,
			avg_cycle_time_minutes = excluded.avg_cycle_time_minutes,
			example_trajectory_ids_json = excluded.example_trajectory_ids_json,
			confidence = excluded.confidence,
			updated_at = excluded.updated_at
	`,
		p.ID, p.ProblemType, p.RepoType, string(seqBytes), p.SuccessRate,
		p.OccurrenceCount, p.AvgCycleTimeMin, string(exampleBytes),
		p.Confidence, now, now,
	)

	return err
}

func (d *DB) GetTrajectoryPatternByID(id string) (*TrajectoryPattern, error) {
	row := d.sql.QueryRow(`
		SELECT id, problem_type, repo_type, optimal_agent_sequence_json, success_rate,
		       occurrence_count, avg_cycle_time_minutes, example_trajectory_ids_json,
		       confidence, created_at, updated_at
		FROM openclaw_trajectory_patterns
		WHERE id = ?
	`, id)

	return scanTrajectoryPattern(row)
}

func (d *DB) GetTrajectoryPatternsByProblemType(problemType, repoType string) ([]TrajectoryPattern, error) {
	query := `SELECT id, problem_type, repo_type, optimal_agent_sequence_json, success_rate,
	          occurrence_count, avg_cycle_time_minutes, example_trajectory_ids_json,
	          confidence, created_at, updated_at
	          FROM openclaw_trajectory_patterns WHERE problem_type = ?`
	args := []any{problemType}

	if repoType != "" {
		query += " AND repo_type = ?"
		args = append(args, repoType)
	}

	query += " ORDER BY success_rate DESC, confidence DESC LIMIT 10"

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get trajectory patterns: %w", err)
	}
	defer rows.Close()

	return scanTrajectoryPatterns(rows)
}

func (d *DB) ListTrajectoryPatterns(limit int) ([]TrajectoryPattern, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := d.sql.Query(`
		SELECT id, problem_type, repo_type, optimal_agent_sequence_json, success_rate,
		       occurrence_count, avg_cycle_time_minutes, example_trajectory_ids_json,
		       confidence, created_at, updated_at
		FROM openclaw_trajectory_patterns
		ORDER BY confidence DESC, success_rate DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list trajectory patterns: %w", err)
	}
	defer rows.Close()

	return scanTrajectoryPatterns(rows)
}

func (d *DB) DeleteTrajectoryPattern(id string) error {
	_, err := d.sql.Exec("DELETE FROM openclaw_trajectory_patterns WHERE id = ?", id)
	return err
}

func (d *DB) CountTrajectoriesByResult(taskType string) (success, failure int, err error) {
	rows, err := d.sql.Query(`
		SELECT final_result, COUNT(*) 
		FROM openclaw_trajectories 
		WHERE task_type = ?
		GROUP BY final_result
	`, taskType)
	if err != nil {
		return 0, 0, fmt.Errorf("count trajectories by result: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var result string
		var count int
		if err := rows.Scan(&result, &count); err != nil {
			return 0, 0, err
		}
		switch result {
		case "success":
			success = count
		case "failure":
			failure = count
		}
	}

	return success, failure, nil
}

func (d *DB) GetAgentSequenceStats(taskType string) ([]AgentSequenceStats, error) {
	trajectories, err := d.ListTrajectories(TrajectoryFilters{TaskType: taskType, Limit: 500})
	if err != nil {
		return nil, err
	}

	sequenceMap := make(map[string]*AgentSequenceStats)

	for _, t := range trajectories {
		if len(t.Steps) == 0 {
			continue
		}

		seq := make([]string, 0, len(t.Steps))
		for _, step := range t.Steps {
			if step.AgentName != "" {
				seq = append(seq, step.AgentName)
			}
		}

		if len(seq) == 0 {
			continue
		}

		seqKey := fmt.Sprintf("%v", seq)
		stats, exists := sequenceMap[seqKey]
		if !exists {
			stats = &AgentSequenceStats{
				Sequence: seq,
				Count:    0,
			}
			sequenceMap[seqKey] = stats
		}
		stats.Count++
		if t.FinalResult == "success" {
			stats.SuccessRate += 1
		}
		stats.AvgDuration += t.CycleTimeMin
	}

	results := make([]AgentSequenceStats, 0, len(sequenceMap))
	for _, stats := range sequenceMap {
		stats.SuccessRate = stats.SuccessRate / float64(stats.Count)
		stats.AvgDuration = stats.AvgDuration / stats.Count
		results = append(results, *stats)
	}

	return results, nil
}

func scanTrajectory(row *sql.Row) (*Trajectory, error) {
	var t Trajectory
	var stepsJSON string
	var completedAt sql.NullString

	err := row.Scan(
		&t.ID, &t.WorkflowID, &t.TaskType, &t.RepoType, &t.WorkflowType,
		&stepsJSON, &t.StepCount, &t.FinalResult, &t.CycleTimeMin,
		&t.StartedAt, &completedAt, &t.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan trajectory: %w", err)
	}

	if stepsJSON != "" {
		if err := json.Unmarshal([]byte(stepsJSON), &t.Steps); err != nil {
			return nil, fmt.Errorf("unmarshal steps: %w", err)
		}
	}

	if completedAt.Valid {
		pt, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err == nil {
			t.CompletedAt = &pt
		} else {
			slog.Debug("failed to parse completed_at timestamp", "value", completedAt.String, "error", err)
		}
	}

	return &t, nil
}

func scanTrajectories(rows *sql.Rows) ([]Trajectory, error) {
	var trajectories []Trajectory
	for rows.Next() {
		var t Trajectory
		var stepsJSON string
		var completedAt sql.NullString

		err := rows.Scan(
			&t.ID, &t.WorkflowID, &t.TaskType, &t.RepoType, &t.WorkflowType,
			&stepsJSON, &t.StepCount, &t.FinalResult, &t.CycleTimeMin,
			&t.StartedAt, &completedAt, &t.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan trajectory row: %w", err)
		}

		if stepsJSON != "" {
			if err := json.Unmarshal([]byte(stepsJSON), &t.Steps); err != nil {
				return nil, fmt.Errorf("unmarshal steps: %w", err)
			}
		}

		if completedAt.Valid {
			pt, err := time.Parse(time.RFC3339Nano, completedAt.String)
			if err == nil {
				t.CompletedAt = &pt
			} else {
				slog.Debug("failed to parse completed_at timestamp", "value", completedAt.String, "error", err)
			}
		}

		trajectories = append(trajectories, t)
	}

	if trajectories == nil {
		trajectories = []Trajectory{}
	}

	return trajectories, nil
}

func scanTrajectoryPattern(row *sql.Row) (*TrajectoryPattern, error) {
	var p TrajectoryPattern
	var seqJSON, exampleJSON string

	err := row.Scan(
		&p.ID, &p.ProblemType, &p.RepoType, &seqJSON, &p.SuccessRate,
		&p.OccurrenceCount, &p.AvgCycleTimeMin, &exampleJSON,
		&p.Confidence, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan trajectory pattern: %w", err)
	}

	if seqJSON != "" {
		if err := json.Unmarshal([]byte(seqJSON), &p.OptimalAgentSequence); err != nil {
			return nil, fmt.Errorf("unmarshal sequence: %w", err)
		}
	}

	if exampleJSON != "" {
		if err := json.Unmarshal([]byte(exampleJSON), &p.ExampleTrajectoryIDs); err != nil {
			return nil, fmt.Errorf("unmarshal examples: %w", err)
		}
	}

	return &p, nil
}

func scanTrajectoryPatterns(rows *sql.Rows) ([]TrajectoryPattern, error) {
	var patterns []TrajectoryPattern
	for rows.Next() {
		var p TrajectoryPattern
		var seqJSON, exampleJSON string

		err := rows.Scan(
			&p.ID, &p.ProblemType, &p.RepoType, &seqJSON, &p.SuccessRate,
			&p.OccurrenceCount, &p.AvgCycleTimeMin, &exampleJSON,
			&p.Confidence, &p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan trajectory pattern row: %w", err)
		}

		if seqJSON != "" {
			if err := json.Unmarshal([]byte(seqJSON), &p.OptimalAgentSequence); err != nil {
				return nil, fmt.Errorf("unmarshal sequence: %w", err)
			}
		}

		if exampleJSON != "" {
			if err := json.Unmarshal([]byte(exampleJSON), &p.ExampleTrajectoryIDs); err != nil {
				return nil, fmt.Errorf("unmarshal examples: %w", err)
			}
		}

		patterns = append(patterns, p)
	}

	if patterns == nil {
		patterns = []TrajectoryPattern{}
	}

	return patterns, nil
}
