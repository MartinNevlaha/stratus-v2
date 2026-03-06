package api

import (
	"log"
	"net/http"
	"strconv"
)

func (s *Server) handleGetOpenClawStatus(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.db.GetOpenClawMetrics()
	if err != nil {
		log.Printf("warning: failed to get openclaw metrics: %v", err)
	}

	state, err := s.db.GetOpenClawState()
	if err != nil {
		log.Printf("warning: failed to get openclaw state: %v", err)
	}

	patterns, err := s.db.ListOpenClawPatterns("", 0.5, 10)
	if err != nil {
		log.Printf("warning: failed to get patterns: %v", err)
	}

	analyses, err := s.db.ListOpenClawAnalyses("full", 5)
	if err != nil {
		log.Printf("warning: failed to get analyses: %v", err)
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
			log.Printf("OpenClaw analysis failed: %v", err)
		}
	}()

	json200(w, map[string]any{
		"status":  "analysis_triggered",
		"message": "OpenClaw analysis started in background",
	})
}

func (s *Server) handleGetOpenClawPatterns(w http.ResponseWriter, r *http.Request) {
	patternType := r.URL.Query().Get("type")
	minConfidence := 0.5
	if mc := r.URL.Query().Get("min_confidence"); mc != "" {
		if parsed, err := strconv.ParseFloat(mc, 64); err == nil {
			minConfidence = parsed
		}
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	patterns, err := s.db.ListOpenClawPatterns(patternType, minConfidence, limit)
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
			limit = parsed
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
