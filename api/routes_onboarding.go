package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/onboarding"
	wiki_engine "github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
)

type onboardRequest struct {
	Depth     string `json:"depth"`
	OutputDir string `json:"output_dir"`
	MaxPages  int    `json:"max_pages"`
}

// handleOnboard triggers an asynchronous project onboarding that generates wiki pages.
func (s *Server) handleOnboard(w http.ResponseWriter, r *http.Request) {
	var req onboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Validate depth — default to standard if not specified.
	if req.Depth == "" {
		req.Depth = "standard"
	}
	validDepths := map[string]bool{"shallow": true, "standard": true, "deep": true}
	if !validDepths[req.Depth] {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": fmt.Sprintf("invalid depth %q, must be shallow|standard|deep", req.Depth),
		})
		return
	}

	// Validate max_pages: must be 0 (use default) or in [1, 50].
	if req.MaxPages < 0 || req.MaxPages > 50 {
		http.Error(w, `{"error":"max_pages must be between 0 and 50"}`, http.StatusBadRequest)
		return
	}

	// Validate output_dir — reject path traversal attempts.
	if req.OutputDir != "" {
		abs, err := filepath.Abs(req.OutputDir)
		if err != nil {
			http.Error(w, `{"error":"invalid output_dir"}`, http.StatusBadRequest)
			return
		}
		projectRoot, _ := filepath.Abs(s.projectRoot)
		if !strings.HasPrefix(abs, projectRoot+string(filepath.Separator)) && abs != projectRoot {
			http.Error(w, `{"error":"output_dir must not escape project root"}`, http.StatusBadRequest)
			return
		}
	}

	// Serialize access to onboarding state.
	s.onboardingMu.Lock()

	// Reject if an onboarding is already running.
	if s.onboardingProgress != nil &&
		s.onboardingProgress.Status != "complete" &&
		s.onboardingProgress.Status != "failed" &&
		s.onboardingProgress.Status != "idle" {
		s.onboardingMu.Unlock()
		http.Error(w, `{"error":"onboarding already in progress"}`, http.StatusConflict)
		return
	}

	// Require LLM to be configured.
	if s.cfg.LLM.Provider == "" {
		s.onboardingMu.Unlock()
		http.Error(w, `{"error":"LLM not configured. Set llm.provider in .stratus.json"}`, http.StatusServiceUnavailable)
		return
	}

	jobID := fmt.Sprintf("onboard-%d", time.Now().Unix())
	progress := &onboarding.OnboardingProgress{
		JobID:  jobID,
		Status: "scanning",
	}
	s.onboardingProgress = progress
	s.onboardingResult = nil
	s.onboardingMu.Unlock()

	// Capture values needed by the goroutine before they escape the request scope.
	depth := req.Depth
	maxPages := req.MaxPages
	outputDir := req.OutputDir
	projectRoot := s.projectRoot

	go func() {
		profile, err := onboarding.ScanProject(projectRoot, depth)
		if err != nil {
			s.onboardingMu.Lock()
			s.onboardingProgress.Status = "failed"
			s.onboardingProgress.Errors = append(s.onboardingProgress.Errors, fmt.Sprintf("scan project: %v", err))
			s.onboardingMu.Unlock()
			return
		}

		s.onboardingMu.Lock()
		s.onboardingProgress.Status = "generating"
		s.onboardingMu.Unlock()

		// Build LLM config from server config, resolving API key from env if not set.
		llmCfg := llm.Config{
			Provider:    s.cfg.LLM.Provider,
			Model:       s.cfg.LLM.Model,
			APIKey:      s.cfg.LLM.APIKey,
			BaseURL:     s.cfg.LLM.BaseURL,
			Timeout:     s.cfg.LLM.Timeout,
			MaxTokens:   s.cfg.LLM.MaxTokens,
			Temperature: s.cfg.LLM.Temperature,
			MaxRetries:  s.cfg.LLM.MaxRetries,
		}.WithEnv()

		bareClient, err := llm.NewClient(llmCfg)
		if err != nil {
			s.onboardingMu.Lock()
			s.onboardingProgress.Status = "failed"
			s.onboardingProgress.Errors = append(s.onboardingProgress.Errors, fmt.Sprintf("create LLM client: %v", err))
			s.onboardingMu.Unlock()
			return
		}
		budgetedClient := llm.NewBudgetedClient(bareClient, s.db, s.cfg.Evolution.DailyTokenBudget)
		llmClient := llm.NewSubsystemClient(budgetedClient, "onboarding", llm.PriorityMedium)

		wikiStore := wiki_engine.NewDBWikiStore(s.db)
		linker := wiki_engine.NewLinker(wikiStore)

		effective := maxPages
		if effective <= 0 {
			effective = s.cfg.Wiki.OnboardingMaxPages
		}
		if effective <= 0 {
			effective = 20
		}

		result, err := onboarding.RunOnboarding(
			context.Background(),
			wikiStore,
			llmClient,
			linker,
			s.vaultSync,
			profile,
			onboarding.OnboardingOpts{
				Depth:     depth,
				MaxPages:  effective,
				OutputDir: outputDir,
				ProgressFn: func(p onboarding.OnboardingProgress) {
					s.onboardingMu.Lock()
					p.JobID = jobID
					s.onboardingProgress = &p
					s.onboardingMu.Unlock()
					if s.hub != nil {
						s.hub.BroadcastJSON("onboarding_progress", p)
					}
				},
			},
		)

		s.onboardingMu.Lock()
		if err != nil {
			s.onboardingProgress.Status = "failed"
			s.onboardingProgress.Errors = append(s.onboardingProgress.Errors, err.Error())
		} else {
			s.onboardingProgress.Status = "complete"
		}
		s.onboardingResult = result
		s.onboardingMu.Unlock()
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"job_id":  jobID,
		"status":  "scanning",
		"message": "Onboarding started",
	})
}

// handleOnboardStatus returns the current onboarding progress and result.
func (s *Server) handleOnboardStatus(w http.ResponseWriter, r *http.Request) {
	s.onboardingMu.Lock()
	progress := s.onboardingProgress
	result := s.onboardingResult
	s.onboardingMu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if progress == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job_id":       "",
			"status":       "idle",
			"current_page": "",
			"generated":    0,
			"total":        0,
			"errors":       []string{},
			"result":       nil,
		})
		return
	}

	errors := progress.Errors
	if errors == nil {
		errors = []string{}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"job_id":       progress.JobID,
		"status":       progress.Status,
		"current_page": progress.CurrentPage,
		"generated":    progress.Generated,
		"total":        progress.Total,
		"errors":       errors,
		"result":       result,
	})
}
