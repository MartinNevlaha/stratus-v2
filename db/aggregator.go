package db

import (
	"log"
	"time"
)

func (d *DB) AggregateDailyMetrics(date string) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var totalWorkflows int
	err = tx.QueryRow(`SELECT COUNT(*) FROM workflows WHERE DATE(created_at) = ?`, date).Scan(&totalWorkflows)
	if err != nil {
		return err
	}

	var completedWorkflows int
	err = tx.QueryRow(`SELECT COUNT(*) FROM workflows WHERE DATE(updated_at) = ? AND phase = 'complete'`, date).Scan(&completedWorkflows)
	if err != nil {
		return err
	}

	var avgDuration float64
	err = tx.QueryRow(`
		SELECT COALESCE(AVG(metric_value), 0)
		FROM workflow_metrics
		WHERE metric_name = 'phase_duration' 
		  AND DATE(recorded_at) = ?
	`, date).Scan(&avgDuration)
	if err != nil {
		return err
	}

	var totalTasks, completedTasks int
	err = tx.QueryRow(`
		SELECT COUNT(*)
		FROM workflow_metrics
		WHERE metric_name = 'task_completed' AND DATE(recorded_at) = ?
	`, date).Scan(&totalTasks)
	if err != nil {
		return err
	}

	err = tx.QueryRow(`
		SELECT COALESCE(SUM(metric_value), 0)
		FROM workflow_metrics
		WHERE metric_name = 'task_completed' AND DATE(recorded_at) = ?
	`, date).Scan(&completedTasks)
	if err != nil {
		return err
	}

	var successRate float64
	if totalTasks > 0 {
		successRate = float64(completedTasks) / float64(totalTasks)
	}

	_, err = tx.Exec(`
		INSERT INTO daily_metrics 
		(metric_date, total_workflows, completed_workflows, avg_workflow_duration_ms, 
		 total_tasks, completed_tasks, success_rate)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(metric_date) DO UPDATE SET
			total_workflows = excluded.total_workflows,
			completed_workflows = excluded.completed_workflows,
			avg_workflow_duration_ms = excluded.avg_workflow_duration_ms,
			total_tasks = excluded.total_tasks,
			completed_tasks = excluded.completed_tasks,
			success_rate = excluded.success_rate,
			computed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
	`, date, totalWorkflows, completedWorkflows, int(avgDuration), totalTasks, completedTasks, successRate)

	if err != nil {
		return err
	}

	return tx.Commit()
}

func (d *DB) AggregateYesterday() error {
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	return d.AggregateDailyMetrics(yesterday)
}

func (d *DB) AggregateAllMissing() error {
	rows, err := d.sql.Query(`
		SELECT DISTINCT DATE(recorded_at) as metric_date
		FROM workflow_metrics
		WHERE DATE(recorded_at) NOT IN (SELECT metric_date FROM daily_metrics)
		ORDER BY metric_date ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			return err
		}
		dates = append(dates, date)
	}

	for _, date := range dates {
		if err := d.AggregateDailyMetrics(date); err != nil {
			log.Printf("warning: aggregation failed for %s: %v", date, err)
		}
	}

	return nil
}

func (d *DB) AggregateLastNDays(n int) error {
	for i := 1; i <= n; i++ {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		if err := d.AggregateDailyMetrics(date); err != nil {
			log.Printf("warning: aggregation failed for %s: %v", date, err)
		}
	}
	return nil
}
