package proposals

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type ProposalStore interface {
	SaveProposal(ctx context.Context, proposal Proposal) error
	GetRecentProposals(ctx context.Context, limit int) ([]Proposal, error)
	FindSimilarProposal(ctx context.Context, proposal Proposal, within time.Duration) (*Proposal, error)
	GetProposalsByStatus(ctx context.Context, status ProposalStatus, limit int) ([]Proposal, error)
	GetProposalByID(ctx context.Context, id string) (*Proposal, error)
	UpdateProposalStatus(ctx context.Context, id string, status ProposalStatus) error
}

type DBProposalStore struct {
	database *db.DB
}

func NewDBProposalStore(database *db.DB) *DBProposalStore {
	return &DBProposalStore{database: database}
}

func (s *DBProposalStore) SaveProposal(ctx context.Context, proposal Proposal) error {
	dbProposal := proposalToDB(proposal)
	return s.database.SaveOpenClawProposal(dbProposal)
}

func (s *DBProposalStore) GetRecentProposals(ctx context.Context, limit int) ([]Proposal, error) {
	dbProposals, err := s.database.ListOpenClawProposals("", "", "", 0.0, limit, 0)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}

	proposals := make([]Proposal, len(dbProposals))
	for i, p := range dbProposals {
		proposals[i] = dbProposalToModel(p)
	}
	return proposals, nil
}

func (s *DBProposalStore) FindSimilarProposal(ctx context.Context, proposal Proposal, within time.Duration) (*Proposal, error) {
	affectedEntity := extractAffectedEntity(proposal.Evidence)
	if affectedEntity == "" {
		return nil, nil
	}

	withinHours := int(within.Hours())

	dbProposal, err := s.database.FindSimilarOpenClawProposal(
		string(proposal.Type),
		proposal.SourcePatternID,
		affectedEntity,
		withinHours,
	)
	if err != nil {
		return nil, fmt.Errorf("find similar proposal: %w", err)
	}
	if dbProposal == nil {
		return nil, nil
	}

	proposalModel := dbProposalToModel(*dbProposal)
	return &proposalModel, nil
}

func (s *DBProposalStore) GetProposalsByStatus(ctx context.Context, status ProposalStatus, limit int) ([]Proposal, error) {
	dbProposals, err := s.database.ListOpenClawProposals("", string(status), "", 0.0, limit, 0)
	if err != nil {
		return nil, fmt.Errorf("list proposals by status: %w", err)
	}

	proposals := make([]Proposal, len(dbProposals))
	for i, p := range dbProposals {
		proposals[i] = dbProposalToModel(p)
	}
	return proposals, nil
}

func (s *DBProposalStore) GetProposalByID(ctx context.Context, id string) (*Proposal, error) {
	dbProposal, err := s.database.GetOpenClawProposalByID(id)
	if err != nil {
		return nil, fmt.Errorf("get proposal by id: %w", err)
	}
	if dbProposal == nil {
		return nil, nil
	}

	proposal := dbProposalToModel(*dbProposal)
	return &proposal, nil
}

func (s *DBProposalStore) UpdateProposalStatus(ctx context.Context, id string, status ProposalStatus) error {
	return s.database.UpdateOpenClawProposalStatus(id, string(status))
}

func proposalToDB(p Proposal) *db.OpenClawProposal {
	return &db.OpenClawProposal{
		ID:              p.ID,
		Type:            string(p.Type),
		Status:          string(p.Status),
		Title:           p.Title,
		Description:     p.Description,
		Confidence:      p.Confidence,
		RiskLevel:       string(p.RiskLevel),
		SourcePatternID: p.SourcePatternID,
		Evidence:        p.Evidence,
		Recommendation:  p.Recommendation,
		CreatedAt:       p.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:       p.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func dbProposalToModel(p db.OpenClawProposal) Proposal {
	var createdAt, updatedAt time.Time
	var err error
	if p.CreatedAt != "" {
		createdAt, err = time.Parse(time.RFC3339Nano, p.CreatedAt)
		if err != nil {
			log.Printf("warning: failed to parse created_at timestamp '%s': %v", p.CreatedAt, err)
		}
	}
	if p.UpdatedAt != "" {
		updatedAt, err = time.Parse(time.RFC3339Nano, p.UpdatedAt)
		if err != nil {
			log.Printf("warning: failed to parse updated_at timestamp '%s': %v", p.UpdatedAt, err)
		}
	}

	return Proposal{
		ID:              p.ID,
		Type:            ProposalType(p.Type),
		Status:          ProposalStatus(p.Status),
		Title:           p.Title,
		Description:     p.Description,
		Confidence:      p.Confidence,
		RiskLevel:       RiskLevel(p.RiskLevel),
		SourcePatternID: p.SourcePatternID,
		Evidence:        p.Evidence,
		Recommendation:  p.Recommendation,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}
}

func extractAffectedEntity(evidence map[string]any) string {
	if workflow, ok := evidence["affected_workflow"].(string); ok && workflow != "" {
		return workflow
	}
	if agent, ok := evidence["agent_id"].(string); ok && agent != "" {
		return agent
	}
	return ""
}
