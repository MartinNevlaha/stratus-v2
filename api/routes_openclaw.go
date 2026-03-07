package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func (s *Server) handleGetOpenClawStatus(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.db.GetOpenClawMetrics()
	if err != nil {
		slog.Warn("failed to get openclaw metrics", "error", err)
	}

	state, err := s.db.GetOpenClawState()
	if err != nil {
		slog.Warn("failed to get openclaw state", "error", err)
	}

	patterns, err := s.db.ListOpenClawPatterns("", "", 0.5, 10)
	if err != nil {
		slog.Warn("failed to get patterns", "error", err)
	}

	analyses, err := s.db.ListOpenClawAnalyses("full", 5)
	if err != nil {
		slog.Warn("failed to get analyses", "error", err)
	}

	status := map[string]any{
		"enabled":         true,
		"state":           state,
		"metrics":         metrics,
		"recent_patterns": patterns,
		"recent_analyses": analyses,
	}

	json200(w, status)
}

func (s *Server) handleTriggerOpenClawAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.openclaw == nil {
		jsonErr(w, http.StatusServiceUnavailable, "openclaw not initialized")
		return
	}

	go func() {
		if err := s.openclaw.RunAnalysis(); err != nil {
			slog.Error("OpenClaw analysis failed", "error", err)
		}
	}()

	json200(w, map[string]any{
		"status":  "analysis_triggered",
		"message": "OpenClaw analysis started in background",
	})
}

func (s *Server) handleGetOpenClawPatterns(w http.ResponseWriter, r *http.Request) {
	patternType := r.URL.Query().Get("type")
	severity := r.URL.Query().Get("severity")
	minConfidence := 0.5
	if mc := r.URL.Query().Get("min_confidence"); mc != "" {
		if parsed, err := strconv.ParseFloat(mc, 64); err == nil {
			if parsed >= 0 && parsed <= 1.0 {
				minConfidence = parsed
			}
		}
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			if parsed > 0 && parsed <= 500 {
				limit = parsed
			}
		}
	}

	patterns, err := s.db.ListOpenClawPatterns(patternType, severity, minConfidence, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"patterns": patterns,
		"count":    len(patterns),
	})
}

func (s *Server) handleGetOpenClawAnalyses(w http.ResponseWriter, r *http.Request) {
	analysisType := r.URL.Query().Get("type")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			if parsed > 0 && parsed <= 500 {
				limit = parsed
			}
		}
	}

	analyses, err := s.db.ListOpenClawAnalyses(analysisType, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"analyses": analyses,
		"count":    len(analyses),
	})
}

func (s *Server) handleGetOpenClawProposals(w http.ResponseWriter, r *http.Request) {
	proposalType := r.URL.Query().Get("type")
	status := r.URL.Query().Get("status")
	riskLevel := r.URL.Query().Get("risk")
	minConfidence := 0.0
	if mc := r.URL.Query().Get("min_confidence"); mc != "" {
		if parsed, err := strconv.ParseFloat(mc, 64); err == nil {
			if parsed >= 0 && parsed <= 1.0 {
				minConfidence = parsed
			}
		}
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			if parsed > 0 && parsed <= 200 {
				limit = parsed
			}
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil {
			if parsed >= 0 {
				offset = parsed
			}
		}
	}

	proposals, err := s.db.ListOpenClawProposals(proposalType, status, riskLevel, minConfidence, limit, offset)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"proposals": proposals,
		"count":     len(proposals),
	})
}

func (s *Server) handleGetOpenClawProposal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing proposal id")
		return
	}

	proposal, err := s.db.GetOpenClawProposalByID(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if proposal == nil {
		jsonErr(w, http.StatusNotFound, "proposal not found")
		return
	}

	json200(w, proposal)
}

func (s *Server) handleTriggerOpenClawProposalGeneration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.openclaw == nil {
		jsonErr(w, http.StatusServiceUnavailable, "openclaw not initialized")
		return
	}

	go func() {
		ctx := context.Background()
		if err := s.openclaw.RunProposalGeneration(ctx); err != nil {
			slog.Error("OpenClaw proposal generation failed", "error", err)
		}
	}()

	json200(w, map[string]any{
		"status":  "generation_triggered",
		"message": "OpenClaw proposal generation started in background",
	})
}

func (s *Server) handleGetOpenClawDashboard(w http.ResponseWriter, r *http.Request) {
	summary, err := s.db.GetOpenClawDashboardSummary()
	if err != nil {
		slog.Error("failed to get openclaw dashboard summary", "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get dashboard summary")
		return
	}

	json200(w, summary)
}

func (s *Server) handleUpdateOpenClawProposalStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing proposal id")
		return
	}

	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !db.IsValidProposalStatus(req.Status) {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid status: %s", req.Status))
		return
	}

	proposal, err := s.db.GetOpenClawProposalByID(id)
	if err != nil {
		slog.Error("failed to get proposal", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get proposal")
		return
	}

	if proposal == nil {
		jsonErr(w, http.StatusNotFound, "proposal not found")
		return
	}

	if err := db.ValidTransition(db.ProposalStatus(proposal.Status), db.ProposalStatus(req.Status)); err != nil {
		slog.Warn("invalid status transition",
			"id", id,
			"current", proposal.Status,
			"requested", req.Status,
			"error", err,
		)

		if db.IsInvalidTransitionError(err) {
			jsonErr(w, http.StatusBadRequest, err.Error())
		} else {
			jsonErr(w, http.StatusInternalServerError, "failed to validate transition")
		}
		return
	}

	if req.Reason != "" {
		err = s.db.UpdateOpenClawProposalStatusWithReason(id, req.Status, req.Reason)
	} else {
		err = s.db.UpdateOpenClawProposalStatus(id, req.Status)
	}

	if err != nil {
		slog.Error("failed to update proposal status",
			"id", id,
			"status", req.Status,
			"error", err,
		)
		jsonErr(w, http.StatusInternalServerError, "failed to update proposal status")
		return
	}

	slog.Info("proposal status updated",
		"id", id,
		"status", req.Status,
		"reason", req.Reason,
	)

	updatedProposal, err := s.db.GetOpenClawProposalByID(id)
	if err != nil {
		slog.Error("failed to fetch updated proposal", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to fetch updated proposal")
		return
	}

	json200(w, updatedProposal)
}

func (s *Server) handleGetAgentScorecards(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "7d"
	}

	sortBy := r.URL.Query().Get("sortBy")
	sortDir := r.URL.Query().Get("sortDirection")
	if sortDir == "" {
		sortDir = "DESC"
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			if parsed > 0 && parsed <= 200 {
				limit = parsed
			}
		}
	}

	cards, err := s.db.ListAgentScorecards(window, sortBy, sortDir, limit)
	if err != nil {
		slog.Error("failed to list agent scorecards", "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to list agent scorecards")
		return
	}

	highlights, _ := s.db.GetScorecardHighlights(window)

	json200(w, map[string]any{
		"scorecards": cards,
		"window":     window,
		"count":      len(cards),
		"highlights": highlights,
	})
}

func (s *Server) handleGetAgentScorecardByName(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "missing agent name")
		return
	}

	window := r.URL.Query().Get("window")
	if window == "" {
		window = "7d"
	}

	card, err := s.db.GetAgentScorecardByName(name, window)
	if err != nil {
		slog.Error("failed to get agent scorecard", "name", name, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get agent scorecard")
		return
	}

	if card == nil {
		jsonErr(w, http.StatusNotFound, "agent scorecard not found")
		return
	}

	json200(w, card)
}

func (s *Server) handleGetWorkflowScorecards(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "7d"
	}

	sortBy := r.URL.Query().Get("sortBy")
	sortDir := r.URL.Query().Get("sortDirection")
	if sortDir == "" {
		sortDir = "DESC"
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			if parsed > 0 && parsed <= 200 {
				limit = parsed
			}
		}
	}

	cards, err := s.db.ListWorkflowScorecards(window, sortBy, sortDir, limit)
	if err != nil {
		slog.Error("failed to list workflow scorecards", "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to list workflow scorecards")
		return
	}

	highlights, _ := s.db.GetScorecardHighlights(window)

	json200(w, map[string]any{
		"scorecards": cards,
		"window":     window,
		"count":      len(cards),
		"highlights": highlights,
	})
}

func (s *Server) handleGetWorkflowScorecardByType(w http.ResponseWriter, r *http.Request) {
	wfType := r.PathValue("type")
	if wfType == "" {
		jsonErr(w, http.StatusBadRequest, "missing workflow type")
		return
	}

	window := r.URL.Query().Get("window")
	if window == "" {
		window = "7d"
	}

	card, err := s.db.GetWorkflowScorecardByType(wfType, window)
	if err != nil {
		slog.Error("failed to get workflow scorecard", "type", wfType, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get workflow scorecard")
		return
	}

	if card == nil {
		jsonErr(w, http.StatusNotFound, "workflow scorecard not found")
		return
	}

	json200(w, card)
}

func (s *Server) handleTriggerScorecardComputation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.openclaw == nil {
		jsonErr(w, http.StatusServiceUnavailable, "openclaw not initialized")
		return
	}

	go func() {
		ctx := context.Background()
		if err := s.openclaw.RunScorecardComputation(ctx); err != nil {
			slog.Error("OpenClaw scorecard computation failed", "error", err)
		}
	}()

	json200(w, map[string]any{
		"status":  "computation_triggered",
		"message": "OpenClaw scorecard computation started in background",
	})
}

func (s *Server) handleGetScorecardHighlights(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "7d"
	}

	highlights, err := s.db.GetScorecardHighlights(window)
	if err != nil {
		slog.Error("failed to get scorecard highlights", "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get scorecard highlights")
		return
	}

	agentCards, _ := s.db.ListAgentScorecards(window, "success_rate", "DESC", 5)
	workflowCards, _ := s.db.ListWorkflowScorecards(window, "completion_rate", "DESC", 5)

	response := map[string]any{
		"window":              window,
		"highlights":          highlights,
		"top_agents":          agentCards,
		"top_workflows":       workflowCards,
		"metric_definitions":  getScorecardMetricDefinitions(),
		"approximation_notes": getScorecardApproximationNotes(),
	}

	json200(w, response)
}

func getScorecardMetricDefinitions() map[string]any {
	return map[string]any{
		"agent": map[string]any{
			"success_rate":     "agent.completed / (agent.completed + agent.failed)",
			"failure_rate":     "agent.failed / total agent runs",
			"review_pass_rate": "review.passed / (review.passed + review.failed) for this agent",
			"rework_rate":      "cycles of failure→retry / total runs",
			"avg_cycle_time":   "avg time between agent.spawned and agent.completed",
			"regression_rate":  "failed after success / total successful",
			"confidence_score": "based on sample size and event completeness",
		},
		"workflow": map[string]any{
			"completion_rate":       "workflow.completed / total workflows",
			"failure_rate":          "workflow.failed / total workflows",
			"review_rejection_rate": "review.failed / total reviews",
			"rework_rate":           "phase backtracks / total runs",
			"avg_duration":          "avg time from started to completed/failed",
			"confidence_score":      "based on sample size and event completeness",
		},
	}
}

func getScorecardApproximationNotes() []string {
	return []string{
		"Rework Rate (Agent): Approximated by counting retry cycles (failed→spawned pairs) within the same workflow instance",
		"Rework Rate (Workflow): Approximated by counting phase transitions that move backwards in the workflow",
		"Regression Rate: Approximated by counting failures that occur after a successful completion for the same agent/workflow",
		"Cycle Time: Calculated from agent.spawned to agent.completed events",
		"Review metrics: Attributed to the agent that spawned the work being reviewed",
		"Confidence Score: 0.3 base for <5 samples, increases with sample size up to 0.95",
		"Trend: improving/degrading based on >5% change in 2+ key metrics",
	}
}

func (s *Server) handleGetRoutingRecommendations(w http.ResponseWriter, r *http.Request) {
	workflowType := r.URL.Query().Get("workflow")
	recType := r.URL.Query().Get("type")
	minConfidence := 0.0
	if mc := r.URL.Query().Get("min_confidence"); mc != "" {
		if parsed, err := strconv.ParseFloat(mc, 64); err == nil {
			if parsed >= 0 && parsed <= 1.0 {
				minConfidence = parsed
			}
		}
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			if parsed > 0 && parsed <= 200 {
				limit = parsed
			}
		}
	}

	filters := db.RoutingRecommendationFilters{
		WorkflowType:  workflowType,
		RecType:       recType,
		MinConfidence: minConfidence,
	}

	recommendations, err := s.db.ListRoutingRecommendations(filters, limit)
	if err != nil {
		slog.Error("failed to list routing recommendations", "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to list routing recommendations")
		return
	}

	json200(w, map[string]any{
		"recommendations": recommendations,
		"count":           len(recommendations),
	})
}

func (s *Server) handleGetRoutingRecommendation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing recommendation id")
		return
	}

	rec, err := s.db.GetRoutingRecommendationByID(id)
	if err != nil {
		slog.Error("failed to get routing recommendation", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get routing recommendation")
		return
	}

	if rec == nil {
		jsonErr(w, http.StatusNotFound, "recommendation not found")
		return
	}

	json200(w, rec)
}

func (s *Server) handleTriggerRoutingAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.openclaw == nil {
		jsonErr(w, http.StatusServiceUnavailable, "openclaw not initialized")
		return
	}

	go func() {
		ctx := context.Background()
		if err := s.openclaw.RunRoutingAnalysis(ctx); err != nil {
			slog.Error("OpenClaw routing analysis failed", "error", err)
		}
	}()

	json200(w, map[string]any{
		"status":  "analysis_triggered",
		"message": "OpenClaw routing analysis started in background",
	})
}
