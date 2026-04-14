package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

// GET /api/llm/config — returns the global LLM config with APIKey masked.
func (s *Server) handleGetLLMConfig(w http.ResponseWriter, r *http.Request) {
	masked := maskLLMConfig(s.cfg.LLM)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(masked)
}

// PUT /api/llm/config — updates the global LLM config. Bounds-checks input,
// restores the stored API key when incoming is empty or the "***" sentinel,
// persists to .stratus.json, and returns the masked updated config.
func (s *Server) handleUpdateLLMConfig(w http.ResponseWriter, r *http.Request) {
	var incoming config.LLMConfig
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, fmt.Sprintf("decode llm config: %v", err), http.StatusBadRequest)
		return
	}
	restoreLLMAPIKey(&incoming, s.cfg.LLM)
	if err := validateLLMConfig(incoming, true); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.cfg.LLM = incoming
	if err := s.cfg.Save(filepath.Join(s.projectRoot, ".stratus.json")); err != nil {
		http.Error(w, fmt.Sprintf("save config: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(maskLLMConfig(s.cfg.LLM))
}

func (s *Server) handleGetLLMStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.LLM

	// Get today's usage
	today := time.Now().UTC().Format("2006-01-02")
	input, output, err := s.db.GetDailyTokenUsageTotal(today)
	if err != nil {
		log.Printf("llm status: get daily usage: %v", err)
		http.Error(w, `{"error":"failed to retrieve usage data"}`, http.StatusInternalServerError)
		return
	}
	used := input + output

	budget := s.cfg.Evolution.DailyTokenBudget
	remaining := budget - used
	if remaining < 0 {
		remaining = 0
	}

	// Reset time is midnight UTC tomorrow
	tomorrow := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)

	resp := map[string]any{
		"configured":      cfg.Provider != "" && cfg.Model != "",
		"provider":        cfg.Provider,
		"model":           cfg.Model,
		"daily_budget":    budget,
		"daily_used":      used,
		"daily_remaining": remaining,
		"reset_at":        tomorrow.Format(time.RFC3339),
	}
	// NEVER include api_key in response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleGetLLMUsage(w http.ResponseWriter, r *http.Request) {
	daysStr := r.URL.Query().Get("days")
	days := 7 // default
	if daysStr != "" {
		var err error
		days, err = strconv.Atoi(daysStr)
		if err != nil || days < 1 || days > 90 {
			http.Error(w, `{"error":"days must be between 1 and 90"}`, http.StatusBadRequest)
			return
		}
	}

	entries, err := s.db.GetTokenUsageHistory(days)
	if err != nil {
		log.Printf("llm usage: get history: %v", err)
		http.Error(w, `{"error":"failed to retrieve usage history"}`, http.StatusInternalServerError)
		return
	}

	totalTokens := 0
	usage := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		total := e.InputTokens + e.OutputTokens
		totalTokens += total
		usage = append(usage, map[string]any{
			"date":          e.Date,
			"subsystem":     e.Subsystem,
			"input_tokens":  e.InputTokens,
			"output_tokens": e.OutputTokens,
			"requests":      e.Requests,
		})
	}

	resp := map[string]any{
		"usage":        usage,
		"total_tokens": totalTokens,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleTestLLM(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.LLM
	if cfg.Provider == "" || cfg.Model == "" {
		http.Error(w, `{"error":"llm not configured: provider and model required"}`, http.StatusBadRequest)
		return
	}

	llmCfg := llm.Config{
		Provider:    cfg.Provider,
		Model:       cfg.Model,
		APIKey:      cfg.APIKey,
		BaseURL:     cfg.BaseURL,
		Timeout:     cfg.Timeout,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
		MaxRetries:  cfg.MaxRetries,
		Concurrency: cfg.Concurrency,
	}
	llmCfg = llmCfg.WithEnv()

	client, err := llm.NewClient(llmCfg)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"create client: %s"}`, err), http.StatusServiceUnavailable)
		return
	}

	start := time.Now()
	_, err = client.Complete(r.Context(), llm.CompletionRequest{
		SystemPrompt: "",
		Messages:     []llm.Message{{Role: "user", Content: "Reply with the single word: ok"}},
		MaxTokens:    16,
	})
	latencyMs := time.Since(start).Milliseconds()

	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"llm endpoint unreachable: %s"}`, err), http.StatusServiceUnavailable)
		return
	}

	resp := map[string]any{
		"ok":         true,
		"latency_ms": latencyMs,
		"model":      cfg.Model,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
