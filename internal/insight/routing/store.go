package routing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type RoutingStore interface {
	SaveRecommendation(ctx context.Context, rec RoutingRecommendation) error
	GetRecentRecommendations(ctx context.Context, limit int, filters RecommendationFilters) ([]RoutingRecommendation, error)
	GetRecommendationByID(ctx context.Context, id string) (*RoutingRecommendation, error)
	FindSimilarRecommendation(ctx context.Context, rec RoutingRecommendation, within time.Duration) (*RoutingRecommendation, error)
	DeleteOldRecommendations(ctx context.Context, olderThan time.Duration) (int64, error)
}

type RecommendationFilters struct {
	WorkflowType  string
	RecType       RecommendationType
	MinConfidence float64
}

type DBRoutingStore struct {
	database *db.DB
}

func NewDBRoutingStore(database *db.DB) *DBRoutingStore {
	return &DBRoutingStore{database: database}
}

func (s *DBRoutingStore) SaveRecommendation(ctx context.Context, rec RoutingRecommendation) error {
	evidenceBytes, err := json.Marshal(rec.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	if rec.Evidence == nil {
		evidenceBytes = []byte("{}")
	}

	now := rec.CreatedAt.Format(time.RFC3339Nano)
	if now == "" || rec.CreatedAt.IsZero() {
		now = time.Now().UTC().Format(time.RFC3339Nano)
	}

	_, err = s.database.SQL().ExecContext(ctx, `
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
		rec.ID, rec.WorkflowType, string(rec.RecommendationType),
		rec.RecommendedAgent, rec.CurrentAgent,
		rec.Confidence, string(rec.RiskLevel), rec.Reason,
		string(evidenceBytes), rec.Observations, now,
	)

	return err
}

func (s *DBRoutingStore) GetRecentRecommendations(ctx context.Context, limit int, filters RecommendationFilters) ([]RoutingRecommendation, error) {
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
		args = append(args, string(filters.RecType))
	}
	if filters.MinConfidence > 0 {
		query += " AND confidence >= ?"
		args = append(args, filters.MinConfidence)
	}

	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.database.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query recommendations: %w", err)
	}
	defer rows.Close()

	var recommendations []RoutingRecommendation
	for rows.Next() {
		var rec RoutingRecommendation
		var recType, riskLevel, evidenceJSON string
		var createdAtStr string
		var recommendedAgent, currentAgent sql.NullString

		if err := rows.Scan(
			&rec.ID, &rec.WorkflowType, &recType, &recommendedAgent, &currentAgent,
			&rec.Confidence, &riskLevel, &rec.Reason, &evidenceJSON, &rec.Observations, &createdAtStr,
		); err != nil {
			return nil, fmt.Errorf("scan recommendation: %w", err)
		}

		rec.RecommendationType = RecommendationType(recType)
		rec.RiskLevel = RiskLevel(riskLevel)
		if recommendedAgent.Valid {
			rec.RecommendedAgent = recommendedAgent.String
		}
		if currentAgent.Valid {
			rec.CurrentAgent = currentAgent.String
		}

		if err := json.Unmarshal([]byte(evidenceJSON), &rec.Evidence); err != nil {
			rec.Evidence = make(map[string]any)
		}

		rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
		recommendations = append(recommendations, rec)
	}

	return recommendations, rows.Err()
}

func (s *DBRoutingStore) GetRecommendationByID(ctx context.Context, id string) (*RoutingRecommendation, error) {
	query := `
		SELECT id, workflow_type, recommendation_type, recommended_agent, current_agent,
		       confidence, risk_level, reason, evidence, observations, created_at
		FROM insight_routing_recommendations
		WHERE id = ?
	`

	var rec RoutingRecommendation
	var recType, riskLevel, evidenceJSON string
	var createdAtStr string
	var recommendedAgent, currentAgent sql.NullString

	err := s.database.SQL().QueryRowContext(ctx, query, id).Scan(
		&rec.ID, &rec.WorkflowType, &recType, &recommendedAgent, &currentAgent,
		&rec.Confidence, &riskLevel, &rec.Reason, &evidenceJSON, &rec.Observations, &createdAtStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get recommendation: %w", err)
	}

	rec.RecommendationType = RecommendationType(recType)
	rec.RiskLevel = RiskLevel(riskLevel)
	if recommendedAgent.Valid {
		rec.RecommendedAgent = recommendedAgent.String
	}
	if currentAgent.Valid {
		rec.CurrentAgent = currentAgent.String
	}

	if err := json.Unmarshal([]byte(evidenceJSON), &rec.Evidence); err != nil {
		rec.Evidence = make(map[string]any)
	}

	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	return &rec, nil
}

func (s *DBRoutingStore) FindSimilarRecommendation(ctx context.Context, rec RoutingRecommendation, within time.Duration) (*RoutingRecommendation, error) {
	query := `
		SELECT id, workflow_type, recommendation_type, recommended_agent, current_agent,
		       confidence, risk_level, reason, evidence, observations, created_at
		FROM insight_routing_recommendations
		WHERE workflow_type = ?
		  AND recommendation_type = ?
		  AND created_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-' || ? || ' hours')
	`
	args := []any{rec.WorkflowType, string(rec.RecommendationType), int(within.Hours())}

	if rec.RecommendedAgent != "" {
		query += " AND recommended_agent = ?"
		args = append(args, rec.RecommendedAgent)
	}
	if rec.CurrentAgent != "" {
		query += " AND current_agent = ?"
		args = append(args, rec.CurrentAgent)
	}

	query += " ORDER BY created_at DESC LIMIT 1"

	var found RoutingRecommendation
	var recType, riskLevel, evidenceJSON string
	var createdAtStr string
	var recommendedAgent, currentAgent sql.NullString

	err := s.database.SQL().QueryRowContext(ctx, query, args...).Scan(
		&found.ID, &found.WorkflowType, &recType, &recommendedAgent, &currentAgent,
		&found.Confidence, &riskLevel, &found.Reason, &evidenceJSON, &found.Observations, &createdAtStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find similar recommendation: %w", err)
	}

	found.RecommendationType = RecommendationType(recType)
	found.RiskLevel = RiskLevel(riskLevel)
	if recommendedAgent.Valid {
		found.RecommendedAgent = recommendedAgent.String
	}
	if currentAgent.Valid {
		found.CurrentAgent = currentAgent.String
	}

	if err := json.Unmarshal([]byte(evidenceJSON), &found.Evidence); err != nil {
		found.Evidence = make(map[string]any)
	}

	found.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	return &found, nil
}

func (s *DBRoutingStore) DeleteOldRecommendations(ctx context.Context, olderThan time.Duration) (int64, error) {
	result, err := s.database.SQL().ExecContext(ctx, `
		DELETE FROM insight_routing_recommendations
		WHERE created_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-' || ? || ' hours')
	`, int(olderThan.Hours()))
	if err != nil {
		return 0, fmt.Errorf("delete old recommendations: %w", err)
	}

	return result.RowsAffected()
}
