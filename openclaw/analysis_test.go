package openclaw

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

func setupTestDBWithData(t *testing.T) *db.DB {
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

	now := time.Now().UTC().Format("2006-01-02")
	for i := 0; i < 7; i++ {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		_, err := database.SQL().Exec(`
			INSERT INTO daily_metrics 
			(metric_date, total_workflows, completed_workflows, avg_workflow_duration_ms, total_tasks, completed_tasks, success_rate, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, date, 10, 6, 35000, 50, 30, 0.6, now)
		if err != nil {
			t.Fatalf("Failed to insert test metrics: %v", err)
		}
	}

	return database
}

func TestAnalyzeMetrics(t *testing.T) {
	database := setupTestDBWithData(t)

	cfg := config.OpenClawConfig{
		Enabled:       true,
		Interval:      1,
		MaxProposals:  5,
		MinConfidence: 0.7,
	}

	engine := NewEngine(database, cfg)

	err := engine.RunAnalysis()
	if err != nil {
		t.Fatalf("RunAnalysis failed: %v", err)
	}

	patterns, err := database.ListOpenClawPatterns("", "", 0, 100)
	if err != nil {
		t.Fatalf("ListOpenClawPatterns failed: %v", err)
	}

	if len(patterns) == 0 {
		t.Error("Expected patterns to be created")
	}
}

func TestAnalyzeMetricsEmptyDB(t *testing.T) {
	database := setupTestDB(t)

	cfg := config.OpenClawConfig{
		Enabled:  true,
		Interval: 1,
	}

	engine := NewEngine(database, cfg)

	err := engine.RunAnalysis()
	if err != nil {
		t.Fatalf("RunAnalysis failed on empty DB: %v", err)
	}

	patterns, err := database.ListOpenClawPatterns("", "", 0, 100)
	if err != nil {
		t.Fatalf("ListOpenClawPatterns failed: %v", err)
	}

	if len(patterns) != 0 {
		t.Error("Expected no patterns on empty metrics")
	}
}

func TestAnalyzeMetricsIdempotency(t *testing.T) {
	database := setupTestDBWithData(t)

	cfg := config.OpenClawConfig{
		Enabled:       true,
		Interval:      1,
		MaxProposals:  5,
		MinConfidence: 0.7,
	}

	engine := NewEngine(database, cfg)

	err := engine.RunAnalysis()
	if err != nil {
		t.Fatalf("First RunAnalysis failed: %v", err)
	}

	patternsAfterFirst, err := database.ListOpenClawPatterns("", "", 0, 100)
	if err != nil {
		t.Fatalf("ListOpenClawPatterns failed after first run: %v", err)
	}

	err = engine.RunAnalysis()
	if err != nil {
		t.Fatalf("Second RunAnalysis failed: %v", err)
	}

	patternsAfterSecond, err := database.ListOpenClawPatterns("", "", 0, 100)
	if err != nil {
		t.Fatalf("ListOpenClawPatterns failed after second run: %v", err)
	}

	if len(patternsAfterSecond) > len(patternsAfterFirst)*2 {
		t.Errorf("Patterns should not duplicate excessively: first=%d second=%d",
			len(patternsAfterFirst), len(patternsAfterSecond))
	}

	for _, p := range patternsAfterSecond {
		if p.Frequency >= 2 {
			return
		}
	}
	t.Error("Expected at least one pattern to have frequency >= 2 after re-analysis")
}

func TestAnalyzeMetricsLowSuccessRate(t *testing.T) {
	database := setupTestDB(t)

	now := time.Now().UTC().Format("2006-01-02")
	for i := 0; i < 7; i++ {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		_, err := database.SQL().Exec(`
			INSERT INTO daily_metrics 
			(metric_date, total_workflows, completed_workflows, avg_workflow_duration_ms, total_tasks, completed_tasks, success_rate, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, date, 10, 5, 5000, 50, 25, 0.5, now)
		if err != nil {
			t.Fatalf("Failed to insert test metrics: %v", err)
		}
	}

	cfg := config.OpenClawConfig{
		Enabled:       true,
		Interval:      1,
		MaxProposals:  5,
		MinConfidence: 0.7,
	}

	engine := NewEngine(database, cfg)

	err := engine.RunAnalysis()
	if err != nil {
		t.Fatalf("RunAnalysis failed: %v", err)
	}

	patterns, err := database.ListOpenClawPatterns("quality", "", 0, 100)
	if err != nil {
		t.Fatalf("ListOpenClawPatterns failed: %v", err)
	}

	found := false
	for _, p := range patterns {
		if p.PatternName == "low_success_rate" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected low_success_rate pattern to be detected")
	}
}

func TestAnalyzeMetricsHighSuccessRate(t *testing.T) {
	database := setupTestDB(t)

	now := time.Now().UTC().Format("2006-01-02")
	for i := 0; i < 7; i++ {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		_, err := database.SQL().Exec(`
			INSERT INTO daily_metrics 
			(metric_date, total_workflows, completed_workflows, avg_workflow_duration_ms, total_tasks, completed_tasks, success_rate, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, date, 10, 10, 3000, 50, 50, 0.95, now)
		if err != nil {
			t.Fatalf("Failed to insert test metrics: %v", err)
		}
	}

	cfg := config.OpenClawConfig{
		Enabled:       true,
		Interval:      1,
		MaxProposals:  5,
		MinConfidence: 0.7,
	}

	engine := NewEngine(database, cfg)

	err := engine.RunAnalysis()
	if err != nil {
		t.Fatalf("RunAnalysis failed: %v", err)
	}

	patterns, err := database.ListOpenClawPatterns("success", "", 0, 100)
	if err != nil {
		t.Fatalf("ListOpenClawPatterns failed: %v", err)
	}

	found := false
	for _, p := range patterns {
		if p.PatternName == "high_success_rate" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected high_success_rate pattern to be detected")
	}
}
