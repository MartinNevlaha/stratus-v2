package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/google/uuid"
)

// allowedEvolutionCategories is the set of valid hypothesis categories.
var allowedEvolutionCategories = map[string]struct{}{
	"prompt_tuning":        {},
	"workflow_routing":     {},
	"agent_selection":      {},
	"threshold_adjustment": {},
}

// GET /api/evolution/runs
func (s *Server) handleListEvolutionRuns(w http.ResponseWriter, r *http.Request) {
	filters := db.EvolutionRunFilters{
		Status: queryStr(r, "status"),
		Limit:  queryInt(r, "limit", 20),
		Offset: queryInt(r, "offset", 0),
	}

	runs, count, err := s.db.ListEvolutionRuns(filters)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("list evolution runs: %v", err))
		return
	}
	if runs == nil {
		runs = []db.EvolutionRun{}
	}

	json200(w, map[string]any{
		"runs":  runs,
		"count": count,
	})
}

// GET /api/evolution/runs/{id}
func (s *Server) handleGetEvolutionRun(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing run id")
		return
	}

	run, err := s.db.GetEvolutionRun(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("get evolution run: %v", err))
		return
	}
	if run == nil {
		jsonErr(w, http.StatusNotFound, "evolution run not found")
		return
	}

	hypotheses, err := s.db.ListEvolutionHypotheses(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("list evolution hypotheses: %v", err))
		return
	}
	if hypotheses == nil {
		hypotheses = []db.EvolutionHypothesis{}
	}

	json200(w, map[string]any{
		"run":        run,
		"hypotheses": hypotheses,
	})
}

// triggerRequest is the request body for POST /api/evolution/trigger.
type triggerRequest struct {
	TimeoutMs  int64    `json:"timeout_ms"`
	Categories []string `json:"categories"`
}

// POST /api/evolution/trigger
func (s *Server) handleTriggerEvolution(w http.ResponseWriter, r *http.Request) {
	if s.insight == nil {
		jsonErr(w, http.StatusServiceUnavailable, "evolution engine not available")
		return
	}

	var req triggerRequest
	if err := decodeBody(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	// Cap timeout. When timeout_ms is omitted (0) or exceeds the max, default to
	// maxTimeoutMs. The evolution loop will further override with config.TimeoutMs
	// if the value passed is 0, so this cap only prevents abuse.
	const maxTimeoutMs = 600000
	if req.TimeoutMs < 0 || req.TimeoutMs > maxTimeoutMs {
		req.TimeoutMs = maxTimeoutMs
	}

	// Validate categories.
	for _, cat := range req.Categories {
		if _, ok := allowedEvolutionCategories[cat]; !ok {
			jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid category %q: allowed values are prompt_tuning, workflow_routing, agent_selection, threshold_adjustment", cat))
			return
		}
	}

	// reqID is a request-scoped correlation ID. The actual run ID is assigned by
	// the evolution loop when it persists its run record.
	reqID := uuid.NewString()

	timeoutMs := req.TimeoutMs
	categories := req.Categories

	go func() {
		ctx := context.Background()
		result, err := s.insight.RunEvolutionCycle(ctx, "manual", timeoutMs, categories)
		if err != nil {
			slog.Error("evolution cycle failed", "req_id", reqID, "error", err)
			return
		}
		if result != nil {
			slog.Info("evolution cycle completed",
				"req_id", reqID,
				"run_id", result.RunID,
				"hypotheses_tested", result.HypothesesTested,
			)
		}
	}()

	json200(w, map[string]any{
		"status":  "evolution_triggered",
		"run_id":  reqID,
		"message": fmt.Sprintf("evolution cycle started in background (timeout: %dms)", req.TimeoutMs),
	})
}

// GET /api/evolution/config
func (s *Server) handleGetEvolutionConfig(w http.ResponseWriter, r *http.Request) {
	json200(w, s.cfg.Evolution)
}

// POST /api/evolution/config
func (s *Server) handleUpdateEvolutionConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.EvolutionConfig
	if err := decodeBody(r, &cfg); err != nil {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if cfg.AutoApplyThreshold < 0 || cfg.AutoApplyThreshold > 1 {
		jsonErr(w, http.StatusBadRequest, "auto_apply_threshold must be between 0 and 1")
		return
	}
	if cfg.ProposalThreshold < 0 || cfg.ProposalThreshold > 1 {
		jsonErr(w, http.StatusBadRequest, "proposal_threshold must be between 0 and 1")
		return
	}
	if cfg.TimeoutMs < 1000 || cfg.TimeoutMs > 600000 {
		jsonErr(w, http.StatusBadRequest, "timeout_ms must be between 1000 and 600000")
		return
	}
	if cfg.MinSampleSize < 1 {
		jsonErr(w, http.StatusBadRequest, "min_sample_size must be at least 1")
		return
	}
	if cfg.MaxHypothesesPerRun < 1 || cfg.MaxHypothesesPerRun > 100 {
		jsonErr(w, http.StatusBadRequest, "max_hypotheses_per_run must be between 1 and 100")
		return
	}
	if cfg.DailyTokenBudget < 0 {
		jsonErr(w, http.StatusBadRequest, "daily_token_budget must be non-negative")
		return
	}
	for _, c := range cfg.Categories {
		if _, ok := allowedEvolutionCategories[c]; !ok {
			jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid category: %s", c))
			return
		}
	}

	s.cfg.Evolution = cfg
	json200(w, s.cfg.Evolution)
}
