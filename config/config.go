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

// Sentinel errors for config validation.
var (
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

type SyncState struct {
	SyncedVersion string            `json:"synced_version"`
	AssetHashes   map[string]string `json:"asset_hashes,omitempty"`
	SkippedFiles  []string          `json:"skipped_files,omitempty"`
}

type LLMConfig struct {
	Provider             string  `json:"provider"`
	Model                string  `json:"model"`
	APIKey               string  `json:"api_key,omitempty"`
	BaseURL              string  `json:"base_url,omitempty"`
	Timeout              int     `json:"timeout,omitempty"`
	MaxTokens            int     `json:"max_tokens,omitempty"`
	Temperature          float64 `json:"temperature,omitempty"`
	MaxRetries           int     `json:"max_retries,omitempty"`
	Effort               string  `json:"effort,omitempty"`
	Concurrency          int     `json:"concurrency,omitempty"`
	MinRequestIntervalMs int     `json:"min_request_interval_ms,omitempty"`
}

type InsightConfig struct {
	Enabled  bool      `json:"enabled"`
	Interval int       `json:"interval"`
	LLM      LLMConfig `json:"llm"`
}

// LearnConfig configures the workflow `learn → complete` pipeline.
type LearnConfig struct {
	// PipelineTimeoutSec caps the total runtime of artifact build,
	// knowledge analysis, and wiki autodoc. Default 180 — increase if
	// using a slow local LLM enricher (Ollama, large context).
	PipelineTimeoutSec int `json:"pipeline_timeout_sec"`
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
	WorkflowTokenBudget    int       `json:"workflow_token_budget"` // subagent tokens per active workflow; warns at 80%, critical at 100%; 0 = off
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
	Enabled       bool `json:"enabled"`
	IngestOnEvent bool `json:"ingest_on_event"`
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

// Config holds the stratus configuration.
type Config struct {
	Port                     int            `json:"port"`
	DevMode                  bool           `json:"dev_mode,omitempty"`
	DataDir                  string         `json:"data_dir"`
	ProjectRoot              string         `json:"project_root"`
	Language                 string         `json:"language"`
	LLM                      LLMConfig      `json:"llm"`
	Vexor                    VexorConfig    `json:"vexor"`
	STT                      STTConfig      `json:"stt"`
	Guardian                 GuardianConfig `json:"guardian"`
	SyncState                *SyncState     `json:"sync_state,omitempty"`
	MetricsBroadcastInterval int            `json:"metrics_broadcast_interval"`
	Insight                  InsightConfig  `json:"insight"`
	Wiki                     WikiConfig     `json:"wiki"`
	// DailyTokenBudget caps LLM tokens per day across all subsystems.
	DailyTokenBudget int         `json:"daily_token_budget"`
	Learn            LearnConfig `json:"learn"`
}

// ValidLanguage returns true if s is a supported UI language code.
// Supported values: "en", "sk".
func ValidLanguage(s string) bool {
	return s == "en" || s == "sk"
}

type VexorConfig struct {
	BinaryPath      string `json:"binary_path"`
	Model           string `json:"model"`
	TimeoutSec      int    `json:"timeout_sec"`
	IndexTimeoutSec int    `json:"index_timeout_sec"`
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
			BinaryPath:      "vexor",
			Model:           "nomic-embed-text-v1.5",
			TimeoutSec:      15,
			IndexTimeoutSec: 600,
		},
		STT: STTConfig{
			Endpoint: "http://localhost:8011",
			Model:    "matoog/whisper-large-v3-turbo-sk-ct2",
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
			Enabled:  false,
			Interval: 1,
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
		DailyTokenBudget: 2000000,
		Learn: LearnConfig{
			PipelineTimeoutSec: 180,
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
		applyLegacyDailyTokenBudget(data, &cfg)
		// If project_root is not set in JSON, derive it from the file location.
		if cfg.ProjectRoot == "" || cfg.ProjectRoot == Default().ProjectRoot {
			cfg.ProjectRoot = filepath.Dir(path)
		}
	}

	// Migrate legacy flat Guardian LLM fields into the nested LLM struct.
	migrateGuardianLegacyLLM(&cfg.Guardian)

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

// applyLegacyDailyTokenBudget honours the budget stored under the removed
// "evolution" block when the top-level key is absent, so existing configs keep
// their limit.
func applyLegacyDailyTokenBudget(data []byte, cfg *Config) {
	var raw struct {
		DailyTokenBudget *int `json:"daily_token_budget"`
		Evolution        struct {
			DailyTokenBudget int `json:"daily_token_budget"`
		} `json:"evolution"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	if raw.DailyTokenBudget == nil && raw.Evolution.DailyTokenBudget > 0 {
		cfg.DailyTokenBudget = raw.Evolution.DailyTokenBudget
	}
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
