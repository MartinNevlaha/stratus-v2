# Spec: Fix evolution analysis to target the framework's codebase

**Workflow ID:** `spec-evolution-target-codebase`
**Type:** `spec` (simple)
**Karpathy:** Simplicity First + Surgical Changes + Goal-Driven

## Problem

`POST /api/evolution/trigger` currently produces Stratus self-improvement hypotheses instead of analysis of the target project that the binary is running on. Evidence:

- `insight/engine.go:1535` routes `RunEvolutionCycle` to `evolutionLoop.Run()` (legacy path).
- Legacy path runs `HypothesisGenerator.generateWithLLM` in `internal/insight/evolution_loop/hypothesis.go:159`.
- `allCategories = ["prompt_tuning"]` is hardcoded on line 20; LLM responses are filtered against it on line 208-218, so target-project categories requested by the API are dropped.
- System prompt `prompts.HypothesisGeneration` (`internal/insight/prompts/prompts.go:24`) tells the LLM it is an "autonomous improvement engine for a software development workflow system"; no baseline data is passed → LLM invents Stratus-self findings.
- The correct target-project path (`RunCycle` in `internal/insight/evolution_loop/loop.go:138`) is fully wired in `initEvolutionLoop` (engine.go:279-339) with `BaselineBuilder`, `generators.Registry`, `ProposalWriter`, and `projectRoot` — but nothing calls it.

## Success criteria (Goal-Driven)

1. With `stratus_self_enabled=false` (default), `POST /api/evolution/trigger` results in an `evolution_runs` row whose child `evolution_hypotheses` rows are all target-project categories (`refactor_opportunity`, `test_gap`, `architecture_drift`, `feature_idea`, `dx_improvement`, `doc_drift`) — zero `prompt_tuning` rows.
2. The Evolution tab in the dashboard continues to render runs and hypotheses without schema changes.
3. With `stratus_self_enabled=true`, the legacy HypothesisGenerator runs (repurposed as the Stratus-self meta path). Target-project analysis is NOT triggered in this mode — the two modes are mutually exclusive to keep the UI unambiguous.
4. Integration tests assert both modes end-to-end.

## Non-goals

- No UI changes in this PR.
- No DB schema migration.
- No LLM-judge wiring (stays disabled per `engine.go:315`).
- No deletion of `HypothesisGenerator` / seeds / `ExperimentRunner` / `Evaluator` — governance flagged that for a follow-up PR (Surgical Changes).
- No `retrieve(corpus="wiki")` deduplication against proposal-generated pages (out of scope, file follow-up if noise appears).

## Budget (Principle 2)

- Go production code: ≤ 150 LOC (new adapter + engine.go switch).
- Test code: ≤ 180 LOC (new integration + unit tests).
- Frontend: 0 LOC.
- DB schema: 0 changes.
- Files touched: expected 3-5 (engine.go, loop.go, hypothesis.go/new adapter file, test files).

## Design

### Two-mode dispatch in `RunEvolutionCycle`

```go
func (e *Engine) RunEvolutionCycle(ctx, triggerType, timeoutMs, categories) (*RunResult, error) {
    if e.evolutionLoop == nil { return nil, nil }
    if e.evoCfg.StratusSelfEnabled {
        // Meta mode — legacy path (Stratus self-analysis)
        return e.evolutionLoop.Run(ctx, triggerType, timeoutMs, categories)
    }
    // Default — target-project analysis with evolution_runs bookkeeping
    return e.evolutionLoop.RunTargetCycle(ctx, triggerType, timeoutMs)
}
```

### New method `EvolutionLoop.RunTargetCycle`

Thin wrapper around the existing `RunCycle` that adds the bookkeeping `Run()` already does (so the `evolution_runs`/`evolution_hypotheses` UI contract keeps working):

1. Serialize via `l.mu` + `l.running` flag (reuse guard).
2. Create `db.EvolutionRun{Status: "running", TriggerType: triggerType, StartedAt: now}` via `l.store.SaveRun`.
3. Invoke `l.RunCycle(ctx)`.
4. Map each scored hypothesis returned by `RunCycle` into a `db.EvolutionHypothesis` row (category, description=Title+Rationale, baseline_value/proposed_value=empty, metric="", baseline_metric=0, evidence=signal_refs+scores JSON) and persist via `l.store.SaveHypothesis` or equivalent.
5. Update `run.Status` to `completed`/`failed`, set `CompletedAt`, `DurationMs`, `HypothesesCount`.
6. Return a `RunResult` populated with `RunID`, `HypothesesTested`, `DurationMs`.

**To enable step 4, `RunCycle` must expose the scored hypotheses.** Change its return signature from `CycleResult` to one that includes `[]ScoredHypothesis` (a new type: `{Hypothesis scoring.Hypothesis; Final float64}`). The only in-tree callers are test code, which we update. This is a small surgical change.

### Why not also run the legacy path when `stratus_self_enabled=true`?

Governance flagged dual-path concurrency as risk (two run records per trigger, UI confusion). Single-mode dispatch is simpler and honours "Surgical Changes".

## Tasks (ordered, TDD first)

1. **[TDD] Write failing test** `internal/insight/evolution_loop/run_target_cycle_test.go` that drives `RunTargetCycle` with a fixture `BaselineBuilder` producing synthetic TODOs + test ratios + git commits, and asserts:
   - An `evolution_runs` row is created with status `completed`.
   - `evolution_hypotheses` rows exist for at least one target category (`test_gap` from the fixture).
   - Zero `prompt_tuning` rows.

2. **[TDD] Write failing integration test** in `api/routes_evolution_test.go` (or add to existing) that fires `POST /api/evolution/trigger` with `stratus_self_enabled=false` and asserts the run record + target-category hypotheses (using a test double for the LLM + baseline).

3. **Implement type + signature change**: add `ScoredHypothesis` struct in `internal/insight/evolution_loop/loop.go`; change `RunCycle` to return `([]ScoredHypothesis, CycleResult, error)`; update existing callers and tests.

4. **Implement `EvolutionLoop.RunTargetCycle`** in `internal/insight/evolution_loop/run_target_cycle.go` (new file): builds on `RunCycle`, adds bookkeeping to `evolution_runs` + `evolution_hypotheses` via `l.store`.

5. **Rewire engine**: modify `insight/engine.go:1531-1540` to dispatch by `stratus_self_enabled`.

6. **[TDD] Meta-mode test**: add a test asserting that with `stratus_self_enabled=true` the legacy path is used (prompt_tuning hypotheses appear, target-project categories do not).

7. **Run tests**: `go test ./internal/insight/evolution_loop/... ./api/... ./insight/...` — all green.

8. **Manual verification**: `stratus serve` running against this repo, hit `POST /api/evolution/trigger`, confirm the returned run's hypotheses describe `stratus-v2` code (not Stratus framework internals).

## Risk register

| Risk | Mitigation |
|------|------------|
| Source code egress to LLM when `BaselineBuilder` gathers TODOs / git subjects | Already wired as of `redacted = baseline.Redact(&bundle)` on loop.go:171. Verify secret-masking covers the new output path. |
| `evolution_hypotheses` schema mismatch (required fields) | Map sensible defaults for baseline_value/metric/etc.; surface them in `evidence_json`. Existing UI handles empty strings. |
| Wiki page duplication from `ProposalWriter` for `feature_idea` | Unchanged behaviour from `RunCycle`; not a regression. Flag as follow-up if observed noise. |
| Breaking `mcp__stratus__evolution_*` tools | Tools proxy to the same HTTP routes — UI contract preserved, so MCP preserved. |
| Legacy path rot if never exercised outside tests | Behind `stratus_self_enabled=true`; follow-up PR to delete after telemetry confirms zero usage. |
| Hook/guard that relies on legacy categories | None found in `hooks/` — verified via grep. |

## Rollback

Revert the change to `RunEvolutionCycle`; `Run()` legacy path is unchanged and returns to being the default. No data migration needed (both paths write to the same `evolution_runs` table).
