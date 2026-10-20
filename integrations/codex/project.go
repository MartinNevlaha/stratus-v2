package codex

import (
	"encoding/json"
	"fmt"
	"github.com/MartinNevlaha/stratus-v2/config"
	"os"
	"path/filepath"
	"strconv"
)

func ResolveProject(cwd string) (config.Config, error) {
	cfg := config.Default()
	if cwd == "" {
		return cfg, fmt.Errorf("hook cwd is missing")
	}
	dir, e := filepath.Abs(cwd)
	if e != nil {
		return cfg, e
	}
	for {
		p := filepath.Join(dir, ".stratus.json")
		b, e := os.ReadFile(p)
		if e == nil {
			cfg.ProjectRoot = dir
			if e = json.Unmarshal(b, &cfg); e != nil {
				return cfg, fmt.Errorf("invalid %s: %w", p, e)
			}
			if cfg.ProjectRoot == "" {
				cfg.ProjectRoot = dir
			}
			if !filepath.IsAbs(cfg.ProjectRoot) {
				cfg.ProjectRoot = filepath.Join(dir, cfg.ProjectRoot)
			}
			break
		}
		if !os.IsNotExist(e) {
			return cfg, e
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cfg, fmt.Errorf("no .stratus.json found from hook cwd")
		}
		dir = parent
	}
	if v := os.Getenv("STRATUS_PORT"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 65535 {
			return cfg, fmt.Errorf("invalid STRATUS_PORT")
		}
		cfg.Port = n
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return cfg, fmt.Errorf("invalid Stratus port")
	}
	if v := os.Getenv("STRATUS_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	return cfg, nil
}
