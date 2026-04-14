# Technical Design: Repurpose Evolution Loop for Target-Project Analysis

Status: Proposed
Date: 2026-04-14
Scope: `internal/insight/evolution_loop/**`, `config/config.go`, `db/schema.go`, `api/routes_insight.go`, `frontend/src/routes/Evolution.svelte` (impact only).

## 1. Overview

The evolution loop today generates hypotheses about Stratus' own routing/threshold/prompt tuning and runs simulated A/B experiments. This redesign retargets the loop at `cfg.ProjectRoot` (the user's project) and replaces simulated experiments with a grounded baseline-bundle + scoring pipeline. No execution, no auto-apply: the loop emits ranked proposals and companion wiki idea pages.

## 2. Architecture

```
            +------------------------------+
trigger --> |       EvolutionLoop.Run      |
            +--------------+---------------+
                           |
                           v
          +-------- baseline.Builder --------+
          |  Vexor top-K | git log (30d)     |
          |  tree (2 lvl)| TODO scan         |
          |  wiki staleness | governance     |
          +--------------+-------------------+
                         |  Bundle
                         v
          +---- hypothesis.Generator --------+
          |  one sub-generator per category  |
          |  (refactor_opportunity,          |
          |   test_gap, architecture_drift,  |
          |   feature_idea, dx_improvement,  |
          |   doc_drift, [+prompt_tuning if  |
          |   StratusSelfEnabled])           |
          +--------------+-------------------+
                         |  []Hypothesis
                         v
             +--- scoring pipeline ---+
             | StaticScorer (churn,   |
             |  test-gap, TODO,       |
             |  staleness, ADR viol.) |
             | LLMJudge (impact,      |
             |  effort, confidence,   |
             |  novelty)              |
             +-----------+------------+
                         |  ScoredHypothesis
                         v
                +--- proposal writer ---+----------+
                |  TX: insight_proposals           |
                |      + wiki page (ideas/*.md)    |
                |      idempotency by content hash |
                +----------------------------------+
```

## 3. Module Map

New:
- `internal/insight/evolution_loop/baseline/builder.go` — assembles grounded `Bundle` from Vexor, git, tree, TODO, wiki, governance.
- `internal/insight/evolution_loop/baseline/types.go` — `Bundle`, `FileStat`, `TodoHit`, `WikiRef`, `GovRef`.
- `internal/insight/evolution_loop/scoring/static.go` — deterministic signal scorers (0–1).
- `internal/insight/evolution_loop/scoring/llm_judge.go` — LLM scorer (impact/effort/confidence/novelty).
- `internal/insight/evolution_loop/scoring/rank.go` — weighted blend using `cfg.ScoringWeights`.
- `internal/insight/evolution_loop/generators/*.go` — one file per new category.
- `internal/insight/evolution_loop/proposal_writer.go` — dual-write TX (proposal + wiki idea page).

Modified:
- `loop.go` — remove `applyFn` auto-apply branch; wire in baseline → generators → scorer → writer; refuse if `MaxTokensPerCycle == 0`.
- `hypothesis.go` — drop `workflow_routing`, `agent_selection`, `threshold_adjustment` generators; retain `prompt_tuning` only when `StratusSelfEnabled`.
- `config/config.go` — see §5.
- `db/schema.go` — see §4.
- `api/routes_insight.go` — see §6.

Removed (evolution code path only; files can stay for DB read compat):
- `experiment.go` simulated `categoryBaselines` and `Execute*` (replaced by scorer).
- Any `AutoApply`/`applyFn` wiring from `loop.go`, `loop_test.go`.

## 4. Data Model Changes

Append-only migration on `insight_proposals` (table discrepancy: decisions refer to `learning_proposals`; we reuse the existing `insight_proposals` table — see open question §12.1):

```sql
ALTER TABLE insight_proposals ADD COLUMN wiki_page_id       TEXT NULL
    REFERENCES wiki_pages(id);
ALTER TABLE insight_proposals ADD COLUMN idempotency_hash   TEXT NULL;
ALTER TABLE insight_proposals ADD COLUMN last_seen_at       INTEGER NULL;
ALTER TABLE insight_proposals ADD COLUMN signal_refs        TEXT NULL; -- JSON
CREATE UNIQUE INDEX IF NOT EXISTS idx_insight_proposals_idem
    ON insight_proposals(idempotency_hash) WHERE idempotency_hash IS NOT NULL;
```

Enum extension on `insight_proposals.type`: add `idea` (soft enum, column is TEXT — no DDL change, only documentation + API validator update).

Migration strategy: additive only, `ADD COLUMN` is backward compatible on SQLite. No backfill needed; legacy rows have NULL in new columns. Old `workflow_routing`/`agent_selection`/`threshold_adjustment` rows remain readable; no deletion.

Idempotency key:
```
sha256(category + "\n" + normalize(title) + "\n" + strings.Join(sort(signal_refs), ","))
```
If an existing row has the same `idempotency_hash` AND `last_seen_at` is within 30 days, `UPDATE last_seen_at = now()` instead of inserting.

## 5. Config Additions (`config.EvolutionConfig`)

```go
type EvolutionConfig struct {
    // existing fields preserved...
    Enabled             bool
    TimeoutMs           int64
    MaxHypothesesPerRun int
    AutoApplyThreshold  float64 // DEPRECATED: ignored by project-targeted categories
    ProposalThreshold   float64
    MinSampleSize       int
    DailyTokenBudget    int
    Categories          []string

    // NEW
    StratusSelfEnabled bool            `json:"stratus_self_enabled"` // default false
    MaxTokensPerCycle  int             `json:"max_tokens_per_cycle"` // REQUIRED > 0
    ScoringWeights     ScoringWeights  `json:"scoring_weights"`
    BaselineLimits     BaselineLimits  `json:"baseline_limits"`
}

type ScoringWeights struct {
    Churn         float64 `json:"churn"`          // default 0.15
    TestGap       float64 `json:"test_gap"`       // 0.15
    TODO          float64 `json:"todo"`           // 0.05
    Staleness     float64 `json:"staleness"`      // 0.05
    ADRViolation  float64 `json:"adr_violation"`  // 0.10
    LLMImpact     float64 `json:"llm_impact"`     // 0.20
    LLMEffort     float64 `json:"llm_effort"`     // 0.10 (inverted: lower effort = higher score)
    LLMConfidence float64 `json:"llm_confidence"` // 0.10
    LLMNovelty    float64 `json:"llm_novelty"`    // 0.10
}

type BaselineLimits struct {
    VexorTopK     int `json:"vexor_top_k"`      // default 30
    GitLogCommits int `json:"git_log_commits"`  // default 200
    TODOMax       int `json:"todo_max"`         // default 50
}
```

Allowed categories (validated in `config.Load`):
```go
var AllowedEvolutionCategories = map[string]struct{}{
    "refactor_opportunity": {},
    "test_gap":             {},
    "architecture_drift":   {},
    "feature_idea":         {},
    "dx_improvement":       {},
    "doc_drift":            {},
    "prompt_tuning":        {}, // only effective when StratusSelfEnabled
}
```

Validation rules (per `.claude/rules/config-validation.md`):
- `MaxTokensPerCycle > 0` else loop returns `ErrTokenCapRequired`.
- All `ScoringWeights.*` in [0,1]; sum normalized at load time (log warning if >1.5).
- `BaselineLimits.*` capped at hard ceilings (VexorTopK ≤ 100, GitLogCommits ≤ 500, TODOMax ≤ 200).
- `Categories` validated against `AllowedEvolutionCategories`.

`AutoApplyThreshold` retained in struct, logged once at loop start as `deprecated, ignored`.

## 6. API Changes

`POST /api/learning/proposals` (existing insight proposals endpoint):

Request additions:
```
{
  "type":              "idea" | "refactor_opportunity" | ...,
  "wiki_page_id":      "w_abc123" | null,
  "signal_refs":       ["path/to/file.go:42", "commit:abc123"],
  "idempotency_hash":  "sha256:..."  // optional; server computes if absent
}
```
422 if `type` not in allowed set. Forward `signal_refs`/`wiki_page_id` to storage (per `.claude/rules/api-parameter-passthrough.md`).

`GET /api/evolution/status` — add:
```
"category_breakdown": {
  "refactor_opportunity": 3,
  "test_gap":             5,
  ...
}
```

No new endpoints; no breaking changes to existing consumers (fields are additive, legacy clients ignore them).

## 7. Key Interfaces

```go
// baseline
package baseline

type Bundle struct {
    Root       string
    VexorHits  []VexorHit
    GitLog     []GitCommit   // cap BaselineLimits.GitLogCommits
    Tree       []FileStat    // 2 levels
    Langs      map[string]int
    TODOs      []TodoHit     // cap BaselineLimits.TODOMax
    WikiRefs   []WikiRef     // title + staleness
    GovRefs    []GovRef      // from retrieve(corpus=governance)
    TestRatio  map[string]float64 // per-dir: tests/src files
    CollectedAt time.Time
}

type Builder interface {
    Build(ctx context.Context, root string, limits config.BaselineLimits) (Bundle, error)
}

// scoring
package scoring

type StaticScores struct {
    Churn, TestGap, TODO, Staleness, ADRViolation float64 // 0..1
}
type LLMScores struct {
    Impact, Effort, Confidence, Novelty float64 // 0..1
}
type Scored struct {
    Hypothesis db.EvolutionHypothesis
    Static     StaticScores
    LLM        LLMScores
    Final      float64
    TokensUsed int
}

type StaticScorer interface {
    Score(ctx context.Context, h db.EvolutionHypothesis, b baseline.Bundle) StaticScores
}
type LLMJudge interface {
    Score(ctx context.Context, h db.EvolutionHypothesis, b baseline.Bundle) (LLMScores, int, error) // int = tokens
}

// generators (one per category)
package generators

type Generator interface {
    Category() string
    Generate(ctx context.Context, runID string, b baseline.Bundle, lang string) ([]db.EvolutionHypothesis, error)
}
```

Token accounting: `EvolutionLoop.Run` sums `Scored.TokensUsed` and aborts further `LLMJudge` calls when total ≥ `MaxTokensPerCycle` (remaining hypotheses are scored static-only and flagged `partial_scoring`).

## 8. Idea Pipeline Detail

Happy path for a `feature_idea` hypothesis:

```
feature_idea generator (reads Bundle.GovRefs + TODOs + WikiRefs)
  -> Hypothesis{Category:"feature_idea", Title, Description, SignalRefs}
  -> StaticScorer.Score(h, bundle)
  -> LLMJudge.Score(h, bundle)              // may be skipped if token cap hit
  -> rank.Blend(static, llm, weights) => Final
  -> idempotency_hash = sha256(...)
  -> proposal_writer.Write(tx):
       SELECT id, last_seen_at FROM insight_proposals WHERE idempotency_hash = ?
       IF found AND last_seen_at within 30d:
          UPDATE last_seen_at = now(), signal_refs = merged
          COMMIT; return existing id
       ELSE:
          INSERT INTO wiki_pages (id, slug="ideas/<slug>", body=<frontmatter+desc>, ...)
          INSERT INTO insight_proposals (id, type="idea", title, description,
                    confidence=Final, wiki_page_id, idempotency_hash,
                    last_seen_at=now(), signal_refs=json(...))
          COMMIT
```

Transaction semantics: single `BEGIN IMMEDIATE` … `COMMIT`. On any error the TX is rolled back; no orphan wiki page. `wiki_page_id` FK prevents dangling references. Writer is invoked by the loop after scoring; loop treats writer error as per-hypothesis failure (logged, loop continues).

## 9. Test Strategy

Unit:
- `scoring/static_test.go` — table-driven: given synthetic `Bundle`, assert each score in [0,1] and monotonic vs inputs.
- `scoring/rank_test.go` — weighted blend correctness, weight normalization.
- `proposal_writer_test.go` — idempotency hash stability (same inputs ⇒ same hash); 30-day window updates vs new insert.
- `baseline/builder_test.go` — mock vexor/git/fs; asserts caps respected.
- `generators/*_test.go` — each category generator produces ≥1 hypothesis from a minimal bundle; respects `MaxHypothesesPerRun`.
- `loop_test.go` updated — removes auto-apply assertions; adds token-cap enforcement, `MaxTokensPerCycle=0 ⇒ error`, `StratusSelfEnabled=false ⇒ no prompt_tuning hypotheses`.

Integration:
- `evolution_loop_integration_test.go` — real SQLite (`:memory:`), mock LLM client, verify dual-write TX: one row in `insight_proposals`, one in `wiki_pages`, FK matches.

Mock LLM client in `internal/insight/llm/mock.go` (reuse existing test doubles) returns deterministic score JSON.

Coverage target: 80% per `.claude/rules/tdd-requirements.md`.

## 10. Backward Compatibility / Migration

- Old hypothesis rows with categories `workflow_routing|agent_selection|threshold_adjustment` remain in `evolution_hypotheses`; they are read-only in the UI. Generator code for those categories is deleted; nothing writes them again.
- Optional `category_deprecated` boolean on `evolution_hypotheses` (nullable; set true by a one-shot migration on rows older than 30d with deprecated category). Defer unless UI needs it (see open question §12.3).
- `AutoApplyThreshold` kept in config struct (documented as deprecated) for `.stratus.json` forward/backward compatibility.
- `applyFn` callback on `EvolutionLoop` kept but marked `// Deprecated: never invoked by project-targeted categories.` Removal in a later major.

Frontend (`frontend/src/routes/Evolution.svelte`) impact — NOT designed here, listed for delivery-frontend:
- Category filter chips must be regenerated from new allowed set.
- "Auto-applied" counter should be hidden or relabeled "Ideas created".
- Proposal row needs a link to its `wiki_page_id` when present.
- Stratus-self toggle (binds to `StratusSelfEnabled`) in Settings.
- i18n keys for new categories (en + sk).

## 11. Out of Scope for v1

- Real coverage runners (`go test -cover`, `pytest --cov`, …).
- Auto-apply / auto-PR.
- Language-specific AST plugins.
- Idea voting / ranking UI.
- Multi-repo targets (only `cfg.ProjectRoot`).

## 12. Open Questions — RESOLVED

1. **Table name.** Extend existing `insight_proposals` (`db/schema.go:341`). No new table.
2. **Wiki FK target.** `wiki_pages(id) TEXT` (`db/schema.go:772-773`). `insight_proposals.wiki_page_id TEXT NULL REFERENCES wiki_pages(id)`.
3. **Deprecation marker.** README note only; no schema column. Legacy rows remain readable; frontend filters by new category enum.

## 13. Governance Hardening (MUST be honored in implementation)

1. **Server-authoritative idempotency hash.** API MUST compute `idempotency_hash` server-side from `(category, normalized_title, sorted signal_refs)`. Reject any client-supplied `idempotency_hash` field (400) — prevents forged-collision attacks on `last_seen_at`.
2. **Per-call LLM token budget.** Generators never call LLM directly. `LLMJudge.Score` receives `perCallCap = min(remaining, ScoringWeights.MaxTokensPerJudgeCall)` and passes it as max_tokens to the client. Track `cycleTokensUsed` atomically; abort cycle when remaining < minimum per-call floor.
3. **PII / secret scrubbing in baseline.** Before `LLMJudge.Score`, baseline bundle passes through a redaction pass: drop lines matching secret regexes (API keys, tokens, `.env` content, private keys, AWS/GCP creds). Reuse existing scrubber if present, else new `baseline/redact.go`. Applies to file snippets, TODO lines, commit messages.
4. **Atomic dual-write via ON CONFLICT.** Replace SELECT-then-INSERT with single `INSERT ... ON CONFLICT(idempotency_hash) DO UPDATE SET last_seen_at = excluded.last_seen_at RETURNING id`. Eliminates race under concurrent cycles. `BEGIN IMMEDIATE` retained for wiki + proposal co-write.
5. **Config validation (hard).** `ScoringWeights` validated at load: each field ∈ [0,1], sum of LLM-group weights ≤ 1.0, sum of static-group weights ≤ 1.0. Invalid → `config.Load()` returns error (per `.claude/rules/config-validation.md` — never silently normalize).
6. **Sentinel errors.** `var ErrTokenCapRequired = errors.New("evolution: MaxTokensPerCycle must be > 0")` and similar exported in `internal/insight/evolution_loop` for `errors.Is` checks.
7. **Delete deprecated applyFn.** Remove `AutoApplyThreshold` callback paths outright from evolution loop — no dead code. Config field retained for backward compat but marked `// Deprecated: ignored; evolution runs in proposals-only mode` and unused.
