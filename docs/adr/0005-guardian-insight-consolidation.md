# ADR-0005: Guardian / Insight consolidation

**Status:** Accepted
**Date:** 2026-04-19
**Delivered:** 2026-04-19 as a single feature branch (see "Delivery notes" below).

## Context

Stratus v2 runs two long-lived background subsystems that both observe the running system and surface findings to the dashboard:

1. **Guardian** (`guardian/`) — a ticker loop (default 15 min) that runs a fixed set of rule-based checks (`checkStaleWorkflows`, `checkStaleWorkers`, `checkStaleVerifying`, `checkOverdueTickets`, `checkMemoryHealth`, `checkTechDebt`, `checkCoverageDrift`, `checkGovernanceViolations`) against the DB and local project tree, and broadcasts `alert.*` messages via `hubBroadcaster.BroadcastJSON`.

2. **Insight** (`insight/` + `internal/insight/*`) — a ticker loop (default hourly) that runs pattern detection, proposal generation, agent & workflow scorecards, routing recommendations, code analysis, wiki ingest/maintenance, trajectory & workflow synthesis, and an evolution loop. Writes proposals, patterns, and scorecards to the DB; surfaces them in the `/insight` dashboard tab.

These subsystems were added at different times and evolved independently. They share:

- **The same LLM abstraction** — ADR-0001 already collapsed that; both now take a shared `internal/insight/llm.Client` via dependency injection.
- **The same DB** — both read `workflows`, `events`, and swarm tables; both write alerts/findings back.
- **Near-identical ticker patterns** — `guardian.Guardian.Run` and `insight.Scheduler.Run` are structurally the same `for-select-ticker` loop with hot config re-read.

They drift in areas where they *should* share:

1. **Event sourcing.** Insight has an in-process event bus (`insight/events`) with `Event{ID, Type, Timestamp, Source, Payload}` and typed events for workflow/agent/review lifecycle. Guardian ignores the bus entirely — it polls the DB. Every signal Guardian emits (stale workflow, reviewer timeout, overdue ticket) is a *derivation* of a workflow event the bus could deliver in real time. The result: Guardian's alert latency is bounded by its tick interval (minutes), not by the event.

2. **Signal overlap.**
   - `checkTechDebt` (Guardian) counts TODO/FIXME/HACK files and alerts on growth. Insight's `patterns` engine also mines repeat-occurring code smells. Neither consumes the other's output.
   - `checkGovernanceViolations` (Guardian, LLM-assisted) alerts on changed files that look like governance breaches. Insight's `proposals` engine generates fixes for similar concerns. An alert without a paired proposal is half the story; a proposal without the alert lacks the "this is happening *right now*" signal.
   - `checkCoverageDrift` (Guardian) is a numeric drift check. Insight's scorecards are the natural consumer of drift deltas, but today the value lives only in a one-shot alert.

3. **Tick cadence coupling.** Guardian ticks every 15 min, Insight every hour — by independent config paths. A user who turns on "aggressive monitoring" must tune both. There is no shared notion of "how hot should the observer layer run right now."

4. **Two cancellation paths, two shutdown bugs waiting to happen.** `main.go` manages `insightCancel` and `guardianCancel` as two separate `context.CancelFunc`s. Adding a third observer means a third cancel.

The question this ADR answers: *is this dvojkoľajnosť worth collapsing, and if so, how much of it?*

## Decision

Collapse the **shared plumbing** between Guardian and Insight. Keep their **responsibilities** distinct.

Concretely:

1. **Shared event bus is the single source of truth for observations.**
   The `insight/events` bus is promoted out of the `insight/` package tree to a top-level `events/` package. Guardian subscribes to the bus and, on workflow/agent/review events, evaluates its rule checks synchronously rather than waiting for a DB poll. Guardian retains a slow ticker *only* for checks that have no corresponding event (tech debt scan, coverage drift, memory-store size).

2. **Guardian = synchronous rule layer. Insight = asynchronous analysis layer.** The contract becomes:
   - Guardian consumes events, evaluates fast deterministic rules (no LLM except `checkGovernanceViolations`), emits `alert.*` events back onto the bus, broadcasts to the dashboard. Latency target: seconds from triggering event.
   - Insight consumes events *and* Guardian's `alert.*` stream, runs LLM-heavy analysis, writes patterns / proposals / scorecards / evolution hypotheses. Latency target: minutes.

   This makes the relationship explicit: Guardian alerts are *inputs* to Insight's pattern mining, not a competing output channel.

3. **One scheduler helper.** `guardian/guardian.go:Run` and `insight/scheduler.go:Run` are collapsed into a shared `internal/scheduler` package with `Scheduler.Run(ctx, interval func() time.Duration, tick func(ctx))`. Each subsystem keeps its own instance and its own cadence — no runtime coupling — but the boilerplate (hot-reload of interval, graceful shutdown, single-flight protection) lives in one tested place.

4. **Overlapping signal extraction is moved behind shared helpers.**
   - `checkTechDebt` and Insight's code-smell pattern mining both call a new `internal/scan/techdebt` helper that returns `TechDebtSnapshot{FileCount, DeltaSinceBaseline, NewFiles}`. Guardian alerts on the delta; Insight feeds the snapshot into pattern mining.
   - `checkCoverageDrift` emits a `coverage.drift` event onto the bus. Scorecards subscribe and use the numeric delta as an input signal; Guardian still surfaces the user-visible alert.
   - `checkGovernanceViolations` stays in Guardian for the *detect* step, but emits a `governance.violation` event that Insight's proposal engine can pick up and pair with a generated fix proposal. No more orphan alerts.

5. **One cancellation and lifecycle path.** `main.go` gains a single `observers.Start(ctx, bus, db, coord, hub, cfg)` call that wires both Guardian and Insight against the shared bus and returns a single `Stop()` closer. The two subsystems retain their own packages and tests; only the wiring site collapses.

What we are **not** doing:

- **Not merging packages.** `guardian/` and `insight/` stay separate. Insight's LLM-heavy work should not slow down Guardian's alert latency, and Guardian's hot loop should not be forced through Insight's engine struct.
- **Not deduplicating Settings cards.** Both keep their own toggles. The consolidation is plumbing, not UX.
- **Not changing the public HTTP surface.** `/api/guardian/*` and `/api/insight/*` remain intact — same payloads, same handlers.

## Consequences

**Gains**

- Guardian alert latency drops from "up to one tick" (minutes) to "eventloop roundtrip" (ms) for event-triggered rules.
- Every governance alert carries a paired proposal — the user sees "this is wrong" *and* "here is a fix to review" in the same UI flow.
- Adding a third observer (or replacing Guardian's internals with a rules DSL) no longer requires touching `main.go`'s wiring or writing a fresh ticker loop — it registers a subscriber.
- Tech-debt growth, coverage drift, and governance violations stop being siloed numbers and become trend inputs to Insight's scorecards and evolution loop.
- Shared `internal/scheduler` removes a class of "oh, we forgot to drain the ticker on shutdown" bugs from future observer code.

**Costs**

- Refactor touches `main.go`, `guardian/guardian.go`, `guardian/checks.go`, `insight/scheduler.go`, and introduces `events/`, `internal/scheduler/`, `internal/scan/techdebt/`, `observers/`.
- Moving `insight/events` → `events/` is a breaking import path change for every file under `internal/insight/*` that references the bus. Mitigated by a single find-replace plus `go build ./...` as verification.
- Introducing the bus as a Guardian input means Guardian's test suite grows a fake-bus harness (`events.NewInMemoryBus` already exists in insight/events; it becomes `events.InMemoryBus`).
- Users relying on the current Guardian tick cadence for heavy scans (tech debt, coverage drift) see no change — those stay on the ticker. Users relying on *other* Guardian checks (stale workflow, reviewer timeout) see alerts sooner, which is the intended direction but worth calling out in release notes.

## Delivery notes

Originally planned as five independently-shippable releases (v0.12.0 through v0.16.0). During implementation we chose to bundle all five phases behind a single feature branch because:

- Phases 1–2 are pure refactors with zero user-visible effect — no point in a separate release just to move packages.
- Phase 3's Guardian→bus wiring only becomes useful once Phase 4's paired-proposal consumer is present; shipping Phase 3 alone would publish alerts to the bus with nobody listening.
- The whole set is small enough (≈1k LOC diff, single-day turnaround) that coordinated rollback is simpler than tracking five release boundaries.

### Implemented

1. **Shared scheduler** (`internal/scheduler`) — ticker primitive with hot-reload, single-flight, graceful shutdown. Six unit tests. Guardian and Insight both ported.
2. **Bus promotion** — `insight/events` moved to top-level `events/`. Nine `.go` files and two docs had their import paths updated.
3. **Guardian on the bus** — `Guardian.SetEventBus()` method; outbound `alert.emitted` + `governance.violation` + `coverage.drift` events; inbound subscribers for `agent.failed` and `review.failed` that emit immediate alerts without waiting for the tick. Eight new integration tests.
4. **Paired governance flow** — `ProposalTypeGovernanceRemediation` + `NewGovernanceRemediationProposal` helper. `insight.Engine.HandleEvent` now pairs every `governance.violation` event with a persisted remediation proposal linked via `SourcePatternID = "guardian-alert:<id>"`. Three new integration tests.
5. **Coverage drift as an event** — `EventCoverageDrift` published when Guardian's `coverage_drift` alert fires; Insight's event store persists it so future consumers (scorecards, evolution loop) can read it without further plumbing.

### Deliberately deferred

- **`internal/scan/techdebt` helper.** The two existing callers (Guardian's `checkTechDebt` doing a file count; `internal/insight/code_analyst/collector.go:CollectTechDebt` doing per-file counts) have genuinely different output shapes today. Extracting a shared helper to satisfy both would be speculative abstraction (Karpathy principle #2). Revisit when a third caller appears or when one of the two starts wanting the other's output.
- **Scorecards consumption of `coverage.drift`.** Adding a code-quality axis to the scorecard model is a feature change, not plumbing. The event is available on the bus and persisted; a future feature can subscribe.

### Acceptance

- `go test ./events/... ./insight/... ./guardian/... ./internal/scheduler/...` — all green.
- The only pre-existing full-suite failure (`TestHandleGetLLMUsage_TotalTokensCalculation` in `api/`) was verified to exist on `main` before this branch and is unrelated.
- 17 new tests cover the new surface (scheduler: 6, guardian bus: 9, insight governance: 3 — one overlaps).

## Rejected alternatives

1. **Full merge into a single `observability/` package.** Rejected. Guardian's hot path (fast, deterministic, optionally LLM-free) and Insight's hot path (LLM-heavy, slow, evolution-capable) have opposite performance profiles. Forcing them through the same engine struct couples shutdown, rate-limiting, and error handling in ways that help neither.

2. **Delete Guardian, let Insight do everything.** Rejected. Insight's tick is hourly by design — it burns LLM tokens per cycle. Moving stale-workflow detection into Insight means either paying for LLM cycles to do arithmetic, or building a non-LLM fast path inside Insight that is structurally a re-invention of Guardian.

3. **Delete Insight, let Guardian do everything.** Rejected. Guardian's rule model is deliberately deterministic and shallow. Pattern mining, proposal generation, routing recommendations, scorecards, and the evolution loop do not fit that shape, and retrofitting them would make Guardian the thing Insight already is.

4. **Keep them fully independent, write a style guide.** Rejected. This is the status quo. It leaves the latency gap, the orphan-alert problem, and the dual wiring path unaddressed — which is why this ADR exists.

5. **Move only the event bus, leave everything else as-is.** Rejected as a *terminal* state (acceptable as step 2 of the migration). Shared bus without a contract ("Guardian's alerts are Insight's inputs") is just another coupling point without a governance rule — future authors will reinvent the overlap.
