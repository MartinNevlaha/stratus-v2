package proposals

import (
	"context"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/openclaw/patterns"
)

type MockPatternStore struct {
	patterns []patterns.Pattern
}

func (m *MockPatternStore) SavePattern(ctx context.Context, pattern patterns.Pattern) error {
	m.patterns = append(m.patterns, pattern)
	return nil
}

func (m *MockPatternStore) GetRecentPatterns(ctx context.Context, limit int) ([]patterns.Pattern, error) {
	if limit > len(m.patterns) {
		return m.patterns, nil
	}
	return m.patterns[:limit], nil
}

func (m *MockPatternStore) GetPatternsByType(ctx context.Context, patternType patterns.PatternType, limit int) ([]patterns.Pattern, error) {
	var result []patterns.Pattern
	for _, p := range m.patterns {
		if p.Type == patternType {
			result = append(result, p)
		}
	}
	if limit > 0 && len(result) > limit {
		return result[:limit], nil
	}
	return result, nil
}

func (m *MockPatternStore) FindPatternByName(ctx context.Context, patternName string) (*patterns.Pattern, error) {
	for _, p := range m.patterns {
		if string(p.Type) == patternName {
			return &p, nil
		}
	}
	return nil, nil
}

func (m *MockPatternStore) UpdatePattern(ctx context.Context, pattern patterns.Pattern) error {
	for i, p := range m.patterns {
		if p.ID == pattern.ID {
			m.patterns[i] = pattern
			return nil
		}
	}
	return nil
}

type MockProposalStore struct {
	proposals []Proposal
}

func (m *MockProposalStore) SaveProposal(ctx context.Context, proposal Proposal) error {
	m.proposals = append(m.proposals, proposal)
	return nil
}

func (m *MockProposalStore) GetRecentProposals(ctx context.Context, limit int) ([]Proposal, error) {
	if limit > len(m.proposals) {
		return m.proposals, nil
	}
	return m.proposals[:limit], nil
}

func (m *MockProposalStore) FindSimilarProposal(ctx context.Context, proposal Proposal, within time.Duration) (*Proposal, error) {
	for _, p := range m.proposals {
		if p.Type == proposal.Type && p.SourcePatternID == proposal.SourcePatternID {
			timeDiff := time.Since(p.CreatedAt)
			if timeDiff <= within {
				return &p, nil
			}
		}
	}
	return nil, nil
}

func (m *MockProposalStore) GetProposalsByStatus(ctx context.Context, status ProposalStatus, limit int) ([]Proposal, error) {
	var result []Proposal
	for _, p := range m.proposals {
		if p.Status == status {
			result = append(result, p)
		}
	}
	if limit > 0 && len(result) > limit {
		return result[:limit], nil
	}
	return result, nil
}

func (m *MockProposalStore) GetProposalByID(ctx context.Context, id string) (*Proposal, error) {
	for _, p := range m.proposals {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, nil
}

func (m *MockProposalStore) UpdateProposalStatus(ctx context.Context, id string, status ProposalStatus) error {
	for i, p := range m.proposals {
		if p.ID == id {
			m.proposals[i].Status = status
			return nil
		}
	}
	return nil
}

func TestGenerateProposalFromWorkflowFailureCluster(t *testing.T) {
	pattern := patterns.Pattern{
		ID:          "test-pattern-1",
		Type:        patterns.PatternWorkflowFailureCluster,
		Timestamp:   time.Now().UTC(),
		Severity:    patterns.SeverityCritical,
		Description: "Test failure cluster",
		Evidence: map[string]any{
			"failure_rate":      0.75,
			"failed_count":      15,
			"completed_count":   5,
			"total_count":       20,
			"affected_workflow": "spec-complex",
		},
		Frequency:  5,
		Confidence: 0.80,
		FirstSeen:  time.Now().Add(-24 * time.Hour).UTC(),
		LastSeen:   time.Now().UTC(),
	}

	generator := &WorkflowFailureClusterGenerator{}

	if !generator.CanGenerate(patterns.PatternWorkflowFailureCluster) {
		t.Fatal("Generator should be able to handle workflow failure cluster pattern")
	}

	proposal, err := generator.Generate(pattern)
	if err != nil {
		t.Fatalf("Failed to generate proposal: %v", err)
	}

	if proposal.Type != ProposalTypeRoutingChange {
		t.Errorf("Expected proposal type %s, got %s", ProposalTypeRoutingChange, proposal.Type)
	}

	if proposal.SourcePatternID != pattern.ID {
		t.Errorf("Expected source pattern ID %s, got %s", pattern.ID, proposal.SourcePatternID)
	}

	if proposal.Confidence < 0.0 || proposal.Confidence > 1.0 {
		t.Errorf("Confidence should be between 0 and 1, got %f", proposal.Confidence)
	}

	if proposal.RiskLevel != RiskHigh {
		t.Errorf("Expected risk level %s for critical severity with high confidence, got %s", RiskHigh, proposal.RiskLevel)
	}

	if proposal.Status != ProposalStatusDetected {
		t.Errorf("Expected status %s, got %s", ProposalStatusDetected, proposal.Status)
	}

	if _, ok := proposal.Recommendation["suggested_action"]; !ok {
		t.Error("Recommendation should contain suggested_action field")
	}
}

func TestGenerateProposalFromAgentPerformanceDrop(t *testing.T) {
	pattern := patterns.Pattern{
		ID:          "test-pattern-2",
		Type:        patterns.PatternAgentPerformanceDrop,
		Timestamp:   time.Now().UTC(),
		Severity:    patterns.SeverityHigh,
		Description: "Test agent performance drop",
		Evidence: map[string]any{
			"performance_drop": 0.35,
			"agent_id":         "mobile-dev-specialist",
			"current_rate":     0.62,
			"previous_rate":    0.86,
		},
		Frequency:  3,
		Confidence: 0.75,
		FirstSeen:  time.Now().Add(-12 * time.Hour).UTC(),
		LastSeen:   time.Now().UTC(),
	}

	generator := &AgentPerformanceDropGenerator{}

	if !generator.CanGenerate(patterns.PatternAgentPerformanceDrop) {
		t.Fatal("Generator should be able to handle agent performance drop pattern")
	}

	proposal, err := generator.Generate(pattern)
	if err != nil {
		t.Fatalf("Failed to generate proposal: %v", err)
	}

	if proposal.Type != ProposalTypeAgentDeprioritize {
		t.Errorf("Expected proposal type %s for 35%% drop, got %s", ProposalTypeAgentDeprioritize, proposal.Type)
	}

	if agentID, ok := proposal.Recommendation["agent_id"].(string); !ok || agentID != "mobile-dev-specialist" {
		t.Errorf("Recommendation should contain correct agent_id")
	}
}

func TestProposalDeduplication(t *testing.T) {
	pattern := patterns.Pattern{
		ID:          "test-pattern-3",
		Type:        patterns.PatternWorkflowFailureCluster,
		Timestamp:   time.Now().UTC(),
		Severity:    patterns.SeverityMedium,
		Description: "Test deduplication",
		Evidence: map[string]any{
			"failure_rate":      0.40,
			"failed_count":      8,
			"completed_count":   12,
			"total_count":       20,
			"affected_workflow": "spec-simple",
		},
		Frequency:  2,
		Confidence: 0.65,
		FirstSeen:  time.Now().Add(-6 * time.Hour).UTC(),
		LastSeen:   time.Now().UTC(),
	}

	existingProposal := Proposal{
		ID:              "existing-proposal-1",
		Type:            ProposalTypeWorkflowInvestigation,
		Status:          ProposalStatusDetected,
		Title:           "Test proposal",
		Description:     "Test description",
		Confidence:      0.65,
		RiskLevel:       RiskLow,
		SourcePatternID: pattern.ID,
		Evidence:        pattern.Evidence,
		Recommendation:  map[string]any{},
		CreatedAt:       time.Now().Add(-2 * time.Hour).UTC(),
		UpdatedAt:       time.Now().Add(-2 * time.Hour).UTC(),
	}

	patternStore := &MockPatternStore{
		patterns: []patterns.Pattern{pattern},
	}

	proposalStore := &MockProposalStore{
		proposals: []Proposal{existingProposal},
	}

	engine := NewEngine(patternStore, proposalStore, DefaultEngineConfig())

	err := engine.RunGeneration(context.Background())
	if err != nil {
		t.Fatalf("RunGeneration failed: %v", err)
	}

	if len(proposalStore.proposals) != 1 {
		t.Errorf("Expected 1 proposal (existing), got %d", len(proposalStore.proposals))
	}
}

func TestProposalConfidenceScoring(t *testing.T) {
	tests := []struct {
		name            string
		pattern         patterns.Pattern
		minExpectedConf float64
		maxExpectedConf float64
	}{
		{
			name: "low confidence pattern",
			pattern: patterns.Pattern{
				Severity:   patterns.SeverityLow,
				Frequency:  1,
				Confidence: 0.40,
				Evidence:   map[string]any{"total_count": 5},
				LastSeen:   time.Now().Add(-12 * time.Hour).UTC(),
			},
			minExpectedConf: 0.40,
			maxExpectedConf: 0.60,
		},
		{
			name: "medium confidence pattern",
			pattern: patterns.Pattern{
				Severity:   patterns.SeverityMedium,
				Frequency:  3,
				Confidence: 0.65,
				Evidence:   map[string]any{"total_count": 10},
				LastSeen:   time.Now().Add(-3 * time.Hour).UTC(),
			},
			minExpectedConf: 0.70,
			maxExpectedConf: 0.85,
		},
		{
			name: "high confidence pattern",
			pattern: patterns.Pattern{
				Severity:   patterns.SeverityCritical,
				Frequency:  7,
				Confidence: 0.85,
				Evidence:   map[string]any{"total_count": 25},
				LastSeen:   time.Now().UTC(),
			},
			minExpectedConf: 0.90,
			maxExpectedConf: 0.95,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			confidence := calculateConfidence(tt.pattern)

			if confidence < tt.minExpectedConf {
				t.Errorf("Confidence %f is below minimum expected %f", confidence, tt.minExpectedConf)
			}

			if confidence > tt.maxExpectedConf {
				t.Errorf("Confidence %f exceeds maximum expected %f", confidence, tt.maxExpectedConf)
			}

			if confidence > 0.95 {
				t.Errorf("Confidence should be capped at 0.95, got %f", confidence)
			}
		})
	}
}

func TestProposalStoreSaveAndLoad(t *testing.T) {
	pattern := patterns.Pattern{
		ID:          "test-pattern-4",
		Type:        patterns.PatternWorkflowFailureCluster,
		Timestamp:   time.Now().UTC(),
		Severity:    patterns.SeverityHigh,
		Description: "Test pattern",
		Evidence: map[string]any{
			"failure_rate":      0.60,
			"failed_count":      12,
			"total_count":       20,
			"affected_workflow": "bug-fix",
		},
		Frequency:  4,
		Confidence: 0.75,
		FirstSeen:  time.Now().Add(-8 * time.Hour).UTC(),
		LastSeen:   time.Now().UTC(),
	}

	proposal := NewProposal(
		ProposalTypeReviewGateAddition,
		"Test proposal",
		"Test description",
		pattern,
		map[string]any{
			"workflow_type":    "bug-fix",
			"suggested_action": "add_review_gate",
		},
	)

	proposalStore := &MockProposalStore{}

	err := proposalStore.SaveProposal(context.Background(), proposal)
	if err != nil {
		t.Fatalf("Failed to save proposal: %v", err)
	}

	loaded, err := proposalStore.GetRecentProposals(context.Background(), 10)
	if err != nil {
		t.Fatalf("Failed to load proposals: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("Expected 1 proposal, got %d", len(loaded))
	}

	if loaded[0].ID != proposal.ID {
		t.Errorf("Expected ID %s, got %s", proposal.ID, loaded[0].ID)
	}

	if loaded[0].Type != proposal.Type {
		t.Errorf("Expected type %s, got %s", proposal.Type, loaded[0].Type)
	}

	if loaded[0].Confidence != proposal.Confidence {
		t.Errorf("Expected confidence %f, got %f", proposal.Confidence, loaded[0].Confidence)
	}
}

func TestMultipleGenerators(t *testing.T) {
	testPatterns := []patterns.Pattern{
		{
			ID:         "pattern-1",
			Type:       patterns.PatternWorkflowFailureCluster,
			Severity:   patterns.SeverityHigh,
			Evidence:   map[string]any{"failure_rate": 0.60, "affected_workflow": "spec-simple"},
			Frequency:  3,
			Confidence: 0.70,
			LastSeen:   time.Now().UTC(),
		},
		{
			ID:         "pattern-2",
			Type:       patterns.PatternAgentPerformanceDrop,
			Severity:   patterns.SeverityMedium,
			Evidence:   map[string]any{"performance_drop": 0.25, "agent_id": "test-agent"},
			Frequency:  2,
			Confidence: 0.65,
			LastSeen:   time.Now().UTC(),
		},
		{
			ID:         "pattern-3",
			Type:       patterns.PatternReviewRejectionSpike,
			Severity:   patterns.SeverityLow,
			Evidence:   map[string]any{"rejection_rate": 0.45, "affected_workflow": "spec-complex"},
			Frequency:  1,
			Confidence: 0.60,
			LastSeen:   time.Now().UTC(),
		},
	}

	patternStore := &MockPatternStore{patterns: testPatterns}
	proposalStore := &MockProposalStore{}
	engine := NewEngine(patternStore, proposalStore, DefaultEngineConfig())

	err := engine.RunGeneration(context.Background())
	if err != nil {
		t.Fatalf("RunGeneration failed: %v", err)
	}

	if len(proposalStore.proposals) != 3 {
		t.Errorf("Expected 3 proposals from 3 patterns, got %d", len(proposalStore.proposals))
	}

	proposalTypes := make(map[ProposalType]bool)
	for _, p := range proposalStore.proposals {
		proposalTypes[p.Type] = true
	}

	if len(proposalTypes) < 2 {
		t.Errorf("Expected at least 2 different proposal types, got %d", len(proposalTypes))
	}
}

func TestRiskLevelDetermination(t *testing.T) {
	tests := []struct {
		name         string
		severity     patterns.SeverityLevel
		confidence   float64
		expectedRisk RiskLevel
	}{
		{
			name:         "critical high confidence",
			severity:     patterns.SeverityCritical,
			confidence:   0.80,
			expectedRisk: RiskHigh,
		},
		{
			name:         "critical low confidence",
			severity:     patterns.SeverityCritical,
			confidence:   0.65,
			expectedRisk: RiskLow,
		},
		{
			name:         "high severity",
			severity:     patterns.SeverityHigh,
			confidence:   0.70,
			expectedRisk: RiskMedium,
		},
		{
			name:         "medium severity",
			severity:     patterns.SeverityMedium,
			confidence:   0.75,
			expectedRisk: RiskLow,
		},
		{
			name:         "low severity",
			severity:     patterns.SeverityLow,
			confidence:   0.85,
			expectedRisk: RiskLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			risk := determineRiskLevel(tt.severity, tt.confidence)
			if risk != tt.expectedRisk {
				t.Errorf("Expected risk level %s, got %s", tt.expectedRisk, risk)
			}
		})
	}
}
