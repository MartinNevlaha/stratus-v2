package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Sentinel errors for evolution config validation.
var (
	// ErrTokenCapRequired is returned when Evolution is enabled but MaxTokensPerCycle is not set.
	ErrTokenCapRequired = errors.New("evolution: MaxTokensPerCycle must be > 0 when Evolution is enabled")

	// ErrInvalidScoringWeights is returned when any ScoringWeights field is out of range.
	ErrInvalidScoringWeights = errors.New("evolution: invalid ScoringWeights")

	// ErrInvalidBaselineLimits is returned when any BaselineLimits field is negative.
	ErrInvalidBaselineLimits = errors.New("evolution: invalid BaselineLimits")

	// ErrInvalidCategory is returned when an unknown or disallowed evolution category is used.
	ErrInvalidCategory = errors.New("evolution: invalid evolution category")

	// ErrInvalidWikiConfig is returned when a WikiConfig field violates its bounds.
	// 0 is the unlimited sentinel for page/token count fields; negative values are rejected.
	ErrInvalidWikiConfig = errors.New("wiki: invalid config")
)

// ValidateWikiConfig checks WikiConfig bounds. It enforces:
//   - MaxPagesPerIngest >= 0  (0 = unlimited sentinel)
//   - OnboardingMaxPages >= 0 (0 = unlimited sentinel)
//   - IngestTokenBudget >= 0  (0 = unlimited sentinel)
//
// All other fields are not validated here; use the API-layer validateWikiConfig
// for VaultPath resolution and enum checks.
func ValidateWikiConfig(cfg *WikiConfig) error {
	if cfg.MaxPagesPerIngest < 0 {
		return fmt.Errorf("%w: MaxPagesPerIngest=%d must be >= 0 (0 = unlimited)", ErrInvalidWikiConfig, cfg.MaxPagesPerIngest)
	}
	if cfg.OnboardingMaxPages < 0 {
		return fmt.Errorf("%w: OnboardingMaxPages=%d must be >= 0 (0 = unlimited)", ErrInvalidWikiConfig, cfg.OnboardingMaxPages)
	}
	if cfg.IngestTokenBudget < 0 {
		return fmt.Errorf("%w: IngestTokenBudget=%d must be >= 0 (0 = unlimited)", ErrInvalidWikiConfig, cfg.IngestTokenBudget)
	}
	if cfg.ClusterIntervalMinutes != 0 && cfg.ClusterIntervalMinutes < 5 {
		return fmt.Errorf("%w: ClusterIntervalMinutes=%d must be 0 or >= 5", ErrInvalidWikiConfig, cfg.ClusterIntervalMinutes)
	}
	if cfg.ClusterMinSources < 0 || (cfg.ClusterMinSources > 0 && cfg.ClusterMinSources < 2) {
		return fmt.Errorf("%w: ClusterMinSources=%d must be 0 or >= 2", ErrInvalidWikiConfig, cfg.ClusterMinSources)
	}
	if cfg.VaultPullIntervalMinutes < 0 {
		return fmt.Errorf("%w: VaultPullIntervalMinutes=%d must be >= 0", ErrInvalidWikiConfig, cfg.VaultPullIntervalMinutes)
	}
	if cfg.IngestHTTPTimeoutSec < 0 {
		return fmt.Errorf("%w: IngestHTTPTimeoutSec=%d must be >= 0", ErrInvalidWikiConfig, cfg.IngestHTTPTimeoutSec)
	}
	return nil
}

// knownEvolutionCategories is the full set of valid evolution category values.
var knownEvolutionCategories = map[string]struct{}{
	"refactor_opportunity": {},
	"test_gap":             {},
	"architecture_drift":   {},
	"feature_idea":         {},
	"dx_improvement":       {},
	"doc_drift":            {},
	"prompt_tuning":        {},
}

// defaultProjectCategories are the six project-targeted evolution categories
// applied when AllowedEvolutionCategories is empty.
var defaultProjectCategories = []string{
	"refactor_opportunity",
	"test_gap",
	"architecture_drift",
	"feature_idea",
	"dx_improvement",
	"doc_drift",
}

type SyncState struct {
	SyncedVersion string            `json:"synced_version"`
	AssetHashes   map[string]string `json:"asset_hashes,omitempty"`
	SkippedFiles  []string          `json:"skipped_files,omitempty"`
}

type LLMConfig struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	APIKey      string  `json:"api_key,omitempty"`
	BaseURL     string  `json:"base_url,omitempty"`
	Timeout     int     `json:"timeout,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxRetries  int     `json:"max_retries,omitempty"`
	Concurrency          int `json:"concurrency,omitempty"`
	MinRequestIntervalMs int `json:"min_request_interval_ms,omitempty"`
}

type InsightConfig struct {
	Enabled       bool      `json:"enabled"`
	Interval      int       `json:"interval"`
	MaxProposals  int       `json:"max_proposals"`
	MinConfidence float64   `json:"min_confidence"`
	LLM           LLMConfig `json:"llm"`
}

// GuardianConfig configures the Ambient Codebase Guardian.
type GuardianConfig struct {
	Enabled                bool      `json:"enabled"`
	IntervalMinutes        int       `json:"interval_minutes"`
	CoverageDriftPct       float64   `json:"coverage_drift_pct"`
	StaleWorkflowHours     int       `json:"stale_workflow_hours"`
	MemoryThreshold        int       `json:"memory_threshold"`
	TechDebtThreshold      int       `json:"tech_debt_threshold"`
	ReviewerTimeoutMinutes int       `json:"reviewer_timeout_minutes"`
	TicketTimeoutMinutes   int       `json:"ticket_timeout_minutes"`
	LLM                    LLMConfig `json:"llm"`

	// Legacy flat fields — read on load and migrated into LLM.
	// TODO(v0.10.0): remove legacy guardian.llm_* fields.
	LegacyLLMEndpoint    string  `json:"llm_endpoint,omitempty"`
	LegacyLLMAPIKey      string  `json:"llm_api_key,omitempty"`
	LegacyLLMModel       string  `json:"llm_model,omitempty"`
	LegacyLLMTemperature float64 `json:"llm_temperature,omitempty"`
	LegacyLLMMaxTokens   int     `json:"llm_max_tokens,omitempty"`
}

// WikiConfig configures the Wiki ingestion and Vault sync subsystem.
type WikiConfig struct {
	Enabled       bool    `json:"enabled"`
	IngestOnEvent bool    `json:"ingest_on_event"`
	// MaxPagesPerIngest caps the number of pages processed per ingest run.
	// 0 is the unlimited sentinel — all pages will be processed.
	// Negative values are invalid and will be rejected by ValidateWikiConfig.
	MaxPagesPerIngest  int     `json:"max_pages_per_ingest"`
	StalenessThreshold float64 `json:"staleness_threshold"`
	MaxPageSizeTokens  int     `json:"max_page_size_tokens"`
	VaultPath          string  `json:"vault_path"`
	VaultSyncOnSave    bool    `json:"vault_sync_on_save"`
	OnboardingDepth    string  `json:"onboarding_depth"`
	// OnboardingMaxPages caps the number of wiki pages generated during onboarding.
	// 0 is the unlimited sentinel — all pages computed by auto_depth will be generated.
	// Negative values are invalid and will be rejected by ValidateWikiConfig.
	OnboardingMaxPages int `json:"onboarding_max_pages"`
	// IngestTokenBudget is the cumulative LLM token budget for a single full-ingest run.
	// 0 is the unlimited sentinel — no budget guard is applied.
	// Negative values are invalid and will be rejected by ValidateWikiConfig.
	//
	// Note: the orchestrator checks the budget AFTER each page is persisted,
	// so one over-budget page is always written before aborting. This is
	// intentional — it preserves partial-success semantics and avoids
	// mid-page rollback.
	IngestTokenBudget int `json:"ingest_token_budget"`

	// --- Karpathy-style ingest & second-brain settings ---

	// InboxWatcherEnabled turns on the fsnotify watcher that auto-ingests
	// files dropped into <VaultPath>/<InboxDir>. Requires VaultPath != "".
	InboxWatcherEnabled bool `json:"inbox_watcher_enabled"`

	// InboxDir is the subdirectory of the vault watched for new raw sources.
	// Default: "00-Inbox".
	InboxDir string `json:"inbox_dir"`

	// LinkSuggesterEnabled runs a second LLM pass after page generation that
	// proposes stub wiki pages for concepts mentioned but not yet present.
	LinkSuggesterEnabled bool `json:"link_suggester_enabled"`

	// ClusterSynthesisEnabled turns on the periodic job that groups raw
	// pages by tag and synthesizes a single "topic" page per cluster.
	ClusterSynthesisEnabled bool `json:"cluster_synthesis_enabled"`

	// ClusterIntervalMinutes is how often the cluster job runs. Min 5.
	ClusterIntervalMinutes int `json:"cluster_interval_minutes"`

	// ClusterMinSources is the minimum number of raw pages per cluster
	// bucket required before a topic page is synthesized. Min 2.
	ClusterMinSources int `json:"cluster_min_sources"`

	// VaultPullIntervalMinutes controls periodic pull of external .md edits
	// back into the DB. 0 disables periodic pull. Min 1 when > 0.
	VaultPullIntervalMinutes int `json:"vault_pull_interval_minutes"`

	// IngestPdfBinary is the path to the pdftotext binary. Default: "pdftotext".
	IngestPdfBinary string `json:"ingest_pdf_binary"`

	// IngestYoutubeBinary is the path to yt-dlp. Default: "yt-dlp".
	IngestYoutubeBinary string `json:"ingest_youtube_binary"`

	// IngestHTTPTimeoutSec caps URL fetches. Default 30.
	IngestHTTPTimeoutSec int `json:"ingest_http_timeout_sec"`
}

// ScoringWeights holds the weighted blend of static + LLM-judge signals into
// the final hypothesis rank.
type ScoringWeights struct {
	Churn                 float64 `json:"churn"`
	TestGap               float64 `json:"test_gap"`
	TODO                  float64 `json:"todo"`
	Staleness             float64 `json:"staleness"`
	ADRViolation          float64 `json:"adr_violation"`
	LLMImpact             float64 `json:"llm_impact"`
	LLMEffort             float64 `json:"llm_effort"`
	LLMConfidence         float64 `json:"llm_confidence"`
	LLMNovelty            float64 `json:"llm_novelty"`
	MaxTokensPerJudgeCall int     `json:"max_tokens_per_judge_call"`
}

// BaselineLimits caps the baseline builder inputs.
type BaselineLimits struct {
	VexorTopK     int `json:"vexor_top_k"`
	GitLogCommits int `json:"git_log_commits"`
	TODOMax       int `json:"todo_max"`
}

// EvolutionConfig configures the autonomous rule evolution subsystem.
type EvolutionConfig struct {
	Enabled             bool     `json:"enabled"`
	TimeoutMs           int64    `json:"timeout_ms"`
	MaxHypothesesPerRun int      `json:"max_hypotheses_per_run"`
	// Deprecated: ignored in proposals-only mode. Retained for backward-compat config files.
	AutoApplyThreshold  float64  `json:"auto_apply_threshold"`
	ProposalThreshold   float64  `json:"proposal_threshold"`
	MinSampleSize       int      `json:"min_sample_size"`
	DailyTokenBudget    int      `json:"daily_token_budget"`
	Categories          []string `json:"categories"`

	// Opt-in toggle: when true, evolution may also generate Stratus-self hypotheses (prompt_tuning).
	// Default false — evolution targets the working-directory project.
	StratusSelfEnabled bool `json:"stratus_self_enabled"`

	// MANDATORY when Evolution enabled: max tokens spent per cycle across all LLM judge calls.
	// Zero (or unset) causes LoadAndValidate() to return ErrTokenCapRequired.
	MaxTokensPerCycle int `json:"max_tokens_per_cycle"`

	// ScoringWeights holds the weighted blend of static + LLM-judge signals into the final rank.
	ScoringWeights ScoringWeights `json:"scoring_weights"`

	// BaselineLimits caps baseline builder inputs (defaults 30 / 200 / 50 when zero).
	BaselineLimits BaselineLimits `json:"baseline_limits"`

	// AllowedEvolutionCategories is the allowlist of hypothesis categories that generators may emit.
	// Defaults to the six project categories. prompt_tuning may appear ONLY if StratusSelfEnabled.
	AllowedEvolutionCategories []string `json:"allowed_evolution_categories"`
}

// CodeAnalysisConfig configures the Project Code Evolution subsystem.
// WARNING: When enabled, source file contents from the highest-risk files
// are sent to the configured LLM provider for analysis.
type CodeAnalysisConfig struct {
	Enabled             bool      `json:"enabled"`
	MaxFilesPerRun      int       `json:"max_files_per_run"`
	TokenBudgetPerRun   int       `json:"token_budget_per_run"`
	MinChurnScore       float64   `json:"min_churn_score"`
	ConfidenceThreshold float64   `json:"confidence_threshold"`
	ScanInterval        int       `json:"scan_interval"`         // minutes between scheduled scans
	IncludeGitHistory   bool      `json:"include_git_history"`
	GitHistoryDepth     int       `json:"git_history_depth"`     // number of commits to analyze
	Categories          []string  `json:"categories"`            // empty = all categories
	LLM                 LLMConfig `json:"llm"`                   // per-subsystem LLM override
}

// AllowedCodeAnalysisCategories is the set of valid category values for CodeAnalysisConfig.Categories.
var AllowedCodeAnalysisCategories = map[string]struct{}{
	"anti_pattern":   {},
	"duplication":    {},
	"coverage_gap":   {},
	"error_handling": {},
	"complexity":     {},
	"dead_code":      {},
	"security":       {},
}

// Config holds the stratus configuration.
type Config struct {
	Port                     int                `json:"port"`
	DevMode                  bool               `json:"dev_mode,omitempty"`
	DataDir                  string             `json:"data_dir"`
	ProjectRoot              string             `json:"project_root"`
	Language                 string             `json:"language"`
	LLM                      LLMConfig          `json:"llm"`
	Vexor                    VexorConfig        `json:"vexor"`
	STT                      STTConfig          `json:"stt"`
	Guardian                 GuardianConfig     `json:"guardian"`
	SyncState                *SyncState         `json:"sync_state,omitempty"`
	MetricsBroadcastInterval int                `json:"metrics_broadcast_interval"`
	Insight                  InsightConfig      `json:"insight"`
	Wiki                     WikiConfig         `json:"wiki"`
	Evolution                EvolutionConfig    `json:"evolution"`
	CodeAnalysis             CodeAnalysisConfig `json:"code_analysis"`
}

// ValidLanguage returns true if s is a supported UI language code.
// Supported values: "en", "sk".
func ValidLanguage(s string) bool {
	return s == "en" || s == "sk"
}

type VexorConfig struct {
	BinaryPath string `json:"binary_path"`
	Model      string `json:"model"`
	TimeoutSec int    `json:"timeout_sec"`
}

type STTConfig struct {
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
}

func Default() Config {
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	return Config{
		Port:        41777,
		DataDir:     filepath.Join(home, ".stratus", "data"),
		ProjectRoot: wd,
		Language:    "en",
		LLM: LLMConfig{
			Timeout:     120,
			MaxTokens:   16384,
			Temperature: 0.7,
			MaxRetries:  3,
		},
		Vexor: VexorConfig{
			BinaryPath: "vexor",
			Model:      "nomic-embed-text-v1.5",
			TimeoutSec: 15,
		},
		STT: STTConfig{
			Endpoint: "http://localhost:8011",
			Model:    "Systran/faster-whisper-large-v3",
		},
		Guardian: GuardianConfig{
			Enabled:                true,
			IntervalMinutes:        15,
			CoverageDriftPct:       5.0,
			StaleWorkflowHours:     2,
			MemoryThreshold:        5000,
			TechDebtThreshold:      50,
			ReviewerTimeoutMinutes: 30,
			TicketTimeoutMinutes:   30,
			LLM: LLMConfig{
				Temperature: 0.3,
				MaxTokens:   1024,
			},
		},
		MetricsBroadcastInterval: 30,
		Insight: InsightConfig{
			Enabled:       false,
			Interval:      1,
			MaxProposals:  5,
			MinConfidence: 0.7,
			LLM: LLMConfig{
				Provider:    "",
				Model:       "",
				Timeout:     120,
				MaxTokens:   16384,
				Temperature: 0.7,
			},
		},
		Wiki: WikiConfig{
			Enabled:            false,
			IngestOnEvent:      true,
			MaxPagesPerIngest:  20,
			StalenessThreshold: 0.7,
			MaxPageSizeTokens:  4096,
			VaultPath:          "",
			VaultSyncOnSave:    true,
			OnboardingDepth:    "standard",
			OnboardingMaxPages: 20,

			InboxWatcherEnabled:      true,
			InboxDir:                 "00-Inbox",
			LinkSuggesterEnabled:     true,
			ClusterSynthesisEnabled:  true,
			ClusterIntervalMinutes:   360,
			ClusterMinSources:        5,
			VaultPullIntervalMinutes: 5,
			IngestPdfBinary:          "pdftotext",
			IngestYoutubeBinary:      "yt-dlp",
			IngestHTTPTimeoutSec:     30,
		},
		Evolution: EvolutionConfig{
			Enabled:             false,
			TimeoutMs:           600000,
			MaxHypothesesPerRun: 10,
			AutoApplyThreshold:  0.85,
			ProposalThreshold:   0.65,
			MinSampleSize:       10,
			DailyTokenBudget:    2000000,
			Categories:          []string{},
			StratusSelfEnabled:  false,
			MaxTokensPerCycle:   0,
			ScoringWeights: ScoringWeights{
				Churn:                 0.2,
				TestGap:               0.2,
				TODO:                  0.1,
				Staleness:             0.1,
				ADRViolation:          0.1,
				LLMImpact:             0.15,
				LLMEffort:             0.05,
				LLMConfidence:         0.05,
				LLMNovelty:            0.05,
				MaxTokensPerJudgeCall: 4000,
			},
			BaselineLimits: BaselineLimits{
				VexorTopK:     30,
				GitLogCommits: 200,
				TODOMax:       50,
			},
			AllowedEvolutionCategories: append([]string{}, defaultProjectCategories...),
		},
		CodeAnalysis: CodeAnalysisConfig{
			Enabled:             false,
			MaxFilesPerRun:      10,
			TokenBudgetPerRun:   500000,
			MinChurnScore:       0.1,
			ConfidenceThreshold: 0.75,
			ScanInterval:        60,
			IncludeGitHistory:   true,
			GitHistoryDepth:     100,
			Categories:          []string{},
		},
	}
}

func (c Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SavePort updates the "port" field in an existing .stratus.json file
// using a targeted text replacement to preserve field ordering and formatting.
// If the file doesn't exist or has no port field, it falls back to a full
// Load+Save cycle.
func SavePort(path string, port int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		cfg := Default()
		cfg.Port = port
		return cfg.Save(path)
	}
	// Try targeted replacement of the existing port value.
	re := regexp.MustCompile(`("port"\s*:\s*)\d+`)
	if re.Match(data) {
		updated := re.ReplaceAll(data, []byte(fmt.Sprintf("${1}%d", port)))
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, updated, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
	// No port field found — load, set, and save.
	cfg := Load()
	cfg.Port = port
	return cfg.Save(path)
}

func (c Config) ProjectDataDir() string {
	absRoot, err := filepath.Abs(c.ProjectRoot)
	if err != nil {
		absRoot = c.ProjectRoot
	}
	absRoot = filepath.Clean(absRoot)
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}
	sum := sha256.Sum256([]byte(absRoot))
	hash := hex.EncodeToString(sum[:])[:12]
	return filepath.Join(c.DataDir, hash)
}

func Load() Config {
	cfg := Default()

	// Load .stratus.json — walk up from cwd to find it (like git does).
	if data, path := findStratusJSON(); data != nil {
		_ = json.Unmarshal(data, &cfg)
		// If project_root is not set in JSON, derive it from the file location.
		if cfg.ProjectRoot == "" || cfg.ProjectRoot == Default().ProjectRoot {
			cfg.ProjectRoot = filepath.Dir(path)
		}
	}

	// Migrate legacy flat Guardian LLM fields into the nested LLM struct.
	migrateGuardianLegacyLLM(&cfg.Guardian)

	// Apply defaults for evolution fields that may be absent from JSON.
	applyEvolutionDefaults(&cfg.Evolution)

	// Env vars override JSON values.
	if v := os.Getenv("STRATUS_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("STRATUS_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			cfg.Port = port
		}
	}
	if v := os.Getenv("STRATUS_DEV"); v != "" {
		cfg.DevMode = strings.EqualFold(v, "true") || v == "1"
	}

	return cfg
}

// LoadAndValidate loads the config and validates evolution-related fields.
// It returns an error if any evolution constraint is violated.
// Use this in subsystems that depend on evolution being correctly configured.
func LoadAndValidate() (Config, error) {
	cfg := Load()
	if err := validateEvolutionConfig(&cfg.Evolution); err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// applyEvolutionDefaults fills zero-value evolution fields with sensible defaults
// after JSON unmarshalling. Fields that must be explicitly set (MaxTokensPerCycle)
// are intentionally left at zero.
func applyEvolutionDefaults(ev *EvolutionConfig) {
	sw := &ev.ScoringWeights
	if sw.Churn == 0 && sw.TestGap == 0 && sw.TODO == 0 &&
		sw.Staleness == 0 && sw.ADRViolation == 0 &&
		sw.LLMImpact == 0 && sw.LLMEffort == 0 &&
		sw.LLMConfidence == 0 && sw.LLMNovelty == 0 &&
		sw.MaxTokensPerJudgeCall == 0 {
		// All zero means the user didn't set anything — apply full defaults.
		sw.Churn = 0.2
		sw.TestGap = 0.2
		sw.TODO = 0.1
		sw.Staleness = 0.1
		sw.ADRViolation = 0.1
		sw.LLMImpact = 0.15
		sw.LLMEffort = 0.05
		sw.LLMConfidence = 0.05
		sw.LLMNovelty = 0.05
		sw.MaxTokensPerJudgeCall = 4000
	}

	bl := &ev.BaselineLimits
	if bl.VexorTopK == 0 {
		bl.VexorTopK = 30
	}
	if bl.GitLogCommits == 0 {
		bl.GitLogCommits = 200
	}
	if bl.TODOMax == 0 {
		bl.TODOMax = 50
	}

	if len(ev.AllowedEvolutionCategories) == 0 {
		ev.AllowedEvolutionCategories = append([]string{}, defaultProjectCategories...)
	}
}

// validateEvolutionConfig checks all evolution config constraints and returns
// a sentinel error (wrapped with context) if any constraint is violated.
func validateEvolutionConfig(ev *EvolutionConfig) error {
	// Token cap is mandatory when evolution is enabled.
	if ev.Enabled && ev.MaxTokensPerCycle <= 0 {
		return ErrTokenCapRequired
	}

	// Validate ScoringWeights.
	if err := validateScoringWeights(&ev.ScoringWeights); err != nil {
		return err
	}

	// Validate BaselineLimits.
	if err := validateBaselineLimits(&ev.BaselineLimits); err != nil {
		return err
	}

	// Validate AllowedEvolutionCategories.
	for _, cat := range ev.AllowedEvolutionCategories {
		if _, ok := knownEvolutionCategories[cat]; !ok {
			return fmt.Errorf("%w: %q is not a known category", ErrInvalidCategory, cat)
		}
		if cat == "prompt_tuning" && !ev.StratusSelfEnabled {
			return fmt.Errorf("%w: %q requires StratusSelfEnabled=true", ErrInvalidCategory, cat)
		}
	}

	return nil
}

// validateScoringWeights checks individual field ranges and group sums.
func validateScoringWeights(sw *ScoringWeights) error {
	floatFields := []struct {
		name string
		val  float64
	}{
		{"Churn", sw.Churn},
		{"TestGap", sw.TestGap},
		{"TODO", sw.TODO},
		{"Staleness", sw.Staleness},
		{"ADRViolation", sw.ADRViolation},
		{"LLMImpact", sw.LLMImpact},
		{"LLMEffort", sw.LLMEffort},
		{"LLMConfidence", sw.LLMConfidence},
		{"LLMNovelty", sw.LLMNovelty},
	}
	for _, f := range floatFields {
		if f.val < 0 || f.val > 1 {
			return fmt.Errorf("%w: ScoringWeights.%s=%v out of [0,1]", ErrInvalidScoringWeights, f.name, f.val)
		}
	}

	staticSum := sw.Churn + sw.TestGap + sw.TODO + sw.Staleness + sw.ADRViolation
	if staticSum > 1.0+1e-9 {
		return fmt.Errorf("%w: static weights sum=%v exceeds 1.0", ErrInvalidScoringWeights, staticSum)
	}

	llmSum := sw.LLMImpact + sw.LLMEffort + sw.LLMConfidence + sw.LLMNovelty
	if llmSum > 1.0+1e-9 {
		return fmt.Errorf("%w: LLM weights sum=%v exceeds 1.0", ErrInvalidScoringWeights, llmSum)
	}

	if sw.MaxTokensPerJudgeCall <= 0 {
		return fmt.Errorf("%w: MaxTokensPerJudgeCall must be > 0", ErrInvalidScoringWeights)
	}

	return nil
}

// validateBaselineLimits checks that no limit field is negative.
func validateBaselineLimits(bl *BaselineLimits) error {
	if bl.VexorTopK < 0 {
		return fmt.Errorf("%w: VexorTopK=%d must be >= 0", ErrInvalidBaselineLimits, bl.VexorTopK)
	}
	if bl.GitLogCommits < 0 {
		return fmt.Errorf("%w: GitLogCommits=%d must be >= 0", ErrInvalidBaselineLimits, bl.GitLogCommits)
	}
	if bl.TODOMax < 0 {
		return fmt.Errorf("%w: TODOMax=%d must be >= 0", ErrInvalidBaselineLimits, bl.TODOMax)
	}
	return nil
}

// migrateGuardianLegacyLLM populates cfg.LLM from legacy flat fields if the
// nested shape is empty. Used to upgrade v0.9.20 and older .stratus.json files.
// TODO(v0.10.0): delete along with the Legacy* fields.
func migrateGuardianLegacyLLM(g *GuardianConfig) {
	hasLegacy := g.LegacyLLMEndpoint != "" || g.LegacyLLMAPIKey != "" ||
		g.LegacyLLMModel != "" || g.LegacyLLMTemperature != 0 || g.LegacyLLMMaxTokens != 0
	if !hasLegacy {
		return
	}
	// Nested shape wins if populated.
	if g.LLM.BaseURL == "" && g.LLM.Model == "" && g.LLM.APIKey == "" {
		if g.LLM.Provider == "" {
			g.LLM.Provider = "openai"
		}
		if g.LegacyLLMEndpoint != "" {
			g.LLM.BaseURL = g.LegacyLLMEndpoint
		}
		if g.LegacyLLMAPIKey != "" {
			g.LLM.APIKey = g.LegacyLLMAPIKey
		}
		if g.LegacyLLMModel != "" {
			g.LLM.Model = g.LegacyLLMModel
		}
		if g.LegacyLLMTemperature != 0 {
			g.LLM.Temperature = g.LegacyLLMTemperature
		}
		if g.LegacyLLMMaxTokens != 0 {
			g.LLM.MaxTokens = g.LegacyLLMMaxTokens
		}
	}
	// Clear legacy so subsequent Save() writes only the nested shape.
	g.LegacyLLMEndpoint = ""
	g.LegacyLLMAPIKey = ""
	g.LegacyLLMModel = ""
	g.LegacyLLMTemperature = 0
	g.LegacyLLMMaxTokens = 0
}

// findStratusJSON walks up from the current working directory looking for
// .stratus.json. Returns the file contents and its absolute path, or nil if
// not found.
func findStratusJSON() ([]byte, string) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, ""
	}
	for {
		p := filepath.Join(dir, ".stratus.json")
		if data, err := os.ReadFile(p); err == nil {
			return data, p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ""
		}
		dir = parent
	}
}
