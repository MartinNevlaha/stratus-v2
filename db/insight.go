package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

type InsightState struct {
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

type InsightPattern struct {
	ID          int                    `json:"id"`
	PatternType string                 `json:"pattern_type"`
	PatternName string                 `json:"pattern_name"`
	Description string                 `json:"description"`
	Frequency   int                    `json:"frequency"`
	Confidence  float64                `json:"confidence"`
	Examples    []string               `json:"examples"`
	Metadata    map[string]interface{} `json:"metadata"`
	Severity    string                 `json:"severity"`
	Evidence    map[string]interface{} `json:"evidence"`
	LastSeen    string                 `json:"last_seen"`
	FirstSeen   string                 `json:"first_seen"`
	CreatedAt   string                 `json:"created_at"`
}

type InsightFeedback struct {
	ID           int     `json:"id"`
	ProposalID   string  `json:"proposal_id"`
	FeedbackType string  `json:"feedback_type"`
	Reason       string  `json:"reason"`
	ImpactScore  float64 `json:"impact_score"`
	MeasuredAt   string  `json:"measured_at"`
	CreatedAt    string  `json:"created_at"`
}

type InsightAnalysis struct {
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

type InsightProposal struct {
	ID              string                 `json:"id"`
	Type            string                 `json:"type"`
	Status          string                 `json:"status"`
	Title           string                 `json:"title"`
	Description     string                 `json:"description"`
	Confidence      float64                `json:"confidence"`
	RiskLevel       string                 `json:"risk_level"`
	SourcePatternID string                 `json:"source_pattern_id"`
	Evidence        map[string]interface{} `json:"evidence"`
	Recommendation  map[string]interface{} `json:"recommendation"`
	DecisionReason  string                 `json:"decision_reason,omitempty"`
	CreatedAt       string                 `json:"created_at"`
	UpdatedAt       string                 `json:"updated_at"`
}

func (d *DB) GetInsightState() (*InsightState, error) {
	row := d.sql.QueryRow(`
		SELECT id, last_analysis, next_analysis, patterns_detected, proposals_generated, 
		       proposals_accepted, acceptance_rate, model_version, config_json, created_at, updated_at
		FROM insight_state
		ORDER BY id DESC
		LIMIT 1
	`)

	var state InsightState
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

func (d *DB) SaveInsightState(state *InsightState) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := d.sql.Exec(`
		INSERT INTO insight_state 
		(last_analysis, next_analysis, patterns_detected, proposals_generated, proposals_accepted, acceptance_rate, model_version, config_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		state.LastAnalysis, state.NextAnalysis, state.PatternsDetected,
		state.ProposalsGenerated, state.ProposalsAccepted, state.AcceptanceRate, state.ModelVersion,
		state.ConfigJSON, now, now,
	)

	if err != nil {
		return fmt.Errorf("save insight state: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}

	state.ID = int(id)
	return nil
}

func (d *DB) UpdateInsightState(state *InsightState) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		UPDATE insight_state
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

func (d *DB) SaveInsightPattern(pattern *InsightPattern) error {
	examplesBytes, err := json.Marshal(pattern.Examples)
	if err != nil {
		return fmt.Errorf("marshal examples: %w", err)
	}
	metadataBytes, err := json.Marshal(pattern.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	evidenceBytes, err := json.Marshal(pattern.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	if pattern.Examples == nil {
		examplesBytes = []byte("[]")
	}
	if pattern.Metadata == nil {
		metadataBytes = []byte("{}")
	}
	if pattern.Evidence == nil {
		evidenceBytes = []byte("{}")
	}

	severity := pattern.Severity
	if severity == "" {
		severity = "medium"
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.sql.Exec(`
		INSERT INTO insight_patterns 
		(pattern_type, pattern_name, description, frequency, confidence, examples_json, metadata_json, severity, evidence_json, last_seen, first_seen, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		pattern.PatternType, pattern.PatternName, pattern.Description, pattern.Frequency, pattern.Confidence,
		string(examplesBytes), string(metadataBytes), severity, string(evidenceBytes),
		now, now, now,
	)

	return err
}

func (d *DB) FindPatternByName(patternName string) (*InsightPattern, error) {
	row := d.sql.QueryRow(`
		SELECT id, pattern_type, pattern_name, description, frequency, confidence, 
		       examples_json, metadata_json, severity, evidence_json, last_seen, first_seen, created_at
		FROM insight_patterns
		WHERE pattern_name = ?
		ORDER BY id DESC
		LIMIT 1
	`, patternName)

	var p InsightPattern
	var examplesJSON, metadataJSON, evidenceJSON string
	var severity sql.NullString
	err := row.Scan(
		&p.ID, &p.PatternType, &p.PatternName, &p.Description, &p.Frequency, &p.Confidence,
		&examplesJSON, &metadataJSON, &severity, &evidenceJSON, &p.LastSeen, &p.FirstSeen, &p.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if severity.Valid {
		p.Severity = severity.String
	} else {
		p.Severity = "medium"
	}
	if examplesJSON != "" {
		if err := json.Unmarshal([]byte(examplesJSON), &p.Examples); err != nil {
			log.Printf("warning: failed to parse examples for pattern %d: %v", p.ID, err)
		}
	}
	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &p.Metadata); err != nil {
			log.Printf("warning: failed to parse metadata for pattern %d: %v", p.ID, err)
		}
	}
	if evidenceJSON != "" {
		if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
			log.Printf("warning: failed to parse evidence for pattern %d: %v", p.ID, err)
		}
	}

	return &p, nil
}

func (d *DB) UpdateInsightPattern(pattern *InsightPattern) error {
	examplesBytes, err := json.Marshal(pattern.Examples)
	if err != nil {
		return fmt.Errorf("marshal examples: %w", err)
	}
	metadataBytes, err := json.Marshal(pattern.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	evidenceBytes, err := json.Marshal(pattern.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	if pattern.Examples == nil {
		examplesBytes = []byte("[]")
	}
	if pattern.Metadata == nil {
		metadataBytes = []byte("{}")
	}
	if pattern.Evidence == nil {
		evidenceBytes = []byte("{}")
	}

	severity := pattern.Severity
	if severity == "" {
		severity = "medium"
	}

	_, err = d.sql.Exec(`
		UPDATE insight_patterns
		SET description = ?, frequency = ?, confidence = ?, examples_json = ?, 
		    metadata_json = ?, severity = ?, evidence_json = ?, last_seen = ?
		WHERE id = ?
	`,
		pattern.Description, pattern.Frequency, pattern.Confidence,
		string(examplesBytes), string(metadataBytes), severity, string(evidenceBytes),
		pattern.LastSeen,
		pattern.ID,
	)

	return err
}

func (d *DB) ListInsightPatterns(patternType string, severity string, minConfidence float64, limit int) ([]InsightPattern, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows *sql.Rows
	var err error

	query := `
		SELECT id, pattern_type, pattern_name, description, frequency, confidence, examples_json, metadata_json, severity, evidence_json, last_seen, first_seen, created_at
		FROM insight_patterns
		WHERE confidence >= ?
	`
	args := []interface{}{minConfidence}

	if patternType != "" {
		query += " AND pattern_type = ?"
		args = append(args, patternType)
	}
	if severity != "" {
		query += " AND severity = ?"
		args = append(args, severity)
	}

	query += " ORDER BY confidence DESC LIMIT ?"
	args = append(args, limit)

	rows, err = d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patterns []InsightPattern
	for rows.Next() {
		var p InsightPattern
		var examplesJSON, metadataJSON, evidenceJSON string
		var severityVal sql.NullString
		if err := rows.Scan(
			&p.ID, &p.PatternType, &p.PatternName, &p.Description, &p.Frequency, &p.Confidence,
			&examplesJSON, &metadataJSON, &severityVal, &evidenceJSON, &p.LastSeen, &p.FirstSeen, &p.CreatedAt,
		); err != nil {
			return nil, err
		}

		if severityVal.Valid {
			p.Severity = severityVal.String
		} else {
			p.Severity = "medium"
		}
		if examplesJSON != "" {
			if err := json.Unmarshal([]byte(examplesJSON), &p.Examples); err != nil {
				log.Printf("warning: failed to parse examples for pattern %d: %v", p.ID, err)
			}
		}
		if metadataJSON != "" {
			if err := json.Unmarshal([]byte(metadataJSON), &p.Metadata); err != nil {
				log.Printf("warning: failed to parse metadata for pattern %d: %v", p.ID, err)
			}
		}
		if evidenceJSON != "" {
			if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
				log.Printf("warning: failed to parse evidence for pattern %d: %v", p.ID, err)
			}
		}

		patterns = append(patterns, p)
	}

	return patterns, nil
}

func (d *DB) SaveInsightFeedback(feedback *InsightFeedback) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		INSERT INTO insight_feedback 
		(proposal_id, feedback_type, reason, impact_score, measured_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		feedback.ProposalID, feedback.FeedbackType, feedback.Reason, feedback.ImpactScore, feedback.MeasuredAt, now,
	)

	return err
}

func (d *DB) SaveInsightAnalysis(analysis *InsightAnalysis) error {
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
		INSERT INTO insight_analyses 
		(analysis_type, scope, findings_json, recommendations_json, patterns_found, proposals_created, execution_time_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		analysis.AnalysisType, analysis.Scope, string(findingsBytes), string(recommendationsBytes),
		analysis.PatternsFound, analysis.ProposalsCreated, analysis.ExecutionTimeMs, now,
	)

	return err
}

func (d *DB) ListInsightAnalyses(analysisType string, limit int) ([]InsightAnalysis, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows *sql.Rows
	var err error
	if analysisType != "" {
		rows, err = d.sql.Query(`
			SELECT id, analysis_type, scope, findings_json, recommendations_json, patterns_found, proposals_created, execution_time_ms, created_at
			FROM insight_analyses
			WHERE analysis_type = ?
			ORDER BY created_at DESC
			LIMIT ?
		`, analysisType, limit)
	} else {
		rows, err = d.sql.Query(`
			SELECT id, analysis_type, scope, findings_json, recommendations_json, patterns_found, proposals_created, execution_time_ms, created_at
			FROM insight_analyses
			ORDER BY created_at DESC
			LIMIT ?
		`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var analyses []InsightAnalysis
	for rows.Next() {
		var a InsightAnalysis
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

func (d *DB) GetInsightMetrics() (map[string]any, error) {
	state, err := d.GetInsightState()
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}

	patterns, err := d.ListInsightPatterns("", "", 0, 100)
	if err != nil {
		return nil, err
	}

	var proposalsCount int
	err = d.sql.QueryRow(`
		SELECT COUNT(*) 
		FROM insight_proposals 
		WHERE created_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-24 hours')
	`).Scan(&proposalsCount)
	if err != nil {
		return nil, err
	}

	var acceptedCount int
	err = d.sql.QueryRow(`
		SELECT COUNT(*) 
		FROM insight_proposals 
		WHERE status = 'approved' 
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

func (d *DB) SaveInsightProposal(proposal *InsightProposal) error {
	evidenceBytes, err := json.Marshal(proposal.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	recommendationBytes, err := json.Marshal(proposal.Recommendation)
	if err != nil {
		return fmt.Errorf("marshal recommendation: %w", err)
	}
	if proposal.Evidence == nil {
		evidenceBytes = []byte("{}")
	}
	if proposal.Recommendation == nil {
		recommendationBytes = []byte("{}")
	}

	var decisionReason interface{}
	if proposal.DecisionReason != "" {
		decisionReason = proposal.DecisionReason
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.sql.Exec(`
		INSERT INTO insight_proposals 
		(id, type, status, title, description, confidence, risk_level, 
		 source_pattern_id, evidence, recommendation, decision_reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		proposal.ID, proposal.Type, proposal.Status, proposal.Title,
		proposal.Description, proposal.Confidence, proposal.RiskLevel,
		proposal.SourcePatternID, string(evidenceBytes), string(recommendationBytes),
		decisionReason, now, now,
	)

	return err
}

func (d *DB) GetInsightProposalByID(id string) (*InsightProposal, error) {
	row := d.sql.QueryRow(`
		SELECT id, type, status, title, description, confidence, risk_level,
		       source_pattern_id, evidence, recommendation, decision_reason, created_at, updated_at
		FROM insight_proposals
		WHERE id = ?
	`, id)

	var p InsightProposal
	var evidenceJSON, recommendationJSON string
	var decisionReason sql.NullString
	err := row.Scan(
		&p.ID, &p.Type, &p.Status, &p.Title, &p.Description, &p.Confidence,
		&p.RiskLevel, &p.SourcePatternID, &evidenceJSON, &recommendationJSON,
		&decisionReason, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if decisionReason.Valid {
		p.DecisionReason = decisionReason.String
	}
	if evidenceJSON != "" {
		if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
			log.Printf("warning: failed to parse evidence for proposal %s: %v", p.ID, err)
		}
	}
	if recommendationJSON != "" {
		if err := json.Unmarshal([]byte(recommendationJSON), &p.Recommendation); err != nil {
			log.Printf("warning: failed to parse recommendation for proposal %s: %v", p.ID, err)
		}
	}

	return &p, nil
}

func (d *DB) ListInsightProposals(proposalType string, status string, riskLevel string, minConfidence float64, limit int, offset int) ([]InsightProposal, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var rows *sql.Rows
	var err error

	query := `
		SELECT id, type, status, title, description, confidence, risk_level,
		       source_pattern_id, evidence, recommendation, decision_reason, created_at, updated_at
		FROM insight_proposals
		WHERE confidence >= ?
	`
	args := []interface{}{minConfidence}

	if proposalType != "" {
		query += " AND type = ?"
		args = append(args, proposalType)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	if riskLevel != "" {
		query += " AND risk_level = ?"
		args = append(args, riskLevel)
	}

	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err = d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proposals []InsightProposal
	for rows.Next() {
		var p InsightProposal
		var evidenceJSON, recommendationJSON string
		var decisionReason sql.NullString
		if err := rows.Scan(
			&p.ID, &p.Type, &p.Status, &p.Title, &p.Description, &p.Confidence,
			&p.RiskLevel, &p.SourcePatternID, &evidenceJSON, &recommendationJSON,
			&decisionReason, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if decisionReason.Valid {
			p.DecisionReason = decisionReason.String
		}
		if evidenceJSON != "" {
			if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
				log.Printf("warning: failed to parse evidence for proposal %s: %v", p.ID, err)
			}
		}
		if recommendationJSON != "" {
			if err := json.Unmarshal([]byte(recommendationJSON), &p.Recommendation); err != nil {
				log.Printf("warning: failed to parse recommendation for proposal %s: %v", p.ID, err)
			}
		}

		proposals = append(proposals, p)
	}

	return proposals, nil
}

func (d *DB) FindSimilarInsightProposal(proposalType string, sourcePatternID string, affectedEntity string, withinHours int) (*InsightProposal, error) {
	row := d.sql.QueryRow(`
		SELECT id, type, status, title, description, confidence, risk_level,
		       source_pattern_id, evidence, recommendation, decision_reason, created_at, updated_at
		FROM insight_proposals
		WHERE type = ?
		  AND source_pattern_id = ?
		  AND created_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-' || ? || ' hours')
		  AND (
		      json_extract(evidence, '$.affected_workflow') = ?
		      OR json_extract(evidence, '$.agent_id') = ?
		  )
		ORDER BY created_at DESC
		LIMIT 1
	`, proposalType, sourcePatternID, withinHours, affectedEntity, affectedEntity)

	var p InsightProposal
	var evidenceJSON, recommendationJSON string
	var decisionReason sql.NullString
	err := row.Scan(
		&p.ID, &p.Type, &p.Status, &p.Title, &p.Description, &p.Confidence,
		&p.RiskLevel, &p.SourcePatternID, &evidenceJSON, &recommendationJSON,
		&decisionReason, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if decisionReason.Valid {
		p.DecisionReason = decisionReason.String
	}
	if evidenceJSON != "" {
		if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
			log.Printf("warning: failed to parse evidence for proposal %s: %v", p.ID, err)
		}
	}
	if recommendationJSON != "" {
		if err := json.Unmarshal([]byte(recommendationJSON), &p.Recommendation); err != nil {
			log.Printf("warning: failed to parse recommendation for proposal %s: %v", p.ID, err)
		}
	}

	return &p, nil
}

func (d *DB) UpdateInsightProposalStatus(id string, status string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		UPDATE insight_proposals
		SET status = ?, updated_at = ?
		WHERE id = ?
	`, status, now, id)

	return err
}

func (d *DB) UpdateInsightProposalStatusWithReason(id string, status string, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`
		UPDATE insight_proposals
		SET status = ?, decision_reason = ?, updated_at = ?
		WHERE id = ?
	`, status, reason, now, id)

	return err
}

type InsightDashboardSummary struct {
	RecentProposals      int            `json:"recent_proposals"`
	RecentPatterns       int            `json:"recent_patterns"`
	ProposalsByStatus    map[string]int `json:"proposals_by_status"`
	PatternsBySeverity   map[string]int `json:"patterns_by_severity"`
	TopAffectedWorkflows []string       `json:"top_affected_workflows"`
	TopAffectedAgents    []string       `json:"top_affected_agents"`
	TimeWindowHours      int            `json:"time_window_hours"`
}

func (d *DB) GetInsightDashboardSummary() (*InsightDashboardSummary, error) {
	summary := &InsightDashboardSummary{
		ProposalsByStatus:    make(map[string]int),
		PatternsBySeverity:   make(map[string]int),
		TopAffectedWorkflows: []string{},
		TopAffectedAgents:    []string{},
		TimeWindowHours:      24,
	}

	err := d.sql.QueryRow(`
		SELECT COUNT(*) 
		FROM insight_proposals 
		WHERE created_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-24 hours')
	`).Scan(&summary.RecentProposals)
	if err != nil {
		return nil, fmt.Errorf("count recent proposals: %w", err)
	}

	err = d.sql.QueryRow(`
		SELECT COUNT(*) 
		FROM insight_patterns 
		WHERE created_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-24 hours')
	`).Scan(&summary.RecentPatterns)
	if err != nil {
		return nil, fmt.Errorf("count recent patterns: %w", err)
	}

	statusRows, err := d.sql.Query(`
		SELECT status, COUNT(*) as count
		FROM insight_proposals
		WHERE created_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-24 hours')
		GROUP BY status
	`)
	if err != nil {
		return nil, fmt.Errorf("count proposals by status: %w", err)
	}
	defer statusRows.Close()

	for statusRows.Next() {
		var status string
		var count int
		if err := statusRows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan status count: %w", err)
		}
		summary.ProposalsByStatus[status] = count
	}

	severityRows, err := d.sql.Query(`
		SELECT severity, COUNT(*) as count
		FROM insight_patterns
		WHERE created_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-24 hours')
		GROUP BY severity
	`)
	if err != nil {
		return nil, fmt.Errorf("count patterns by severity: %w", err)
	}
	defer severityRows.Close()

	for severityRows.Next() {
		var severity string
		var count int
		if err := severityRows.Scan(&severity, &count); err != nil {
			return nil, fmt.Errorf("scan severity count: %w", err)
		}
		summary.PatternsBySeverity[severity] = count
	}

	workflowRows, err := d.sql.Query(`
		SELECT json_extract(evidence, '$.affected_workflow') as workflow, COUNT(*) as count
		FROM insight_proposals
		WHERE created_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-24 hours')
		  AND json_extract(evidence, '$.affected_workflow') IS NOT NULL
		GROUP BY workflow
		ORDER BY count DESC
		LIMIT 5
	`)
	if err != nil {
		return nil, fmt.Errorf("get top affected workflows: %w", err)
	}
	defer workflowRows.Close()

	for workflowRows.Next() {
		var workflow string
		var count int
		if err := workflowRows.Scan(&workflow, &count); err != nil {
			return nil, fmt.Errorf("scan workflow count: %w", err)
		}
		if workflow != "" {
			summary.TopAffectedWorkflows = append(summary.TopAffectedWorkflows, workflow)
		}
	}

	agentRows, err := d.sql.Query(`
		SELECT json_extract(evidence, '$.agent_id') as agent, COUNT(*) as count
		FROM insight_proposals
		WHERE created_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-24 hours')
		  AND json_extract(evidence, '$.agent_id') IS NOT NULL
		GROUP BY agent
		ORDER BY count DESC
		LIMIT 5
	`)
	if err != nil {
		return nil, fmt.Errorf("get top affected agents: %w", err)
	}
	defer agentRows.Close()

	for agentRows.Next() {
		var agent string
		var count int
		if err := agentRows.Scan(&agent, &count); err != nil {
			return nil, fmt.Errorf("scan agent count: %w", err)
		}
		if agent != "" {
			summary.TopAffectedAgents = append(summary.TopAffectedAgents, agent)
		}
	}

	return summary, nil
}

type AgentScorecard struct {
	ID              string  `json:"id"`
	AgentName       string  `json:"agent_name"`
	Window          string  `json:"window"`
	WindowStart     string  `json:"window_start"`
	WindowEnd       string  `json:"window_end"`
	TotalRuns       int     `json:"total_runs"`
	SuccessRate     float64 `json:"success_rate"`
	FailureRate     float64 `json:"failure_rate"`
	ReviewPassRate  float64 `json:"review_pass_rate"`
	ReworkRate      float64 `json:"rework_rate"`
	AvgCycleTimeMs  int64   `json:"avg_cycle_time_ms"`
	RegressionRate  float64 `json:"regression_rate"`
	ConfidenceScore float64 `json:"confidence_score"`
	Trend           string  `json:"trend"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type WorkflowScorecard struct {
	ID                  string  `json:"id"`
	WorkflowType        string  `json:"workflow_type"`
	Window              string  `json:"window"`
	WindowStart         string  `json:"window_start"`
	WindowEnd           string  `json:"window_end"`
	TotalRuns           int     `json:"total_runs"`
	CompletionRate      float64 `json:"completion_rate"`
	FailureRate         float64 `json:"failure_rate"`
	ReviewRejectionRate float64 `json:"review_rejection_rate"`
	ReworkRate          float64 `json:"rework_rate"`
	AvgDurationMs       int64   `json:"avg_duration_ms"`
	ConfidenceScore     float64 `json:"confidence_score"`
	Trend               string  `json:"trend"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

func (d *DB) SaveAgentScorecard(card *AgentScorecard) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := d.sql.Exec(`
		INSERT INTO insight_agent_scorecards 
		(id, agent_name, window, window_start, window_end, total_runs, success_rate, failure_rate,
		 review_pass_rate, rework_rate, avg_cycle_time_ms, regression_rate, confidence_score, trend, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_name, window) DO UPDATE SET
			window_start = excluded.window_start,
			window_end = excluded.window_end,
			total_runs = excluded.total_runs,
			success_rate = excluded.success_rate,
			failure_rate = excluded.failure_rate,
			review_pass_rate = excluded.review_pass_rate,
			rework_rate = excluded.rework_rate,
			avg_cycle_time_ms = excluded.avg_cycle_time_ms,
			regression_rate = excluded.regression_rate,
			confidence_score = excluded.confidence_score,
			trend = excluded.trend,
			updated_at = excluded.updated_at
	`,
		card.ID, card.AgentName, card.Window, card.WindowStart, card.WindowEnd,
		card.TotalRuns, card.SuccessRate, card.FailureRate, card.ReviewPassRate,
		card.ReworkRate, card.AvgCycleTimeMs, card.RegressionRate, card.ConfidenceScore,
		card.Trend, now, now,
	)

	return err
}

func (d *DB) SaveWorkflowScorecard(card *WorkflowScorecard) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := d.sql.Exec(`
		INSERT INTO insight_workflow_scorecards 
		(id, workflow_type, window, window_start, window_end, total_runs, completion_rate, failure_rate,
		 review_rejection_rate, rework_rate, avg_duration_ms, confidence_score, trend, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workflow_type, window) DO UPDATE SET
			window_start = excluded.window_start,
			window_end = excluded.window_end,
			total_runs = excluded.total_runs,
			completion_rate = excluded.completion_rate,
			failure_rate = excluded.failure_rate,
			review_rejection_rate = excluded.review_rejection_rate,
			rework_rate = excluded.rework_rate,
			avg_duration_ms = excluded.avg_duration_ms,
			confidence_score = excluded.confidence_score,
			trend = excluded.trend,
			updated_at = excluded.updated_at
	`,
		card.ID, card.WorkflowType, card.Window, card.WindowStart, card.WindowEnd,
		card.TotalRuns, card.CompletionRate, card.FailureRate, card.ReviewRejectionRate,
		card.ReworkRate, card.AvgDurationMs, card.ConfidenceScore, card.Trend, now, now,
	)

	return err
}

func (d *DB) ListAgentScorecards(window string, sortBy string, sortDir string, limit int) ([]AgentScorecard, error) {
	if limit <= 0 {
		limit = 50
	}
	if sortBy == "" {
		sortBy = "confidence_score"
	}
	if sortDir == "" {
		sortDir = "DESC"
	}

	allowedSort := map[string]bool{
		"agent_name": true, "total_runs": true, "success_rate": true,
		"failure_rate": true, "review_pass_rate": true, "rework_rate": true,
		"confidence_score": true, "avg_cycle_time_ms": true, "trend": true,
	}
	if !allowedSort[sortBy] {
		sortBy = "confidence_score"
	}
	if sortDir != "ASC" && sortDir != "DESC" {
		sortDir = "DESC"
	}

	query := fmt.Sprintf(`
		SELECT id, agent_name, window, window_start, window_end, total_runs, success_rate, failure_rate,
		       review_pass_rate, rework_rate, avg_cycle_time_ms, regression_rate, confidence_score, trend, created_at, updated_at
		FROM insight_agent_scorecards
		WHERE window = ?
		ORDER BY %s %s
		LIMIT ?
	`, sortBy, sortDir)

	rows, err := d.sql.Query(query, window, limit)
	if err != nil {
		return nil, fmt.Errorf("list agent scorecards: %w", err)
	}
	defer rows.Close()

	var cards []AgentScorecard
	for rows.Next() {
		var c AgentScorecard
		if err := rows.Scan(
			&c.ID, &c.AgentName, &c.Window, &c.WindowStart, &c.WindowEnd,
			&c.TotalRuns, &c.SuccessRate, &c.FailureRate, &c.ReviewPassRate,
			&c.ReworkRate, &c.AvgCycleTimeMs, &c.RegressionRate, &c.ConfidenceScore,
			&c.Trend, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent scorecard: %w", err)
		}
		cards = append(cards, c)
	}

	return cards, rows.Err()
}

func (d *DB) ListWorkflowScorecards(window string, sortBy string, sortDir string, limit int) ([]WorkflowScorecard, error) {
	if limit <= 0 {
		limit = 50
	}
	if sortBy == "" {
		sortBy = "confidence_score"
	}
	if sortDir == "" {
		sortDir = "DESC"
	}

	allowedSort := map[string]bool{
		"workflow_type": true, "total_runs": true, "completion_rate": true,
		"failure_rate": true, "review_rejection_rate": true, "rework_rate": true,
		"confidence_score": true, "avg_duration_ms": true, "trend": true,
	}
	if !allowedSort[sortBy] {
		sortBy = "confidence_score"
	}
	if sortDir != "ASC" && sortDir != "DESC" {
		sortDir = "DESC"
	}

	query := fmt.Sprintf(`
		SELECT id, workflow_type, window, window_start, window_end, total_runs, completion_rate, failure_rate,
		       review_rejection_rate, rework_rate, avg_duration_ms, confidence_score, trend, created_at, updated_at
		FROM insight_workflow_scorecards
		WHERE window = ?
		ORDER BY %s %s
		LIMIT ?
	`, sortBy, sortDir)

	rows, err := d.sql.Query(query, window, limit)
	if err != nil {
		return nil, fmt.Errorf("list workflow scorecards: %w", err)
	}
	defer rows.Close()

	var cards []WorkflowScorecard
	for rows.Next() {
		var c WorkflowScorecard
		if err := rows.Scan(
			&c.ID, &c.WorkflowType, &c.Window, &c.WindowStart, &c.WindowEnd,
			&c.TotalRuns, &c.CompletionRate, &c.FailureRate, &c.ReviewRejectionRate,
			&c.ReworkRate, &c.AvgDurationMs, &c.ConfidenceScore,
			&c.Trend, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan workflow scorecard: %w", err)
		}
		cards = append(cards, c)
	}

	return cards, rows.Err()
}

func (d *DB) GetAgentScorecardByName(agentName string, window string) (*AgentScorecard, error) {
	row := d.sql.QueryRow(`
		SELECT id, agent_name, window, window_start, window_end, total_runs, success_rate, failure_rate,
		       review_pass_rate, rework_rate, avg_cycle_time_ms, regression_rate, confidence_score, trend, created_at, updated_at
		FROM insight_agent_scorecards
		WHERE agent_name = ? AND window = ?
	`, agentName, window)

	var c AgentScorecard
	err := row.Scan(
		&c.ID, &c.AgentName, &c.Window, &c.WindowStart, &c.WindowEnd,
		&c.TotalRuns, &c.SuccessRate, &c.FailureRate, &c.ReviewPassRate,
		&c.ReworkRate, &c.AvgCycleTimeMs, &c.RegressionRate, &c.ConfidenceScore,
		&c.Trend, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent scorecard: %w", err)
	}

	return &c, nil
}

func (d *DB) GetWorkflowScorecardByType(workflowType string, window string) (*WorkflowScorecard, error) {
	row := d.sql.QueryRow(`
		SELECT id, workflow_type, window, window_start, window_end, total_runs, completion_rate, failure_rate,
		       review_rejection_rate, rework_rate, avg_duration_ms, confidence_score, trend, created_at, updated_at
		FROM insight_workflow_scorecards
		WHERE workflow_type = ? AND window = ?
	`, workflowType, window)

	var c WorkflowScorecard
	err := row.Scan(
		&c.ID, &c.WorkflowType, &c.Window, &c.WindowStart, &c.WindowEnd,
		&c.TotalRuns, &c.CompletionRate, &c.FailureRate, &c.ReviewRejectionRate,
		&c.ReworkRate, &c.AvgDurationMs, &c.ConfidenceScore,
		&c.Trend, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workflow scorecard: %w", err)
	}

	return &c, nil
}

func (d *DB) GetScorecardHighlights(window string) (map[string]interface{}, error) {
	highlights := make(map[string]interface{})

	var bestAgent struct {
		AgentName   string  `json:"agent_name"`
		SuccessRate float64 `json:"success_rate"`
	}
	err := d.sql.QueryRow(`
		SELECT agent_name, success_rate
		FROM insight_agent_scorecards
		WHERE window = ? AND total_runs >= 5
		ORDER BY success_rate DESC
		LIMIT 1
	`, window).Scan(&bestAgent.AgentName, &bestAgent.SuccessRate)
	if err == nil {
		highlights["best_agent"] = bestAgent
	}

	var worstAgent struct {
		AgentName   string  `json:"agent_name"`
		SuccessRate float64 `json:"success_rate"`
		Trend       string  `json:"trend"`
	}
	err = d.sql.QueryRow(`
		SELECT agent_name, success_rate, trend
		FROM insight_agent_scorecards
		WHERE window = ? AND total_runs >= 5 AND trend = 'degrading'
		ORDER BY success_rate ASC
		LIMIT 1
	`, window).Scan(&worstAgent.AgentName, &worstAgent.SuccessRate, &worstAgent.Trend)
	if err == nil {
		highlights["most_degraded_agent"] = worstAgent
	}

	var slowestWorkflow struct {
		WorkflowType  string `json:"workflow_type"`
		AvgDurationMs int64  `json:"avg_duration_ms"`
	}
	err = d.sql.QueryRow(`
		SELECT workflow_type, avg_duration_ms
		FROM insight_workflow_scorecards
		WHERE window = ? AND total_runs >= 3
		ORDER BY avg_duration_ms DESC
		LIMIT 1
	`, window).Scan(&slowestWorkflow.WorkflowType, &slowestWorkflow.AvgDurationMs)
	if err == nil {
		highlights["slowest_workflow"] = slowestWorkflow
	}

	var highestRework struct {
		WorkflowType string  `json:"workflow_type"`
		ReworkRate   float64 `json:"rework_rate"`
	}
	err = d.sql.QueryRow(`
		SELECT workflow_type, rework_rate
		FROM insight_workflow_scorecards
		WHERE window = ? AND total_runs >= 3
		ORDER BY rework_rate DESC
		LIMIT 1
	`, window).Scan(&highestRework.WorkflowType, &highestRework.ReworkRate)
	if err == nil {
		highlights["highest_rework_workflow"] = highestRework
	}

	return highlights, nil
}

type RoutingRecommendation struct {
	ID                 string         `json:"id"`
	WorkflowType       string         `json:"workflow_type"`
	RecommendationType string         `json:"recommendation_type"`
	RecommendedAgent   string         `json:"recommended_agent,omitempty"`
	CurrentAgent       string         `json:"current_agent,omitempty"`
	Confidence         float64        `json:"confidence"`
	RiskLevel          string         `json:"risk_level"`
	Reason             string         `json:"reason"`
	Evidence           map[string]any `json:"evidence"`
	Observations       int            `json:"observations"`
	CreatedAt          string         `json:"created_at"`
}

type RoutingRecommendationFilters struct {
	WorkflowType  string
	RecType       string
	MinConfidence float64
}

func (d *DB) SaveRoutingRecommendation(rec *RoutingRecommendation) error {
	evidenceBytes, err := json.Marshal(rec.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	if rec.Evidence == nil {
		evidenceBytes = []byte("{}")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = d.sql.Exec(`
		INSERT INTO insight_routing_recommendations 
		(id, workflow_type, recommendation_type, recommended_agent, current_agent, 
		 confidence, risk_level, reason, evidence, observations, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			confidence = excluded.confidence,
			risk_level = excluded.risk_level,
			reason = excluded.reason,
			evidence = excluded.evidence,
			observations = excluded.observations
	`,
		rec.ID, rec.WorkflowType, rec.RecommendationType,
		rec.RecommendedAgent, rec.CurrentAgent,
		rec.Confidence, rec.RiskLevel, rec.Reason,
		string(evidenceBytes), rec.Observations, now,
	)

	return err
}

func (d *DB) ListRoutingRecommendations(filters RoutingRecommendationFilters, limit int) ([]RoutingRecommendation, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, workflow_type, recommendation_type, recommended_agent, current_agent,
		       confidence, risk_level, reason, evidence, observations, created_at
		FROM insight_routing_recommendations
		WHERE 1=1
	`
	args := []any{}

	if filters.WorkflowType != "" {
		query += " AND workflow_type = ?"
		args = append(args, filters.WorkflowType)
	}
	if filters.RecType != "" {
		query += " AND recommendation_type = ?"
		args = append(args, filters.RecType)
	}
	if filters.MinConfidence > 0 {
		query += " AND confidence >= ?"
		args = append(args, filters.MinConfidence)
	}

	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query routing recommendations: %w", err)
	}
	defer rows.Close()

	var recommendations []RoutingRecommendation
	for rows.Next() {
		var rec RoutingRecommendation
		var evidenceJSON string
		var recommendedAgent, currentAgent sql.NullString

		if err := rows.Scan(
			&rec.ID, &rec.WorkflowType, &rec.RecommendationType, &recommendedAgent, &currentAgent,
			&rec.Confidence, &rec.RiskLevel, &rec.Reason, &evidenceJSON, &rec.Observations, &rec.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan routing recommendation: %w", err)
		}

		if recommendedAgent.Valid {
			rec.RecommendedAgent = recommendedAgent.String
		}
		if currentAgent.Valid {
			rec.CurrentAgent = currentAgent.String
		}

		if err := json.Unmarshal([]byte(evidenceJSON), &rec.Evidence); err != nil {
			rec.Evidence = make(map[string]any)
		}

		recommendations = append(recommendations, rec)
	}

	return recommendations, rows.Err()
}

func (d *DB) GetRoutingRecommendationByID(id string) (*RoutingRecommendation, error) {
	query := `
		SELECT id, workflow_type, recommendation_type, recommended_agent, current_agent,
		       confidence, risk_level, reason, evidence, observations, created_at
		FROM insight_routing_recommendations
		WHERE id = ?
	`

	var rec RoutingRecommendation
	var evidenceJSON string
	var recommendedAgent, currentAgent sql.NullString

	err := d.sql.QueryRow(query, id).Scan(
		&rec.ID, &rec.WorkflowType, &rec.RecommendationType, &recommendedAgent, &currentAgent,
		&rec.Confidence, &rec.RiskLevel, &rec.Reason, &evidenceJSON, &rec.Observations, &rec.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get routing recommendation: %w", err)
	}

	if recommendedAgent.Valid {
		rec.RecommendedAgent = recommendedAgent.String
	}
	if currentAgent.Valid {
		rec.CurrentAgent = currentAgent.String
	}

	if err := json.Unmarshal([]byte(evidenceJSON), &rec.Evidence); err != nil {
		rec.Evidence = make(map[string]any)
	}

	return &rec, nil
}
