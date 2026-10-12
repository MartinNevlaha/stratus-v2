package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
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
	sorted := make([]string, len(signalRefs))
	copy(sorted, signalRefs)
	sort.Strings(sorted)
	input := strings.Join(append([]string{proposalType, title}, sorted...), "|")
	sum := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", sum)
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
