package db

import (
	"database/sql"
	"encoding/json"
	"time"
)

type DailyMetrics struct {
	TotalWorkflows        int
	CompletedWorkflows    int
	AvgWorkflowDurationMs int
	TotalTasks            int
	CompletedTasks        int
	SuccessRate           float64
	MetricsJSON           string
}

func (d *DB) RecordMetric(workflowID, metricType, metricName string, value float64, metadata map[string]any) error {
	var metadataJSON []byte
	if metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(metadata)
		if err != nil {
			return err
		}
	}

	_, err := d.sql.Exec(`
		INSERT INTO workflow_metrics (workflow_id, metric_type, metric_name, metric_value, metadata)
		VALUES (?, ?, ?, ?, ?)
	`, workflowID, metricType, metricName, value, string(metadataJSON))

	return err
}

func (d *DB) GetWorkflowMetrics(workflowID string) (map[string]any, error) {
	rows, err := d.sql.Query(`
		SELECT metric_type, metric_name, metric_value, metadata, recorded_at
		FROM workflow_metrics
		WHERE workflow_id = ?
		ORDER BY recorded_at ASC
	`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	metrics := []map[string]any{}
	for rows.Next() {
		var mType, mName, recordedAt string
		var value float64
		var metadataJSON []byte
		if err := rows.Scan(&mType, &mName, &value, &metadataJSON, &recordedAt); err != nil {
			return nil, err
		}

		var metadata map[string]any
		if len(metadataJSON) > 0 {
			json.Unmarshal(metadataJSON, &metadata)
		}

		metrics = append(metrics, map[string]any{
			"type":        mType,
			"name":        mName,
			"value":       value,
			"metadata":    metadata,
			"recorded_at": recordedAt,
		})
	}

	return map[string]any{
		"workflow_id": workflowID,
		"metrics":     metrics,
	}, nil
}

func (d *DB) GetMetricsSummary(days int) (map[string]any, error) {
	query := `
		SELECT 
			COALESCE(SUM(total_workflows), 0) as total_workflows,
			COALESCE(SUM(completed_workflows), 0) as completed_workflows,
			COALESCE(AVG(avg_workflow_duration_ms), 0) as avg_workflow_duration_ms,
			COALESCE(SUM(total_tasks), 0) as total_tasks,
			COALESCE(SUM(completed_tasks), 0) as completed_tasks
		FROM daily_metrics
		WHERE date(metric_date) >= date('now', '-' || ? || ' days')
	`

	var totalWorkflows, completedWorkflows, totalTasks, completedTasks int
	var avgDuration float64

	err := d.sql.QueryRow(query, days).Scan(
		&totalWorkflows,
		&completedWorkflows,
		&avgDuration,
		&totalTasks,
		&completedTasks,
	)

	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	todayMetrics, err := d.GetTodayMetrics()
	if err != nil {
		return nil, err
	}

	totalWorkflows += todayMetrics["total_workflows"].(int)
	completedWorkflows += todayMetrics["completed_workflows"].(int)
	totalTasks += todayMetrics["total_tasks"].(int)
	completedTasks += todayMetrics["completed_tasks"].(int)

	if totalTasks > 0 {
		avgDuration = (float64(avgDuration)*float64(totalTasks-todayMetrics["total_tasks"].(int)) + float64(todayMetrics["avg_workflow_duration_ms"].(int))*float64(todayMetrics["total_tasks"].(int))) / float64(totalTasks)
	}

	var successRate float64
	if totalTasks > 0 {
		successRate = float64(completedTasks) / float64(totalTasks)
	}

	return map[string]any{
		"total_workflows":          totalWorkflows,
		"completed_workflows":      completedWorkflows,
		"avg_workflow_duration_ms": int(avgDuration),
		"total_tasks":              totalTasks,
		"completed_tasks":          completedTasks,
		"success_rate":             successRate,
	}, nil
}

func (d *DB) GetRecentDailyMetrics(limit int) ([]map[string]any, error) {
	rows, err := d.sql.Query(`
		SELECT metric_date, total_workflows, completed_workflows,
		       avg_workflow_duration_ms, total_tasks, completed_tasks, success_rate
		FROM daily_metrics
		ORDER BY metric_date DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var date string
		var dm DailyMetrics
		if err := rows.Scan(&date, &dm.TotalWorkflows, &dm.CompletedWorkflows,
			&dm.AvgWorkflowDurationMs, &dm.TotalTasks, &dm.CompletedTasks, &dm.SuccessRate); err != nil {
			return nil, err
		}

		results = append(results, map[string]any{
			"date":                     date,
			"total_workflows":          dm.TotalWorkflows,
			"completed_workflows":      dm.CompletedWorkflows,
			"avg_workflow_duration_ms": dm.AvgWorkflowDurationMs,
			"total_tasks":              dm.TotalTasks,
			"completed_tasks":          dm.CompletedTasks,
			"success_rate":             dm.SuccessRate,
		})
	}

	todayMetrics, err := d.GetTodayMetrics()
	if err == nil {
		foundToday := false
		for _, result := range results {
			if result["date"].(string) == todayMetrics["date"].(string) {
				foundToday = true
				break
			}
		}
		if !foundToday {
			results = append([]map[string]any{todayMetrics}, results...)
			if len(results) > limit {
				results = results[:limit]
			}
		}
	}

	return results, nil
}

func (d *DB) GetAgentMetrics(days int) ([]map[string]any, error) {
	query := `
		SELECT 
			metadata->>'$.agent_id' as agent_id,
			COUNT(*) as tasks_completed,
			AVG(CAST(metadata->>'$.duration_ms' AS REAL)) as avg_task_duration_ms,
			SUM(CASE WHEN metric_value = 1 THEN 1 ELSE 0 END) * 1.0 / COUNT(*) as success_rate,
			MAX(recorded_at) as last_active
		FROM workflow_metrics
		WHERE metric_name = 'task_completed'
		  AND metadata != ''
		  AND metadata != 'null'
		  AND metadata->>'$.agent_id' IS NOT NULL
		  AND metadata->>'$.agent_id' != ''
		  AND date(recorded_at) >= date('now', '-' || ? || ' days')
		GROUP BY metadata->>'$.agent_id'
		ORDER BY tasks_completed DESC
	`

	rows, err := d.sql.Query(query, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var agentID string
		var tasksCompleted int
		var avgDuration float64
		var successRate float64
		var lastActive string

		if err := rows.Scan(&agentID, &tasksCompleted, &avgDuration, &successRate, &lastActive); err != nil {
			return nil, err
		}

		results = append(results, map[string]any{
			"agent_id":             agentID,
			"tasks_completed":      tasksCompleted,
			"avg_task_duration_ms": int(avgDuration),
			"success_rate":         successRate,
			"last_active":          lastActive,
		})
	}

	return results, nil
}

func (d *DB) GetTodayMetrics() (map[string]any, error) {
	today := time.Now().Format("2006-01-02")

	var totalWorkflows int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM workflows WHERE DATE(created_at) = ?`, today).Scan(&totalWorkflows)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	var completedWorkflows int
	err = d.sql.QueryRow(`SELECT COUNT(*) FROM workflows WHERE DATE(updated_at) = ? AND phase = 'complete'`, today).Scan(&completedWorkflows)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	var avgDuration float64
	err = d.sql.QueryRow(`
		SELECT COALESCE(AVG(metric_value), 0)
		FROM workflow_metrics
		WHERE metric_name = 'phase_duration' AND DATE(recorded_at) = ?
	`, today).Scan(&avgDuration)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	var totalTasks int
	err = d.sql.QueryRow(`
		SELECT COUNT(*)
		FROM workflow_metrics
		WHERE metric_name = 'task_completed' AND DATE(recorded_at) = ?
	`, today).Scan(&totalTasks)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	var completedTasks int
	err = d.sql.QueryRow(`
		SELECT COALESCE(SUM(metric_value), 0)
		FROM workflow_metrics
		WHERE metric_name = 'task_completed' AND DATE(recorded_at) = ?
	`, today).Scan(&completedTasks)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	var successRate float64
	if totalTasks > 0 {
		successRate = float64(completedTasks) / float64(totalTasks)
	}

	return map[string]any{
		"date":                     today,
		"total_workflows":          totalWorkflows,
		"completed_workflows":      completedWorkflows,
		"avg_workflow_duration_ms": int(avgDuration),
		"total_tasks":              totalTasks,
		"completed_tasks":          completedTasks,
		"success_rate":             successRate,
	}, nil
}
