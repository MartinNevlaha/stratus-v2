package api

import (
	"net/http"
	"strconv"
	"time"
)

type metricsSummary struct {
	TotalWorkflows       int     `json:"total_workflows"`
	CompletedWorkflows   int     `json:"completed_workflows"`
	SuccessRate          float64 `json:"success_rate"`
	AvgWorkflowDurationMs int64  `json:"avg_workflow_duration_ms"`
	TotalTasks           int     `json:"total_tasks"`
	CompletedTasks       int     `json:"completed_tasks"`
}

type dailyMetric struct {
	Date        string `json:"date"`
	Total       int    `json:"total"`
	Completed   int    `json:"completed"`
	Failed      int    `json:"failed"`
	AvgDurationMs int64 `json:"avg_duration_ms"`
}

type agentMetric struct {
	AgentID        string  `json:"agent_id"`
	TotalTasks     int     `json:"total_tasks"`
	CompletedTasks int     `json:"completed_tasks"`
	AvgDurationMs  int64   `json:"avg_duration_ms"`
	SuccessRate    float64 `json:"success_rate"`
}

type projectMetric struct {
	Project            string  `json:"project"`
	TotalWorkflows     int     `json:"total_workflows"`
	CompletedWorkflows int     `json:"completed_workflows"`
	AvgDurationMs      int64   `json:"avg_duration_ms"`
	SuccessRate        float64 `json:"success_rate"`
}

func (s *Server) handleMetricsAggregate(w http.ResponseWriter, r *http.Request) {
	json200(w, map[string]string{"status": "ok"})
}

func (s *Server) handleMetricsSummary(w http.ResponseWriter, r *http.Request) {
	daysStr := r.URL.Query().Get("days")
	days := 30
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
		days = d
	}

	since := time.Now().UTC().AddDate(0, 0, -days)

	workflows, err := s.coordinator.ListAll()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var summary metricsSummary
	var totalDurationMs int64
	var durationCount int

	for _, wf := range workflows {
		created, parseErr := time.Parse(time.RFC3339Nano, wf.CreatedAt)
		if parseErr != nil {
			created, parseErr = time.Parse(time.RFC3339, wf.CreatedAt)
			if parseErr != nil {
				continue
			}
		}
		if created.Before(since) {
			continue
		}

		summary.TotalWorkflows++
		if wf.Phase == "complete" && !wf.Aborted {
			summary.CompletedWorkflows++

			updated, parseErr2 := time.Parse(time.RFC3339Nano, wf.UpdatedAt)
			if parseErr2 != nil {
				updated, parseErr2 = time.Parse(time.RFC3339, wf.UpdatedAt)
			}
			if parseErr2 == nil {
				totalDurationMs += updated.Sub(created).Milliseconds()
				durationCount++
			}
		}

		summary.TotalTasks += wf.TotalTasks
		for _, t := range wf.Tasks {
			if t.Status == "done" {
				summary.CompletedTasks++
			}
		}
	}

	if summary.TotalWorkflows > 0 {
		summary.SuccessRate = float64(summary.CompletedWorkflows) / float64(summary.TotalWorkflows)
	}
	if durationCount > 0 {
		summary.AvgWorkflowDurationMs = totalDurationMs / int64(durationCount)
	}

	json200(w, map[string]any{"summary": summary})
}

func (s *Server) handleMetricsDaily(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 30
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	since := time.Now().UTC().AddDate(0, 0, -limit)

	workflows, err := s.coordinator.ListAll()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	type dayData struct {
		total       int
		completed   int
		failed      int
		totalDurMs  int64
		durCount    int
	}
	byDay := map[string]*dayData{}

	for _, wf := range workflows {
		created, parseErr := time.Parse(time.RFC3339Nano, wf.CreatedAt)
		if parseErr != nil {
			created, parseErr = time.Parse(time.RFC3339, wf.CreatedAt)
			if parseErr != nil {
				continue
			}
		}
		if created.Before(since) {
			continue
		}

		day := created.UTC().Format("2006-01-02")
		if byDay[day] == nil {
			byDay[day] = &dayData{}
		}
		d := byDay[day]
		d.total++

		if wf.Phase == "complete" {
			if wf.Aborted {
				d.failed++
			} else {
				d.completed++
				updated, parseErr2 := time.Parse(time.RFC3339Nano, wf.UpdatedAt)
				if parseErr2 != nil {
					updated, parseErr2 = time.Parse(time.RFC3339, wf.UpdatedAt)
				}
				if parseErr2 == nil {
					d.totalDurMs += updated.Sub(created).Milliseconds()
					d.durCount++
				}
			}
		}
	}

	metrics := make([]dailyMetric, 0, len(byDay))
	for day, d := range byDay {
		m := dailyMetric{
			Date:      day,
			Total:     d.total,
			Completed: d.completed,
			Failed:    d.failed,
		}
		if d.durCount > 0 {
			m.AvgDurationMs = d.totalDurMs / int64(d.durCount)
		}
		metrics = append(metrics, m)
	}

	json200(w, map[string]any{"metrics": metrics})
}

func (s *Server) handleMetricsAgents(w http.ResponseWriter, r *http.Request) {
	// Agent-level metrics are not tracked in the current DB schema.
	// Return empty list to satisfy the frontend contract.
	json200(w, map[string]any{"metrics": []agentMetric{}})
}

func (s *Server) handleMetricsProjects(w http.ResponseWriter, r *http.Request) {
	// Project-level metrics are not tracked in the current DB schema.
	// Return empty list to satisfy the frontend contract.
	json200(w, map[string]any{"metrics": []projectMetric{}})
}

func (s *Server) handleMetricsWorkflows(w http.ResponseWriter, r *http.Request) {
	// Alias to summary for frontend compatibility.
	s.handleMetricsSummary(w, r)
}
