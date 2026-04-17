package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/onboarding"
	wiki_engine "github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
	"github.com/google/uuid"
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
	validDepths := map[string]bool{"shallow": true, "standard": true, "deep": true, "auto": true}
	if !validDepths[req.Depth] {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": fmt.Sprintf("invalid depth %q, must be shallow|standard|deep|auto", req.Depth),
		})
		return
	}

	// Validate max_pages: must be 0 (use default) or in [1, 500]. Large projects
	// can legitimately need 50-100+ wiki pages, so the cap is only a sanity guard
	// against runaway LLM spend from typos.
	if req.MaxPages < 0 || req.MaxPages > 500 {
		http.Error(w, `{"error":"max_pages must be between 0 and 500"}`, http.StatusBadRequest)
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

		// Resolve auto depth after scanning.
		if depth == "auto" {
			resolved := onboarding.ResolveAutoDepth(profile)
			depth = resolved.Depth
			if maxPages <= 0 {
				maxPages = resolved.MaxPages
			}
			slog.Info("onboarding: auto depth resolved",
				"depth", resolved.Depth,
				"max_pages", resolved.MaxPages,
				"reason", resolved.Reason)
		}

		s.onboardingMu.Lock()
		s.onboardingProgress.Status = "generating"
		s.onboardingMu.Unlock()

		// Build LLM config from server config, resolving API key from env if not set.
		llmCfg := llm.Config{
			Provider:             s.cfg.LLM.Provider,
			Model:                s.cfg.LLM.Model,
			APIKey:               s.cfg.LLM.APIKey,
			BaseURL:              s.cfg.LLM.BaseURL,
			Timeout:              s.cfg.LLM.Timeout,
			MaxTokens:            s.cfg.LLM.MaxTokens,
			Temperature:          s.cfg.LLM.Temperature,
			MaxRetries:           s.cfg.LLM.MaxRetries,
			Concurrency:          s.cfg.LLM.Concurrency,
			MinRequestIntervalMs: s.cfg.LLM.MinRequestIntervalMs,
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

		// Resolve effective max-pages respecting the 0=unlimited sentinel.
		// Priority: request param > config > hard default of 20.
		// If either the request param or config is explicitly 0 (unlimited),
		// we preserve 0 so RunOnboarding receives the unlimited sentinel.
		effective := maxPages
		if effective < 0 {
			// Negative from request — treat as "use config".
			effective = s.cfg.Wiki.OnboardingMaxPages
		}
		if effective < 0 {
			// Negative config (invalid, but guard against it) — fall back to default.
			effective = 20
		}
		// If effective is still 0 at this point it means unlimited was explicitly
		// requested (either via request param or config). RunOnboarding handles
		// MaxPages==0 by falling back to 10; we need to preserve the unlimited
		// signal by passing it through as-is (0 → default 10 in orchestrator is fine
		// unless config says unlimited). For a clean unlimited pass-through when
		// OnboardingMaxPages=0, we keep effective=0 (handled inside RunOnboarding).

		result, err := onboarding.RunOnboarding(
			context.Background(),
			wikiStore,
			llmClient,
			linker,
			s.vaultSync,
			profile,
			onboarding.OnboardingOpts{
				Depth:             depth,
				MaxPages:          effective,
				IngestTokenBudget: s.cfg.Wiki.IngestTokenBudget,
				OutputDir:         outputDir,
				ProgressFn: func(p onboarding.OnboardingProgress) {
					s.onboardingMu.Lock()
					p.JobID = jobID
					s.onboardingProgress = &p
					s.onboardingMu.Unlock()
					if s.hub != nil {
						s.hub.BroadcastJSON("onboarding_progress", p)
					}
				},
				SaveAssetProposals: func(proposals []onboarding.AssetProposal) error {
					existingPaths, _ := s.db.ListAssetProposalPaths()
					existingMap := make(map[string]bool, len(existingPaths))
					for _, p := range existingPaths {
						existingMap[p] = true
					}
					saved := 0
					for _, ap := range proposals {
						if existingMap[ap.ProposedPath] {
							continue
						}
						ip := assetToInsightProposal(ap)
						if saveErr := s.db.SaveInsightProposal(ip); saveErr != nil {
							slog.Warn("onboarding: save asset proposal", "title", ap.Title, "err", saveErr)
							continue
						}
						saved++
					}
					slog.Info("onboarding: asset proposals saved", "count", saved)
					return nil
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

// handleProposeAssets scans the project, generates asset proposals, persists them
// to the DB (skipping duplicates), and returns the newly created proposals.
//
// POST /api/onboarding/propose-assets
func (s *Server) handleProposeAssets(w http.ResponseWriter, r *http.Request) {
	// 1. Collect already-known proposal paths from the DB for deduplication.
	existingPaths, err := s.db.ListAssetProposalPaths()
	if err != nil {
		slog.Error("propose-assets: list existing paths", "error", err)
		http.Error(w, `{"error":"failed to query existing proposals"}`, http.StatusInternalServerError)
		return
	}
	existingMap := make(map[string]bool, len(existingPaths))
	for _, p := range existingPaths {
		existingMap[p] = true
	}

	// 2. Scan the project to build a profile.
	profile, err := onboarding.ScanProject(s.projectRoot, "standard")
	if err != nil {
		slog.Error("propose-assets: scan project", "error", err)
		http.Error(w, `{"error":"failed to scan project"}`, http.StatusInternalServerError)
		return
	}

	// 3. Generate proposals (dedup against disk + DB is done inside GenerateAssetProposals).
	rawProposals := onboarding.GenerateAssetProposals(profile, s.projectRoot, existingMap)

	// 4. Persist each proposal and build the response payload.
	type proposalSummary struct {
		ID         string  `json:"id"`
		Type       string  `json:"type"`
		Title      string  `json:"title"`
		Path       string  `json:"path"`
		Target     string  `json:"target"`
		Confidence float64 `json:"confidence"`
	}

	saved := make([]proposalSummary, 0, len(rawProposals))
	skipped := 0
	for _, ap := range rawProposals {
		p := assetToInsightProposal(ap)
		if err := s.db.SaveInsightProposal(p); err != nil {
			slog.Error("propose-assets: save proposal", "path", ap.ProposedPath, "error", err)
			skipped++
			continue
		}
		saved = append(saved, proposalSummary{
			ID:         p.ID,
			Type:       p.Type,
			Title:      p.Title,
			Path:       ap.ProposedPath,
			Target:     ap.Target,
			Confidence: p.Confidence,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"proposals": saved,
		"count":     len(saved),
		"skipped":   skipped,
	})
}

// assetToInsightProposal converts an onboarding.AssetProposal to a db.InsightProposal
// suitable for persistence.
func assetToInsightProposal(ap onboarding.AssetProposal) *db.InsightProposal {
	return &db.InsightProposal{
		ID:              uuid.NewString(),
		Type:            ap.Type,
		Status:          "drafted",
		Title:           ap.Title,
		Description:     ap.Description,
		Confidence:      ap.Confidence,
		RiskLevel:       "low",
		SourcePatternID: "onboarding",
		Evidence: map[string]any{
			"signals": ap.Signals,
			"target":  ap.Target,
		},
		Recommendation: map[string]any{
			"proposed_path":    ap.ProposedPath,
			"proposed_content": ap.ProposedContent,
			"target":           ap.Target,
		},
	}
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
