package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWikiConfig_OnboardingDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Wiki.OnboardingDepth != "standard" {
		t.Errorf("OnboardingDepth = %q, want %q", cfg.Wiki.OnboardingDepth, "standard")
	}
	if cfg.Wiki.OnboardingMaxPages != 20 {
		t.Errorf("OnboardingMaxPages = %d, want %d", cfg.Wiki.OnboardingMaxPages, 20)
	}
}

func TestWikiConfig_OnboardingLoadFromJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".stratus.json")

	raw := map[string]any{
		"wiki": map[string]any{
			"onboarding_depth":     "deep",
			"onboarding_max_pages": 50,
		},
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal test config: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	// Change working directory so findStratusJSON picks up the temp file.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to temp dir: %v", err)
	}

	cfg := Load()
	if cfg.Wiki.OnboardingDepth != "deep" {
		t.Errorf("OnboardingDepth = %q, want %q", cfg.Wiki.OnboardingDepth, "deep")
	}
	if cfg.Wiki.OnboardingMaxPages != 50 {
		t.Errorf("OnboardingMaxPages = %d, want %d", cfg.Wiki.OnboardingMaxPages, 50)
	}
}
