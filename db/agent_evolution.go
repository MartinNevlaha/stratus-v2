package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type AgentCandidate struct {
	ID              string                 `json:"id"`
	AgentName       string                 `json:"agent_name"`
	BaseAgent       string                 `json:"base_agent"`
	Specialization  string                 `json:"specialization"`
	Reason          string                 `json:"reason"`
	Confidence      float64                `json:"confidence"`
	PromptDiff      map[string]interface{} `json:"prompt_diff"`
	Status          string                 `json:"status"`
	Evidence        map[string]interface{} `json:"evidence"`
	OpportunityType string                 `json:"opportunity_type"`
	CreatedAt       string                 `json:"created_at"`
	UpdatedAt       string                 `json:"updated_at"`
}

type AgentBanditState struct {
	CandidateAlpha float64 `json:"candidate_alpha"`
	CandidateBeta  float64 `json:"candidate_beta"`
	BaselineAlpha  float64 `json:"baseline_alpha"`
	BaselineBeta   float64 `json:"baseline_beta"`
}

type AgentExperiment struct {
	ID             string           `json:"id"`
	CandidateID    string           `json:"candidate_id"`
	CandidateAgent string           `json:"candidate_agent"`
	BaselineAgent  string           `json:"baseline_agent"`
	TrafficPercent float64          `json:"traffic_percent"`
	Status         string           `json:"status"`
	SampleSize     int              `json:"sample_size"`
	RunsCandidate  int              `json:"runs_candidate"`
	RunsBaseline   int              `json:"runs_baseline"`
	BanditState    AgentBanditState `json:"bandit_state"`
	StartedAt      time.Time        `json:"started_at"`
	CompletedAt    *time.Time       `json:"completed_at,omitempty"`
	Winner         string           `json:"winner,omitempty"`
	CreatedAt      string           `json:"created_at"`
	UpdatedAt      string           `json:"updated_at"`
}

type AgentExperimentResult struct {
	ID            int64  `json:"id"`
	ExperimentID  string `json:"experiment_id"`
	WorkflowID    string `json:"workflow_id"`
	TaskType      string `json:"task_type"`
	UsedCandidate bool   `json:"used_candidate"`
	Success       bool   `json:"success"`
	CycleTimeMs   int64  `json:"cycle_time_ms"`
	ReviewPassed  bool   `json:"review_passed"`
	ReworkCount   int    `json:"rework_count"`
	CreatedAt     string `json:"created_at"`
}

type AgentEvaluationMetrics struct {
	SuccessRate    float64 `json:"success_rate"`
	AvgCycleTimeMs float64 `json:"avg_cycle_time_ms"`
	ReviewPassRate float64 `json:"review_pass_rate"`
	ReworkRate     float64 `json:"rework_rate"`
	SampleSize     int     `json:"sample_size"`
}

func (d *DB) SaveAgentCandidate(c *AgentCandidate) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}

	promptDiffBytes := []byte("{}")
	if c.PromptDiff != nil {
		var err error
		promptDiffBytes, err = json.Marshal(c.PromptDiff)
		if err != nil {
			return fmt.Errorf("marshal prompt_diff: %w", err)
		}
	}

	evidenceBytes := []byte("{}")
	if c.Evidence != nil {
		var err error
		evidenceBytes, err = json.Marshal(c.Evidence)
		if err != nil {
			return fmt.Errorf("marshal evidence: %w", err)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		INSERT INTO openclaw_agent_candidates 
		(id, agent_name, base_agent, specialization, reason, confidence, prompt_diff_json,
		 status, evidence_json, opportunity_type, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			agent_name = excluded.agent_name,
			base_agent = excluded.base_agent,
			specialization = excluded.specialization,
			reason = excluded.reason,
			confidence = excluded.confidence,
			prompt_diff_json = excluded.prompt_diff_json,
			status = excluded.status,
			evidence_json = excluded.evidence_json,
			opportunity_type = excluded.opportunity_type,
			updated_at = excluded.updated_at
	`,
		c.ID, c.AgentName, c.BaseAgent, c.Specialization, c.Reason, c.Confidence,
		string(promptDiffBytes), c.Status, string(evidenceBytes), c.OpportunityType, now, now,
	)

	return err
}

func (d *DB) GetAgentCandidateByID(id string) (*AgentCandidate, error) {
	row := d.sql.QueryRow(`
		SELECT id, agent_name, base_agent, specialization, reason, confidence, prompt_diff_json,
		       status, evidence_json, opportunity_type, created_at, updated_at
		FROM openclaw_agent_candidates
		WHERE id = ?
	`, id)

	return scanAgentCandidate(row)
}

func (d *DB) GetAgentCandidateByName(agentName string) (*AgentCandidate, error) {
	row := d.sql.QueryRow(`
		SELECT id, agent_name, base_agent, specialization, reason, confidence, prompt_diff_json,
		       status, evidence_json, opportunity_type, created_at, updated_at
		FROM openclaw_agent_candidates
		WHERE agent_name = ?
	`, agentName)

	return scanAgentCandidate(row)
}

func (d *DB) ListAgentCandidates(status string, limit int) ([]AgentCandidate, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT id, agent_name, base_agent, specialization, reason, confidence, prompt_diff_json,
	          status, evidence_json, opportunity_type, created_at, updated_at
	          FROM openclaw_agent_candidates`
	args := []any{}

	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}

	query += " ORDER BY confidence DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list agent candidates: %w", err)
	}
	defer rows.Close()

	return scanAgentCandidates(rows)
}

func (d *DB) UpdateAgentCandidateStatus(id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		UPDATE openclaw_agent_candidates
		SET status = ?, updated_at = ?
		WHERE id = ?
	`, status, now, id)
	return err
}

func (d *DB) DeleteAgentCandidate(id string) error {
	_, err := d.sql.Exec("DELETE FROM openclaw_agent_candidates WHERE id = ?", id)
	return err
}

func (d *DB) SaveAgentExperiment(e *AgentExperiment) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}

	if e.TrafficPercent < 0 {
		e.TrafficPercent = 0
	} else if e.TrafficPercent > 100 {
		e.TrafficPercent = 100
	}

	if e.SampleSize <= 0 {
		e.SampleSize = 100
	}

	banditBytes, err := json.Marshal(e.BanditState)
	if err != nil {
		return fmt.Errorf("marshal bandit state: %w", err)
	}

	var completedAt sql.NullString
	if e.CompletedAt != nil {
		completedAt = sql.NullString{String: e.CompletedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}

	var winner sql.NullString
	if e.Winner != "" {
		winner = sql.NullString{String: e.Winner, Valid: true}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.sql.Exec(`
		INSERT INTO openclaw_agent_experiments 
		(id, candidate_id, candidate_agent, baseline_agent, traffic_percent, status, sample_size,
		 runs_candidate, runs_baseline, bandit_state_json, started_at, completed_at, winner, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			candidate_agent = excluded.candidate_agent,
			baseline_agent = excluded.baseline_agent,
			traffic_percent = excluded.traffic_percent,
			status = excluded.status,
			sample_size = excluded.sample_size,
			runs_candidate = excluded.runs_candidate,
			runs_baseline = excluded.runs_baseline,
			bandit_state_json = excluded.bandit_state_json,
			completed_at = excluded.completed_at,
			winner = excluded.winner,
			updated_at = excluded.updated_at
	`,
		e.ID, e.CandidateID, e.CandidateAgent, e.BaselineAgent, e.TrafficPercent, e.Status,
		e.SampleSize, e.RunsCandidate, e.RunsBaseline, string(banditBytes),
		e.StartedAt.UTC().Format(time.RFC3339Nano), completedAt, winner, now, now,
	)

	return err
}

func (d *DB) GetAgentExperimentByID(id string) (*AgentExperiment, error) {
	row := d.sql.QueryRow(`
		SELECT id, candidate_id, candidate_agent, baseline_agent, traffic_percent, status, sample_size,
		       runs_candidate, runs_baseline, bandit_state_json, started_at, completed_at, winner, created_at, updated_at
		FROM openclaw_agent_experiments
		WHERE id = ?
	`, id)

	return scanAgentExperiment(row)
}

func (d *DB) GetAgentExperimentByCandidateID(candidateID string) (*AgentExperiment, error) {
	row := d.sql.QueryRow(`
		SELECT id, candidate_id, candidate_agent, baseline_agent, traffic_percent, status, sample_size,
		       runs_candidate, runs_baseline, bandit_state_json, started_at, completed_at, winner, created_at, updated_at
		FROM openclaw_agent_experiments
		WHERE candidate_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, candidateID)

	return scanAgentExperiment(row)
}

func (d *DB) ListRunningAgentExperiments() ([]AgentExperiment, error) {
	rows, err := d.sql.Query(`
		SELECT id, candidate_id, candidate_agent, baseline_agent, traffic_percent, status, sample_size,
		       runs_candidate, runs_baseline, bandit_state_json, started_at, completed_at, winner, created_at, updated_at
		FROM openclaw_agent_experiments
		WHERE status = 'running'
		ORDER BY started_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list running agent experiments: %w", err)
	}
	defer rows.Close()

	return scanAgentExperiments(rows)
}

func (d *DB) ListAgentExperiments(status string, limit int) ([]AgentExperiment, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT id, candidate_id, candidate_agent, baseline_agent, traffic_percent, status, sample_size,
	          runs_candidate, runs_baseline, bandit_state_json, started_at, completed_at, winner, created_at, updated_at
	          FROM openclaw_agent_experiments`
	args := []any{}

	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}

	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list agent experiments: %w", err)
	}
	defer rows.Close()

	return scanAgentExperiments(rows)
}

func (d *DB) UpdateAgentExperimentBandit(id string, bandit AgentBanditState, runsCandidate, runsBaseline int) error {
	banditBytes, err := json.Marshal(bandit)
	if err != nil {
		return fmt.Errorf("marshal bandit state: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.sql.Exec(`
		UPDATE openclaw_agent_experiments
		SET bandit_state_json = ?, runs_candidate = ?, runs_baseline = ?, updated_at = ?
		WHERE id = ?
	`, string(banditBytes), runsCandidate, runsBaseline, now, id)
	return err
}

func (d *DB) UpdateAgentExperimentStatus(id, status, winner string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var completedAt any
	if status == "completed" || status == "cancelled" {
		completedAt = now
	} else {
		completedAt = nil
	}

	var winnerVal any
	if winner != "" {
		winnerVal = winner
	} else {
		winnerVal = nil
	}

	_, err := d.sql.Exec(`
		UPDATE openclaw_agent_experiments
		SET status = ?, completed_at = ?, winner = ?, updated_at = ?
		WHERE id = ?
	`, status, completedAt, winnerVal, now, id)
	return err
}

func (d *DB) SaveAgentExperimentResult(r *AgentExperimentResult) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	usedCandidate := 0
	if r.UsedCandidate {
		usedCandidate = 1
	}
	success := 0
	if r.Success {
		success = 1
	}
	reviewPassed := 0
	if r.ReviewPassed {
		reviewPassed = 1
	}

	result, err := d.sql.Exec(`
		INSERT INTO openclaw_agent_experiment_results 
		(experiment_id, workflow_id, task_type, used_candidate, success, cycle_time_ms, review_passed, rework_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ExperimentID, r.WorkflowID, r.TaskType, usedCandidate, success,
		r.CycleTimeMs, reviewPassed, r.ReworkCount, now,
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err == nil {
		r.ID = id
	}
	return nil
}

func (d *DB) GetAgentExperimentResults(experimentID string) ([]AgentExperimentResult, error) {
	rows, err := d.sql.Query(`
		SELECT id, experiment_id, workflow_id, task_type, used_candidate, success, cycle_time_ms, review_passed, rework_count, created_at
		FROM openclaw_agent_experiment_results
		WHERE experiment_id = ?
		ORDER BY created_at DESC
	`, experimentID)
	if err != nil {
		return nil, fmt.Errorf("get agent experiment results: %w", err)
	}
	defer rows.Close()

	return scanAgentExperimentResults(rows)
}

func (d *DB) GetAgentExperimentMetrics(experimentID string) (candidate, baseline AgentEvaluationMetrics, err error) {
	rows, err := d.sql.Query(`
		SELECT used_candidate, success, cycle_time_ms, review_passed, rework_count
		FROM openclaw_agent_experiment_results
		WHERE experiment_id = ?
	`, experimentID)
	if err != nil {
		return candidate, baseline, fmt.Errorf("get agent experiment metrics: %w", err)
	}
	defer rows.Close()

	var candCount, baseCount int
	var candSuccess, baseSuccess int
	var candCycleTime, baseCycleTime int64
	var candReviewPasses, baseReviewPasses int
	var candReworkCount, baseReworkCount int

	for rows.Next() {
		var usedCandidate int
		var success int
		var cycleTimeMs int64
		var reviewPassed int
		var reworkCount int

		err := rows.Scan(&usedCandidate, &success, &cycleTimeMs, &reviewPassed, &reworkCount)
		if err != nil {
			return candidate, baseline, err
		}

		if usedCandidate == 1 {
			candCount++
			if success == 1 {
				candSuccess++
			}
			candCycleTime += cycleTimeMs
			candReviewPasses += reviewPassed
			candReworkCount += reworkCount
		} else {
			baseCount++
			if success == 1 {
				baseSuccess++
			}
			baseCycleTime += cycleTimeMs
			baseReviewPasses += reviewPassed
			baseReworkCount += reworkCount
		}
	}

	if candCount > 0 {
		candidate.SampleSize = candCount
		candidate.SuccessRate = float64(candSuccess) / float64(candCount)
		candidate.AvgCycleTimeMs = float64(candCycleTime) / float64(candCount)
		candidate.ReviewPassRate = float64(candReviewPasses) / float64(candCount)
		if candCount > 0 {
			candidate.ReworkRate = float64(candReworkCount) / float64(candCount)
		}
	}

	if baseCount > 0 {
		baseline.SampleSize = baseCount
		baseline.SuccessRate = float64(baseSuccess) / float64(baseCount)
		baseline.AvgCycleTimeMs = float64(baseCycleTime) / float64(baseCount)
		baseline.ReviewPassRate = float64(baseReviewPasses) / float64(baseCount)
		if baseCount > 0 {
			baseline.ReworkRate = float64(baseReworkCount) / float64(baseCount)
		}
	}

	return candidate, baseline, nil
}

func (d *DB) DeleteAgentExperiment(id string) error {
	_, err := d.sql.Exec("DELETE FROM openclaw_agent_experiments WHERE id = ?", id)
	return err
}

func scanAgentCandidate(row *sql.Row) (*AgentCandidate, error) {
	var c AgentCandidate
	var promptDiffJSON, evidenceJSON string

	err := row.Scan(
		&c.ID, &c.AgentName, &c.BaseAgent, &c.Specialization, &c.Reason, &c.Confidence,
		&promptDiffJSON, &c.Status, &evidenceJSON, &c.OpportunityType, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan agent candidate: %w", err)
	}

	if promptDiffJSON != "" {
		if err := json.Unmarshal([]byte(promptDiffJSON), &c.PromptDiff); err != nil {
			return nil, fmt.Errorf("unmarshal prompt_diff: %w", err)
		}
	}

	if evidenceJSON != "" {
		if err := json.Unmarshal([]byte(evidenceJSON), &c.Evidence); err != nil {
			return nil, fmt.Errorf("unmarshal evidence: %w", err)
		}
	}

	return &c, nil
}

func scanAgentCandidates(rows *sql.Rows) ([]AgentCandidate, error) {
	var candidates []AgentCandidate
	for rows.Next() {
		var c AgentCandidate
		var promptDiffJSON, evidenceJSON string

		err := rows.Scan(
			&c.ID, &c.AgentName, &c.BaseAgent, &c.Specialization, &c.Reason, &c.Confidence,
			&promptDiffJSON, &c.Status, &evidenceJSON, &c.OpportunityType, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan agent candidate row: %w", err)
		}

		if promptDiffJSON != "" {
			if err := json.Unmarshal([]byte(promptDiffJSON), &c.PromptDiff); err != nil {
				return nil, fmt.Errorf("unmarshal prompt_diff: %w", err)
			}
		}

		if evidenceJSON != "" {
			if err := json.Unmarshal([]byte(evidenceJSON), &c.Evidence); err != nil {
				return nil, fmt.Errorf("unmarshal evidence: %w", err)
			}
		}

		candidates = append(candidates, c)
	}

	if candidates == nil {
		candidates = []AgentCandidate{}
	}

	return candidates, nil
}

func scanAgentExperiment(row *sql.Row) (*AgentExperiment, error) {
	var e AgentExperiment
	var banditJSON string
	var completedAt sql.NullString
	var winner sql.NullString

	err := row.Scan(
		&e.ID, &e.CandidateID, &e.CandidateAgent, &e.BaselineAgent, &e.TrafficPercent, &e.Status,
		&e.SampleSize, &e.RunsCandidate, &e.RunsBaseline, &banditJSON,
		&e.StartedAt, &completedAt, &winner, &e.CreatedAt, &e.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan agent experiment: %w", err)
	}

	if banditJSON != "" {
		if err := json.Unmarshal([]byte(banditJSON), &e.BanditState); err != nil {
			return nil, fmt.Errorf("unmarshal bandit state: %w", err)
		}
	}

	if completedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err == nil {
			e.CompletedAt = &t
		}
	}

	if winner.Valid {
		e.Winner = winner.String
	}

	return &e, nil
}

func scanAgentExperiments(rows *sql.Rows) ([]AgentExperiment, error) {
	var experiments []AgentExperiment
	for rows.Next() {
		var e AgentExperiment
		var banditJSON string
		var completedAt sql.NullString
		var winner sql.NullString

		err := rows.Scan(
			&e.ID, &e.CandidateID, &e.CandidateAgent, &e.BaselineAgent, &e.TrafficPercent, &e.Status,
			&e.SampleSize, &e.RunsCandidate, &e.RunsBaseline, &banditJSON,
			&e.StartedAt, &completedAt, &winner, &e.CreatedAt, &e.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan agent experiment row: %w", err)
		}

		if banditJSON != "" {
			if err := json.Unmarshal([]byte(banditJSON), &e.BanditState); err != nil {
				return nil, fmt.Errorf("unmarshal bandit state: %w", err)
			}
		}

		if completedAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, completedAt.String)
			if err == nil {
				e.CompletedAt = &t
			}
		}

		if winner.Valid {
			e.Winner = winner.String
		}

		experiments = append(experiments, e)
	}

	if experiments == nil {
		experiments = []AgentExperiment{}
	}

	return experiments, nil
}

func scanAgentExperimentResults(rows *sql.Rows) ([]AgentExperimentResult, error) {
	var results []AgentExperimentResult
	for rows.Next() {
		var r AgentExperimentResult
		var usedCandidate, success, reviewPassed int

		err := rows.Scan(
			&r.ID, &r.ExperimentID, &r.WorkflowID, &r.TaskType, &usedCandidate, &success,
			&r.CycleTimeMs, &reviewPassed, &r.ReworkCount, &r.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan agent experiment result row: %w", err)
		}

		r.UsedCandidate = usedCandidate == 1
		r.Success = success == 1
		r.ReviewPassed = reviewPassed == 1
		results = append(results, r)
	}

	if results == nil {
		results = []AgentExperimentResult{}
	}

	return results, nil
}
