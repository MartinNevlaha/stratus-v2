package proposals

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/patterns"
)

type Engine struct {
	patternStore  patterns.PatternStore
	proposalStore ProposalStore
	generators    []ProposalGenerator
	config        EngineConfig
	mu            sync.Mutex
}

type EngineConfig struct {
	DedupWindowHours int     `json:"dedup_window_hours"`
	MinConfidence    float64 `json:"min_confidence"`
	MaxProposals     int     `json:"max_proposals"`
}

func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		DedupWindowHours: 24,
		MinConfidence:    0.40,
		MaxProposals:     100,
	}
}

func NewEngine(patternStore patterns.PatternStore, proposalStore ProposalStore, config EngineConfig) *Engine {
	if patternStore == nil {
		slog.Warn("insight proposals: pattern store is nil, engine will not function properly")
	}
	if proposalStore == nil {
		slog.Warn("insight proposals: proposal store is nil, engine will not function properly")
	}

	if config.DedupWindowHours <= 0 {
		config.DedupWindowHours = 24
	}
	if config.MinConfidence <= 0 || config.MinConfidence > 1.0 {
		config.MinConfidence = 0.40
	}
	if config.MaxProposals <= 0 {
		config.MaxProposals = 100
	}

	return &Engine{
		patternStore:  patternStore,
		proposalStore: proposalStore,
		config:        config,
		generators: []ProposalGenerator{
			&WorkflowFailureClusterGenerator{},
			&AgentPerformanceDropGenerator{},
			&ReviewRejectionSpikeGenerator{},
			&WorkflowDurationSpikeGenerator{},
			&WorkflowLoopGenerator{},
			&WorkflowReviewFailureGenerator{},
			&WorkflowSlowExecutionGenerator{},
		},
	}
}

func (e *Engine) RunGeneration(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := time.Now()
	slog.Info("insight proposals: generation started")

	patternsList, err := e.patternStore.GetRecentPatterns(ctx, e.config.MaxProposals)
	if err != nil {
		return fmt.Errorf("load patterns: %w", err)
	}

	slog.Debug("insight proposals: loaded patterns", "count", len(patternsList))

	if len(patternsList) == 0 {
		slog.Info("insight proposals: no patterns available for generation")
		return nil
	}

	var proposals []*Proposal
	for _, pattern := range patternsList {
		for _, generator := range e.generators {
			if !generator.CanGenerate(pattern.Type) {
				continue
			}

			proposal, err := generator.Generate(pattern)
			if err != nil {
				slog.Error("proposal generation failed",
					"pattern_id", pattern.ID,
					"generator", generator.Name(),
					"error", err)
				continue
			}

			if proposal.Confidence >= e.config.MinConfidence {
				proposals = append(proposals, proposal)
			}
		}
	}

	slog.Debug("insight proposals: generated proposals", "count", len(proposals))

	var saved, skipped int
	for _, proposal := range proposals {
		isDup, err := e.isDuplicate(ctx, *proposal)
		if err != nil {
			slog.Error("deduplication check failed", "error", err, "proposal_id", proposal.ID)
			continue
		}

		if isDup {
			skipped++
			continue
		}

		if err := e.proposalStore.SaveProposal(ctx, *proposal); err != nil {
			slog.Error("save proposal failed", "error", err, "proposal_id", proposal.ID)
			continue
		}

		saved++

		slog.Info("proposal generated",
			"id", proposal.ID,
			"type", proposal.Type,
			"risk", proposal.RiskLevel,
			"confidence", proposal.Confidence,
			"pattern_id", proposal.SourcePatternID)
	}

	duration := time.Since(start)
	slog.Info("insight proposals: generation complete",
		"generated", len(proposals),
		"saved", saved,
		"deduplicated", skipped,
		"duration_ms", duration.Milliseconds())

	return nil
}

func (e *Engine) isDuplicate(ctx context.Context, proposal Proposal) (bool, error) {
	dedupWindow := time.Duration(e.config.DedupWindowHours) * time.Hour

	similar, err := e.proposalStore.FindSimilarProposal(ctx, proposal, dedupWindow)
	if err != nil {
		return false, err
	}

	if similar != nil {
		slog.Info("proposal deduplicated",
			"type", proposal.Type,
			"pattern_id", proposal.SourcePatternID,
			"similar_id", similar.ID)
		return true, nil
	}

	return false, nil
}

func (e *Engine) AddGenerator(generator ProposalGenerator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.generators = append(e.generators, generator)
}

func (e *Engine) SetConfig(config EngineConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

func (e *Engine) Config() EngineConfig {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.config
}

func (e *Engine) GetRecentProposals(ctx context.Context, limit int) ([]Proposal, error) {
	return e.proposalStore.GetRecentProposals(ctx, limit)
}

func (e *Engine) GetProposalsByStatus(ctx context.Context, status ProposalStatus, limit int) ([]Proposal, error) {
	return e.proposalStore.GetProposalsByStatus(ctx, status, limit)
}

func (e *Engine) GetProposalByID(ctx context.Context, id string) (*Proposal, error) {
	return e.proposalStore.GetProposalByID(ctx, id)
}

func (e *Engine) UpdateProposalStatus(ctx context.Context, id string, status ProposalStatus) error {
	return e.proposalStore.UpdateProposalStatus(ctx, id, status)
}
