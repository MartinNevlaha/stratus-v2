# Implementation Plan: Repurpose Evolution Loop for Target-Project Analysis

**Spec:** `/home/martin/Documents/projects/stratus-v2/docs/plans/spec-evolution-target-project-design.md`
**Mode:** Proposals only (no auto-apply). Target: `cfg.ProjectRoot`.

## Ordered Task List

### T1. Schema migration: extend `insight_proposals`
Additive columns `wiki_page_id`, `idempotency_hash`, `last_seen_at`, `signal_refs`; unique partial index on `idempotency_hash`. Document `type=idea` soft-enum addition.
- **Agent:** database
- **Files:** `db/schema.go`, `db/migrations/*`
- **Tests first:** `db/schema_test.go` asserts new cols + unique index; round-trip insert with hash.

### T2. Config additions + hard validation
Add `StratusSelfEnabled`, `MaxTokensPerCycle` (required >0), `ScoringWeights`, `BaselineLimits`, `AllowedEvolutionCategories`. Hard-reject invalid weights. Export `ErrTokenCapRequired`, `ErrInvalidScoringWeights`, `ErrInvalidCategory` sentinels.
- **Agent:** backend
- **Files:** `config/config.go`, `internal/insight/evolution_loop/errors.go`
- **Depends:** T1
- **Tests first:** `config/config_test.go` — missing cap errors; weights out of range; unknown category.

### T3. Baseline builder + secret redaction (§13.3)
`baseline/types.go`, `baseline/builder.go` (Vexor/git/tree/TODO/wiki/governance/test-ratio heuristic, honors `BaselineLimits`). `baseline/redact.go` drops secret-pattern lines from snippets/TODOs/commit messages.
- **Agent:** backend
- **Files:** `internal/insight/evolution_loop/baseline/{types,builder,redact}.go`
- **Depends:** T2
- **Tests first:** builder cap enforcement; table-driven secret regex (AWS/GCP/API keys/.env/PEM).

### T4. Static scorer + rank blender
`scoring/static.go` (churn/test-gap/TODO/staleness/ADR-violation in [0,1]); `scoring/rank.go` weighted blend honoring validated weights.
- **Agent:** backend
- **Files:** `internal/insight/evolution_loop/scoring/{static,rank}.go`
- **Depends:** T3

### T5. LLM judge with per-call token cap (§13.2)
`scoring/llm_judge.go` receives `perCallCap`, returns `LLMScores` + `tokensUsed`. Deterministic JSON parse; error wrapping with sentinels.
- **Agent:** backend
- **Files:** `internal/insight/evolution_loop/scoring/llm_judge.go`, `internal/insight/llm/mock.go`
- **Depends:** T4

### T6. Hypothesis generators for 6 new categories
One file per category. Retain `prompt_tuning` gated behind `StratusSelfEnabled`. Generators never call LLM directly.
- **Agent:** backend
- **Files:** `internal/insight/evolution_loop/generators/{refactor,test_gap,architecture_drift,feature_idea,dx_improvement,doc_drift,prompt_tuning}.go`
- **Depends:** T3

### T7. Proposal writer (atomic dual-write, ON CONFLICT)
`INSERT … ON CONFLICT(idempotency_hash) DO UPDATE SET last_seen_at=excluded.last_seen_at RETURNING id`. Wiki page + proposal in single `BEGIN IMMEDIATE` TX.
- **Agent:** database
- **Files:** `internal/insight/evolution_loop/proposal_writer.go`
- **Depends:** T1, T6
- **Tests first:** hash stability, concurrent race, rollback-no-orphan-wiki.

### T8. Loop driver retarget + token accounting
Wires baseline → generators → scorer → writer; enforces `MaxTokensPerCycle`; deletes `applyFn`/`AutoApplyThreshold` invocation paths. Config field retained as deprecated no-op.
- **Agent:** implementation-expert
- **Files:** `internal/insight/evolution_loop/loop.go`
- **Depends:** T4, T5, T6, T7

### T9. Remove deprecated generators & simulated experiment
Delete `workflow_routing`, `agent_selection`, `threshold_adjustment`; strip `experiment.go` simulated metrics. Preserve DB read compat.
- **Agent:** implementation-expert
- **Files:** `internal/insight/evolution_loop/{hypothesis.go,experiment.go}`
- **Depends:** T8

### T10. API handler updates (server-authoritative hash §13.1)
`POST /api/learning/proposals` — accept allowlisted types incl. `idea`, **reject client `idempotency_hash` with 400**, compute server-side. Add `category_breakdown` to status endpoint.
- **Agent:** backend
- **Files:** `api/routes_insight.go`, `api/routes_evolution.go`
- **Depends:** T1, T7

### T11. Integration test: full loop E2E
In-memory SQLite, mock LLM, real baseline stubs. Verifies dual-write TX, FK integrity, idempotency across two runs.
- **Agent:** backend
- **Files:** `internal/insight/evolution_loop/evolution_loop_integration_test.go`
- **Depends:** T8, T10

### T12. Frontend Evolution.svelte updates
Category filter chips from new allowlist; rename "Auto-applied" → "Ideas created"; `wiki_page_id` link column; Stratus-self toggle in Settings. i18n en+sk for 6 new categories.
- **Agent:** frontend
- **Files:** `frontend/src/routes/Evolution.svelte`, `frontend/src/routes/Settings.svelte`, `frontend/src/lib/i18n/{en,sk}.json`
- **Depends:** T10

## Dependency Graph

```
T1 → T2 → T3 → T4 → T5 ─┐
                  └ T6 ─┤
T1 ─────────────── T7 ──┤
                        T8 → T9
T1 → T7 → T10
T8,T10 → T11
T10 → T12
```

## Test-First

TDD applies to every task. Governance-hardening items each pair with a red test: T2 (weights rejection), T3 (redaction), T5 (per-call cap), T7 (ON CONFLICT race + rollback), T8 (token cap sentinel + applyFn deletion), T10 (server-hash 400). Coverage target 80%.
