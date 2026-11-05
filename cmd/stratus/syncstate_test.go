package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
)

func TestSaveSyncState_KeepsSettingsChangedOnDiskMeanwhile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, ".stratus.json")
	if err := os.WriteFile(path, []byte(`{"language":"en"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := config.Load() // what refresh loaded before copying assets
	// The dashboard changes a setting while the refresh is copying assets.
	if err := os.WriteFile(path, []byte(`{"language":"sk"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if stale.Language != "en" {
		t.Fatalf("test setup: stale language = %q", stale.Language)
	}
	err := saveSyncState(path, func(cfg *config.Config) {
		cfg.SyncState = &config.SyncState{SyncedVersion: "9.9.9"}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	got := config.Load()
	if got.Language != "sk" {
		t.Fatalf("settings change lost: language = %q", got.Language)
	}
	if got.SyncState == nil || got.SyncState.SyncedVersion != "9.9.9" {
		t.Fatalf("sync state not saved: %+v", got.SyncState)
	}
}
