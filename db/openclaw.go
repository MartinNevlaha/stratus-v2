package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

type OpenClawState struct {
	ID                 int     `json:"id"`
	LastAnalysis       string  `json:"last_analysis"`
	NextAnalysis       string  `json:"next_analysis"`
	PatternsDetected   int     `json:"patterns_detected"`
	ProposalsGenerated int     `json:"proposals_generated"`
	ProposalsAccepted  int     `json:"proposals_accepted"`
	AcceptanceRate     float64 `json:"acceptance_rate"`
	ModelVersion       string  `json:"model_version"`
	ConfigJSON         string  `json:"config_json"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

type OpenClawPattern struct {
	ID          int                    `json:"id"`
	PatternType string                 `json:"pattern_type"`
	PatternName string                 `json:"pattern_name"`
	Description string                 `json:"description"`
	Frequency   int                    `json:"frequency"`
	Confidence  float64                `json:"confidence"`
	Examples    []string               `json:"examples"`
	Metadata    map[string]interface{} `json:"metadata"`
	LastSeen    string                 `json:"last_seen"`
	FirstSeen   string                 `json:"first_seen"`
	CreatedAt   string                 `json:"created_at"`
}

type OpenClawFeedback struct {
	ID           int     `json:"id"`
	ProposalID   string  `json:"proposal_id"`
	FeedbackType string  `json:"feedback_type"`
	Reason       string  `json:"reason"`
	ImpactScore  float64 `json:"impact_score"`
	MeasuredAt   string  `json:"measured_at"`
	CreatedAt    string  `json:"created_at"`
}

type OpenClawAnalysis struct {
	ID               int                    `json:"id"`
	AnalysisType     string                 `json:"analysis_type"`
	Scope            string                 `json:"scope"`
	Findings         map[string]interface{} `json:"findings"`
	Recommendations  map[string]interface{} `json:"recommendations"`
	PatternsFound    int                    `json:"patterns_found"`
	ProposalsCreated int                    `json:"proposals_created"`
	ExecutionTimeMs  int                    `json:"execution_time_ms"`
	CreatedAt        string                 `json:"created_at"`
}

func (d *DB) GetOpenClawState() (*OpenClawState, error) {
	row := d.sql.QueryRow(`
		SELECT id, last_analysis, next_analysis, patterns_detected, proposals_generated, 
		       proposals_accepted, acceptance_rate, model_version, config_json, created_at, updated_at
		FROM openclaw_state
		ORDER BY id DESC
		LIMIT 1
	`)

	var state OpenClawState
	var configJSON sql.NullString
	err := row.Scan(
		&state.ID, &state.LastAnalysis, &state.NextAnalysis, &state.PatternsDetected,
		&state.ProposalsGenerated, &state.ProposalsAccepted, &state.AcceptanceRate, &state.ModelVersion,
		&configJSON, &state.CreatedAt, &state.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if configJSON.Valid {
		state.ConfigJSON = configJSON.String
	} else {
		state.ConfigJSON = "{}"
	}

	return &state, nil
}

func (d *DB) SaveOpenClawState(state *OpenClawState) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := d.sql.Exec(`
		INSERT INTO openclaw_state 
		(last_analysis, next_analysis, patterns_detected, proposals_generated, proposals_accepted, acceptance_rate, model_version, config_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		state.LastAnalysis, state.NextAnalysis, state.PatternsDetected,
		state.ProposalsGenerated, state.ProposalsAccepted, state.AcceptanceRate, state.ModelVersion,
		state.ConfigJSON, now, now,
	)

	if err != nil {
		return fmt.Errorf("save openclaw state: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}

	state.ID = int(id)
	return nil
}

func (d *DB) UpdateOpenClawState(state *OpenClawState) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		UPDATE openclaw_state
		SET last_analysis = ?, next_analysis = ?, patterns_detected = ?, 
		    proposals_generated = ?, proposals_accepted = ?, acceptance_rate = ?, 
		    config_json = ?, updated_at = ?
		WHERE id = ?
	`,
		state.LastAnalysis, state.NextAnalysis, state.PatternsDetected,
		state.ProposalsGenerated, state.ProposalsAccepted, state.AcceptanceRate,
		state.ConfigJSON, now,
		state.ID,
	)

	return err
}

func (d *DB) SaveOpenClawPattern(pattern *OpenClawPattern) error {
	examplesBytes, _ := json.Marshal(pattern.Examples)
	metadataBytes, _ := json.Marshal(pattern.Metadata)
	if pattern.Examples == nil {
		examplesBytes = []byte("[]")
	}
	if pattern.Metadata == nil {
		metadataBytes = []byte("{}")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		INSERT INTO openclaw_patterns 
		(pattern_type, pattern_name, description, frequency, confidence, examples_json, metadata_json, last_seen, first_seen, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		pattern.PatternType, pattern.PatternName, pattern.Description, pattern.Frequency, pattern.Confidence,
		string(examplesBytes), string(metadataBytes),
		now, now, now,
	)

	return err
}

func (d *DB) ListOpenClawPatterns(patternType string, minConfidence float64, limit int) ([]OpenClawPattern, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows *sql.Rows
	var err error
	if patternType != "" {
		rows, err = d.sql.Query(`
			SELECT id, pattern_type, pattern_name, description, frequency, confidence, examples_json, metadata_json, last_seen, first_seen, created_at
			FROM openclaw_patterns
			WHERE pattern_type = ? AND confidence >= ?
			ORDER BY confidence DESC
			LIMIT ?
		`, patternType, minConfidence, limit)
	} else {
		rows, err = d.sql.Query(`
			SELECT id, pattern_type, pattern_name, description, frequency, confidence, examples_json, metadata_json, last_seen, first_seen, created_at
			FROM openclaw_patterns
			WHERE confidence >= ?
			ORDER BY confidence DESC
			LIMIT ?
		`, minConfidence, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patterns []OpenClawPattern
	for rows.Next() {
		var p OpenClawPattern
		var examplesJSON, metadataJSON string
		if err := rows.Scan(
			&p.ID, &p.PatternType, &p.PatternName, &p.Description, &p.Frequency, &p.Confidence,
			&examplesJSON, &metadataJSON, &p.LastSeen, &p.FirstSeen, &p.CreatedAt,
		); err != nil {
			return nil, err
		}

		if examplesJSON != "" {
			json.Unmarshal([]byte(examplesJSON), &p.Examples)
		}
		if metadataJSON != "" {
			json.Unmarshal([]byte(metadataJSON), &p.Metadata)
		}

		patterns = append(patterns, p)
	}

	return patterns, nil
}

func (d *DB) SaveOpenClawFeedback(feedback *OpenClawFeedback) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		INSERT INTO openclaw_feedback 
		(proposal_id, feedback_type, reason, impact_score, measured_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		feedback.ProposalID, feedback.FeedbackType, feedback.Reason, feedback.ImpactScore, feedback.MeasuredAt, now,
	)

	return err
}

func (d *DB) SaveOpenClawAnalysis(analysis *OpenClawAnalysis) error {
	findingsBytes, _ := json.Marshal(analysis.Findings)
	recommendationsBytes, _ := json.Marshal(analysis.Recommendations)
	if analysis.Findings == nil {
		findingsBytes = []byte("{}")
	}
	if analysis.Recommendations == nil {
		recommendationsBytes = []byte("{}")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		INSERT INTO openclaw_analyses 
		(analysis_type, scope, findings_json, recommendations_json, patterns_found, proposals_created, execution_time_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		analysis.AnalysisType, analysis.Scope, string(findingsBytes), string(recommendationsBytes),
		analysis.PatternsFound, analysis.ProposalsCreated, analysis.ExecutionTimeMs, now,
	)

	return err
}

func (d *DB) ListOpenClawAnalyses(analysisType string, limit int) ([]OpenClawAnalysis, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows *sql.Rows
	var err error
	if analysisType != "" {
		rows, err = d.sql.Query(`
			SELECT id, analysis_type, scope, findings_json, recommendations_json, patterns_found, proposals_created, execution_time_ms, created_at
			FROM openclaw_analyses
			WHERE analysis_type = ?
			ORDER BY created_at DESC
			LIMIT ?
		`, analysisType, limit)
	} else {
		rows, err = d.sql.Query(`
			SELECT id, analysis_type, scope, findings_json, recommendations_json, patterns_found, proposals_created, execution_time_ms, created_at
			FROM openclaw_analyses
			ORDER BY created_at DESC
			LIMIT ?
		`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var analyses []OpenClawAnalysis
	for rows.Next() {
		var a OpenClawAnalysis
		var findingsJSON, recommendationsJSON string
		if err := rows.Scan(
			&a.ID, &a.AnalysisType, &a.Scope, &findingsJSON, &recommendationsJSON,
			&a.PatternsFound, &a.ProposalsCreated, &a.ExecutionTimeMs, &a.CreatedAt,
		); err != nil {
			return nil, err
		}

		if findingsJSON != "" {
			if err := json.Unmarshal([]byte(findingsJSON), &a.Findings); err != nil {
				log.Printf("warning: failed to parse findings for analysis %d: %v", a.ID, err)
			}
		}
		if recommendationsJSON != "" {
			if err := json.Unmarshal([]byte(recommendationsJSON), &a.Recommendations); err != nil {
				log.Printf("warning: failed to parse recommendations for analysis %d: %v", a.ID, err)
			}
		}

		analyses = append(analyses, a)
	}

	return analyses, nil
}

func (d *DB) GetOpenClawMetrics() (map[string]any, error) {
	state, err := d.GetOpenClawState()
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}

	patterns, err := d.ListOpenClawPatterns("", 0, 100)
	if err != nil {
		return nil, err
	}

	var proposalsCount int
	err = d.sql.QueryRow(`
		SELECT COUNT(*) 
		FROM proposals 
		WHERE created_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-24 hours')
	`).Scan(&proposalsCount)
	if err != nil {
		return nil, err
	}

	var acceptedCount int
	err = d.sql.QueryRow(`
		SELECT COUNT(*) 
		FROM proposals 
		WHERE status = 'accepted' 
		  AND created_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-24 hours')
	`).Scan(&acceptedCount)
	if err != nil {
		return nil, err
	}

	metrics := map[string]any{
		"state":           state,
		"patterns_total":  len(patterns),
		"proposals_today": proposalsCount,
		"accepted_today":  acceptedCount,
	}

	return metrics, nil
}
