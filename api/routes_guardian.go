package api

import (
	"context"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	insightllm "github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

// GET /api/guardian/alerts
func (s *Server) handleListGuardianAlerts(w http.ResponseWriter, r *http.Request) {
	alertType := queryStr(r, "type")
	alerts, err := s.db.ListGuardianAlerts(alertType)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if alerts == nil {
		alerts = []db.GuardianAlert{}
	}
	json200(w, alerts)
}

// PUT /api/guardian/alerts/{id}/dismiss
func (s *Server) handleDismissGuardianAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(pathParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.db.DismissGuardianAlert(id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	json200(w, map[string]bool{"ok": true})
}

// POST /api/guardian/alerts/dismiss-all
func (s *Server) handleDismissAllGuardianAlerts(w http.ResponseWriter, r *http.Request) {
	count, err := s.db.DismissAllGuardianAlerts()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	json200(w, map[string]interface{}{"ok": true, "dismissed": count})
}

// DELETE /api/guardian/alerts/{id}
func (s *Server) handleDeleteGuardianAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(pathParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.db.DeleteGuardianAlert(id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	json200(w, map[string]bool{"ok": true})
}

// GET /api/guardian/config
func (s *Server) handleGetGuardianConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Guardian
	// Mask API key — only show whether it's set.
	masked := cfg
	masked.LLM = maskLLMConfig(masked.LLM)
	json200(w, masked)
}

// PUT /api/guardian/config
func (s *Server) handleUpdateGuardianConfig(w http.ResponseWriter, r *http.Request) {
	var incoming config.GuardianConfig
	if err := decodeBody(r, &incoming); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	// If the masked sentinel or empty string is sent back, keep the existing key.
	restoreLLMAPIKey(&incoming.LLM, s.cfg.Guardian.LLM)

	if err := validateLLMConfig(incoming.LLM, true); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.cfg.Guardian = incoming
	if err := s.cfg.Save(filepath.Join(s.projectRoot, ".stratus.json")); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save config: "+err.Error())
		return
	}

	masked := incoming
	masked.LLM = maskLLMConfig(masked.LLM)
	json200(w, masked)
}

// POST /api/guardian/run — triggers an immediate scan (non-blocking)
func (s *Server) handleRunGuardianScan(w http.ResponseWriter, r *http.Request) {
	if s.guardianSvc == nil {
		jsonErr(w, http.StatusServiceUnavailable, "guardian not running")
		return
	}
	s.guardianSvc.RunOnce(context.Background())
	json200(w, map[string]bool{"ok": true})
}

// POST /api/guardian/test-llm — tests the configured LLM endpoint with an optional override body
func (s *Server) handleTestGuardianLLM(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LLM config.LLMConfig `json:"llm"`
	}
	_ = decodeBody(r, &body)

	// Allow testing with stored key when body sends sentinel or empty.
	restoreLLMAPIKey(&body.LLM, s.cfg.Guardian.LLM)

	// Resolve: merge with top-level LLM config.
	resolved := config.ResolveLLMConfig(s.cfg.LLM, body.LLM)

	// For a live test call, provider and model must be present.
	if err := validateLLMConfig(resolved, false); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	client, err := insightllm.NewClient(insightllm.Config{
		Provider:    resolved.Provider,
		Model:       resolved.Model,
		APIKey:      resolved.APIKey,
		BaseURL:     resolved.BaseURL,
		Timeout:     resolved.Timeout,
		MaxTokens:   resolved.MaxTokens,
		Temperature: resolved.Temperature,
		MaxRetries:  resolved.MaxRetries,
		Concurrency: resolved.Concurrency,
	})
	if err != nil {
		http.Error(w, "llm init failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	start := time.Now()
	_, err = client.Complete(r.Context(), insightllm.CompletionRequest{
		SystemPrompt: "You are a helpful assistant.",
		Messages:     []insightllm.Message{insightllm.UserMessage("Say 'OK'.")},
		MaxTokens:    10,
	})
	if err != nil {
		http.Error(w, "llm test failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json200(w, map[string]interface{}{
		"ok":         true,
		"latency_ms": time.Since(start).Milliseconds(),
		"model":      resolved.Model,
	})
}
