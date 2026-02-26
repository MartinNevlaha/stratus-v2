package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

// SyncState tracks which version of the embedded assets was last written to disk
// and the SHA-256 hash of each file at the time it was written.  This lets
// smart-refresh detect user customizations and skip overwriting them.
type SyncState struct {
	SyncedVersion string            `json:"synced_version"`
	AssetHashes   map[string]string `json:"asset_hashes,omitempty"`
	SkippedFiles  []string          `json:"skipped_files,omitempty"`
}

// Config holds the stratus configuration.
type Config struct {
	Port        int         `json:"port"`
	DataDir     string      `json:"data_dir"`
	ProjectRoot string      `json:"project_root"`
	Vexor       VexorConfig `json:"vexor"`
	STT         STTConfig   `json:"stt"`
	SyncState   *SyncState  `json:"sync_state,omitempty"`
}

// VexorConfig configures the Vexor code search backend.
type VexorConfig struct {
	BinaryPath string `json:"binary_path"`
	Model      string `json:"model"`
	TimeoutSec int    `json:"timeout_sec"`
}

// STTConfig configures speech-to-text.
type STTConfig struct {
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
}

// Default returns sensible defaults.
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
	}
}

// Save marshals the config to JSON and atomically writes it to path.
// It writes to a temp file first, then renames, so a crash mid-write never
// produces a corrupted .stratus.json.
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

// ProjectDataDir returns the project-specific data directory.
// Each project gets its own subdirectory under DataDir, keyed by a
// SHA-256 hash prefix of the resolved absolute ProjectRoot path.
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

// Load loads config from .stratus.json in the current directory, merging with defaults.
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
