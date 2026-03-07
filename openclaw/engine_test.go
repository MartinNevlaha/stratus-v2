package openclaw

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "stratus-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	return database
}

func TestEngineStartStop(t *testing.T) {
	database := setupTestDB(t)

	cfg := config.OpenClawConfig{
		Enabled:  true,
		Interval: 1,
	}

	engine := NewEngine(database, cfg)

	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !engine.IsRunning() {
		t.Error("Engine should be running after Start()")
	}

	err := engine.Start(ctx)
	if err == nil {
		t.Error("Expected error on double start")
	}

	engine.Stop()

	time.Sleep(100 * time.Millisecond)

	if engine.IsRunning() {
		t.Error("Engine should not be running after Stop()")
	}
}

func TestEngineContextCancellation(t *testing.T) {
	database := setupTestDB(t)

	cfg := config.OpenClawConfig{
		Enabled:  true,
		Interval: 1,
	}

	engine := NewEngine(database, cfg)

	ctx, cancel := context.WithCancel(context.Background())

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !engine.IsRunning() {
		t.Error("Engine should be running")
	}

	cancel()

	time.Sleep(100 * time.Millisecond)

	if engine.IsRunning() {
		t.Error("Engine should stop on context cancellation")
	}
}

func TestEngineStopIdempotent(t *testing.T) {
	database := setupTestDB(t)

	cfg := config.OpenClawConfig{
		Enabled:  true,
		Interval: 1,
	}

	engine := NewEngine(database, cfg)

	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	engine.Stop()
	time.Sleep(50 * time.Millisecond)

	engine.Stop()
	engine.Stop()

	if engine.IsRunning() {
		t.Error("Engine should not be running after multiple stops")
	}
}

func TestEngineDisabledByDefault(t *testing.T) {
	database := setupTestDB(t)

	cfg := config.OpenClawConfig{
		Enabled:  false,
		Interval: 1,
	}

	engine := NewEngine(database, cfg)

	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start should succeed even when disabled: %v", err)
	}

	engine.Stop()
}
