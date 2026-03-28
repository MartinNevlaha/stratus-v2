package product_intelligence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

type Engine struct {
	config           EngineConfig
	store            Store
	llm              llm.Client
	domainDetector   *DomainDetector
	featureExtractor *FeatureExtractor
	marketResearch   *MarketResearchEngine
	gapAnalyzer      *GapAnalyzer
	featureScorer    *FeatureScorer
	proposalEngine   *ProposalEngine
}

func NewEngine(store Store, config EngineConfig, llmClient llm.Client) *Engine {
	return &Engine{
		config:           config,
		store:            store,
		llm:              llmClient,
		domainDetector:   NewDomainDetector(DefaultDomainDetectorConfig(), llmClient),
		featureExtractor: NewFeatureExtractor(DefaultFeatureExtractorConfig(), llmClient),
		marketResearch:   NewMarketResearchEngine(DefaultMarketResearchConfig(), store, llmClient),
		gapAnalyzer:      NewGapAnalyzer(DefaultGapAnalyzerConfig(), store, llmClient),
		featureScorer:    NewFeatureScorer(DefaultFeatureScorerConfig(), store, llmClient),
		proposalEngine:   NewProposalEngine(DefaultProposalEngineConfig(), store, llmClient),
	}
}

func (e *Engine) RegisterProject(ctx context.Context, projectPath, projectName string) (*Project, error) {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	existing, err := e.store.GetProjectByPath(ctx, absPath)
	if err != nil {
		return nil, fmt.Errorf("check existing: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	project := &Project{
		ID:        generateProjectID(absPath),
		Name:      projectName,
		Path:      absPath,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	if err := e.store.SaveProject(ctx, *project); err != nil {
		return nil, fmt.Errorf("save project: %w", err)
	}

	return project, nil
}

func (e *Engine) AnalyzeProject(ctx context.Context, projectID string, cfg ProjectAnalysisConfig) (*AnalysisResult, error) {
	start := time.Now()
	result := &AnalysisResult{
		ProjectID:  projectID,
		AnalyzedAt: time.Now(),
	}

	project, err := e.store.GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	if project == nil {
		return nil, fmt.Errorf("project not found: %s", projectID)
	}

	if !cfg.SkipDomainDetection {
		domain, err := e.detectDomain(ctx, project)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("domain detection: %v", err))
			slog.Warn("product_intelligence: domain detection failed", "error", err)
		} else {
			result.Domain = domain
		}
	}

	if !cfg.SkipFeatureExtraction {
		features, err := e.extractFeatures(ctx, project)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("feature extraction: %v", err))
			slog.Warn("product_intelligence: feature extraction failed", "error", err)
		} else {
			result.Features = features
		}
	}

	if !cfg.SkipMarketResearch && result.Domain != nil {
		marketFeatures, err := e.researchMarket(ctx, result.Domain.Domain)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("market research: %v", err))
			slog.Warn("product_intelligence: market research failed", "error", err)
		}

		if !cfg.SkipGapAnalysis && len(marketFeatures) > 0 {
			gaps, err := e.analyzeGaps(ctx, project.ID, result.Features, marketFeatures)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("gap analysis: %v", err))
				slog.Warn("product_intelligence: gap analysis failed", "error", err)
			} else {
				result.Gaps = gaps
			}

			if !cfg.SkipScoring && len(gaps) > 0 {
				scored, err := e.scoreFeatures(ctx, gaps, result.Features)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("scoring: %v", err))
					slog.Warn("product_intelligence: scoring failed", "error", err)
				}

				if !cfg.SkipProposalGeneration && len(scored) > 0 {
					proposals, err := e.generateProposals(ctx, project.ID, gaps, scored, result.Features)
					if err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("proposal generation: %v", err))
						slog.Warn("product_intelligence: proposal generation failed", "error", err)
					} else {
						result.Proposals = proposals
					}
				}
			}
		}
	}

	if err := e.store.UpdateProjectLastAnalyzed(ctx, projectID); err != nil {
		slog.Warn("product_intelligence: failed to update last analyzed", "error", err)
	}

	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

func (e *Engine) detectDomain(ctx context.Context, project *Project) (*DomainResult, error) {
	domain, err := e.domainDetector.Detect(ctx, project.Path)
	if err != nil {
		return nil, fmt.Errorf("detect domain: %w", err)
	}

	if err := e.store.UpdateProjectDomain(ctx, project.ID, domain.Domain, domain.Confidence); err != nil {
		slog.Warn("product_intelligence: failed to update domain", "error", err)
	}

	slog.Info("product_intelligence: domain detected",
		"project", project.ID,
		"domain", domain.Domain,
		"confidence", domain.Confidence)

	return domain, nil
}

func (e *Engine) extractFeatures(ctx context.Context, project *Project) ([]ProjectFeature, error) {
	features, err := e.featureExtractor.Extract(ctx, project.Path, project.ID)
	if err != nil {
		return nil, fmt.Errorf("extract features: %w", err)
	}

	for _, feature := range features {
		if err := e.store.SaveProjectFeature(ctx, feature); err != nil {
			slog.Warn("product_intelligence: failed to save feature",
				"feature", feature.FeatureName,
				"error", err)
		}
	}

	slog.Info("product_intelligence: features extracted",
		"project", project.ID,
		"count", len(features))

	return features, nil
}

func (e *Engine) researchMarket(ctx context.Context, domain string) ([]MarketFeature, error) {
	features, err := e.marketResearch.Research(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("research market: %w", err)
	}

	slog.Info("product_intelligence: market research complete",
		"domain", domain,
		"features", len(features))

	return features, nil
}

func (e *Engine) analyzeGaps(ctx context.Context, projectID string, projectFeatures []ProjectFeature, marketFeatures []MarketFeature) ([]FeatureGap, error) {
	gaps, err := e.gapAnalyzer.Analyze(ctx, projectID, projectFeatures, marketFeatures)
	if err != nil {
		return nil, fmt.Errorf("analyze gaps: %w", err)
	}

	for _, gap := range gaps {
		if err := e.store.SaveFeatureGap(ctx, gap); err != nil {
			slog.Warn("product_intelligence: failed to save gap",
				"feature", gap.FeatureName,
				"error", err)
		}
	}

	slog.Info("product_intelligence: gap analysis complete",
		"project", projectID,
		"gaps", len(gaps))

	return gaps, nil
}

func (e *Engine) scoreFeatures(ctx context.Context, gaps []FeatureGap, projectFeatures []ProjectFeature) ([]ScoredFeature, error) {
	scored, err := e.featureScorer.Score(ctx, gaps, projectFeatures)
	if err != nil {
		return nil, fmt.Errorf("score features: %w", err)
	}

	for i := range gaps {
		for _, s := range scored {
			if s.FeatureName == gaps[i].FeatureName {
				gaps[i].ImpactScore = s.ImpactScore
				gaps[i].ComplexityScore = s.ComplexityScore
				gaps[i].StrategicFit = s.StrategicFit
				gaps[i].Confidence = s.Confidence
				gaps[i].Status = GapStatusScored
				if err := e.store.SaveFeatureGap(ctx, gaps[i]); err != nil {
					slog.Warn("product_intelligence: failed to update gap score", "error", err)
				}
				break
			}
		}
	}

	slog.Info("product_intelligence: feature scoring complete",
		"count", len(scored))

	return scored, nil
}

func (e *Engine) generateProposals(ctx context.Context, projectID string, gaps []FeatureGap, scored []ScoredFeature, projectFeatures []ProjectFeature) ([]FeatureProposal, error) {
	proposals, err := e.proposalEngine.Generate(ctx, projectID, gaps, scored, projectFeatures)
	if err != nil {
		return nil, fmt.Errorf("generate proposals: %w", err)
	}

	if e.config.MaxProposalsPerRun > 0 && len(proposals) > e.config.MaxProposalsPerRun {
		proposals = proposals[:e.config.MaxProposalsPerRun]
	}

	for _, proposal := range proposals {
		if err := e.store.SaveFeatureProposal(ctx, proposal); err != nil {
			slog.Warn("product_intelligence: failed to save proposal",
				"feature", proposal.FeatureName,
				"error", err)
		}
	}

	slog.Info("product_intelligence: proposals generated",
		"project", projectID,
		"count", len(proposals))

	return proposals, nil
}

func (e *Engine) GetProject(ctx context.Context, projectID string) (*Project, error) {
	return e.store.GetProjectByID(ctx, projectID)
}

func (e *Engine) ListProjects(ctx context.Context, limit int) ([]Project, error) {
	return e.store.ListProjects(ctx, limit)
}

func (e *Engine) GetProjectFeatures(ctx context.Context, projectID string) ([]ProjectFeature, error) {
	return e.store.GetProjectFeatures(ctx, projectID)
}

func (e *Engine) GetFeatureGaps(ctx context.Context, projectID string) ([]FeatureGap, error) {
	return e.store.GetFeatureGapsByProject(ctx, projectID)
}

func (e *Engine) GetFeatureProposals(ctx context.Context, projectID string) ([]FeatureProposal, error) {
	return e.store.GetFeatureProposalsByProject(ctx, projectID)
}

func (e *Engine) GetProposalByID(ctx context.Context, proposalID string) (*FeatureProposal, error) {
	return e.store.GetFeatureProposalByID(ctx, proposalID)
}

func (e *Engine) AcceptProposal(ctx context.Context, proposalID string, workflowID string) error {
	return e.store.UpdateFeatureProposalStatus(ctx, proposalID, ProposalStatusAccepted, workflowID)
}

func (e *Engine) RejectProposal(ctx context.Context, proposalID string, reason string) error {
	return e.store.UpdateFeatureProposalStatus(ctx, proposalID, ProposalStatusRejected, "")
}

func (e *Engine) DeleteProject(ctx context.Context, projectID string) error {
	return e.store.DeleteProject(ctx, projectID)
}

func (e *Engine) GetMarketFeatures(ctx context.Context, domain string) ([]MarketFeature, error) {
	return e.store.GetMarketFeaturesByDomain(ctx, domain)
}

func (e *Engine) RefreshMarketResearch(ctx context.Context, domain string) error {
	return e.marketResearch.RefreshResearch(ctx, domain)
}

func generateProjectID(path string) string {
	normalized := strings.TrimSuffix(strings.TrimSuffix(path, "/"), "\\")
	hash := sha256.Sum256([]byte(normalized))
	return "proj_" + hex.EncodeToString(hash[:8])
}
