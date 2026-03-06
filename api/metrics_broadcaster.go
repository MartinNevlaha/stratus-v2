package api

import (
	"context"
	"log"
	"time"

	"github.com/MartinNevlaha/stratus-v2/analytics"
	"github.com/MartinNevlaha/stratus-v2/db"
)

type MetricsBroadcaster struct {
	database         *db.DB
	hub              *Hub
	interval         time.Duration
	stopCh           chan struct{}
	lastAnomalyCount int
}

func NewMetricsBroadcaster(database *db.DB, hub *Hub, intervalSec int) *MetricsBroadcaster {
	if intervalSec <= 0 {
		intervalSec = 30
	}
	return &MetricsBroadcaster{
		database: database,
		hub:      hub,
		interval: time.Duration(intervalSec) * time.Second,
		stopCh:   make(chan struct{}),
	}
}

func (b *MetricsBroadcaster) Start() {
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	b.broadcastMetrics()

	for {
		select {
		case <-ticker.C:
			b.broadcastMetrics()
			b.checkAnomalies()
		case <-b.stopCh:
			return
		}
	}
}

func (b *MetricsBroadcaster) Stop() {
	close(b.stopCh)
}

func (b *MetricsBroadcaster) broadcastMetrics() {
	summary, err := b.database.GetMetricsSummary(7)
	if err != nil {
		log.Printf("metrics broadcaster: failed to get summary: %v", err)
		return
	}

	dailyMetrics, err := b.database.GetRecentDailyMetrics(7)
	if err != nil {
		log.Printf("metrics broadcaster: failed to get daily metrics: %v", err)
		return
	}

	agentMetrics, err := b.database.GetAgentMetrics(30)
	if err != nil {
		log.Printf("metrics broadcaster: failed to get agent metrics: %v", err)
		agentMetrics = []map[string]any{}
	}

	b.hub.BroadcastJSON("metrics_update", map[string]any{
		"summary": summary,
		"daily":   dailyMetrics,
		"agents":  agentMetrics,
		"ts":      time.Now().UnixMilli(),
	})
}

func (b *MetricsBroadcaster) checkAnomalies() {
	dailyMetrics, err := b.database.GetRecentDailyMetrics(7)
	if err != nil {
		return
	}

	if len(dailyMetrics) < 3 {
		return
	}

	analyticsMetrics := convertToAnalyticsMetrics(dailyMetrics)

	detector := analytics.NewAnomalyDetector()
	anomalies := detector.DetectAnomalies(analyticsMetrics)

	currentCount := len(anomalies)
	if currentCount > b.lastAnomalyCount && currentCount > 0 {
		newAnomalies := anomalies[b.lastAnomalyCount:]
		for _, anomaly := range newAnomalies {
			b.hub.BroadcastJSON("metrics_anomaly", map[string]any{
				"anomaly":   anomaly,
				"ts":        time.Now().UnixMilli(),
				"alert_msg": b.generateAlertMessage(anomaly),
			})
		}
	}

	b.lastAnomalyCount = currentCount

	if detector.ShouldAlert(anomalies) {
		alertMsg := detector.GenerateAlertMessage(anomalies)
		b.hub.BroadcastJSON("metrics_alert", map[string]any{
			"message":  alertMsg,
			"severity": "high",
			"count":    currentCount,
			"ts":       time.Now().UnixMilli(),
		})
	}
}

func (b *MetricsBroadcaster) generateAlertMessage(anomaly analytics.Anomaly) string {
	switch anomaly.Severity {
	case "critical":
		return "CRITICAL: " + anomaly.Description
	case "high":
		return "WARNING: " + anomaly.Description
	default:
		return "INFO: " + anomaly.Description
	}
}

func (b *MetricsBroadcaster) BroadcastOnce(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(100 * time.Millisecond):
		b.broadcastMetrics()
	}
}
