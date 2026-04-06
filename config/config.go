package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

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
	Enabled                bool    `json:"enabled"`
	IntervalMinutes        int     `json:"interval_minutes"`
	CoverageDriftPct       float64 `json:"coverage_drift_pct"`
	StaleWorkflowHours     int     `json:"stale_workflow_hours"`
	MemoryThreshold        int     `json:"memory_threshold"`
	TechDebtThreshold      int     `json:"tech_debt_threshold"`
	ReviewerTimeoutMinutes int     `json:"reviewer_timeout_minutes"`
	TicketTimeoutMinutes   int     `json:"ticket_timeout_minutes"`
	LLMEndpoint            string  `json:"llm_endpoint"`
	LLMAPIKey              string  `json:"llm_api_key"`
	LLMModel               string  `json:"llm_model"`
	LLMTemperature         float64 `json:"llm_temperature"`
	LLMMaxTokens           int     `json:"llm_max_tokens"`
}

// WikiConfig configures the Wiki ingestion and Vault sync subsystem.
type WikiConfig struct {
	Enabled            bool    `json:"enabled"`
	IngestOnEvent      bool    `json:"ingest_on_event"`
	MaxPagesPerIngest  int     `json:"max_pages_per_ingest"`
	StalenessThreshold float64 `json:"staleness_threshold"`
	MaxPageSizeTokens  int     `json:"max_page_size_tokens"`
	VaultPath          string  `json:"vault_path"`
	VaultSyncOnSave    bool    `json:"vault_sync_on_save"`
}

// EvolutionConfig configures the autonomous rule evolution subsystem.
type EvolutionConfig struct {
	Enabled             bool     `json:"enabled"`
	TimeoutMs           int64    `json:"timeout_ms"`
	MaxHypothesesPerRun int      `json:"max_hypotheses_per_run"`
	AutoApplyThreshold  float64  `json:"auto_apply_threshold"`
	ProposalThreshold   float64  `json:"proposal_threshold"`
	MinSampleSize       int      `json:"min_sample_size"`
	DailyTokenBudget    int      `json:"daily_token_budget"`
	Categories          []string `json:"categories"`
}

// Config holds the stratus configuration.
type Config struct {
	Port                     int            `json:"port"`
	DataDir                  string         `json:"data_dir"`
	ProjectRoot              string         `json:"project_root"`
	Vexor                    VexorConfig    `json:"vexor"`
	STT                      STTConfig      `json:"stt"`
	Guardian                 GuardianConfig `json:"guardian"`
	SyncState                *SyncState     `json:"sync_state,omitempty"`
	MetricsBroadcastInterval int             `json:"metrics_broadcast_interval"`
	Insight                  InsightConfig   `json:"insight"`
	Wiki                     WikiConfig      `json:"wiki"`
	Evolution                EvolutionConfig `json:"evolution"`
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
			LLMTemperature:         0.3,
			LLMMaxTokens:           1024,
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
		},
		Evolution: EvolutionConfig{
			Enabled:             false,
			TimeoutMs:           120000,
			MaxHypothesesPerRun: 10,
			AutoApplyThreshold:  0.85,
			ProposalThreshold:   0.65,
			MinSampleSize:       10,
			DailyTokenBudget:    100000,
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

	// Env vars override JSON values.
	if v := os.Getenv("STRATUS_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("STRATUS_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			cfg.Port = port
		}
	}

	return cfg
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
