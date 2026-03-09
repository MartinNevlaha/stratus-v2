package product_intelligence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type Store interface {
	SaveProject(ctx context.Context, project Project) error
	GetProjectByID(ctx context.Context, id string) (*Project, error)
	GetProjectByPath(ctx context.Context, path string) (*Project, error)
	ListProjects(ctx context.Context, limit int) ([]Project, error)
	UpdateProjectDomain(ctx context.Context, id, domain string, confidence float64) error
	UpdateProjectLastAnalyzed(ctx context.Context, id string) error
	DeleteProject(ctx context.Context, id string) error

	SaveProjectFeature(ctx context.Context, feature ProjectFeature) error
	GetProjectFeatures(ctx context.Context, projectID string) ([]ProjectFeature, error)
	DeleteProjectFeatures(ctx context.Context, projectID string) error

	SaveMarketFeature(ctx context.Context, feature MarketFeature) error
	GetMarketFeaturesByDomain(ctx context.Context, domain string) ([]MarketFeature, error)
	ListMarketFeatures(ctx context.Context, limit int) ([]MarketFeature, error)

	SaveFeatureGap(ctx context.Context, gap FeatureGap) error
	GetFeatureGapsByProject(ctx context.Context, projectID string) ([]FeatureGap, error)
	GetFeatureGapByID(ctx context.Context, id string) (*FeatureGap, error)
	UpdateFeatureGapStatus(ctx context.Context, id string, status GapStatus) error
	DeleteFeatureGapsByProject(ctx context.Context, projectID string) error

	SaveFeatureProposal(ctx context.Context, proposal FeatureProposal) error
	GetFeatureProposalsByProject(ctx context.Context, projectID string) ([]FeatureProposal, error)
	GetFeatureProposalByID(ctx context.Context, id string) (*FeatureProposal, error)
	GetFeatureProposalsByStatus(ctx context.Context, status ProposalStatus, limit int) ([]FeatureProposal, error)
	UpdateFeatureProposalStatus(ctx context.Context, id string, status ProposalStatus, workflowID string) error
}

type DBStore struct {
	database *db.DB
	sql      *sql.DB
}

func NewDBStore(database *db.DB) *DBStore {
	return &DBStore{database: database, sql: database.SQL()}
}

func (s *DBStore) SaveProject(ctx context.Context, project Project) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.sql.ExecContext(ctx, `
		INSERT INTO pi_projects (id, name, path, domain, domain_confidence, readme_hash, last_analyzed, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			path = excluded.path,
			domain = excluded.domain,
			domain_confidence = excluded.domain_confidence,
			readme_hash = excluded.readme_hash,
			updated_at = excluded.updated_at
	`, project.ID, project.Name, project.Path, project.Domain, project.DomainConfidence,
		project.ReadmeHash, project.LastAnalyzed, now, now)
	return err
}

func (s *DBStore) GetProjectByID(ctx context.Context, id string) (*Project, error) {
	var p Project
	err := s.sql.QueryRowContext(ctx, `
		SELECT id, name, path, domain, domain_confidence, readme_hash, last_analyzed, created_at, updated_at
		FROM pi_projects WHERE id = ?
	`, id).Scan(&p.ID, &p.Name, &p.Path, &p.Domain, &p.DomainConfidence, &p.ReadmeHash, &p.LastAnalyzed, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return &p, nil
}

func (s *DBStore) GetProjectByPath(ctx context.Context, path string) (*Project, error) {
	var p Project
	err := s.sql.QueryRowContext(ctx, `
		SELECT id, name, path, domain, domain_confidence, readme_hash, last_analyzed, created_at, updated_at
		FROM pi_projects WHERE path = ?
	`, path).Scan(&p.ID, &p.Name, &p.Path, &p.Domain, &p.DomainConfidence, &p.ReadmeHash, &p.LastAnalyzed, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project by path: %w", err)
	}
	return &p, nil
}

func (s *DBStore) ListProjects(ctx context.Context, limit int) ([]Project, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.sql.QueryContext(ctx, `
		SELECT id, name, path, domain, domain_confidence, readme_hash, last_analyzed, created_at, updated_at
		FROM pi_projects ORDER BY updated_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.Domain, &p.DomainConfidence, &p.ReadmeHash, &p.LastAnalyzed, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *DBStore) UpdateProjectDomain(ctx context.Context, id, domain string, confidence float64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.sql.ExecContext(ctx, `
		UPDATE pi_projects SET domain = ?, domain_confidence = ?, updated_at = ? WHERE id = ?
	`, domain, confidence, now, id)
	return err
}

func (s *DBStore) UpdateProjectLastAnalyzed(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.sql.ExecContext(ctx, `
		UPDATE pi_projects SET last_analyzed = ?, updated_at = ? WHERE id = ?
	`, now, now, id)
	return err
}

func (s *DBStore) DeleteProject(ctx context.Context, id string) error {
	_, err := s.sql.ExecContext(ctx, `DELETE FROM pi_projects WHERE id = ?`, id)
	return err
}

func (s *DBStore) SaveProjectFeature(ctx context.Context, feature ProjectFeature) error {
	evidenceJSON, err := json.Marshal(feature.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}

	_, err = s.sql.ExecContext(ctx, `
		INSERT INTO pi_project_features (id, project_id, feature_name, feature_type, description, evidence_json, confidence, source, detected_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, feature_name) DO UPDATE SET
			feature_type = excluded.feature_type,
			description = excluded.description,
			evidence_json = excluded.evidence_json,
			confidence = excluded.confidence,
			source = excluded.source,
			detected_at = excluded.detected_at
	`, feature.ID, feature.ProjectID, feature.FeatureName, feature.FeatureType,
		feature.Description, string(evidenceJSON), feature.Confidence, feature.Source, feature.DetectedAt)
	return err
}

func (s *DBStore) GetProjectFeatures(ctx context.Context, projectID string) ([]ProjectFeature, error) {
	rows, err := s.sql.QueryContext(ctx, `
		SELECT id, project_id, feature_name, feature_type, description, evidence_json, confidence, source, detected_at
		FROM pi_project_features WHERE project_id = ? ORDER BY confidence DESC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project features: %w", err)
	}
	defer rows.Close()

	var features []ProjectFeature
	for rows.Next() {
		var f ProjectFeature
		var evidenceJSON string
		if err := rows.Scan(&f.ID, &f.ProjectID, &f.FeatureName, &f.FeatureType, &f.Description, &evidenceJSON, &f.Confidence, &f.Source, &f.DetectedAt); err != nil {
			return nil, fmt.Errorf("scan feature: %w", err)
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &f.Evidence); err != nil {
			f.Evidence = make(map[string]any)
		}
		features = append(features, f)
	}
	return features, rows.Err()
}

func (s *DBStore) DeleteProjectFeatures(ctx context.Context, projectID string) error {
	_, err := s.sql.ExecContext(ctx, `DELETE FROM pi_project_features WHERE project_id = ?`, projectID)
	return err
}

func (s *DBStore) SaveMarketFeature(ctx context.Context, feature MarketFeature) error {
	sourcesJSON, err := json.Marshal(feature.Sources)
	if err != nil {
		return fmt.Errorf("marshal sources: %w", err)
	}

	_, err = s.sql.ExecContext(ctx, `
		INSERT INTO pi_market_features (id, domain, feature_name, feature_type, prevalence, importance, sources_json, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(domain, feature_name) DO UPDATE SET
			feature_type = excluded.feature_type,
			prevalence = excluded.prevalence,
			importance = excluded.importance,
			sources_json = excluded.sources_json,
			discovered_at = excluded.discovered_at
	`, feature.ID, feature.Domain, feature.FeatureName, feature.FeatureType,
		feature.Prevalence, feature.Importance, string(sourcesJSON), feature.DiscoveredAt)
	return err
}

func (s *DBStore) GetMarketFeaturesByDomain(ctx context.Context, domain string) ([]MarketFeature, error) {
	rows, err := s.sql.QueryContext(ctx, `
		SELECT id, domain, feature_name, feature_type, prevalence, importance, sources_json, discovered_at
		FROM pi_market_features WHERE domain = ? ORDER BY prevalence DESC, importance DESC
	`, domain)
	if err != nil {
		return nil, fmt.Errorf("get market features: %w", err)
	}
	defer rows.Close()

	var features []MarketFeature
	for rows.Next() {
		var f MarketFeature
		var sourcesJSON string
		if err := rows.Scan(&f.ID, &f.Domain, &f.FeatureName, &f.FeatureType, &f.Prevalence, &f.Importance, &sourcesJSON, &f.DiscoveredAt); err != nil {
			return nil, fmt.Errorf("scan market feature: %w", err)
		}
		if err := json.Unmarshal([]byte(sourcesJSON), &f.Sources); err != nil {
			f.Sources = []string{}
		}
		features = append(features, f)
	}
	return features, rows.Err()
}

func (s *DBStore) ListMarketFeatures(ctx context.Context, limit int) ([]MarketFeature, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.sql.QueryContext(ctx, `
		SELECT id, domain, feature_name, feature_type, prevalence, importance, sources_json, discovered_at
		FROM pi_market_features ORDER BY prevalence DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list market features: %w", err)
	}
	defer rows.Close()

	var features []MarketFeature
	for rows.Next() {
		var f MarketFeature
		var sourcesJSON string
		if err := rows.Scan(&f.ID, &f.Domain, &f.FeatureName, &f.FeatureType, &f.Prevalence, &f.Importance, &sourcesJSON, &f.DiscoveredAt); err != nil {
			return nil, fmt.Errorf("scan market feature: %w", err)
		}
		if err := json.Unmarshal([]byte(sourcesJSON), &f.Sources); err != nil {
			f.Sources = []string{}
		}
		features = append(features, f)
	}
	return features, rows.Err()
}

func (s *DBStore) SaveFeatureGap(ctx context.Context, gap FeatureGap) error {
	_, err := s.sql.ExecContext(ctx, `
		INSERT INTO pi_feature_gaps (id, project_id, feature_name, gap_type, impact_score, complexity_score, strategic_fit, confidence, reasoning, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			gap_type = excluded.gap_type,
			impact_score = excluded.impact_score,
			complexity_score = excluded.complexity_score,
			strategic_fit = excluded.strategic_fit,
			confidence = excluded.confidence,
			reasoning = excluded.reasoning,
			status = excluded.status
	`, gap.ID, gap.ProjectID, gap.FeatureName, gap.GapType, gap.ImpactScore, gap.ComplexityScore,
		gap.StrategicFit, gap.Confidence, gap.Reasoning, gap.Status, gap.CreatedAt)
	return err
}

func (s *DBStore) GetFeatureGapsByProject(ctx context.Context, projectID string) ([]FeatureGap, error) {
	rows, err := s.sql.QueryContext(ctx, `
		SELECT id, project_id, feature_name, gap_type, impact_score, complexity_score, strategic_fit, confidence, reasoning, status, created_at
		FROM pi_feature_gaps WHERE project_id = ? ORDER BY impact_score DESC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("get feature gaps: %w", err)
	}
	defer rows.Close()

	var gaps []FeatureGap
	for rows.Next() {
		var g FeatureGap
		if err := rows.Scan(&g.ID, &g.ProjectID, &g.FeatureName, &g.GapType, &g.ImpactScore, &g.ComplexityScore, &g.StrategicFit, &g.Confidence, &g.Reasoning, &g.Status, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan gap: %w", err)
		}
		gaps = append(gaps, g)
	}
	return gaps, rows.Err()
}

func (s *DBStore) GetFeatureGapByID(ctx context.Context, id string) (*FeatureGap, error) {
	var g FeatureGap
	err := s.sql.QueryRowContext(ctx, `
		SELECT id, project_id, feature_name, gap_type, impact_score, complexity_score, strategic_fit, confidence, reasoning, status, created_at
		FROM pi_feature_gaps WHERE id = ?
	`, id).Scan(&g.ID, &g.ProjectID, &g.FeatureName, &g.GapType, &g.ImpactScore, &g.ComplexityScore, &g.StrategicFit, &g.Confidence, &g.Reasoning, &g.Status, &g.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get gap: %w", err)
	}
	return &g, nil
}

func (s *DBStore) UpdateFeatureGapStatus(ctx context.Context, id string, status GapStatus) error {
	_, err := s.sql.ExecContext(ctx, `UPDATE pi_feature_gaps SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *DBStore) DeleteFeatureGapsByProject(ctx context.Context, projectID string) error {
	_, err := s.sql.ExecContext(ctx, `DELETE FROM pi_feature_gaps WHERE project_id = ?`, projectID)
	return err
}

func (s *DBStore) SaveFeatureProposal(ctx context.Context, proposal FeatureProposal) error {
	evidenceJSON, err := json.Marshal(proposal.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	hintsJSON, err := json.Marshal(proposal.ImplementationHints)
	if err != nil {
		return fmt.Errorf("marshal hints: %w", err)
	}

	_, err = s.sql.ExecContext(ctx, `
		INSERT INTO pi_feature_proposals (id, project_id, gap_id, feature_name, title, description, rationale, impact_score, complexity_score, strategic_fit, confidence, evidence_json, implementation_hints_json, status, workflow_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			rationale = excluded.rationale,
			impact_score = excluded.impact_score,
			complexity_score = excluded.complexity_score,
			strategic_fit = excluded.strategic_fit,
			confidence = excluded.confidence,
			evidence_json = excluded.evidence_json,
			implementation_hints_json = excluded.implementation_hints_json,
			status = excluded.status,
			workflow_id = excluded.workflow_id,
			updated_at = excluded.updated_at
	`, proposal.ID, proposal.ProjectID, proposal.GapID, proposal.FeatureName, proposal.Title,
		proposal.Description, proposal.Rationale, proposal.ImpactScore, proposal.ComplexityScore,
		proposal.StrategicFit, proposal.Confidence, string(evidenceJSON), string(hintsJSON),
		proposal.Status, proposal.WorkflowID, proposal.CreatedAt, proposal.UpdatedAt)
	return err
}

func (s *DBStore) GetFeatureProposalsByProject(ctx context.Context, projectID string) ([]FeatureProposal, error) {
	rows, err := s.sql.QueryContext(ctx, `
		SELECT id, project_id, gap_id, feature_name, title, description, rationale, impact_score, complexity_score, strategic_fit, confidence, evidence_json, implementation_hints_json, status, workflow_id, created_at, updated_at
		FROM pi_feature_proposals WHERE project_id = ? ORDER BY impact_score DESC, created_at DESC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("get proposals: %w", err)
	}
	defer rows.Close()

	var proposals []FeatureProposal
	for rows.Next() {
		var p FeatureProposal
		var gapID, workflowID sql.NullString
		var evidenceJSON, hintsJSON string
		if err := rows.Scan(&p.ID, &p.ProjectID, &gapID, &p.FeatureName, &p.Title, &p.Description,
			&p.Rationale, &p.ImpactScore, &p.ComplexityScore, &p.StrategicFit, &p.Confidence,
			&evidenceJSON, &hintsJSON, &p.Status, &workflowID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		p.GapID = gapID.String
		p.WorkflowID = workflowID.String
		if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
			p.Evidence = make(map[string]any)
		}
		if err := json.Unmarshal([]byte(hintsJSON), &p.ImplementationHints); err != nil {
			p.ImplementationHints = []string{}
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

func (s *DBStore) GetFeatureProposalByID(ctx context.Context, id string) (*FeatureProposal, error) {
	var p FeatureProposal
	var gapID, workflowID sql.NullString
	var evidenceJSON, hintsJSON string
	err := s.sql.QueryRowContext(ctx, `
		SELECT id, project_id, gap_id, feature_name, title, description, rationale, impact_score, complexity_score, strategic_fit, confidence, evidence_json, implementation_hints_json, status, workflow_id, created_at, updated_at
		FROM pi_feature_proposals WHERE id = ?
	`, id).Scan(&p.ID, &p.ProjectID, &gapID, &p.FeatureName, &p.Title, &p.Description,
		&p.Rationale, &p.ImpactScore, &p.ComplexityScore, &p.StrategicFit, &p.Confidence,
		&evidenceJSON, &hintsJSON, &p.Status, &workflowID, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get proposal: %w", err)
	}
	p.GapID = gapID.String
	p.WorkflowID = workflowID.String
	if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
		p.Evidence = make(map[string]any)
	}
	if err := json.Unmarshal([]byte(hintsJSON), &p.ImplementationHints); err != nil {
		p.ImplementationHints = []string{}
	}
	return &p, nil
}

func (s *DBStore) GetFeatureProposalsByStatus(ctx context.Context, status ProposalStatus, limit int) ([]FeatureProposal, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.sql.QueryContext(ctx, `
		SELECT id, project_id, gap_id, feature_name, title, description, rationale, impact_score, complexity_score, strategic_fit, confidence, evidence_json, implementation_hints_json, status, workflow_id, created_at, updated_at
		FROM pi_feature_proposals WHERE status = ? ORDER BY impact_score DESC, created_at DESC LIMIT ?
	`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("get proposals by status: %w", err)
	}
	defer rows.Close()

	var proposals []FeatureProposal
	for rows.Next() {
		var p FeatureProposal
		var gapID, workflowID sql.NullString
		var evidenceJSON, hintsJSON string
		if err := rows.Scan(&p.ID, &p.ProjectID, &gapID, &p.FeatureName, &p.Title, &p.Description,
			&p.Rationale, &p.ImpactScore, &p.ComplexityScore, &p.StrategicFit, &p.Confidence,
			&evidenceJSON, &hintsJSON, &p.Status, &workflowID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		p.GapID = gapID.String
		p.WorkflowID = workflowID.String
		if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
			p.Evidence = make(map[string]any)
		}
		if err := json.Unmarshal([]byte(hintsJSON), &p.ImplementationHints); err != nil {
			p.ImplementationHints = []string{}
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

func (s *DBStore) UpdateFeatureProposalStatus(ctx context.Context, id string, status ProposalStatus, workflowID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.sql.ExecContext(ctx, `
		UPDATE pi_feature_proposals SET status = ?, workflow_id = ?, updated_at = ? WHERE id = ?
	`, status, workflowID, now, id)
	return err
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			log.Printf("warning: failed to parse time '%s': %v", s, err)
			return time.Time{}
		}
	}
	return t
}
