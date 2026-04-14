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
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

// guardianLLM is the interface used by all guardian checks for LLM operations.
// Satisfied by *llmAdapter, which wraps the shared internal/insight/llm client.
type guardianLLM interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	configured() bool
}

// hubBroadcaster is the subset of api.Hub used by the Guardian.
type hubBroadcaster interface {
	BroadcastJSON(msgType string, payload any)
}

// Guardian runs periodic codebase health checks.
type Guardian struct {
	db          *db.DB
	coord       *orchestration.Coordinator
	cfg         func() config.GuardianConfig
	langFn      func() string  // optional; returns active UI language code ("en", "sk", …)
	hub         hubBroadcaster
	projRoot    string
	injectedLLM llm.Client // optional: if set, used instead of creating llmClient from config each tick
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

// SetLLMClient injects a shared LLM client. If set, runChecks() uses this
// client (via llmAdapter) instead of creating a new llmClient from config
// each tick. This allows sharing a single client across subsystems.
func (g *Guardian) SetLLMClient(c llm.Client) {
	g.injectedLLM = c
}

// SetLangFn sets the callback used to retrieve the active UI language code at
// check time. When not set (or set to nil) the Guardian defaults to "en". Call
// this before g.Run() to propagate language changes without a restart.
func (g *Guardian) SetLangFn(fn func() string) {
	g.langFn = fn
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
	// The adapter is nil-safe: when g.injectedLLM is nil, configured() returns
	// false and LLM-dependent checks fall back to their FTS-only path.
	var llmCli guardianLLM = newLLMAdapter(g.injectedLLM)

	// Resolve active language — read once per check run so UI changes take effect.
	lang := "en"
	if g.langFn != nil {
		lang = g.langFn()
	}

	var candidates []alertInput

	// 1. Stale workflows
	candidates = append(candidates, checkStaleWorkflows(g.coord, cfg, lang)...)

	// 1b. Stale swarm workers
	candidates = append(candidates, checkStaleWorkers(g.db, cfg, lang)...)

	// 1c. Reviewer timeout (mission stuck in verifying)
	candidates = append(candidates, checkStaleVerifying(g.db, cfg, lang)...)

	// 1d. Overdue tickets (in_progress too long)
	candidates = append(candidates, checkOverdueTickets(g.db, cfg, lang)...)

	// 2. Memory health
	candidates = append(candidates, checkMemoryHealth(g.db, cfg, lang)...)

	// 3. Tech debt
	candidates = append(candidates, checkTechDebt(g.db, g.projRoot, cfg, lang)...)

	// 4. Coverage drift
	candidates = append(candidates, checkCoverageDrift(g.db, g.projRoot, cfg, lang)...)

	// 5. Governance violations
	candidates = append(candidates, checkGovernanceViolations(ctx, g.db, llmCli, g.projRoot, lang)...)

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
