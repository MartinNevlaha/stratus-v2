package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

type Artifact struct {
	ID              string         `json:"id"`
	WorkflowID      string         `json:"workflow_id"`
	TaskType        string         `json:"task_type"`
	WorkflowType    string         `json:"workflow_type"`
	RepoType        string         `json:"repo_type"`
	ProblemClass    string         `json:"problem_class"`
	AgentsUsed      []string       `json:"agents_used"`
	RootCause       string         `json:"root_cause"`
	SolutionPattern string         `json:"solution_pattern"`
	FilesChanged    []string       `json:"files_changed"`
	ReviewResult    string         `json:"review_result"`
	CycleTimeMin    int            `json:"cycle_time_minutes"`
	Success         bool           `json:"success"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       string         `json:"created_at"`
}

type ArtifactFilters struct {
	WorkflowType string
	ProblemClass string
	RepoType     string
	Success      *bool
	Limit        int
	Offset       int
}

func (d *DB) SaveArtifact(artifact *Artifact) error {
	agentsBytes, err := json.Marshal(artifact.AgentsUsed)
	if err != nil {
		return fmt.Errorf("marshal agents: %w", err)
	}
	filesBytes, err := json.Marshal(artifact.FilesChanged)
	if err != nil {
		return fmt.Errorf("marshal files: %w", err)
	}
	metadataBytes, err := json.Marshal(artifact.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	if artifact.AgentsUsed == nil {
		agentsBytes = []byte("[]")
	}
	if artifact.FilesChanged == nil {
		filesBytes = []byte("[]")
	}
	if artifact.Metadata == nil {
		metadataBytes = []byte("{}")
	}

	successInt := 0
	if artifact.Success {
		successInt = 1
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.sql.Exec(`
		INSERT INTO insight_artifacts 
		(id, workflow_id, task_type, workflow_type, repo_type, problem_class,
		 agents_used_json, root_cause, solution_pattern, files_changed_json,
		 review_result, cycle_time_minutes, success, metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			task_type = excluded.task_type,
			workflow_type = excluded.workflow_type,
			repo_type = excluded.repo_type,
			problem_class = excluded.problem_class,
			agents_used_json = excluded.agents_used_json,
			root_cause = excluded.root_cause,
			solution_pattern = excluded.solution_pattern,
			files_changed_json = excluded.files_changed_json,
			review_result = excluded.review_result,
			cycle_time_minutes = excluded.cycle_time_minutes,
			success = excluded.success,
			metadata_json = excluded.metadata_json
	`,
		artifact.ID, artifact.WorkflowID, artifact.TaskType, artifact.WorkflowType,
		artifact.RepoType, artifact.ProblemClass, string(agentsBytes), artifact.RootCause,
		artifact.SolutionPattern, string(filesBytes), artifact.ReviewResult,
		artifact.CycleTimeMin, successInt, string(metadataBytes), now,
	)

	return err
}

func (d *DB) GetArtifactByID(id string) (*Artifact, error) {
	row := d.sql.QueryRow(`
		SELECT id, workflow_id, task_type, workflow_type, repo_type, problem_class,
		       agents_used_json, root_cause, solution_pattern, files_changed_json,
		       review_result, cycle_time_minutes, success, metadata_json, created_at
		FROM insight_artifacts
		WHERE id = ?
	`, id)

	return scanArtifact(row)
}

func (d *DB) GetArtifactByWorkflowID(workflowID string) (*Artifact, error) {
	row := d.sql.QueryRow(`
		SELECT id, workflow_id, task_type, workflow_type, repo_type, problem_class,
		       agents_used_json, root_cause, solution_pattern, files_changed_json,
		       review_result, cycle_time_minutes, success, metadata_json, created_at
		FROM insight_artifacts
		WHERE workflow_id = ?
	`, workflowID)

	return scanArtifact(row)
}

func (d *DB) ListArtifacts(filters ArtifactFilters) ([]Artifact, error) {
	if filters.Limit <= 0 {
		filters.Limit = 100
	}
	if filters.Offset < 0 {
		filters.Offset = 0
	}

	query := `
		SELECT id, workflow_id, task_type, workflow_type, repo_type, problem_class,
		       agents_used_json, root_cause, solution_pattern, files_changed_json,
		       review_result, cycle_time_minutes, success, metadata_json, created_at
		FROM insight_artifacts
		WHERE 1=1
	`
	args := []any{}

	if filters.WorkflowType != "" {
		query += " AND workflow_type = ?"
		args = append(args, filters.WorkflowType)
	}
	if filters.ProblemClass != "" {
		query += " AND problem_class = ?"
		args = append(args, filters.ProblemClass)
	}
	if filters.RepoType != "" {
		query += " AND repo_type = ?"
		args = append(args, filters.RepoType)
	}
	if filters.Success != nil {
		successInt := 0
		if *filters.Success {
			successInt = 1
		}
		query += " AND success = ?"
		args = append(args, successInt)
	}

	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, filters.Limit, filters.Offset)

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query artifacts: %w", err)
	}
	defer rows.Close()

	return scanArtifacts(rows)
}

func (d *DB) GetArtifactsByProblemClass(problemClass string, limit int) ([]Artifact, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := d.sql.Query(`
		SELECT id, workflow_id, task_type, workflow_type, repo_type, problem_class,
		       agents_used_json, root_cause, solution_pattern, files_changed_json,
		       review_result, cycle_time_minutes, success, metadata_json, created_at
		FROM insight_artifacts
		WHERE problem_class = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, problemClass, limit)
	if err != nil {
		return nil, fmt.Errorf("query artifacts by problem: %w", err)
	}
	defer rows.Close()

	return scanArtifacts(rows)
}

func (d *DB) GetArtifactsByRepoType(repoType string, limit int) ([]Artifact, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := d.sql.Query(`
		SELECT id, workflow_id, task_type, workflow_type, repo_type, problem_class,
		       agents_used_json, root_cause, solution_pattern, files_changed_json,
		       review_result, cycle_time_minutes, success, metadata_json, created_at
		FROM insight_artifacts
		WHERE repo_type = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, repoType, limit)
	if err != nil {
		return nil, fmt.Errorf("query artifacts by repo: %w", err)
	}
	defer rows.Close()

	return scanArtifacts(rows)
}

func (d *DB) GetSuccessfulArtifactsWithSolution(limit int) ([]Artifact, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := d.sql.Query(`
		SELECT id, workflow_id, task_type, workflow_type, repo_type, problem_class,
		       agents_used_json, root_cause, solution_pattern, files_changed_json,
		       review_result, cycle_time_minutes, success, metadata_json, created_at
		FROM insight_artifacts
		WHERE success = 1 AND solution_pattern != '' AND problem_class != ''
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query successful artifacts: %w", err)
	}
	defer rows.Close()

	return scanArtifacts(rows)
}

func (d *DB) CountArtifacts() (int, error) {
	var count int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM insight_artifacts`).Scan(&count)
	return count, err
}

func (d *DB) CountArtifactsByProblemClass(problemClass string) (int, error) {
	var count int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM insight_artifacts WHERE problem_class = ?`, problemClass).Scan(&count)
	return count, err
}

func (d *DB) GetProblemClassStats() ([]map[string]any, error) {
	rows, err := d.sql.Query(`
		SELECT problem_class, COUNT(*) as count,
		       SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as success_count,
		       AVG(cycle_time_minutes) as avg_cycle_time
		FROM insight_artifacts
		WHERE problem_class != ''
		GROUP BY problem_class
		ORDER BY count DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query problem stats: %w", err)
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var problemClass string
		var count, successCount int
		var avgCycleTime sql.NullFloat64
		if err := rows.Scan(&problemClass, &count, &successCount, &avgCycleTime); err != nil {
			return nil, fmt.Errorf("scan problem stats: %w", err)
		}
		result := map[string]any{
			"problem_class":  problemClass,
			"count":          count,
			"success_count":  successCount,
			"success_rate":   float64(0),
			"avg_cycle_time": float64(0),
		}
		if count > 0 {
			result["success_rate"] = float64(successCount) / float64(count)
		}
		if avgCycleTime.Valid {
			result["avg_cycle_time"] = avgCycleTime.Float64
		}
		results = append(results, result)
	}

	return results, rows.Err()
}

func (d *DB) GetAgentSuccessByProblem() ([]map[string]any, error) {
	rows, err := d.sql.Query(`
		SELECT problem_class, agents_used_json, success
		FROM insight_artifacts
		WHERE problem_class != '' AND agents_used_json != '[]'
	`)
	if err != nil {
		return nil, fmt.Errorf("query agent success: %w", err)
	}
	defer rows.Close()

	agentStats := make(map[string]map[string]map[string]int)

	for rows.Next() {
		var problemClass, agentsJSON string
		var successInt int
		if err := rows.Scan(&problemClass, &agentsJSON, &successInt); err != nil {
			return nil, fmt.Errorf("scan agent success: %w", err)
		}

		var agents []string
		if err := json.Unmarshal([]byte(agentsJSON), &agents); err != nil {
			continue
		}

		if agentStats[problemClass] == nil {
			agentStats[problemClass] = make(map[string]map[string]int)
		}

		for _, agent := range agents {
			if agentStats[problemClass][agent] == nil {
				agentStats[problemClass][agent] = map[string]int{"total": 0, "success": 0}
			}
			agentStats[problemClass][agent]["total"]++
			if successInt == 1 {
				agentStats[problemClass][agent]["success"]++
			}
		}
	}

	var results []map[string]any
	for problemClass, agents := range agentStats {
		agentSuccessRates := make(map[string]float64)
		var bestAgent string
		var bestRate float64
		for agent, stats := range agents {
			if stats["total"] > 0 {
				rate := float64(stats["success"]) / float64(stats["total"])
				agentSuccessRates[agent] = rate
				if rate > bestRate {
					bestRate = rate
					bestAgent = agent
				}
			}
		}
		results = append(results, map[string]any{
			"problem_class":      problemClass,
			"best_agent":         bestAgent,
			"best_agent_success": bestRate,
			"agents_success":     agentSuccessRates,
		})
	}

	return results, rows.Err()
}

func scanArtifact(row *sql.Row) (*Artifact, error) {
	var a Artifact
	var agentsJSON, filesJSON, metadataJSON string
	var successInt int

	err := row.Scan(
		&a.ID, &a.WorkflowID, &a.TaskType, &a.WorkflowType, &a.RepoType, &a.ProblemClass,
		&agentsJSON, &a.RootCause, &a.SolutionPattern, &filesJSON,
		&a.ReviewResult, &a.CycleTimeMin, &successInt, &metadataJSON, &a.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan artifact: %w", err)
	}

	a.Success = successInt == 1

	if agentsJSON != "" {
		if err := json.Unmarshal([]byte(agentsJSON), &a.AgentsUsed); err != nil {
			log.Printf("warning: failed to parse agents for artifact %s: %v", a.ID, err)
		}
	}
	if filesJSON != "" {
		if err := json.Unmarshal([]byte(filesJSON), &a.FilesChanged); err != nil {
			log.Printf("warning: failed to parse files for artifact %s: %v", a.ID, err)
		}
	}
	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &a.Metadata); err != nil {
			log.Printf("warning: failed to parse metadata for artifact %s: %v", a.ID, err)
		}
	}

	return &a, nil
}

func scanArtifacts(rows *sql.Rows) ([]Artifact, error) {
	var artifacts []Artifact
	for rows.Next() {
		var a Artifact
		var agentsJSON, filesJSON, metadataJSON string
		var successInt int

		if err := rows.Scan(
			&a.ID, &a.WorkflowID, &a.TaskType, &a.WorkflowType, &a.RepoType, &a.ProblemClass,
			&agentsJSON, &a.RootCause, &a.SolutionPattern, &filesJSON,
			&a.ReviewResult, &a.CycleTimeMin, &successInt, &metadataJSON, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}

		a.Success = successInt == 1

		if agentsJSON != "" {
			if err := json.Unmarshal([]byte(agentsJSON), &a.AgentsUsed); err != nil {
				log.Printf("warning: failed to parse agents for artifact %s: %v", a.ID, err)
			}
		}
		if filesJSON != "" {
			if err := json.Unmarshal([]byte(filesJSON), &a.FilesChanged); err != nil {
				log.Printf("warning: failed to parse files for artifact %s: %v", a.ID, err)
			}
		}
		if metadataJSON != "" {
			if err := json.Unmarshal([]byte(metadataJSON), &a.Metadata); err != nil {
				log.Printf("warning: failed to parse metadata for artifact %s: %v", a.ID, err)
			}
		}

		artifacts = append(artifacts, a)
	}

	return artifacts, rows.Err()
}
