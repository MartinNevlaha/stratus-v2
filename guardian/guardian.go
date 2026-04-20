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
	"github.com/MartinNevlaha/stratus-v2/events"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/scheduler"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

const (
	defaultGuardianInterval = 15 * time.Minute

	// emitEventTimeout caps how long Guardian waits when publishing an
	// outbound event. Guardian's hot path should never block on the bus.
	emitEventTimeout = 2 * time.Second
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
	langFn      func() string // optional; returns active UI language code ("en", "sk", …)
	hub         hubBroadcaster
	projRoot    string
	injectedLLM llm.Client // optional: if set, used instead of creating llmClient from config each tick

	// bus is the shared event bus. Optional; when nil Guardian behaves
	// exactly as before this change (pure ticker, no subscribers, no
	// outbound alert events).
	bus events.EventBus
	// busSubscriptionID is retained so SetEventBus can be called twice
	// without leaking a handler.
	busSubscriptionID events.SubscriptionID
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

// SetEventBus wires Guardian into the shared event bus.
//
// When a bus is attached:
//   - Guardian publishes events.EventAlertEmitted after every alert it saves.
//   - Guardian publishes events.EventGovernanceViolation for each governance
//     alert (this is what Insight's proposal engine pairs against).
//   - Guardian subscribes to events.EventAgentFailed and events.EventReviewFailed
//     and emits synchronous alerts for those failures, bypassing the tick.
//
// SetEventBus is idempotent: calling it again replaces the subscription. Pass
// nil to detach (used mostly in tests).
func (g *Guardian) SetEventBus(bus events.EventBus) {
	if g.bus != nil {
		g.bus.Unsubscribe(g.busSubscriptionID)
		g.busSubscriptionID = 0
	}
	g.bus = bus
	if bus != nil {
		g.busSubscriptionID = bus.Subscribe(g.handleBusEvent)
	}
}

// Run starts the guardian ticker loop. It blocks until ctx is cancelled.
func (g *Guardian) Run(ctx context.Context) {
	if !g.cfg().Enabled {
		log.Println("guardian: disabled, not starting")
		return
	}

	intervalFn := func() time.Duration {
		d := time.Duration(g.cfg().IntervalMinutes) * time.Minute
		if d <= 0 {
			return defaultGuardianInterval
		}
		return d
	}
	log.Printf("guardian: starting, interval=%v", intervalFn())

	tick := func(ctx context.Context) {
		if !g.cfg().Enabled {
			log.Println("guardian: disabled mid-run, skipping")
			return
		}
		g.runChecks(ctx)
	}

	if err := scheduler.New("guardian", intervalFn, tick).Run(ctx); err != nil && err != context.Canceled {
		log.Printf("guardian: scheduler stopped: %v", err)
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

// maybeEmit deduplicates and saves an alert, broadcasting it over WebSocket
// and (if a bus is attached) publishing corresponding events.
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

	g.publishAlertEvents(id, a)
}

// publishAlertEvents fans the freshly-saved alert out to the event bus. For
// a governance_violation alert the richer EventGovernanceViolation is also
// emitted so Insight can pair a remediation proposal.
//
// Bus absence is the common case in tests and legacy wiring; this function
// returns early and is allocation-free in that path.
func (g *Guardian) publishAlertEvents(alertID int64, a alertInput) {
	if g.bus == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), emitEventTimeout)
	defer cancel()

	basePayload := map[string]any{
		"alert_id": alertID,
		"type":     a.Type,
		"severity": a.Severity,
		"message":  a.Message,
		"metadata": a.Metadata,
	}
	_ = g.bus.Publish(ctx, events.NewEvent(events.EventAlertEmitted, "guardian", basePayload))

	if a.Type == "governance_violation" {
		govPayload := map[string]any{
			"alert_id": alertID,
			"severity": a.Severity,
			"message":  a.Message,
			"file":     a.Metadata["file"],
			"rules":    a.Metadata["rules"],
		}
		_ = g.bus.Publish(ctx, events.NewEvent(events.EventGovernanceViolation, "guardian", govPayload))
	}

	if a.Type == "coverage_drift" {
		covPayload := map[string]any{
			"alert_id": alertID,
			"baseline": a.Metadata["baseline"],
			"current":  a.Metadata["current"],
			"drop":     a.Metadata["drop"],
		}
		_ = g.bus.Publish(ctx, events.NewEvent(events.EventCoverageDrift, "guardian", covPayload))
	}
}

// handleBusEvent is the inbound subscription. It reacts synchronously to
// failure events by emitting an immediate Guardian alert — no waiting for
// the next tick.
//
// Kept deliberately narrow: only events where a faster-than-a-tick alert
// adds real value are handled here. Staleness checks remain ticker-driven
// because they are absence-of-event checks.
func (g *Guardian) handleBusEvent(ctx context.Context, evt events.Event) {
	lang := "en"
	if g.langFn != nil {
		lang = g.langFn()
	}
	switch evt.Type {
	case events.EventAgentFailed:
		agent := stringFromPayload(evt.Payload, "agent_id")
		if agent == "" {
			agent = stringFromPayload(evt.Payload, "id")
		}
		g.maybeEmit(alertInput{
			Type:     "agent_failed",
			Severity: "warning",
			Message:  alertMessage(lang, "agent_failed", agent),
			Metadata: map[string]any{
				"dedup_key": "agent_failed:" + evt.ID,
				"agent_id":  agent,
				"event_id":  evt.ID,
			},
		})
	case events.EventReviewFailed:
		workflow := stringFromPayload(evt.Payload, "workflow_id")
		g.maybeEmit(alertInput{
			Type:     "review_failed",
			Severity: "warning",
			Message:  alertMessage(lang, "review_failed", workflow),
			Metadata: map[string]any{
				"dedup_key":   "review_failed:" + evt.ID,
				"workflow_id": workflow,
				"event_id":    evt.ID,
			},
		})
	}
}

func stringFromPayload(p map[string]any, key string) string {
	if p == nil {
		return ""
	}
	if v, ok := p[key].(string); ok {
		return v
	}
	return ""
}
