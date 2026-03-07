package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/MartinNevlaha/stratus-v2/internal/openclaw/agent_evolution"
)

func (s *Server) handleGetAgentEvolutionSummary(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	summary, err := s.agentEvolutionEngine.GetSummary(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, summary)
}

func (s *Server) handleGetAgentEvolutionOpportunities(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	opportunities, err := s.agentEvolutionEngine.GetOpportunities(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"opportunities": opportunities,
		"count":         len(opportunities),
	})
}

func (s *Server) handleGetAgentCandidates(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	status := r.URL.Query().Get("status")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}

	candidates, err := s.agentEvolutionEngine.GetCandidates(r.Context(), status, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"candidates": candidates,
		"count":      len(candidates),
	})
}

func (s *Server) handleGetAgentCandidateByID(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing candidate id")
		return
	}

	candidate, err := s.agentEvolutionEngine.GetCandidate(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if candidate == nil {
		jsonErr(w, http.StatusNotFound, "candidate not found")
		return
	}

	json200(w, candidate)
}

func (s *Server) handleApproveAgentCandidate(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing candidate id")
		return
	}

	if err := s.agentEvolutionEngine.ApproveCandidate(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	slog.Info("Agent candidate approved", "candidate_id", id)

	json200(w, map[string]any{
		"status":  "approved",
		"message": "Agent candidate approved and files generated",
	})
}

func (s *Server) handleRejectAgentCandidate(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing candidate id")
		return
	}

	if err := s.agentEvolutionEngine.RejectCandidate(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	slog.Info("Agent candidate rejected", "candidate_id", id)

	json200(w, map[string]any{
		"status":  "rejected",
		"message": "Agent candidate rejected",
	})
}

func (s *Server) handleStartAgentExperiment(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing candidate id")
		return
	}

	experiment, err := s.agentEvolutionEngine.StartExperiment(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	slog.Info("Agent experiment started", "experiment_id", experiment.ID, "candidate_id", id)

	json200(w, map[string]any{
		"status":     "started",
		"experiment": experiment,
	})
}

func (s *Server) handleGetAgentExperiments(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	status := r.URL.Query().Get("status")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}

	experiments, err := s.agentEvolutionEngine.GetExperiments(r.Context(), status, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"experiments": experiments,
		"count":       len(experiments),
	})
}

func (s *Server) handleGetAgentExperimentByID(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing experiment id")
		return
	}

	experiment, err := s.agentEvolutionEngine.GetExperiment(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if experiment == nil {
		jsonErr(w, http.StatusNotFound, "experiment not found")
		return
	}

	json200(w, experiment)
}

func (s *Server) handleGetAgentExperimentResults(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing experiment id")
		return
	}

	metrics, err := s.agentEvolutionEngine.EvaluateExperiment(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"metrics": metrics,
	})
}

func (s *Server) handleCancelAgentExperiment(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing experiment id")
		return
	}

	if err := s.agentEvolutionEngine.CancelExperiment(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	slog.Info("Agent experiment cancelled", "experiment_id", id)

	json200(w, map[string]any{
		"status":  "cancelled",
		"message": "Agent experiment cancelled",
	})
}

func (s *Server) handleTriggerAgentEvolution(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	go func() {
		if err := s.agentEvolutionEngine.Run(context.Background()); err != nil {
			slog.Error("Agent evolution analysis failed", "error", err)
		}
	}()

	json200(w, map[string]any{
		"status":  "triggered",
		"message": "Agent evolution analysis started in background",
	})
}

func (s *Server) handleGetAgentCandidateMarkdown(w http.ResponseWriter, r *http.Request) {
	if s.agentEvolutionEngine == nil {
		jsonErr(w, http.StatusServiceUnavailable, "agent evolution engine not initialized")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing candidate id")
		return
	}

	candidate, err := s.agentEvolutionEngine.GetCandidate(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if candidate == nil {
		jsonErr(w, http.StatusNotFound, "candidate not found")
		return
	}

	store := s.agentEvolutionEngine.GetStore()
	generator := agent_evolution.NewAgentCandidateGenerator(store, agent_evolution.DefaultConfig(), "", "")
	claudeContent, opencodeContent := generator.GenerateAgentMarkdown(*candidate)

	json200(w, map[string]any{
		"claude_markdown":   claudeContent,
		"opencode_markdown": opencodeContent,
	})
}
