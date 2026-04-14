package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/generators"
)

// allowedProposalTypes is the complete allowlist of proposal types accepted by
// the create-proposal endpoint.  The new project-level categories were added for
// the evolution loop (T10).
var allowedProposalTypes = map[string]struct{}{
	// Existing types used by the insight engine.
	"rule":              {},
	"adr":               {},
	"template":          {},
	"skill":             {},
	"routing.change":    {},
	"workflow.investigate": {},
	// New types (T10).
	"idea":                  {},
	"refactor_opportunity":  {},
	"test_gap":              {},
	"architecture_drift":    {},
	"feature_idea":          {},
	"dx_improvement":        {},
	"doc_drift":             {},
}

// computeProposalHash returns the server-side idempotency hash for a proposal.
func computeProposalHash(proposalType, title string, signalRefs []string) string {
	input := generators.SignalHashInputs(proposalType, title, signalRefs)
	sum := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", sum)
}

func (s *Server) handleGetInsightStatus(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.db.GetInsightMetrics()
	if err != nil {
		slog.Warn("failed to get insight metrics", "error", err)
	}

	state, err := s.db.GetInsightState()
	if err != nil {
		slog.Warn("failed to get insight state", "error", err)
	}

	patterns, err := s.db.ListInsightPatterns("", "", 0.5, 10)
	if err != nil {
		slog.Warn("failed to get patterns", "error", err)
	}

	analyses, err := s.db.ListInsightAnalyses("full", 5)
	if err != nil {
		slog.Warn("failed to get analyses", "error", err)
	}

	status := map[string]any{
		"enabled":         s.insight != nil,
		"state":           state,
		"metrics":         metrics,
		"recent_patterns": patterns,
		"recent_analyses": analyses,
	}

	json200(w, status)
}

func (s *Server) handleTriggerInsightAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.insight == nil {
		jsonErr(w, http.StatusServiceUnavailable, "insight not initialized")
		return
	}

	go func() {
		if err := s.insight.RunAnalysis(); err != nil {
			slog.Error("Insight analysis failed", "error", err)
		}
	}()

	json200(w, map[string]any{
		"status":  "analysis_triggered",
		"message": "Insight analysis started in background",
	})
}

func (s *Server) handleGetInsightPatterns(w http.ResponseWriter, r *http.Request) {
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

	patterns, err := s.db.ListInsightPatterns(patternType, severity, minConfidence, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"patterns": patterns,
		"count":    len(patterns),
	})
}

func (s *Server) handleGetInsightAnalyses(w http.ResponseWriter, r *http.Request) {
	analysisType := r.URL.Query().Get("type")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			if parsed > 0 && parsed <= 500 {
				limit = parsed
			}
		}
	}

	analyses, err := s.db.ListInsightAnalyses(analysisType, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"analyses": analyses,
		"count":    len(analyses),
	})
}

func (s *Server) handleGetInsightProposals(w http.ResponseWriter, r *http.Request) {
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

	proposals, err := s.db.ListInsightProposals(proposalType, status, riskLevel, minConfidence, limit, offset)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"proposals": proposals,
		"count":     len(proposals),
	})
}

// POST /api/insight/proposals
func (s *Server) handleCreateInsightProposal(w http.ResponseWriter, r *http.Request) {
	// Decode into a raw map first so we can detect forbidden fields before
	// normalising into a typed struct.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Reject client-supplied idempotency_hash (server-authoritative only).
	if hashRaw, present := raw["idempotency_hash"]; present {
		// Even "null" is forbidden — the field must not appear in the request.
		_ = hashRaw
		jsonErr(w, http.StatusBadRequest, "idempotency_hash is server-computed; remove from request")
		return
	}

	// Helper to decode a string field.
	strField := func(key string) string {
		v, ok := raw[key]
		if !ok {
			return ""
		}
		var s string
		_ = json.Unmarshal(v, &s)
		return s
	}

	proposalType := strField("type")
	title := strField("title")
	description := strField("description")

	if proposalType == "" {
		jsonErr(w, http.StatusBadRequest, "type is required")
		return
	}
	if _, ok := allowedProposalTypes[proposalType]; !ok {
		jsonErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("unsupported proposal type: %s", proposalType))
		return
	}
	if title == "" {
		jsonErr(w, http.StatusBadRequest, "title is required")
		return
	}

	// Decode optional signal_refs (defaults to empty slice).
	var signalRefs []string
	if sr, ok := raw["signal_refs"]; ok {
		if err := json.Unmarshal(sr, &signalRefs); err != nil {
			jsonErr(w, http.StatusBadRequest, "signal_refs must be an array of strings")
			return
		}
	}
	if signalRefs == nil {
		signalRefs = []string{}
	}

	// Decode optional wiki_page_id.
	var wikiPageID *string
	if wp, ok := raw["wiki_page_id"]; ok {
		var wpStr string
		if err := json.Unmarshal(wp, &wpStr); err == nil && wpStr != "" {
			wikiPageID = &wpStr
		}
	}

	// Compute idempotency hash server-side.
	idempotencyHash := computeProposalHash(proposalType, title, signalRefs)

	proposal := &db.InsightProposal{
		Type:        proposalType,
		Title:       title,
		Description: description,
		Status:      "pending",
		RiskLevel:   "low",
	}

	resultID, inserted, err := s.db.CreateInsightProposalFull(proposal, idempotencyHash, signalRefs, wikiPageID)
	if err != nil {
		slog.Error("failed to create insight proposal", "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to create proposal")
		return
	}

	statusCode := http.StatusCreated
	if !inserted {
		statusCode = http.StatusOK
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":       resultID,
		"inserted": inserted,
	})
}

func (s *Server) handleGetInsightProposal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing proposal id")
		return
	}

	proposal, err := s.db.GetInsightProposalByID(id)
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

func (s *Server) handleTriggerInsightProposalGeneration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.insight == nil {
		jsonErr(w, http.StatusServiceUnavailable, "insight not initialized")
		return
	}

	go func() {
		ctx := context.Background()
		if err := s.insight.RunProposalGeneration(ctx); err != nil {
			slog.Error("Insight proposal generation failed", "error", err)
		}
	}()

	json200(w, map[string]any{
		"status":  "generation_triggered",
		"message": "Insight proposal generation started in background",
	})
}

func (s *Server) handleGetInsightDashboard(w http.ResponseWriter, r *http.Request) {
	summary, err := s.db.GetInsightDashboardSummary()
	if err != nil {
		slog.Error("failed to get insight dashboard summary", "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to get dashboard summary")
		return
	}

	json200(w, summary)
}

func (s *Server) handleUpdateInsightProposalStatus(w http.ResponseWriter, r *http.Request) {
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

	proposal, err := s.db.GetInsightProposalByID(id)
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

	prevStatus := proposal.Status

	if req.Reason != "" {
		err = s.db.UpdateInsightProposalStatusWithReason(id, req.Status, req.Reason)
	} else {
		err = s.db.UpdateInsightProposalStatus(id, req.Status)
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

	if req.Status == "approved" && strings.HasPrefix(proposal.Type, "asset.") {
		if applyErr := s.applyAssetProposal(proposal); applyErr != nil {
			slog.Error("apply asset proposal failed", "id", id, "err", applyErr)
			// Roll back status
			_ = s.db.UpdateInsightProposalStatus(id, prevStatus)
			jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("apply asset failed: %v", applyErr))
			return
		}
	}

	updatedProposal, err := s.db.GetInsightProposalByID(id)
	if err != nil {
		slog.Error("failed to fetch updated proposal", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, "failed to fetch updated proposal")
		return
	}

	json200(w, updatedProposal)
}

func (s *Server) applyAssetProposal(proposal *db.InsightProposal) error {
	rec := proposal.Recommendation
	path, _ := rec["proposed_path"].(string)
	content, _ := rec["proposed_content"].(string)
	if path == "" || content == "" {
		return fmt.Errorf("apply asset proposal %s: missing proposed_path or proposed_content", proposal.ID)
	}

	absPath, err := filepath.Abs(filepath.Join(s.projectRoot, path))
	if err != nil {
		return fmt.Errorf("apply asset proposal %s: resolve path: %w", proposal.ID, err)
	}

	projectAbs, _ := filepath.Abs(s.projectRoot)
	if !strings.HasPrefix(absPath, projectAbs+string(filepath.Separator)) {
		return fmt.Errorf("apply asset proposal %s: path traversal detected", proposal.ID)
	}

	// Never overwrite existing files
	if _, statErr := os.Stat(absPath); statErr == nil {
		return fmt.Errorf("apply asset proposal %s: file already exists at %s", proposal.ID, path)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("apply asset proposal %s: create dirs: %w", proposal.ID, err)
	}

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("apply asset proposal %s: write file: %w", proposal.ID, err)
	}

	slog.Info("asset proposal applied", "id", proposal.ID, "path", path)
	return nil
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

	if s.insight == nil {
		jsonErr(w, http.StatusServiceUnavailable, "insight not initialized")
		return
	}

	go func() {
		ctx := context.Background()
		if err := s.insight.RunScorecardComputation(ctx); err != nil {
			slog.Error("Insight scorecard computation failed", "error", err)
		}
	}()

	json200(w, map[string]any{
		"status":  "computation_triggered",
		"message": "Insight scorecard computation started in background",
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

	if s.insight == nil {
		jsonErr(w, http.StatusServiceUnavailable, "insight not initialized")
		return
	}

	go func() {
		ctx := context.Background()
		if err := s.insight.RunRoutingAnalysis(ctx); err != nil {
			slog.Error("Insight routing analysis failed", "error", err)
		}
	}()

	json200(w, map[string]any{
		"status":  "analysis_triggered",
		"message": "Insight routing analysis started in background",
	})
}

func (s *Server) handleTestLLMConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.insight == nil {
		jsonErr(w, http.StatusServiceUnavailable, "insight not initialized")
		return
	}

	llmClient := s.insight.LLMClient()
	if llmClient == nil {
		jsonErr(w, http.StatusServiceUnavailable, "llm client not configured")
		return
	}

	ctx := r.Context()
	resp, err := llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: "You are a helpful assistant.",
		Messages:     []llm.Message{llm.UserMessage("Say 'OK' if you can hear me.")},
		MaxTokens:    10,
	})
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("llm test failed: %v", err))
		return
	}

	json200(w, map[string]any{
		"status":        "ok",
		"provider":      llmClient.Provider(),
		"model":         llmClient.Model(),
		"response":      resp.Content,
		"input_tokens":  resp.InputTokens,
		"output_tokens": resp.OutputTokens,
	})
}

// GET /api/insight/config
func (s *Server) handleGetInsightConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Insight
	masked := cfg
	masked.LLM = maskLLMConfig(masked.LLM)
	json200(w, masked)
}

// PUT /api/insight/config
func (s *Server) handleUpdateInsightConfig(w http.ResponseWriter, r *http.Request) {
	var incoming config.InsightConfig
	if err := decodeBody(r, &incoming); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	// If the masked sentinel or empty string is sent back, keep the existing key.
	restoreLLMAPIKey(&incoming.LLM, s.cfg.Insight.LLM)

	if err := validateLLMConfig(incoming.LLM, true); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.cfg.Insight = incoming
	if err := s.cfg.Save(filepath.Join(s.projectRoot, ".stratus.json")); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save config: "+err.Error())
		return
	}

	masked := incoming
	masked.LLM = maskLLMConfig(masked.LLM)
	json200(w, masked)
}
