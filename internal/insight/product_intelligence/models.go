package product_intelligence

import (
	"time"
)

type FeatureType string

const (
	FeatureTypeCapability  FeatureType = "capability"
	FeatureTypeModule      FeatureType = "module"
	FeatureTypeAPI         FeatureType = "api"
	FeatureTypeUI          FeatureType = "ui"
	FeatureTypeIntegration FeatureType = "integration"
	FeatureTypeAuth        FeatureType = "auth"
	FeatureTypeAnalytics   FeatureType = "analytics"
	FeatureTypeReporting   FeatureType = "reporting"
)

type GapType string

const (
	GapTypeMissing     GapType = "missing"
	GapTypeWeak        GapType = "weak"
	GapTypeEnhancement GapType = "enhancement"
)

type ProposalStatus string

const (
	ProposalStatusProposed   ProposalStatus = "proposed"
	ProposalStatusAccepted   ProposalStatus = "accepted"
	ProposalStatusRejected   ProposalStatus = "rejected"
	ProposalStatusInProgress ProposalStatus = "in_progress"
	ProposalStatusCompleted  ProposalStatus = "completed"
)

type GapStatus string

const (
	GapStatusIdentified GapStatus = "identified"
	GapStatusScored     GapStatus = "scored"
	GapStatusProposed   GapStatus = "proposed"
)

type Project struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Path             string  `json:"path"`
	Domain           string  `json:"domain"`
	DomainConfidence float64 `json:"domain_confidence"`
	ReadmeHash       string  `json:"readme_hash,omitempty"`
	LastAnalyzed     string  `json:"last_analyzed,omitempty"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type ProjectFeature struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id"`
	FeatureName string         `json:"feature_name"`
	FeatureType FeatureType    `json:"feature_type"`
	Description string         `json:"description"`
	Evidence    map[string]any `json:"evidence"`
	Confidence  float64        `json:"confidence"`
	Source      string         `json:"source"`
	DetectedAt  string         `json:"detected_at"`
}

type MarketFeature struct {
	ID           string      `json:"id"`
	Domain       string      `json:"domain"`
	FeatureName  string      `json:"feature_name"`
	FeatureType  FeatureType `json:"feature_type"`
	Prevalence   float64     `json:"prevalence"`
	Importance   float64     `json:"importance"`
	Sources      []string    `json:"sources"`
	DiscoveredAt string      `json:"discovered_at"`
}

type FeatureGap struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	FeatureName     string    `json:"feature_name"`
	GapType         GapType   `json:"gap_type"`
	ImpactScore     float64   `json:"impact_score"`
	ComplexityScore float64   `json:"complexity_score"`
	StrategicFit    float64   `json:"strategic_fit"`
	Confidence      float64   `json:"confidence"`
	Reasoning       string    `json:"reasoning"`
	Status          GapStatus `json:"status"`
	CreatedAt       string    `json:"created_at"`
}

type FeatureProposal struct {
	ID                  string         `json:"id"`
	ProjectID           string         `json:"project_id"`
	GapID               string         `json:"gap_id,omitempty"`
	FeatureName         string         `json:"feature_name"`
	Title               string         `json:"title"`
	Description         string         `json:"description"`
	Rationale           string         `json:"rationale"`
	ImpactScore         int            `json:"impact_score"`
	ComplexityScore     int            `json:"complexity_score"`
	StrategicFit        float64        `json:"strategic_fit"`
	Confidence          float64        `json:"confidence"`
	Evidence            map[string]any `json:"evidence"`
	ImplementationHints []string       `json:"implementation_hints"`
	Status              ProposalStatus `json:"status"`
	WorkflowID          string         `json:"workflow_id,omitempty"`
	CreatedAt           string         `json:"created_at"`
	UpdatedAt           string         `json:"updated_at"`
}

type DomainResult struct {
	Domain     string   `json:"domain"`
	Confidence float64  `json:"confidence"`
	Keywords   []string `json:"keywords"`
	Industry   string   `json:"industry,omitempty"`
	Subdomain  string   `json:"subdomain,omitempty"`
}

type FeatureEvidence struct {
	Source      string   `json:"source"`
	Location    string   `json:"location"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords,omitempty"`
}

type GapAnalysisResult struct {
	ProjectID       string       `json:"project_id"`
	Domain          string       `json:"domain"`
	TotalGaps       int          `json:"total_gaps"`
	MissingFeatures []FeatureGap `json:"missing_features"`
	WeakFeatures    []FeatureGap `json:"weak_features"`
	Enhancements    []FeatureGap `json:"enhancements"`
	AnalyzedAt      time.Time    `json:"analyzed_at"`
}

type ScoredFeature struct {
	FeatureName     string  `json:"feature_name"`
	ImpactScore     float64 `json:"impact_score"`
	ComplexityScore float64 `json:"complexity_score"`
	StrategicFit    float64 `json:"strategic_fit"`
	OverallScore    float64 `json:"overall_score"`
	Confidence      float64 `json:"confidence"`
	Reasoning       string  `json:"reasoning"`
}

type AnalysisResult struct {
	ProjectID  string            `json:"project_id"`
	Domain     *DomainResult     `json:"domain,omitempty"`
	Features   []ProjectFeature  `json:"features"`
	Gaps       []FeatureGap      `json:"gaps"`
	Proposals  []FeatureProposal `json:"proposals"`
	DurationMs int64             `json:"duration_ms"`
	AnalyzedAt time.Time         `json:"analyzed_at"`
	Errors     []string          `json:"errors,omitempty"`
}

type ProjectAnalysisConfig struct {
	ProjectPath            string        `json:"project_path"`
	ProjectName            string        `json:"project_name"`
	SkipDomainDetection    bool          `json:"skip_domain_detection"`
	SkipFeatureExtraction  bool          `json:"skip_feature_extraction"`
	SkipMarketResearch     bool          `json:"skip_market_research"`
	SkipGapAnalysis        bool          `json:"skip_gap_analysis"`
	SkipScoring            bool          `json:"skip_scoring"`
	SkipProposalGeneration bool          `json:"skip_proposal_generation"`
	ForceRefresh           bool          `json:"force_refresh"`
	FeatureTypes           []FeatureType `json:"feature_types,omitempty"`
}

type EngineConfig struct {
	MinDomainConfidence   float64 `json:"min_domain_confidence"`
	MinFeatureConfidence  float64 `json:"min_feature_confidence"`
	MinGapConfidence      float64 `json:"min_gap_confidence"`
	MinProposalConfidence float64 `json:"min_proposal_confidence"`
	MaxFeaturesPerProject int     `json:"max_features_per_project"`
	MaxGapsPerProject     int     `json:"max_gaps_per_project"`
	MaxProposalsPerRun    int     `json:"max_proposals_per_run"`
	ImpactWeight          float64 `json:"impact_weight"`
	ComplexityWeight      float64 `json:"complexity_weight"`
	StrategicFitWeight    float64 `json:"strategic_fit_weight"`
}

func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		MinDomainConfidence:   0.5,
		MinFeatureConfidence:  0.4,
		MinGapConfidence:      0.3,
		MinProposalConfidence: 0.5,
		MaxFeaturesPerProject: 100,
		MaxGapsPerProject:     50,
		MaxProposalsPerRun:    10,
		ImpactWeight:          0.4,
		ComplexityWeight:      0.3,
		StrategicFitWeight:    0.3,
	}
}
