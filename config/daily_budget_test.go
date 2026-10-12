package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadWithJSON(t *testing.T, raw string) Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".stratus.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Chdir(dir)
	return Load()
}

func TestDailyTokenBudget_DefaultsToTwoMillion(t *testing.T) {
	cfg := loadWithJSON(t, `{}`)
	if cfg.DailyTokenBudget != 2000000 {
		t.Fatalf("DailyTokenBudget = %d, want 2000000", cfg.DailyTokenBudget)
	}
}

func TestDailyTokenBudget_TopLevelKeyIsRead(t *testing.T) {
	cfg := loadWithJSON(t, `{"daily_token_budget": 123}`)
	if cfg.DailyTokenBudget != 123 {
		t.Fatalf("DailyTokenBudget = %d, want 123", cfg.DailyTokenBudget)
	}
}

func TestDailyTokenBudget_LegacyEvolutionKeyIsHonoured(t *testing.T) {
	cfg := loadWithJSON(t, `{"evolution": {"daily_token_budget": 777}}`)
	if cfg.DailyTokenBudget != 777 {
		t.Fatalf("DailyTokenBudget = %d, want 777 from the legacy key", cfg.DailyTokenBudget)
	}
}

func TestDailyTokenBudget_TopLevelWinsOverLegacy(t *testing.T) {
	cfg := loadWithJSON(t, `{"daily_token_budget": 5, "evolution": {"daily_token_budget": 777}}`)
	if cfg.DailyTokenBudget != 5 {
		t.Fatalf("DailyTokenBudget = %d, want 5", cfg.DailyTokenBudget)
	}
}
