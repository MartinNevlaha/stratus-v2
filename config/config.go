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

type OpenClawConfig struct {
	Enabled       bool    `json:"enabled"`
	Interval      int     `json:"interval"`
	MaxProposals  int     `json:"max_proposals"`
	MinConfidence float64 `json:"min_confidence"`
	LLMProvider   string  `json:"llm_provider"`
	LLMModel      string  `json:"llm_model"`
	LLMAPIKey     string  `json:"llm_api_key"`
	LLMTimeout    int     `json:"llm_timeout"`
}

type Config struct {
	Port                     int            `json:"port"`
	DataDir                  string         `json:"data_dir"`
	ProjectRoot              string         `json:"project_root"`
	Vexor                    VexorConfig    `json:"vexor"`
	STT                      STTConfig      `json:"stt"`
	SyncState                *SyncState     `json:"sync_state,omitempty"`
	MetricsBroadcastInterval int            `json:"metrics_broadcast_interval"`
	OpenClaw                 OpenClawConfig `json:"openclaw"`
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
			Model:    "Systran/faster-whisper-small",
		},
		MetricsBroadcastInterval: 30,
		OpenClaw: OpenClawConfig{
			Enabled:       true,
			Interval:      1,
			MaxProposals:  5,
			MinConfidence: 0.7,
			LLMProvider:   "claude",
			LLMModel:      "claude-3-5-sonnet-20241022",
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
