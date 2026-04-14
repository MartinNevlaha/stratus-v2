package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

// saveTestCodeAnalysisRun is a helper to insert a run and fail the test on error.
func saveTestCodeAnalysisRun(t *testing.T, database *db.DB, run *db.CodeAnalysisRun) {
	t.Helper()
	if err := database.SaveCodeAnalysisRun(run); err != nil {
		t.Fatalf("SaveCodeAnalysisRun: %v", err)
	}
}

// saveTestCodeFinding is a helper to insert a finding and fail the test on error.
func saveTestCodeFinding(t *testing.T, database *db.DB, f *db.CodeFinding) {
	t.Helper()
	if err := database.SaveCodeFinding(f); err != nil {
		t.Fatalf("SaveCodeFinding: %v", err)
	}
}

// TestHandleListCodeAnalysisRuns verifies that all runs are returned with count.
func TestHandleListCodeAnalysisRuns(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	saveTestCodeAnalysisRun(t, database, &db.CodeAnalysisRun{
		ID:     "run-1",
		Status: "completed",
	})
	saveTestCodeAnalysisRun(t, database, &db.CodeAnalysisRun{
		ID:     "run-2",
		Status: "running",
	})

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/code-analysis/runs", nil)
	w := httptest.NewRecorder()

	server.handleListCodeAnalysisRuns(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	runs, ok := resp["runs"].([]any)
	if !ok {
		t.Fatal("expected runs to be an array")
	}
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}

	count, ok := resp["count"].(float64)
	if !ok {
		t.Fatal("expected count to be a number")
	}
	if int(count) != 2 {
		t.Errorf("expected count 2, got %d", int(count))
	}
}

// TestHandleListCodeAnalysisRuns_LimitCap verifies that limit is capped at 100.
func TestHandleListCodeAnalysisRuns_LimitCap(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/code-analysis/runs?limit=999", nil)
	w := httptest.NewRecorder()

	server.handleListCodeAnalysisRuns(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestHandleGetCodeAnalysisRun verifies that a run and its findings are returned.
func TestHandleGetCodeAnalysisRun(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	saveTestCodeAnalysisRun(t, database, &db.CodeAnalysisRun{
		ID:     "run-detail-1",
		Status: "completed",
	})
	saveTestCodeFinding(t, database, &db.CodeFinding{
		ID:       "finding-1",
		RunID:    "run-detail-1",
		FilePath: "pkg/foo.go",
		Category: "complexity",
		Severity: "high",
		Title:    "High complexity",
	})

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/code-analysis/runs/run-detail-1", nil)
	req.SetPathValue("id", "run-detail-1")
	w := httptest.NewRecorder()

	server.handleGetCodeAnalysisRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	runData, ok := resp["run"].(map[string]any)
	if !ok {
		t.Fatal("expected run to be an object")
	}
	if runData["id"] != "run-detail-1" {
		t.Errorf("expected run id run-detail-1, got %v", runData["id"])
	}

	findings, ok := resp["findings"].([]any)
	if !ok {
		t.Fatal("expected findings to be an array")
	}
	if len(findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(findings))
	}
}

// TestHandleGetCodeAnalysisRun_NotFound verifies that a 404 is returned for unknown ID.
func TestHandleGetCodeAnalysisRun_NotFound(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/code-analysis/runs/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	server.handleGetCodeAnalysisRun(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "code analysis run not found" {
		t.Errorf("unexpected error message: %v", resp["error"])
	}
}

// TestHandleListCodeFindings verifies that findings are returned with count.
func TestHandleListCodeFindings(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	saveTestCodeAnalysisRun(t, database, &db.CodeAnalysisRun{ID: "run-a", Status: "completed"})

	for i := 1; i <= 3; i++ {
		saveTestCodeFinding(t, database, &db.CodeFinding{
			ID:       fmt.Sprintf("f-%d", i),
			RunID:    "run-a",
			FilePath: fmt.Sprintf("pkg/file%d.go", i),
			Category: "complexity",
			Severity: "medium",
			Title:    fmt.Sprintf("Finding %d", i),
		})
	}

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/code-analysis/findings?run_id=run-a", nil)
	w := httptest.NewRecorder()

	server.handleListCodeFindings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	findings, ok := resp["findings"].([]any)
	if !ok {
		t.Fatal("expected findings to be an array")
	}
	if len(findings) != 3 {
		t.Errorf("expected 3 findings, got %d", len(findings))
	}

	count, ok := resp["count"].(float64)
	if !ok {
		t.Fatal("expected count to be a number")
	}
	if int(count) != 3 {
		t.Errorf("expected count 3, got %d", int(count))
	}
}

// TestHandleListCodeFindings_CategoryFilter verifies category filtering.
func TestHandleListCodeFindings_CategoryFilter(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	saveTestCodeAnalysisRun(t, database, &db.CodeAnalysisRun{ID: "run-b", Status: "completed"})

	saveTestCodeFinding(t, database, &db.CodeFinding{
		ID: "f-comp", RunID: "run-b", FilePath: "a.go", Category: "complexity", Severity: "high", Title: "complex",
	})
	saveTestCodeFinding(t, database, &db.CodeFinding{
		ID: "f-sec", RunID: "run-b", FilePath: "b.go", Category: "security", Severity: "critical", Title: "sec issue",
	})

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/code-analysis/findings?category=security", nil)
	w := httptest.NewRecorder()

	server.handleListCodeFindings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	findings := resp["findings"].([]any)
	if len(findings) != 1 {
		t.Errorf("expected 1 finding with category=security, got %d", len(findings))
	}
}

// TestHandleTriggerCodeAnalysis_NoEngine verifies that 503 is returned when trigger is nil.
func TestHandleTriggerCodeAnalysis_NoEngine(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database, codeAnalysisTrigger: nil}
	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/code-analysis/trigger", body)
	w := httptest.NewRecorder()

	server.handleTriggerCodeAnalysis(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

// TestHandleTriggerCodeAnalysis_InvalidCategory verifies that invalid category returns 400.
func TestHandleTriggerCodeAnalysis_InvalidCategory(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	triggered := false
	triggerFn := CodeAnalysisTriggerFn(func(ctx context.Context, cats []string) error {
		triggered = true
		return nil
	})

	server := &Server{db: database, codeAnalysisTrigger: triggerFn}
	body := bytes.NewBufferString(`{"categories": ["invalid_cat"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/code-analysis/trigger", body)
	w := httptest.NewRecorder()

	server.handleTriggerCodeAnalysis(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	if triggered {
		t.Error("trigger should not have been called for invalid category")
	}
}

// TestHandleTriggerCodeAnalysis_Valid verifies that a valid trigger returns 200.
func TestHandleTriggerCodeAnalysis_Valid(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	triggerFn := CodeAnalysisTriggerFn(func(ctx context.Context, cats []string) error {
		return nil
	})

	server := &Server{db: database, codeAnalysisTrigger: triggerFn}
	body := bytes.NewBufferString(`{"categories": ["complexity", "security"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/code-analysis/trigger", body)
	w := httptest.NewRecorder()

	server.handleTriggerCodeAnalysis(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "analysis_triggered" {
		t.Errorf("unexpected status: %v", resp["status"])
	}
}

// TestHandleGetCodeAnalysisMetrics verifies that metrics are returned.
func TestHandleGetCodeAnalysisMetrics(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	m := &db.CodeQualityMetric{
		ID:            "metric-1",
		MetricDate:    "2026-04-12",
		TotalFiles:    100,
		FilesAnalyzed: 80,
		FindingsTotal: 5,
	}
	if err := database.SaveCodeQualityMetric(m); err != nil {
		t.Fatalf("SaveCodeQualityMetric: %v", err)
	}

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/code-analysis/metrics?days=30", nil)
	w := httptest.NewRecorder()

	server.handleGetCodeAnalysisMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	metrics, ok := resp["metrics"].([]any)
	if !ok {
		t.Fatal("expected metrics to be an array")
	}
	if len(metrics) != 1 {
		t.Errorf("expected 1 metric, got %d", len(metrics))
	}
}

// TestHandleGetCodeAnalysisConfig verifies that the config is returned correctly.
func TestHandleGetCodeAnalysisConfig(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	cfg.CodeAnalysis.Enabled = true
	cfg.CodeAnalysis.MaxFilesPerRun = 25
	cfg.CodeAnalysis.ScanInterval = 30

	server := &Server{db: database, cfg: &cfg}
	req := httptest.NewRequest(http.MethodGet, "/api/code-analysis/config", nil)
	w := httptest.NewRecorder()

	server.handleGetCodeAnalysisConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp config.CodeAnalysisConfig
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !resp.Enabled {
		t.Error("expected Enabled to be true")
	}
	if resp.MaxFilesPerRun != 25 {
		t.Errorf("expected MaxFilesPerRun 25, got %d", resp.MaxFilesPerRun)
	}
	if resp.ScanInterval != 30 {
		t.Errorf("expected ScanInterval 30, got %d", resp.ScanInterval)
	}
}

// TestHandleUpdateCodeAnalysisConfig_Valid verifies that a valid config update succeeds.
func TestHandleUpdateCodeAnalysisConfig_Valid(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	server := &Server{db: database, cfg: &cfg}

	updated := config.CodeAnalysisConfig{
		Enabled:             true,
		MaxFilesPerRun:      20,
		TokenBudgetPerRun:   1000000,
		MinChurnScore:       0.3,
		ConfidenceThreshold: 0.8,
		ScanInterval:        60,
		IncludeGitHistory:   true,
		GitHistoryDepth:     50,
		Categories:          []string{"complexity", "security"},
	}
	body, _ := json.Marshal(updated)

	req := httptest.NewRequest(http.MethodPost, "/api/code-analysis/config", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleUpdateCodeAnalysisConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp config.CodeAnalysisConfig
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !resp.Enabled {
		t.Error("expected Enabled true in response")
	}
	if resp.MaxFilesPerRun != 20 {
		t.Errorf("expected MaxFilesPerRun 20, got %d", resp.MaxFilesPerRun)
	}
	if resp.ScanInterval != 60 {
		t.Errorf("expected ScanInterval 60, got %d", resp.ScanInterval)
	}

	// Verify in-place mutation.
	if !server.cfg.CodeAnalysis.Enabled {
		t.Error("expected server cfg.CodeAnalysis.Enabled to be updated")
	}
}

// TestHandleUpdateCodeAnalysisConfig_InvalidRange verifies that out-of-range values return 400.
func TestHandleUpdateCodeAnalysisConfig_InvalidRange(t *testing.T) {
	// validBase is a known-good config. Each sub-test mutates one field to an
	// invalid value and expects 400.
	validBase := config.CodeAnalysisConfig{
		Enabled:             true,
		MaxFilesPerRun:      10,
		TokenBudgetPerRun:   500000,
		MinChurnScore:       0.1,
		ConfidenceThreshold: 0.75,
		ScanInterval:        60,
		IncludeGitHistory:   true,
		GitHistoryDepth:     100,
		Categories:          []string{},
	}

	tests := []struct {
		name    string
		mutate  func(c *config.CodeAnalysisConfig)
		wantMsg string
	}{
		{
			name:    "max_files_per_run below 1",
			mutate:  func(c *config.CodeAnalysisConfig) { c.MaxFilesPerRun = 0 },
			wantMsg: "max_files_per_run",
		},
		{
			name:    "max_files_per_run above 50",
			mutate:  func(c *config.CodeAnalysisConfig) { c.MaxFilesPerRun = 51 },
			wantMsg: "max_files_per_run",
		},
		{
			name:    "token_budget_per_run negative",
			mutate:  func(c *config.CodeAnalysisConfig) { c.TokenBudgetPerRun = -1 },
			wantMsg: "token_budget_per_run",
		},
		{
			name:    "token_budget_per_run above 5000000",
			mutate:  func(c *config.CodeAnalysisConfig) { c.TokenBudgetPerRun = 5000001 },
			wantMsg: "token_budget_per_run",
		},
		{
			name:    "min_churn_score below 0",
			mutate:  func(c *config.CodeAnalysisConfig) { c.MinChurnScore = -0.1 },
			wantMsg: "min_churn_score",
		},
		{
			name:    "min_churn_score above 1",
			mutate:  func(c *config.CodeAnalysisConfig) { c.MinChurnScore = 1.1 },
			wantMsg: "min_churn_score",
		},
		{
			name:    "confidence_threshold below 0",
			mutate:  func(c *config.CodeAnalysisConfig) { c.ConfidenceThreshold = -0.1 },
			wantMsg: "confidence_threshold",
		},
		{
			name:    "confidence_threshold above 1",
			mutate:  func(c *config.CodeAnalysisConfig) { c.ConfidenceThreshold = 1.5 },
			wantMsg: "confidence_threshold",
		},
		{
			name:    "scan_interval below 5",
			mutate:  func(c *config.CodeAnalysisConfig) { c.ScanInterval = 4 },
			wantMsg: "scan_interval",
		},
		{
			name:    "scan_interval above 1440",
			mutate:  func(c *config.CodeAnalysisConfig) { c.ScanInterval = 1441 },
			wantMsg: "scan_interval",
		},
		{
			name:    "git_history_depth below 10",
			mutate:  func(c *config.CodeAnalysisConfig) { c.GitHistoryDepth = 9 },
			wantMsg: "git_history_depth",
		},
		{
			name:    "git_history_depth above 1000",
			mutate:  func(c *config.CodeAnalysisConfig) { c.GitHistoryDepth = 1001 },
			wantMsg: "git_history_depth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := setupTestDB(t)
			defer database.Close()

			baseCfg := config.Default()
			server := &Server{db: database, cfg: &baseCfg}

			cfg := validBase
			tt.mutate(&cfg)

			body, err := json.Marshal(cfg)
			if err != nil {
				t.Fatalf("marshal config: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/code-analysis/config", bytes.NewReader(body))
			w := httptest.NewRecorder()

			server.handleUpdateCodeAnalysisConfig(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
			}

			var resp map[string]any
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			errMsg, _ := resp["error"].(string)
			if errMsg == "" {
				t.Error("expected non-empty error message in response")
			}
		})
	}
}

// TestHandleUpdateCodeAnalysisConfig_InvalidCategory verifies that an unknown category returns 400.
func TestHandleUpdateCodeAnalysisConfig_InvalidCategory(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	server := &Server{db: database, cfg: &cfg}

	invalid := config.CodeAnalysisConfig{
		Enabled:             true,
		MaxFilesPerRun:      10,
		TokenBudgetPerRun:   500000,
		MinChurnScore:       0.1,
		ConfidenceThreshold: 0.75,
		ScanInterval:        60,
		GitHistoryDepth:     100,
		Categories:          []string{"complexity", "bad_category"},
	}
	body, _ := json.Marshal(invalid)

	req := httptest.NewRequest(http.MethodPost, "/api/code-analysis/config", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleUpdateCodeAnalysisConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	errMsg, _ := resp["error"].(string)
	if errMsg == "" {
		t.Error("expected non-empty error message")
	}
}

// TestUpdateCodeFindingStatus_OK_Rejected seeds a finding via DB, sends PUT with
// {"status":"rejected"}, and expects 200 with the DB row updated to "rejected".
func TestUpdateCodeFindingStatus_OK_Rejected(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	saveTestCodeAnalysisRun(t, database, &db.CodeAnalysisRun{ID: "run-rej", Status: "completed"})
	saveTestCodeFinding(t, database, &db.CodeFinding{
		ID:       "finding-rej",
		RunID:    "run-rej",
		FilePath: "pkg/foo.go",
		Category: "complexity",
		Severity: "high",
		Title:    "Reject me",
	})

	server := &Server{db: database}
	body := bytes.NewBufferString(`{"status":"rejected"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/code-analysis/findings/finding-rej/status", body)
	req.SetPathValue("id", "finding-rej")
	w := httptest.NewRecorder()

	server.handleUpdateCodeFindingStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("expected ok=true, got %v", resp["ok"])
	}
	if resp["id"] != "finding-rej" {
		t.Errorf("expected id=finding-rej, got %v", resp["id"])
	}
	if resp["status"] != "rejected" {
		t.Errorf("expected status=rejected, got %v", resp["status"])
	}

	// Confirm DB row is actually updated.
	findings, _, err := database.ListCodeFindings(db.CodeFindingFilters{RunID: "run-rej", Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Status != "rejected" {
		t.Errorf("expected DB status=rejected, got %q", findings[0].Status)
	}
}

// TestUpdateCodeFindingStatus_OK_Applied seeds a finding via DB, sends PUT with
// {"status":"applied"}, and expects 200 with the DB row updated to "applied".
func TestUpdateCodeFindingStatus_OK_Applied(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	saveTestCodeAnalysisRun(t, database, &db.CodeAnalysisRun{ID: "run-app", Status: "completed"})
	saveTestCodeFinding(t, database, &db.CodeFinding{
		ID:       "finding-app",
		RunID:    "run-app",
		FilePath: "pkg/bar.go",
		Category: "security",
		Severity: "critical",
		Title:    "Apply me",
	})

	server := &Server{db: database}
	body := bytes.NewBufferString(`{"status":"applied"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/code-analysis/findings/finding-app/status", body)
	req.SetPathValue("id", "finding-app")
	w := httptest.NewRecorder()

	server.handleUpdateCodeFindingStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("expected ok=true, got %v", resp["ok"])
	}
	if resp["status"] != "applied" {
		t.Errorf("expected status=applied, got %v", resp["status"])
	}

	// Confirm DB row is actually updated.
	findings, _, err := database.ListCodeFindings(db.CodeFindingFilters{RunID: "run-app", Limit: 10})
	if err != nil {
		t.Fatalf("ListCodeFindings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Status != "applied" {
		t.Errorf("expected DB status=applied, got %q", findings[0].Status)
	}
}

// TestUpdateCodeFindingStatus_InvalidStatus verifies that a body with an unrecognised
// status value returns 400.
func TestUpdateCodeFindingStatus_InvalidStatus(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	saveTestCodeAnalysisRun(t, database, &db.CodeAnalysisRun{ID: "run-inv", Status: "completed"})
	saveTestCodeFinding(t, database, &db.CodeFinding{
		ID:       "finding-inv",
		RunID:    "run-inv",
		FilePath: "pkg/baz.go",
		Category: "dead_code",
		Severity: "low",
		Title:    "Dead code",
	})

	server := &Server{db: database}
	body := bytes.NewBufferString(`{"status":"foo"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/code-analysis/findings/finding-inv/status", body)
	req.SetPathValue("id", "finding-inv")
	w := httptest.NewRecorder()

	server.handleUpdateCodeFindingStatus(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	errMsg, _ := resp["error"].(string)
	if errMsg == "" {
		t.Error("expected non-empty error message")
	}
}

// TestUpdateCodeFindingStatus_NotFound verifies that updating a non-existent finding
// returns 404.
func TestUpdateCodeFindingStatus_NotFound(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	body := bytes.NewBufferString(`{"status":"rejected"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/code-analysis/findings/does-not-exist/status", body)
	req.SetPathValue("id", "does-not-exist")
	w := httptest.NewRecorder()

	server.handleUpdateCodeFindingStatus(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "code finding not found" {
		t.Errorf("unexpected error message: %v", resp["error"])
	}
}

// TestListCodeFindings_StatusFilter seeds 3 findings with different statuses
// (pending/rejected/applied), GETs with ?status=applied, and asserts only the
// matching finding is returned.
func TestListCodeFindings_StatusFilter(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	saveTestCodeAnalysisRun(t, database, &db.CodeAnalysisRun{ID: "run-sf", Status: "completed"})

	// Seed three findings — all start as "pending" by default.
	for i, id := range []string{"sf-1", "sf-2", "sf-3"} {
		saveTestCodeFinding(t, database, &db.CodeFinding{
			ID:       id,
			RunID:    "run-sf",
			FilePath: fmt.Sprintf("pkg/file%d.go", i+1),
			Category: "complexity",
			Severity: "medium",
			Title:    fmt.Sprintf("Finding %d", i+1),
		})
	}

	// Transition sf-2 → rejected, sf-3 → applied; sf-1 stays pending.
	ctx := context.Background()
	if err := database.UpdateCodeFindingStatus(ctx, "sf-2", "rejected"); err != nil {
		t.Fatalf("UpdateCodeFindingStatus (rejected): %v", err)
	}
	if err := database.UpdateCodeFindingStatus(ctx, "sf-3", "applied"); err != nil {
		t.Fatalf("UpdateCodeFindingStatus (applied): %v", err)
	}

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/code-analysis/findings?status=applied", nil)
	w := httptest.NewRecorder()

	server.handleListCodeFindings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	findings, ok := resp["findings"].([]any)
	if !ok {
		t.Fatal("expected findings to be an array")
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding with status=applied, got %d", len(findings))
	}

	f, ok := findings[0].(map[string]any)
	if !ok {
		t.Fatal("expected finding to be an object")
	}
	if f["id"] != "sf-3" {
		t.Errorf("expected finding id=sf-3, got %v", f["id"])
	}
	if f["status"] != "applied" {
		t.Errorf("expected status=applied, got %v", f["status"])
	}

	count, ok := resp["count"].(float64)
	if !ok {
		t.Fatal("expected count to be a number")
	}
	if int(count) != 1 {
		t.Errorf("expected count=1, got %d", int(count))
	}
}
