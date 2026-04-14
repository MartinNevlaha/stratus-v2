package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

// CodeAnalysisTriggerFn is the signature for a function that triggers a code
// analysis run in the background. categories may be empty (run all categories).
type CodeAnalysisTriggerFn func(ctx context.Context, categories []string) error

// GET /api/code-analysis/runs
func (s *Server) handleListCodeAnalysisRuns(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 20)
	if limit > 100 {
		limit = 100
	}
	if limit < 1 {
		limit = 1
	}
	offset := queryInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}

	runs, count, err := s.db.ListCodeAnalysisRuns(limit, offset)
	if err != nil {
		slog.Error("list code analysis runs", "err", err)
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if runs == nil {
		runs = []db.CodeAnalysisRun{}
	}

	json200(w, map[string]any{
		"runs":  runs,
		"count": count,
	})
}

// GET /api/code-analysis/runs/{id}
func (s *Server) handleGetCodeAnalysisRun(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing run id")
		return
	}

	run, err := s.db.GetCodeAnalysisRun(id)
	if err != nil {
		slog.Error("get code analysis run", "err", err)
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if run == nil {
		jsonErr(w, http.StatusNotFound, "code analysis run not found")
		return
	}

	findings, _, err := s.db.ListCodeFindings(db.CodeFindingFilters{RunID: id, Limit: 200})
	if err != nil {
		slog.Error("list code findings for run", "err", err)
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if findings == nil {
		findings = []db.CodeFinding{}
	}

	json200(w, map[string]any{
		"run":      run,
		"findings": findings,
	})
}

// GET /api/code-analysis/findings
func (s *Server) handleListCodeFindings(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50)
	if limit > 200 {
		limit = 200
	}
	if limit < 1 {
		limit = 1
	}
	offset := queryInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}

	filters := db.CodeFindingFilters{
		RunID:    queryStr(r, "run_id"),
		FilePath: queryStr(r, "file"),
		Category: queryStr(r, "category"),
		Severity: queryStr(r, "severity"),
		Status:   queryStr(r, "status"),
		Query:    queryStr(r, "q"),
		Limit:    limit,
		Offset:   offset,
	}

	findings, count, err := s.db.ListCodeFindings(filters)
	if err != nil {
		slog.Error("list code findings", "err", err)
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if findings == nil {
		findings = []db.CodeFinding{}
	}

	json200(w, map[string]any{
		"findings": findings,
		"count":    count,
	})
}

// codeAnalysisTriggerRequest is the request body for POST /api/code-analysis/trigger.
type codeAnalysisTriggerRequest struct {
	Categories []string `json:"categories"`
}

// POST /api/code-analysis/trigger
func (s *Server) handleTriggerCodeAnalysis(w http.ResponseWriter, r *http.Request) {
	if s.codeAnalysisTrigger == nil {
		jsonErr(w, http.StatusServiceUnavailable, "code analysis engine not available")
		return
	}

	var req codeAnalysisTriggerRequest
	if err := decodeBody(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	// Validate categories if provided.
	for _, cat := range req.Categories {
		if _, ok := config.AllowedCodeAnalysisCategories[cat]; !ok {
			jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid category %q: allowed values are anti_pattern, duplication, coverage_gap, error_handling, complexity, dead_code, security", cat))
			return
		}
	}

	trigger := s.codeAnalysisTrigger
	categories := req.Categories

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("code analysis panic", "recover", r)
			}
		}()
		ctx := context.Background()
		if err := trigger(ctx, categories); err != nil {
			slog.Error("code analysis failed", "error", err)
		} else {
			slog.Info("code analysis completed")
		}
	}()

	json200(w, map[string]any{
		"status":  "analysis_triggered",
		"message": "code analysis started in background",
	})
}

// GET /api/code-analysis/metrics
func (s *Server) handleGetCodeAnalysisMetrics(w http.ResponseWriter, r *http.Request) {
	days := queryInt(r, "days", 30)
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}

	metrics, err := s.db.ListCodeQualityMetrics(days)
	if err != nil {
		slog.Error("list code quality metrics", "err", err)
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if metrics == nil {
		metrics = []db.CodeQualityMetric{}
	}

	json200(w, map[string]any{
		"metrics": metrics,
	})
}

// GET /api/code-analysis/config
func (s *Server) handleGetCodeAnalysisConfig(w http.ResponseWriter, r *http.Request) {
	json200(w, s.cfg.CodeAnalysis)
}

// POST /api/code-analysis/config
func (s *Server) handleUpdateCodeAnalysisConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.CodeAnalysisConfig
	if err := decodeBody(r, &cfg); err != nil {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if cfg.MaxFilesPerRun < 1 || cfg.MaxFilesPerRun > 50 {
		jsonErr(w, http.StatusBadRequest, "max_files_per_run must be between 1 and 50")
		return
	}
	if cfg.TokenBudgetPerRun < 0 || cfg.TokenBudgetPerRun > 5000000 {
		jsonErr(w, http.StatusBadRequest, "token_budget_per_run must be between 0 and 5000000")
		return
	}
	if cfg.MinChurnScore < 0 || cfg.MinChurnScore > 1 {
		jsonErr(w, http.StatusBadRequest, "min_churn_score must be between 0 and 1")
		return
	}
	if cfg.ConfidenceThreshold < 0 || cfg.ConfidenceThreshold > 1 {
		jsonErr(w, http.StatusBadRequest, "confidence_threshold must be between 0 and 1")
		return
	}
	if cfg.ScanInterval < 5 || cfg.ScanInterval > 1440 {
		jsonErr(w, http.StatusBadRequest, "scan_interval must be between 5 and 1440 minutes")
		return
	}
	if cfg.GitHistoryDepth < 10 || cfg.GitHistoryDepth > 1000 {
		jsonErr(w, http.StatusBadRequest, "git_history_depth must be between 10 and 1000")
		return
	}
	for _, cat := range cfg.Categories {
		if _, ok := config.AllowedCodeAnalysisCategories[cat]; !ok {
			jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid category: %s", cat))
			return
		}
	}

	s.cfg.CodeAnalysis = cfg
	if err := s.cfg.Save(filepath.Join(s.projectRoot, ".stratus.json")); err != nil {
		slog.Error("save code analysis config", "err", err)
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	json200(w, s.cfg.CodeAnalysis)
}

// updateCodeFindingStatusRequest is the request body for PUT /api/code-analysis/findings/{id}/status.
type updateCodeFindingStatusRequest struct {
	Status string `json:"status"`
}

// PUT /api/code-analysis/findings/{id}/status
func (s *Server) handleUpdateCodeFindingStatus(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing finding id")
		return
	}

	var req updateCodeFindingStatusRequest
	if err := decodeBody(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	if req.Status == "" {
		jsonErr(w, http.StatusBadRequest, "status is required")
		return
	}

	if err := s.db.UpdateCodeFindingStatus(r.Context(), id, req.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonErr(w, http.StatusNotFound, "code finding not found")
			return
		}
		// DB layer returns a descriptive error for invalid status values.
		if isInvalidStatusError(err) {
			jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid status %q: must be 'rejected' or 'applied'", req.Status))
			return
		}
		slog.Error("update code finding status", "id", id, "err", err)
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	json200(w, map[string]any{
		"ok":     true,
		"id":     id,
		"status": req.Status,
	})
}

// isInvalidStatusError reports whether err originates from an invalid-status validation
// in the DB layer (i.e. the error message contains "invalid status").
func isInvalidStatusError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for i := 0; i+14 <= len(msg); i++ {
		if msg[i:i+14] == "invalid status" {
			return true
		}
	}
	return false
}

// registerCodeAnalysisRoutes registers all code analysis HTTP routes.
func (s *Server) registerCodeAnalysisRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/code-analysis/runs", s.handleListCodeAnalysisRuns)
	mux.HandleFunc("GET /api/code-analysis/runs/{id}", s.handleGetCodeAnalysisRun)
	mux.HandleFunc("GET /api/code-analysis/findings", s.handleListCodeFindings)
	mux.HandleFunc("PUT /api/code-analysis/findings/{id}/status", s.handleUpdateCodeFindingStatus)
	mux.HandleFunc("POST /api/code-analysis/trigger", s.handleTriggerCodeAnalysis)
	mux.HandleFunc("GET /api/code-analysis/metrics", s.handleGetCodeAnalysisMetrics)
	mux.HandleFunc("GET /api/code-analysis/config", s.handleGetCodeAnalysisConfig)
	mux.HandleFunc("POST /api/code-analysis/config", s.handleUpdateCodeAnalysisConfig)
}
