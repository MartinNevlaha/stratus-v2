package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

type SolutionPattern struct {
	ID               string    `json:"id"`
	ProblemClass     string    `json:"problem_class"`
	SolutionPattern  string    `json:"solution_pattern"`
	RepoType         string    `json:"repo_type"`
	SuccessRate      float64   `json:"success_rate"`
	OccurrenceCount  int       `json:"occurrence_count"`
	ExampleArtifacts []string  `json:"example_artifacts"`
	Confidence       float64   `json:"confidence"`
	FirstSeen        time.Time `json:"first_seen"`
	LastSeen         time.Time `json:"last_seen"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ProblemStats struct {
	ID              string             `json:"id"`
	ProblemClass    string             `json:"problem_class"`
	RepoType        string             `json:"repo_type"`
	BestAgent       string             `json:"best_agent"`
	BestWorkflow    string             `json:"best_workflow"`
	SuccessRate     float64            `json:"success_rate"`
	OccurrenceCount int                `json:"occurrence_count"`
	AvgCycleTime    int                `json:"avg_cycle_time"`
	AgentsSuccess   map[string]float64 `json:"agents_success"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type SolutionPatternFilters struct {
	ProblemClass   string
	RepoType       string
	MinSuccessRate float64
	Limit          int
}

type ProblemStatsFilters struct {
	ProblemClass string
	RepoType     string
	Limit        int
}

func (d *DB) SaveSolutionPattern(pattern *SolutionPattern) error {
	examplesBytes, err := json.Marshal(pattern.ExampleArtifacts)
	if err != nil {
		return fmt.Errorf("marshal examples: %w", err)
	}
	if pattern.ExampleArtifacts == nil {
		examplesBytes = []byte("[]")
	}

	if pattern.ID == "" {
		pattern.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	firstSeen := pattern.FirstSeen
	if firstSeen.IsZero() {
		firstSeen = now
	}

	_, err = d.sql.Exec(`
		INSERT INTO insight_solution_patterns 
		(id, problem_class, solution_pattern, repo_type, success_rate, occurrence_count,
		 example_artifacts_json, confidence, first_seen, last_seen, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(problem_class, solution_pattern, repo_type) DO UPDATE SET
			success_rate = excluded.success_rate,
			occurrence_count = excluded.occurrence_count,
			example_artifacts_json = excluded.example_artifacts_json,
			confidence = excluded.confidence,
			last_seen = excluded.last_seen,
			updated_at = excluded.updated_at
	`,
		pattern.ID, pattern.ProblemClass, pattern.SolutionPattern, pattern.RepoType,
		pattern.SuccessRate, pattern.OccurrenceCount, string(examplesBytes),
		pattern.Confidence, firstSeen.Format(time.RFC3339Nano), pattern.LastSeen.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)

	return err
}

func (d *DB) GetSolutionPatternByID(id string) (*SolutionPattern, error) {
	row := d.sql.QueryRow(`
		SELECT id, problem_class, solution_pattern, repo_type, success_rate, occurrence_count,
		       example_artifacts_json, confidence, first_seen, last_seen, created_at, updated_at
		FROM insight_solution_patterns
		WHERE id = ?
	`, id)

	return scanSolutionPattern(row)
}

func (d *DB) ListSolutionPatterns(filters SolutionPatternFilters) ([]SolutionPattern, error) {
	if filters.Limit <= 0 {
		filters.Limit = 100
	}

	query := `
		SELECT id, problem_class, solution_pattern, repo_type, success_rate, occurrence_count,
		       example_artifacts_json, confidence, first_seen, last_seen, created_at, updated_at
		FROM insight_solution_patterns
		WHERE 1=1
	`
	args := []any{}

	if filters.ProblemClass != "" {
		query += " AND problem_class = ?"
		args = append(args, filters.ProblemClass)
	}
	if filters.RepoType != "" {
		query += " AND repo_type = ?"
		args = append(args, filters.RepoType)
	}
	if filters.MinSuccessRate > 0 {
		query += " AND success_rate >= ?"
		args = append(args, filters.MinSuccessRate)
	}

	query += " ORDER BY success_rate DESC, occurrence_count DESC LIMIT ?"
	args = append(args, filters.Limit)

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query solution patterns: %w", err)
	}
	defer rows.Close()

	return scanSolutionPatterns(rows)
}

func (d *DB) GetSolutionPatternsByProblem(problemClass string, limit int) ([]SolutionPattern, error) {
	return d.ListSolutionPatterns(SolutionPatternFilters{
		ProblemClass: problemClass,
		Limit:        limit,
	})
}

func (d *DB) DeleteOldSolutionPatterns(olderThan time.Duration) (int64, error) {
	threshold := time.Now().UTC().Add(-olderThan).Format(time.RFC3339Nano)
	result, err := d.sql.Exec(`
		DELETE FROM insight_solution_patterns
		WHERE last_seen < ? AND occurrence_count < 3
	`, threshold)
	if err != nil {
		return 0, fmt.Errorf("delete old patterns: %w", err)
	}
	return result.RowsAffected()
}

func (d *DB) SaveProblemStats(stats *ProblemStats) error {
	agentsBytes, err := json.Marshal(stats.AgentsSuccess)
	if err != nil {
		return fmt.Errorf("marshal agents success: %w", err)
	}
	if stats.AgentsSuccess == nil {
		agentsBytes = []byte("{}")
	}

	if stats.ID == "" {
		stats.ID = uuid.NewString()
	}

	now := time.Now().UTC()

	_, err = d.sql.Exec(`
		INSERT INTO insight_problem_stats 
		(id, problem_class, repo_type, best_agent, best_workflow, success_rate,
		 occurrence_count, avg_cycle_time, agents_success_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(problem_class, repo_type) DO UPDATE SET
			best_agent = excluded.best_agent,
			best_workflow = excluded.best_workflow,
			success_rate = excluded.success_rate,
			occurrence_count = excluded.occurrence_count,
			avg_cycle_time = excluded.avg_cycle_time,
			agents_success_json = excluded.agents_success_json,
			updated_at = excluded.updated_at
	`,
		stats.ID, stats.ProblemClass, stats.RepoType, stats.BestAgent, stats.BestWorkflow,
		stats.SuccessRate, stats.OccurrenceCount, stats.AvgCycleTime, string(agentsBytes),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)

	return err
}

func (d *DB) GetProblemStatsByID(id string) (*ProblemStats, error) {
	row := d.sql.QueryRow(`
		SELECT id, problem_class, repo_type, best_agent, best_workflow, success_rate,
		       occurrence_count, avg_cycle_time, agents_success_json, created_at, updated_at
		FROM insight_problem_stats
		WHERE id = ?
	`, id)

	return scanProblemStats(row)
}

func (d *DB) GetProblemStatsByClass(problemClass string) (*ProblemStats, error) {
	row := d.sql.QueryRow(`
		SELECT id, problem_class, repo_type, best_agent, best_workflow, success_rate,
		       occurrence_count, avg_cycle_time, agents_success_json, created_at, updated_at
		FROM insight_problem_stats
		WHERE problem_class = ? AND repo_type = ''
	`, problemClass)

	return scanProblemStats(row)
}

func (d *DB) GetProblemStatsByClassAndRepo(problemClass, repoType string) (*ProblemStats, error) {
	row := d.sql.QueryRow(`
		SELECT id, problem_class, repo_type, best_agent, best_workflow, success_rate,
		       occurrence_count, avg_cycle_time, agents_success_json, created_at, updated_at
		FROM insight_problem_stats
		WHERE problem_class = ? AND repo_type = ?
	`, problemClass, repoType)

	return scanProblemStats(row)
}

func (d *DB) ListProblemStats(filters ProblemStatsFilters) ([]ProblemStats, error) {
	if filters.Limit <= 0 {
		filters.Limit = 100
	}

	query := `
		SELECT id, problem_class, repo_type, best_agent, best_workflow, success_rate,
		       occurrence_count, avg_cycle_time, agents_success_json, created_at, updated_at
		FROM insight_problem_stats
		WHERE 1=1
	`
	args := []any{}

	if filters.ProblemClass != "" {
		query += " AND problem_class = ?"
		args = append(args, filters.ProblemClass)
	}
	if filters.RepoType != "" {
		query += " AND repo_type = ?"
		args = append(args, filters.RepoType)
	}

	query += " ORDER BY success_rate DESC, occurrence_count DESC LIMIT ?"
	args = append(args, filters.Limit)

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query problem stats: %w", err)
	}
	defer rows.Close()

	return scanProblemStatsList(rows)
}

func (d *DB) GetAllProblemStats(limit int) ([]ProblemStats, error) {
	return d.ListProblemStats(ProblemStatsFilters{Limit: limit})
}

func (d *DB) CountProblemStats() (int, error) {
	var count int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM insight_problem_stats`).Scan(&count)
	return count, err
}

func (d *DB) CountSolutionPatterns() (int, error) {
	var count int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM insight_solution_patterns`).Scan(&count)
	return count, err
}

func (d *DB) GetBestAgentForProblem(problemClass, repoType string) (string, float64, error) {
	row := d.sql.QueryRow(`
		SELECT best_agent, success_rate
		FROM insight_problem_stats
		WHERE problem_class = ? AND repo_type = ?
		ORDER BY success_rate DESC
		LIMIT 1
	`, problemClass, repoType)

	var bestAgent string
	var successRate float64
	err := row.Scan(&bestAgent, &successRate)
	if err == sql.ErrNoRows {
		row = d.sql.QueryRow(`
			SELECT best_agent, success_rate
			FROM insight_problem_stats
			WHERE problem_class = ? AND repo_type = ''
			ORDER BY success_rate DESC
			LIMIT 1
		`, problemClass)

		err = row.Scan(&bestAgent, &successRate)
		if err == sql.ErrNoRows {
			return "", 0, nil
		}
	}
	if err != nil {
		return "", 0, fmt.Errorf("get best agent: %w", err)
	}

	return bestAgent, successRate, nil
}

func (d *DB) GetBestSolutionForProblem(problemClass, repoType string) (*SolutionPattern, error) {
	var row *sql.Row
	if repoType != "" {
		row = d.sql.QueryRow(`
			SELECT id, problem_class, solution_pattern, repo_type, success_rate, occurrence_count,
			       example_artifacts_json, confidence, first_seen, last_seen, created_at, updated_at
			FROM insight_solution_patterns
			WHERE problem_class = ? AND repo_type = ?
			ORDER BY success_rate DESC, occurrence_count DESC
			LIMIT 1
		`, problemClass, repoType)
	} else {
		row = d.sql.QueryRow(`
			SELECT id, problem_class, solution_pattern, repo_type, success_rate, occurrence_count,
			       example_artifacts_json, confidence, first_seen, last_seen, created_at, updated_at
			FROM insight_solution_patterns
			WHERE problem_class = ?
			ORDER BY success_rate DESC, occurrence_count DESC
			LIMIT 1
		`, problemClass)
	}

	return scanSolutionPattern(row)
}

func scanSolutionPattern(row *sql.Row) (*SolutionPattern, error) {
	var p SolutionPattern
	var examplesJSON string
	var firstSeen, lastSeen, createdAt, updatedAt sql.NullString

	err := row.Scan(
		&p.ID, &p.ProblemClass, &p.SolutionPattern, &p.RepoType,
		&p.SuccessRate, &p.OccurrenceCount, &examplesJSON, &p.Confidence,
		&firstSeen, &lastSeen, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan solution pattern: %w", err)
	}

	if examplesJSON != "" {
		if err := json.Unmarshal([]byte(examplesJSON), &p.ExampleArtifacts); err != nil {
			log.Printf("warning: failed to parse examples for pattern %s: %v", p.ID, err)
		}
	}
	if firstSeen.Valid {
		p.FirstSeen, _ = time.Parse(time.RFC3339Nano, firstSeen.String)
	}
	if lastSeen.Valid {
		p.LastSeen, _ = time.Parse(time.RFC3339Nano, lastSeen.String)
	}
	if createdAt.Valid {
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt.String)
	}
	if updatedAt.Valid {
		p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt.String)
	}

	return &p, nil
}

func scanSolutionPatterns(rows *sql.Rows) ([]SolutionPattern, error) {
	var patterns []SolutionPattern
	for rows.Next() {
		var p SolutionPattern
		var examplesJSON string
		var firstSeen, lastSeen, createdAt, updatedAt sql.NullString

		if err := rows.Scan(
			&p.ID, &p.ProblemClass, &p.SolutionPattern, &p.RepoType,
			&p.SuccessRate, &p.OccurrenceCount, &examplesJSON, &p.Confidence,
			&firstSeen, &lastSeen, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan solution pattern: %w", err)
		}

		if examplesJSON != "" {
			if err := json.Unmarshal([]byte(examplesJSON), &p.ExampleArtifacts); err != nil {
				log.Printf("warning: failed to parse examples for pattern %s: %v", p.ID, err)
			}
		}
		if firstSeen.Valid {
			p.FirstSeen, _ = time.Parse(time.RFC3339Nano, firstSeen.String)
		}
		if lastSeen.Valid {
			p.LastSeen, _ = time.Parse(time.RFC3339Nano, lastSeen.String)
		}
		if createdAt.Valid {
			p.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt.String)
		}
		if updatedAt.Valid {
			p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt.String)
		}

		patterns = append(patterns, p)
	}

	return patterns, rows.Err()
}

func scanProblemStats(row *sql.Row) (*ProblemStats, error) {
	var s ProblemStats
	var agentsJSON string
	var createdAt, updatedAt sql.NullString

	err := row.Scan(
		&s.ID, &s.ProblemClass, &s.RepoType, &s.BestAgent, &s.BestWorkflow,
		&s.SuccessRate, &s.OccurrenceCount, &s.AvgCycleTime, &agentsJSON,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan problem stats: %w", err)
	}

	if agentsJSON != "" {
		if err := json.Unmarshal([]byte(agentsJSON), &s.AgentsSuccess); err != nil {
			log.Printf("warning: failed to parse agents success for stats %s: %v", s.ID, err)
		}
	}
	if createdAt.Valid {
		s.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt.String)
	}
	if updatedAt.Valid {
		s.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt.String)
	}

	return &s, nil
}

func scanProblemStatsList(rows *sql.Rows) ([]ProblemStats, error) {
	var stats []ProblemStats
	for rows.Next() {
		var s ProblemStats
		var agentsJSON string
		var createdAt, updatedAt sql.NullString

		if err := rows.Scan(
			&s.ID, &s.ProblemClass, &s.RepoType, &s.BestAgent, &s.BestWorkflow,
			&s.SuccessRate, &s.OccurrenceCount, &s.AvgCycleTime, &agentsJSON,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan problem stats: %w", err)
		}

		if agentsJSON != "" {
			if err := json.Unmarshal([]byte(agentsJSON), &s.AgentsSuccess); err != nil {
				log.Printf("warning: failed to parse agents success for stats %s: %v", s.ID, err)
			}
		}
		if createdAt.Valid {
			s.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt.String)
		}
		if updatedAt.Valid {
			s.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt.String)
		}

		stats = append(stats, s)
	}

	return stats, rows.Err()
}
