// Package guardian implements the Ambient Codebase Guardian — a background
// service that periodically scans the codebase and surfaces proactive health
// alerts in the Stratus dashboard.
package guardian

import (
	"context"
	"log"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

// hubBroadcaster is the subset of api.Hub used by the Guardian.
type hubBroadcaster interface {
	BroadcastJSON(msgType string, payload any)
}

// Guardian runs periodic codebase health checks.
type Guardian struct {
	db       *db.DB
	coord    *orchestration.Coordinator
	cfg      func() config.GuardianConfig
	hub      hubBroadcaster
	projRoot string
}

// New creates a new Guardian.
// cfgFn is called before each run so config changes are picked up without restart.
func New(d *db.DB, coord *orchestration.Coordinator, cfgFn func() config.GuardianConfig, hub hubBroadcaster, projRoot string) *Guardian {
	return &Guardian{
		db:       d,
		coord:    coord,
		cfg:      cfgFn,
		hub:      hub,
		projRoot: projRoot,
	}
}

// Run starts the guardian ticker loop. It blocks until ctx is cancelled.
func (g *Guardian) Run(ctx context.Context) {
	cfg := g.cfg()
	if !cfg.Enabled {
		log.Println("guardian: disabled, not starting")
		return
	}

	interval := time.Duration(cfg.IntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 15 * time.Minute
	}

	log.Printf("guardian: starting, interval=%v", interval)
	// Run immediately on start, then on ticker.
	g.runChecks(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("guardian: stopping")
			return
		case <-ticker.C:
			// Re-read config each tick so UI changes take effect.
			cfg = g.cfg()
			if !cfg.Enabled {
				log.Println("guardian: disabled mid-run, skipping")
				continue
			}
			// Adjust ticker if interval changed.
			newInterval := time.Duration(cfg.IntervalMinutes) * time.Minute
			if newInterval > 0 && newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
			}
			g.runChecks(ctx)
		}
	}
}

// RunOnce triggers a single scan outside the normal tick schedule.
func (g *Guardian) RunOnce(ctx context.Context) {
	go g.runChecks(ctx)
}

func (g *Guardian) runChecks(ctx context.Context) {
	cfg := g.cfg()
	llm := newLLMClient(cfg.LLMEndpoint, cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMTemperature, cfg.LLMMaxTokens)

	var candidates []alertInput

	// 1. Stale workflows
	candidates = append(candidates, checkStaleWorkflows(g.coord, cfg)...)

	// 1b. Stale swarm workers
	candidates = append(candidates, checkStaleWorkers(g.db, cfg)...)

	// 1c. Reviewer timeout (mission stuck in verifying)
	candidates = append(candidates, checkStaleVerifying(g.db, cfg)...)

	// 1d. Overdue tickets (in_progress too long)
	candidates = append(candidates, checkOverdueTickets(g.db, cfg)...)

	// 2. Memory health
	candidates = append(candidates, checkMemoryHealth(g.db, cfg)...)

	// 3. Tech debt
	candidates = append(candidates, checkTechDebt(g.db, g.projRoot, cfg)...)

	// 4. Coverage drift
	candidates = append(candidates, checkCoverageDrift(g.db, g.projRoot, cfg)...)

	// 5. Governance violations
	candidates = append(candidates, checkGovernanceViolations(ctx, g.db, llm, g.projRoot)...)

	for _, c := range candidates {
		g.maybeEmit(c)
	}
}

// maybeEmit deduplicates and saves an alert, broadcasting it over WebSocket.
func (g *Guardian) maybeEmit(a alertInput) {
	if a.Metadata == nil {
		a.Metadata = map[string]any{}
	}
	dedupKey, _ := a.Metadata["dedup_key"].(string)
	if dedupKey != "" {
		exists, err := g.db.HasRecentAlert(a.Type, dedupKey)
		if err != nil || exists {
			return
		}
	}
	if a.Severity == "" {
		a.Severity = "info"
	}

	id, err := g.db.SaveGuardianAlert(a.Type, a.Severity, a.Message, a.Metadata)
	if err != nil {
		log.Printf("guardian: save alert: %v", err)
		return
	}
	log.Printf("guardian: alert [%s/%s] %s", a.Type, a.Severity, a.Message)
	g.hub.BroadcastJSON("guardian_alert", map[string]any{
		"id":       id,
		"type":     a.Type,
		"severity": a.Severity,
		"message":  a.Message,
		"metadata": a.Metadata,
	})
}
