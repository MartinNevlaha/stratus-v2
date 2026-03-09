package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/product_intelligence"
)

func (s *Server) handlePIListProjects(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	projects, err := s.piEngine.ListProjects(r.Context(), limit)
	if err != nil {
		slog.Error("failed to list pi projects", "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to list projects")
		return
	}

	json200(w, map[string]any{
		"projects": projects,
		"count":    len(projects),
	})
}

func (s *Server) handlePIRegisterProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Path == "" {
		jsonErr(w, http.StatusBadRequest, "path is required")
		return
	}

	if req.Name == "" {
		req.Name = "Unnamed Project"
	}

	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "failed to resolve path")
		return
	}

	fi, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		jsonErr(w, http.StatusBadRequest, "path does not exist")
		return
	}
	if err != nil {
		slog.Error("failed to stat path", "path", absPath, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to access path")
		return
	}
	if !fi.IsDir() {
		jsonErr(w, http.StatusBadRequest, "path is not a directory")
		return
	}

	project, err := s.piEngine.RegisterProject(r.Context(), absPath, req.Name)
	if err != nil {
		slog.Error("failed to register project", "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to register project")
		return
	}

	json200(w, project)
}

func (s *Server) handlePIGetProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing project id")
		return
	}

	project, err := s.piEngine.GetProject(r.Context(), id)
	if err != nil {
		slog.Error("failed to get project", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get project")
		return
	}

	if project == nil {
		jsonErr(w, http.StatusNotFound, "project not found")
		return
	}

	json200(w, project)
}

func (s *Server) handlePIDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing project id")
		return
	}

	if err := s.piEngine.DeleteProject(r.Context(), id); err != nil {
		slog.Error("failed to delete project", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to delete project")
		return
	}

	json200(w, map[string]any{"status": "deleted"})
}

func (s *Server) handlePIAnalyzeProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing project id")
		return
	}

	var cfg product_intelligence.ProjectAnalysisConfig
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			slog.Warn("failed to parse analysis config, using defaults", "error", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	go func() {
		defer cancel()
		result, err := s.piEngine.AnalyzeProject(ctx, id, cfg)
		if err != nil {
			slog.Error("product intelligence analysis failed", "project_id", id, "error", err)
			return
		}
		slog.Info("product intelligence analysis complete",
			"project_id", id,
			"features", len(result.Features),
			"gaps", len(result.Gaps),
			"proposals", len(result.Proposals),
			"duration_ms", result.DurationMs)
	}()

	json200(w, map[string]any{
		"status":  "analysis_started",
		"message": "Product intelligence analysis started in background",
	})
}

func (s *Server) handlePIGetProjectFeatures(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing project id")
		return
	}

	features, err := s.piEngine.GetProjectFeatures(r.Context(), id)
	if err != nil {
		slog.Error("failed to get project features", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get features")
		return
	}

	json200(w, map[string]any{
		"features": features,
		"count":    len(features),
	})
}

func (s *Server) handlePIGetProjectGaps(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing project id")
		return
	}

	gaps, err := s.piEngine.GetFeatureGaps(r.Context(), id)
	if err != nil {
		slog.Error("failed to get feature gaps", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get gaps")
		return
	}

	json200(w, map[string]any{
		"gaps":  gaps,
		"count": len(gaps),
	})
}

func (s *Server) handlePIGetProjectProposals(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing project id")
		return
	}

	proposals, err := s.piEngine.GetFeatureProposals(r.Context(), id)
	if err != nil {
		slog.Error("failed to get proposals", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get proposals")
		return
	}

	json200(w, map[string]any{
		"proposals": proposals,
		"count":     len(proposals),
	})
}

func (s *Server) handlePIGetProposal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing proposal id")
		return
	}

	proposal, err := s.piEngine.GetProposalByID(r.Context(), id)
	if err != nil {
		slog.Error("failed to get proposal", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get proposal")
		return
	}

	if proposal == nil {
		jsonErr(w, http.StatusNotFound, "proposal not found")
		return
	}

	json200(w, proposal)
}

func (s *Server) handlePIAcceptProposal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing proposal id")
		return
	}

	var req struct {
		WorkflowID string `json:"workflow_id"`
	}

	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			slog.Warn("failed to parse accept request", "error", err)
		}
	}

	if err := s.piEngine.AcceptProposal(r.Context(), id, req.WorkflowID); err != nil {
		slog.Error("failed to accept proposal", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to accept proposal")
		return
	}

	proposal, _ := s.piEngine.GetProposalByID(r.Context(), id)

	slog.Info("proposal accepted", "id", id, "workflow_id", req.WorkflowID)

	json200(w, map[string]any{
		"status":      "accepted",
		"proposal":    proposal,
		"workflow_id": req.WorkflowID,
	})
}

func (s *Server) handlePIRejectProposal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing proposal id")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}

	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			slog.Warn("failed to parse reject request", "error", err)
		}
	}

	if err := s.piEngine.RejectProposal(r.Context(), id, req.Reason); err != nil {
		slog.Error("failed to reject proposal", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to reject proposal")
		return
	}

	slog.Info("proposal rejected", "id", id, "reason", req.Reason)

	json200(w, map[string]any{
		"status": "rejected",
	})
}

func (s *Server) handlePIGetMarketFeatures(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		jsonErr(w, http.StatusBadRequest, "domain parameter is required")
		return
	}

	features, err := s.piEngine.GetMarketFeatures(r.Context(), domain)
	if err != nil {
		slog.Error("failed to get market features", "domain", domain, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get market features")
		return
	}

	json200(w, map[string]any{
		"domain":   domain,
		"features": features,
		"count":    len(features),
	})
}

func (s *Server) handlePIRefreshMarketResearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	domain := r.URL.Query().Get("domain")
	if domain == "" {
		jsonErr(w, http.StatusBadRequest, "domain parameter is required")
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.piEngine.RefreshMarketResearch(ctx, domain); err != nil {
			slog.Error("market research refresh failed", "domain", domain, "error", err)
		}
	}()

	json200(w, map[string]any{
		"status":  "refresh_started",
		"domain":  domain,
		"message": "Market research refresh started in background",
	})
}

func (s *Server) handlePIGetDashboard(w http.ResponseWriter, r *http.Request) {
	projects, err := s.piEngine.ListProjects(r.Context(), 50)
	if err != nil {
		slog.Error("failed to get pi dashboard data", "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get dashboard data")
		return
	}

	totalFeatures := 0
	totalGaps := 0
	totalProposals := 0
	proposalsByStatus := make(map[string]int)

	for _, p := range projects {
		features, _ := s.piEngine.GetProjectFeatures(r.Context(), p.ID)
		totalFeatures += len(features)

		gaps, _ := s.piEngine.GetFeatureGaps(r.Context(), p.ID)
		totalGaps += len(gaps)

		proposals, _ := s.piEngine.GetFeatureProposals(r.Context(), p.ID)
		totalProposals += len(proposals)

		for _, prop := range proposals {
			proposalsByStatus[string(prop.Status)]++
		}
	}

	json200(w, map[string]any{
		"projects":            projects,
		"project_count":       len(projects),
		"total_features":      totalFeatures,
		"total_gaps":          totalGaps,
		"total_proposals":     totalProposals,
		"proposals_by_status": proposalsByStatus,
	})
}
