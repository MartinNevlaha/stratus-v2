package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
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
	APIKey      string  `json:"-"`
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
	Enabled            bool    `json:"enabled"`
	IntervalMinutes    int     `json:"interval_minutes"`
	CoverageDriftPct   float64 `json:"coverage_drift_pct"`
	StaleWorkflowHours int     `json:"stale_workflow_hours"`
	MemoryThreshold    int     `json:"memory_threshold"`
	TechDebtThreshold  int     `json:"tech_debt_threshold"`
	LLMEndpoint        string  `json:"llm_endpoint"`
	LLMAPIKey          string  `json:"llm_api_key"`
	LLMModel           string  `json:"llm_model"`
	LLMTemperature     float64 `json:"llm_temperature"`
	LLMMaxTokens       int     `json:"llm_max_tokens"`
}

// PhaseRoutingConfig configures which executor (cc/oc) handles each workflow phase.
type PhaseRoutingConfig struct {
	Enabled     bool              `json:"enabled"`
	Bug         map[string]string `json:"bug,omitempty"`
	Spec        map[string]string `json:"spec,omitempty"`
	SpecComplex map[string]string `json:"spec_complex,omitempty"`
	Swarm       map[string]string `json:"swarm,omitempty"`
}

// Config holds the stratus configuration.
type Config struct {
	Port                     int                `json:"port"`
	DataDir                  string             `json:"data_dir"`
	ProjectRoot              string             `json:"project_root"`
	Vexor                    VexorConfig        `json:"vexor"`
	STT                      STTConfig          `json:"stt"`
	Guardian                 GuardianConfig     `json:"guardian"`
	PhaseRouting             PhaseRoutingConfig `json:"phase_routing"`
	SyncState                *SyncState         `json:"sync_state,omitempty"`
	MetricsBroadcastInterval int                `json:"metrics_broadcast_interval"`
	Insight                  InsightConfig      `json:"insight"`
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
			Enabled:            true,
			IntervalMinutes:    15,
			CoverageDriftPct:   5.0,
			StaleWorkflowHours: 2,
			MemoryThreshold:    5000,
			TechDebtThreshold:  50,
			LLMTemperature:     0.3,
			LLMMaxTokens:       1024,
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

	if v := os.Getenv("STRATUS_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("STRATUS_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			cfg.Port = port
		}
	}

	data, err := os.ReadFile(".stratus.json")
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}
