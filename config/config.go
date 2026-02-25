package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

// Config holds the stratus configuration.
type Config struct {
	Port        int         `json:"port"`
	DataDir     string      `json:"data_dir"`
	ProjectRoot string      `json:"project_root"`
	Vexor       VexorConfig `json:"vexor"`
	STT         STTConfig   `json:"stt"`
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
			Model:    "whisper-1",
		},
	}
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
