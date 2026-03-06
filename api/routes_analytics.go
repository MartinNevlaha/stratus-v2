package api

import (
	"net/http"
	"strconv"
)

func (s *Server) handleGetMetricsSummary(w http.ResponseWriter, r *http.Request) {
	days := queryInt(r, "days", 7)
	if days < 1 {
		days = 1
	}

	if days > 365 {
		days = 365
	}

	summary, err := s.db.GetMetricsSummary(days)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"period_days": days,
		"summary":     summary,
	})
}

func (s *Server) handleGetWorkflowMetrics(w http.ResponseWriter, r *http.Request) {
	workflowID := pathParam(r, "id")
	if workflowID == "" {
		jsonErr(w, http.StatusBadRequest, "workflow id required")
		return
	}

	metrics, err := s.db.GetWorkflowMetrics(workflowID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	json200(w, map[string]any{
		"workflow_id": workflowID,
		"metrics":     metrics,
	})
}

func (s *Server) handleTriggerAggregation(w http.ResponseWriter, r *http.Request) {
	if err := s.db.AggregateYesterday(); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.db.AggregateAllMissing(); err != nil {
		log.Printf("warning: failed to aggregate missing metrics: %v", err)
	}

	json200(w, map[string]any{
		"status": "aggregation_complete",
	})
}
