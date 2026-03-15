package orchestration

import (
	"log"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type MetricsCollector struct {
	db *db.DB
}

func NewMetricsCollector(db *db.DB) *MetricsCollector {
	return &MetricsCollector{db: db}
}

func (m *MetricsCollector) RecordWorkflowStart(workflowID string, workflowType string) error {
	err := m.db.RecordMetric(workflowID, "workflow", "started", 1, map[string]any{
		"type": workflowType,
	})
	if err != nil {
		log.Printf("warning: failed to record workflow start metric: %v", err)
	}
	return err
}

func (m *MetricsCollector) RecordPhaseTransition(workflowID string, fromPhase string, toPhase string, duration time.Duration) error {
	err := m.db.RecordMetric(workflowID, "workflow", "phase_duration", float64(duration.Milliseconds()), map[string]any{
		"from_phase": fromPhase,
		"to_phase":   toPhase,
	})
	if err != nil {
		log.Printf("warning: failed to record phase transition metric: %v", err)
	}
	return err
}

func (m *MetricsCollector) RecordTaskComplete(workflowID string, taskIndex int, agentID string, duration time.Duration, success bool) error {
	value := 1.0
	if !success {
		value = 0.0
	}
	err := m.db.RecordMetric(workflowID, "agent", "task_completed", value, map[string]any{
		"task_index":  taskIndex,
		"agent_id":    agentID,
		"duration_ms": duration.Milliseconds(),
		"success":     success,
	})
	if err != nil {
		log.Printf("warning: failed to record task completion metric: %v", err)
	}
	return err
}

func (m *MetricsCollector) RecordWorkflowComplete(workflowID string, totalDuration time.Duration, success bool) error {
	value := 1.0
	if !success {
		value = 0.0
	}
	err := m.db.RecordMetric(workflowID, "workflow", "completed", value, map[string]any{
		"total_duration_ms": totalDuration.Milliseconds(),
		"success":           success,
	})
	if err != nil {
		log.Printf("warning: failed to record workflow completion metric: %v", err)
	}
	return err
}

func (m *MetricsCollector) RecordDelegation(workflowID string, agentID string, phase Phase) error {
	err := m.db.RecordMetric(workflowID, "agent", "delegated", 1, map[string]any{
		"agent_id": agentID,
		"phase":    string(phase),
	})
	if err != nil {
		log.Printf("warning: failed to record delegation metric: %v", err)
	}
	return err
}
