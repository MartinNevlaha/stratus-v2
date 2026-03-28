package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type WorkflowStep struct {
	Phase      string   `json:"phase"`
	AgentHint  string   `json:"agent_hint,omitempty"`
	NextPhases []string `json:"next_phases"`
}

type WorkflowCandidate struct {
	ID               string            `json:"id"`
	WorkflowName     string            `json:"workflow_name"`
	TaskType         string            `json:"task_type"`
	RepoType         string            `json:"repo_type"`
	BaseWorkflow     string            `json:"base_workflow"`
	Steps            []WorkflowStep    `json:"steps"`
	PhaseTransitions map[string]string `json:"phase_transitions"`
	Confidence       float64           `json:"confidence"`
	Status           string            `json:"status"`
	SourcePatternID  string            `json:"source_pattern_id"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
}

type BanditState struct {
	CandidatePulls  int     `json:"candidate_pulls"`
	BaselinePulls   int     `json:"baseline_pulls"`
	CandidateReward float64 `json:"candidate_reward"`
	BaselineReward  float64 `json:"baseline_reward"`
}

type WorkflowExperiment struct {
	ID               string      `json:"id"`
	CandidateID      string      `json:"candidate_id"`
	BaselineWorkflow string      `json:"baseline_workflow"`
	TrafficPercent   float64     `json:"traffic_percent"`
	Status           string      `json:"status"`
	SampleSize       int         `json:"sample_size"`
	RunsCandidate    int         `json:"runs_candidate"`
	RunsBaseline     int         `json:"runs_baseline"`
	BanditState      BanditState `json:"bandit_state"`
	StartedAt        time.Time   `json:"started_at"`
	CompletedAt      *time.Time  `json:"completed_at,omitempty"`
	CreatedAt        string      `json:"created_at"`
	UpdatedAt        string      `json:"updated_at"`
}

type ExperimentResult struct {
	ID            int64  `json:"id"`
	ExperimentID  string `json:"experiment_id"`
	WorkflowID    string `json:"workflow_id"`
	UsedCandidate bool   `json:"used_candidate"`
	Success       bool   `json:"success"`
	CycleTimeMin  int    `json:"cycle_time_min"`
	RetryCount    int    `json:"retry_count"`
	ReviewPasses  int    `json:"review_passes"`
	CreatedAt     string `json:"created_at"`
}

type EvaluationMetrics struct {
	SuccessRate    float64 `json:"success_rate"`
	AvgCycleTime   float64 `json:"avg_cycle_time_min"`
	AvgRetryCount  float64 `json:"avg_retry_count"`
	ReviewPassRate float64 `json:"review_pass_rate"`
	SampleSize     int     `json:"sample_size"`
}

type ExperimentEvaluation struct {
	ExperimentID     string            `json:"experiment_id"`
	CandidateMetrics EvaluationMetrics `json:"candidate_metrics"`
	BaselineMetrics  EvaluationMetrics `json:"baseline_metrics"`
	SuccessRateDelta float64           `json:"success_rate_delta"`
	CycleTimeDelta   float64           `json:"cycle_time_delta"`
	ShouldPromote    bool              `json:"should_promote"`
	PromotionReason  string            `json:"promotion_reason,omitempty"`
}

func (d *DB) SaveWorkflowCandidate(c *WorkflowCandidate) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}

	stepsBytes, err := json.Marshal(c.Steps)
	if err != nil {
		return fmt.Errorf("marshal steps: %w", err)
	}
	if c.Steps == nil {
		stepsBytes = []byte("[]")
	}

	transitionsBytes, err := json.Marshal(c.PhaseTransitions)
	if err != nil {
		return fmt.Errorf("marshal transitions: %w", err)
	}
	if c.PhaseTransitions == nil {
		transitionsBytes = []byte("{}")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.sql.Exec(`
		INSERT INTO insight_workflow_candidates 
		(id, workflow_name, task_type, repo_type, base_workflow, steps_json,
		 phase_transitions_json, confidence, status, source_pattern_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			workflow_name = excluded.workflow_name,
			task_type = excluded.task_type,
			repo_type = excluded.repo_type,
			base_workflow = excluded.base_workflow,
			steps_json = excluded.steps_json,
			phase_transitions_json = excluded.phase_transitions_json,
			confidence = excluded.confidence,
			status = excluded.status,
			source_pattern_id = excluded.source_pattern_id,
			updated_at = excluded.updated_at
	`,
		c.ID, c.WorkflowName, c.TaskType, c.RepoType, c.BaseWorkflow,
		string(stepsBytes), string(transitionsBytes), c.Confidence,
		c.Status, c.SourcePatternID, now, now,
	)

	return err
}

func (d *DB) GetWorkflowCandidateByID(id string) (*WorkflowCandidate, error) {
	row := d.sql.QueryRow(`
		SELECT id, workflow_name, task_type, repo_type, base_workflow, steps_json,
		       phase_transitions_json, confidence, status, source_pattern_id, created_at, updated_at
		FROM insight_workflow_candidates
		WHERE id = ?
	`, id)

	return scanWorkflowCandidate(row)
}

func (d *DB) GetWorkflowCandidatesByTaskType(taskType, repoType string) ([]WorkflowCandidate, error) {
	query := `SELECT id, workflow_name, task_type, repo_type, base_workflow, steps_json,
	          phase_transitions_json, confidence, status, source_pattern_id, created_at, updated_at
	          FROM insight_workflow_candidates WHERE task_type = ?`
	args := []any{taskType}

	if repoType != "" {
		query += " AND repo_type = ?"
		args = append(args, repoType)
	}

	query += " ORDER BY confidence DESC LIMIT 20"

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get workflow candidates: %w", err)
	}
	defer rows.Close()

	return scanWorkflowCandidates(rows)
}

func (d *DB) GetCandidateWorkflowForTask(taskType, repoType string) (*WorkflowCandidate, error) {
	row := d.sql.QueryRow(`
		SELECT id, workflow_name, task_type, repo_type, base_workflow, steps_json,
		       phase_transitions_json, confidence, status, source_pattern_id, created_at, updated_at
		FROM insight_workflow_candidates
		WHERE task_type = ? AND (repo_type = ? OR repo_type = '') AND status = 'promoted'
		ORDER BY repo_type DESC, confidence DESC
		LIMIT 1
	`, taskType, repoType)

	return scanWorkflowCandidate(row)
}

func (d *DB) GetExperimentForTask(taskType, repoType string) (*WorkflowExperiment, *WorkflowCandidate, error) {
	row := d.sql.QueryRow(`
		SELECT e.id, e.candidate_id, e.baseline_workflow, e.traffic_percent, e.status,
		       e.sample_size, e.runs_candidate, e.runs_baseline, e.bandit_state_json,
		       e.started_at, e.completed_at, e.created_at, e.updated_at,
		       c.id, c.workflow_name, c.task_type, c.repo_type, c.base_workflow, c.steps_json,
		       c.phase_transitions_json, c.confidence, c.status, c.source_pattern_id, c.created_at, c.updated_at
		FROM insight_workflow_experiments e
		JOIN insight_workflow_candidates c ON e.candidate_id = c.id
		WHERE c.task_type = ? AND (c.repo_type = ? OR c.repo_type = '') AND e.status = 'running'
		ORDER BY c.repo_type DESC, c.confidence DESC
		LIMIT 1
	`, taskType, repoType)

	var e WorkflowExperiment
	var c WorkflowCandidate
	var banditJSON string
	var startedAt string
	var completedAt sql.NullString
	var stepsJSON string
	var transitionsJSON string

	err := row.Scan(
		&e.ID, &e.CandidateID, &e.BaselineWorkflow, &e.TrafficPercent, &e.Status,
		&e.SampleSize, &e.RunsCandidate, &e.RunsBaseline, &banditJSON,
		&startedAt, &completedAt, &e.CreatedAt, &e.UpdatedAt,
		&c.ID, &c.WorkflowName, &c.TaskType, &c.RepoType, &c.BaseWorkflow,
		&stepsJSON, &transitionsJSON, &c.Confidence, &c.Status, &c.SourcePatternID, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("scan experiment: %w", err)
	}

	if banditJSON != "" {
		if err := json.Unmarshal([]byte(banditJSON), &e.BanditState); err != nil {
			return nil, nil, fmt.Errorf("unmarshal bandit state: %w", err)
		}
	}

	if err := unmarshalJSONField("steps", stepsJSON, &c.Steps); err != nil {
		return nil, nil, err
	}
	if err := unmarshalJSONField("phase_transitions", transitionsJSON, &c.PhaseTransitions); err != nil {
		return nil, nil, err
	}

	parsedStartedAt, err := parseRequiredTimeRFC3339Nano("started_at", startedAt)
	if err != nil {
		return nil, nil, err
	}
	e.StartedAt = parsedStartedAt

	parsedCompletedAt, err := parseOptionalTimeRFC3339Nano("completed_at", completedAt)
	if err != nil {
		return nil, nil, err
	}
	e.CompletedAt = parsedCompletedAt

	return &e, &c, nil
}

func (d *DB) UpdateWorkflowCandidateStatus(id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		UPDATE insight_workflow_candidates
		SET status = ?, updated_at = ?
		WHERE id = ?
	`, status, now, id)
	return err
}

func (d *DB) ListWorkflowCandidates(status string, limit int) ([]WorkflowCandidate, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT id, workflow_name, task_type, repo_type, base_workflow, steps_json,
	          phase_transitions_json, confidence, status, source_pattern_id, created_at, updated_at
	          FROM insight_workflow_candidates`
	args := []any{}

	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}

	query += " ORDER BY confidence DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list workflow candidates: %w", err)
	}
	defer rows.Close()

	return scanWorkflowCandidates(rows)
}

func (d *DB) SaveWorkflowExperiment(e *WorkflowExperiment) error {
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

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.sql.Exec(`
		INSERT INTO insight_workflow_experiments 
		(id, candidate_id, baseline_workflow, traffic_percent, status, sample_size,
		 runs_candidate, runs_baseline, bandit_state_json, started_at, completed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			baseline_workflow = excluded.baseline_workflow,
			traffic_percent = excluded.traffic_percent,
			status = excluded.status,
			sample_size = excluded.sample_size,
			runs_candidate = excluded.runs_candidate,
			runs_baseline = excluded.runs_baseline,
			bandit_state_json = excluded.bandit_state_json,
			completed_at = excluded.completed_at,
			updated_at = excluded.updated_at
	`,
		e.ID, e.CandidateID, e.BaselineWorkflow, e.TrafficPercent, e.Status,
		e.SampleSize, e.RunsCandidate, e.RunsBaseline, string(banditBytes),
		e.StartedAt.UTC().Format(time.RFC3339Nano), completedAt, now, now,
	)

	return err
}

func (d *DB) GetWorkflowExperimentByID(id string) (*WorkflowExperiment, error) {
	row := d.sql.QueryRow(`
		SELECT id, candidate_id, baseline_workflow, traffic_percent, status, sample_size,
		       runs_candidate, runs_baseline, bandit_state_json, started_at, completed_at, created_at, updated_at
		FROM insight_workflow_experiments
		WHERE id = ?
	`, id)

	return scanWorkflowExperiment(row)
}

func (d *DB) ListRunningExperiments() ([]WorkflowExperiment, error) {
	rows, err := d.sql.Query(`
		SELECT id, candidate_id, baseline_workflow, traffic_percent, status, sample_size,
		       runs_candidate, runs_baseline, bandit_state_json, started_at, completed_at, created_at, updated_at
		FROM insight_workflow_experiments
		WHERE status = 'running'
		ORDER BY started_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list running experiments: %w", err)
	}
	defer rows.Close()

	return scanWorkflowExperiments(rows)
}

func (d *DB) UpdateExperimentBandit(id string, bandit BanditState, runsCandidate, runsBaseline int) error {
	banditBytes, err := json.Marshal(bandit)
	if err != nil {
		return fmt.Errorf("marshal bandit state: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.sql.Exec(`
		UPDATE insight_workflow_experiments
		SET bandit_state_json = ?, runs_candidate = ?, runs_baseline = ?, updated_at = ?
		WHERE id = ?
	`, string(banditBytes), runsCandidate, runsBaseline, now, id)
	return err
}

func (d *DB) UpdateExperimentStatus(id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var completedAt any
	if status == "completed" || status == "aborted" {
		completedAt = now
	} else {
		completedAt = nil
	}

	_, err := d.sql.Exec(`
		UPDATE insight_workflow_experiments
		SET status = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`, status, completedAt, now, id)
	return err
}

func (d *DB) SaveExperimentResult(r *ExperimentResult) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	usedCandidate := 0
	if r.UsedCandidate {
		usedCandidate = 1
	}
	success := 0
	if r.Success {
		success = 1
	}

	result, err := d.sql.Exec(`
		INSERT INTO insight_experiment_results 
		(experiment_id, workflow_id, used_candidate, success, cycle_time_min, retry_count, review_passes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ExperimentID, r.WorkflowID, usedCandidate, success,
		r.CycleTimeMin, r.RetryCount, r.ReviewPasses, now,
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

func (d *DB) GetExperimentResults(experimentID string) ([]ExperimentResult, error) {
	rows, err := d.sql.Query(`
		SELECT id, experiment_id, workflow_id, used_candidate, success, cycle_time_min, retry_count, review_passes, created_at
		FROM insight_experiment_results
		WHERE experiment_id = ?
		ORDER BY created_at DESC
	`, experimentID)
	if err != nil {
		return nil, fmt.Errorf("get experiment results: %w", err)
	}
	defer rows.Close()

	return scanExperimentResults(rows)
}

func (d *DB) GetExperimentMetrics(experimentID string) (candidate, baseline EvaluationMetrics, err error) {
	rows, err := d.sql.Query(`
		SELECT used_candidate, success, cycle_time_min, retry_count, review_passes
		FROM insight_experiment_results
		WHERE experiment_id = ?
	`, experimentID)
	if err != nil {
		return candidate, baseline, fmt.Errorf("get experiment metrics: %w", err)
	}
	defer rows.Close()

	var candCount, baseCount int
	var candSuccess, baseSuccess int
	var candCycleTime, baseCycleTime int
	var candRetries, baseRetries int
	var candReviewPasses, baseReviewPasses int

	for rows.Next() {
		var usedCandidate bool
		var success bool
		var cycleTime, retries, reviewPasses int

		err := rows.Scan(&usedCandidate, &success, &cycleTime, &retries, &reviewPasses)
		if err != nil {
			return candidate, baseline, err
		}

		if usedCandidate {
			candCount++
			if success {
				candSuccess++
			}
			candCycleTime += cycleTime
			candRetries += retries
			candReviewPasses += reviewPasses
		} else {
			baseCount++
			if success {
				baseSuccess++
			}
			baseCycleTime += cycleTime
			baseRetries += retries
			baseReviewPasses += reviewPasses
		}
	}

	if candCount > 0 {
		candidate.SampleSize = candCount
		candidate.SuccessRate = float64(candSuccess) / float64(candCount)
		candidate.AvgCycleTime = float64(candCycleTime) / float64(candCount)
		candidate.AvgRetryCount = float64(candRetries) / float64(candCount)
		if candRetries > 0 {
			candidate.ReviewPassRate = float64(candReviewPasses) / float64(candRetries)
		} else {
			candidate.ReviewPassRate = 1.0
		}
	}

	if baseCount > 0 {
		baseline.SampleSize = baseCount
		baseline.SuccessRate = float64(baseSuccess) / float64(baseCount)
		baseline.AvgCycleTime = float64(baseCycleTime) / float64(baseCount)
		baseline.AvgRetryCount = float64(baseRetries) / float64(baseCount)
		if baseRetries > 0 {
			baseline.ReviewPassRate = float64(baseReviewPasses) / float64(baseRetries)
		} else {
			baseline.ReviewPassRate = 1.0
		}
	}

	return candidate, baseline, nil
}

func (d *DB) DeleteWorkflowCandidate(id string) error {
	_, err := d.sql.Exec("DELETE FROM insight_workflow_candidates WHERE id = ?", id)
	return err
}

func (d *DB) DeleteWorkflowExperiment(id string) error {
	_, err := d.sql.Exec("DELETE FROM insight_workflow_experiments WHERE id = ?", id)
	return err
}

func scanWorkflowCandidate(row *sql.Row) (*WorkflowCandidate, error) {
	var c WorkflowCandidate
	var stepsJSON, transitionsJSON string

	err := row.Scan(
		&c.ID, &c.WorkflowName, &c.TaskType, &c.RepoType, &c.BaseWorkflow,
		&stepsJSON, &transitionsJSON, &c.Confidence, &c.Status,
		&c.SourcePatternID, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan workflow candidate: %w", err)
	}

	if err := unmarshalJSONField("steps", stepsJSON, &c.Steps); err != nil {
		return nil, err
	}

	if err := unmarshalJSONField("phase_transitions", transitionsJSON, &c.PhaseTransitions); err != nil {
		return nil, err
	}

	return &c, nil
}

func scanWorkflowCandidates(rows *sql.Rows) ([]WorkflowCandidate, error) {
	var candidates []WorkflowCandidate
	for rows.Next() {
		var c WorkflowCandidate
		var stepsJSON, transitionsJSON string

		err := rows.Scan(
			&c.ID, &c.WorkflowName, &c.TaskType, &c.RepoType, &c.BaseWorkflow,
			&stepsJSON, &transitionsJSON, &c.Confidence, &c.Status,
			&c.SourcePatternID, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan workflow candidate row: %w", err)
		}

		if err := unmarshalJSONField("steps", stepsJSON, &c.Steps); err != nil {
			return nil, err
		}

		if err := unmarshalJSONField("phase_transitions", transitionsJSON, &c.PhaseTransitions); err != nil {
			return nil, err
		}

		candidates = append(candidates, c)
	}

	if candidates == nil {
		candidates = []WorkflowCandidate{}
	}

	return candidates, nil
}

func scanWorkflowExperiment(row *sql.Row) (*WorkflowExperiment, error) {
	var e WorkflowExperiment
	var banditJSON string
	var startedAt string
	var completedAt sql.NullString

	err := row.Scan(
		&e.ID, &e.CandidateID, &e.BaselineWorkflow, &e.TrafficPercent, &e.Status,
		&e.SampleSize, &e.RunsCandidate, &e.RunsBaseline, &banditJSON,
		&startedAt, &completedAt, &e.CreatedAt, &e.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan workflow experiment: %w", err)
	}

	if banditJSON != "" {
		if err := json.Unmarshal([]byte(banditJSON), &e.BanditState); err != nil {
			return nil, fmt.Errorf("unmarshal bandit state: %w", err)
		}
	}

	parsedStartedAt, err := parseRequiredTimeRFC3339Nano("started_at", startedAt)
	if err != nil {
		return nil, err
	}
	e.StartedAt = parsedStartedAt

	parsedCompletedAt, err := parseOptionalTimeRFC3339Nano("completed_at", completedAt)
	if err != nil {
		return nil, err
	}
	e.CompletedAt = parsedCompletedAt

	return &e, nil
}

func scanWorkflowExperiments(rows *sql.Rows) ([]WorkflowExperiment, error) {
	var experiments []WorkflowExperiment
	for rows.Next() {
		var e WorkflowExperiment
		var banditJSON string
		var startedAt string
		var completedAt sql.NullString

		err := rows.Scan(
			&e.ID, &e.CandidateID, &e.BaselineWorkflow, &e.TrafficPercent, &e.Status,
			&e.SampleSize, &e.RunsCandidate, &e.RunsBaseline, &banditJSON,
			&startedAt, &completedAt, &e.CreatedAt, &e.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan workflow experiment row: %w", err)
		}

		if banditJSON != "" {
			if err := json.Unmarshal([]byte(banditJSON), &e.BanditState); err != nil {
				return nil, fmt.Errorf("unmarshal bandit state: %w", err)
			}
		}

		parsedStartedAt, err := parseRequiredTimeRFC3339Nano("started_at", startedAt)
		if err != nil {
			return nil, err
		}
		e.StartedAt = parsedStartedAt

		parsedCompletedAt, err := parseOptionalTimeRFC3339Nano("completed_at", completedAt)
		if err != nil {
			return nil, err
		}
		e.CompletedAt = parsedCompletedAt

		experiments = append(experiments, e)
	}

	if experiments == nil {
		experiments = []WorkflowExperiment{}
	}

	return experiments, nil
}

func scanExperimentResults(rows *sql.Rows) ([]ExperimentResult, error) {
	var results []ExperimentResult
	for rows.Next() {
		var r ExperimentResult
		var usedCandidate, success int

		err := rows.Scan(
			&r.ID, &r.ExperimentID, &r.WorkflowID, &usedCandidate, &success,
			&r.CycleTimeMin, &r.RetryCount, &r.ReviewPasses, &r.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan experiment result row: %w", err)
		}

		r.UsedCandidate = usedCandidate == 1
		r.Success = success == 1
		results = append(results, r)
	}

	if results == nil {
		results = []ExperimentResult{}
	}

	return results, nil
}
