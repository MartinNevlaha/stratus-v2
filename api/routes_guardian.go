package api

import (
	"context"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/guardian"
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
	if masked.LLMAPIKey != "" {
		masked.LLMAPIKey = "***"
	}
	json200(w, masked)
}

// PUT /api/guardian/config
func (s *Server) handleUpdateGuardianConfig(w http.ResponseWriter, r *http.Request) {
	var incoming config.GuardianConfig
	if err := decodeBody(r, &incoming); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	// If the masked sentinel is sent back, keep the existing key.
	if incoming.LLMAPIKey == "***" {
		incoming.LLMAPIKey = s.cfg.Guardian.LLMAPIKey
	}

	s.cfg.Guardian = incoming
	if err := s.cfg.Save(filepath.Join(s.projectRoot, ".stratus.json")); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save config: "+err.Error())
		return
	}

	masked := incoming
	if masked.LLMAPIKey != "" {
		masked.LLMAPIKey = "***"
	}
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

// POST /api/guardian/test-llm — tests the configured LLM endpoint
func (s *Server) handleTestGuardianLLM(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Guardian
	// Allow overriding with body for "test before save" UX
	var override struct {
		Endpoint    string  `json:"llm_endpoint"`
		APIKey      string  `json:"llm_api_key"`
		Model       string  `json:"llm_model"`
		Temperature float64 `json:"llm_temperature"`
		MaxTokens   int     `json:"llm_max_tokens"`
	}
	_ = decodeBody(r, &override)
	if override.Endpoint != "" {
		cfg.LLMEndpoint = override.Endpoint
	}
	if override.APIKey != "" && override.APIKey != "***" {
		cfg.LLMAPIKey = override.APIKey
	}
	if override.Model != "" {
		cfg.LLMModel = override.Model
	}
	if override.Temperature > 0 {
		cfg.LLMTemperature = override.Temperature
	}
	if override.MaxTokens > 0 {
		cfg.LLMMaxTokens = override.MaxTokens
	}

	// Use the exported test helper via a package-level function.
	if err := guardian.TestLLMEndpoint(r.Context(), cfg.LLMEndpoint, cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMTemperature, cfg.LLMMaxTokens); err != nil {
		jsonErr(w, http.StatusBadGateway, "llm test failed: "+err.Error())
		return
	}
	json200(w, map[string]bool{"ok": true})
}

