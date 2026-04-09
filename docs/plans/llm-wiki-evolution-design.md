# Technical Design: LLM Integration for Wiki Engine & Evolution Loop + Obsidian Skills

## Overview

Integrate a unified local LLM (via OpenAI-compatible endpoint) across Stratus subsystems and introduce an embedded prompt library based on obsidian-skills.

## Component Changes

| Component | Current State | Change |
|-----------|--------------|--------|
| `internal/insight/llm/` | Mature client with OpenAI/Anthropic/ZAI/Ollama support | Add `BudgetedClient` middleware wrapper |
| `config/config.go` | LLM config nested inside `InsightConfig.LLM`; Guardian has flat `LLM*` fields | Add top-level `Config.LLM`, subsystems inherit with overrides |
| `guardian/llm.go` | Duplicated OpenAI HTTP client (149 LOC) | Replace with adapter to `llm.Client` |
| `guardian/guardian.go` | Creates `llmClient` per check run from flat config fields | Accept `llm.Client` in constructor |
| `internal/insight/wiki_engine/` | Uses `LLMClient` interface for page generation | Compose obsidian-markdown prompt into system prompts |
| `internal/insight/evolution_loop/` | No LLM; static `seedHypotheses` map, simulated experiments | Accept `llm.Client`; LLM-powered hypothesis generation and experiment evaluation |
| `internal/insight/prompts/` (new) | Does not exist | New package: embedded prompt library with obsidian-skills content |
| `insight/engine.go` | Wires wiki + evolution; does not pass LLM to evolution loop | Pass `llm.Client` to evolution loop |
| `db/schema.go` | No token usage tracking | Add `llm_token_usage` table |

## API Contract

### New Endpoints

**GET /api/llm/status**
```json
{
  "configured": true,
  "provider": "ollama",
  "model": "llama3.1",
  "daily_budget": 100000,
  "daily_used": 23400,
  "daily_remaining": 76600,
  "reset_at": "2026-04-10T00:00:00Z"
}
```

**GET /api/llm/usage**
```
Query params: ?days=7 (default 7, max 90)
Validation:
  - days must be integer >= 1 and <= 90
  - days <= 0, non-integer, or > 90 → 400 {"error": "days must be between 1 and 90"}
```
```json
{
  "usage": [
    {
      "date": "2026-04-09",
      "subsystem": "wiki_engine",
      "input_tokens": 12000,
      "output_tokens": 3400,
      "requests": 5
    }
  ],
  "total_tokens": 15400
}
```

**POST /api/llm/test**
```
Request: {} (empty — tests current config)
Validation: no body fields required; endpoint returns current config status
Response: { "ok": true, "latency_ms": 245, "model": "llama3.1" }
Errors:
  - 503 {"error": "llm endpoint unreachable: <detail>"} (connection refused, timeout)
  - 500 {"error": "llm test: <detail>"} (internal error)
```

**Note:** `GET /api/llm/status` MUST NOT include `api_key` in the response. API keys are never exposed via any status/usage/config GET endpoint.

### Modified Endpoints

No shape changes to existing endpoints. `GET/POST /api/evolution/config` already has `daily_token_budget`. Guardian config endpoints remain unchanged for backward compatibility.

## Data Model

### New table: `llm_token_usage`

```sql
CREATE TABLE IF NOT EXISTS llm_token_usage (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    date          TEXT    NOT NULL,  -- YYYY-MM-DD
    subsystem     TEXT    NOT NULL,  -- 'wiki_engine', 'evolution_loop', 'guardian', 'synthesizer'
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    requests      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_usage_date_sub
    ON llm_token_usage(date, subsystem);
```

Upsert pattern: `INSERT ... ON CONFLICT(date, subsystem) DO UPDATE SET input_tokens = input_tokens + ?, output_tokens = output_tokens + ?, requests = requests + 1`.

No changes to existing `wiki_pages`, `evolution_runs`, or `evolution_hypotheses` tables. The `evolution_hypotheses.evidence_json` field stores LLM-generated rationale as a new key `"llm_rationale"`.

## Config Schema Changes

### New top-level `LLM` field

```go
type Config struct {
    // ... existing fields ...
    LLM       LLMConfig       `json:"llm"`       // NEW: top-level default
    Insight   InsightConfig   `json:"insight"`
    Wiki      WikiConfig      `json:"wiki"`
    Evolution EvolutionConfig `json:"evolution"`
}
```

### JSON example (.stratus.json)

```json
{
  "llm": {
    "provider": "ollama",
    "model": "llama3.1",
    "base_url": "http://localhost:11434/v1",
    "timeout": 120,
    "max_tokens": 16384,
    "temperature": 0.7,
    "max_retries": 2
  },
  "insight": {
    "enabled": true,
    "llm": {}
  },
  "guardian": {
    "enabled": true
  }
}
```

### Resolution order (per subsystem)

1. Subsystem-specific LLM config (e.g., `insight.llm`, guardian flat fields) — if all required fields (provider + model) are set
2. Top-level `config.LLM` — default for all subsystems
3. `llm.DefaultConfig()` + env vars (`LLM_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`)

**LLM Config Validation Bounds (`llm.Config.Validate()`):**

| Field | Type | Min | Max | Default | Notes |
|-------|------|-----|-----|---------|-------|
| `provider` | enum | — | — | `""` | Must be one of: `""` (disabled), `"openai"`, `"ollama"`, `"anthropic"`, `"zai"` |
| `model` | string | — | — | `""` | Required when provider is set |
| `temperature` | float | 0.0 | 2.0 | 0.7 | Reject values outside [0, 2] |
| `max_tokens` | int | 1 | 131072 | 16384 | Reject values outside [1, 131072] |
| `timeout` | int (sec) | 1 | 600 | 120 | Reject values outside [1, 600] |
| `max_retries` | int | 0 | 10 | 2 | Reject values outside [0, 10] |
| `base_url` | string | — | — | per provider | Must be valid URL if set |

Validation runs at startup (`config.Load()`). Invalid values cause `fmt.Errorf("llm config: <field> must be between <min> and <max>, got <value>")` and the server refuses to start.

**Backward compatibility:** Existing `.stratus.json` files that set `insight.llm` or `guardian.llm_*` fields continue to work unchanged.

## Token Budget Governor

### Interface

```go
// internal/insight/llm/budget.go

type Priority int

const (
    PriorityLow    Priority = 0  // evolution hypothesis generation
    PriorityMedium Priority = 1  // wiki page generation, maintenance
    PriorityHigh   Priority = 2  // user-initiated queries, guardian checks
)

type BudgetedClient struct {
    inner      Client
    db         BudgetStore
    dailyLimit int          // 0 = unlimited
    mu         sync.Mutex
}

type BudgetStore interface {
    GetDailyTokenUsage(date string, subsystem string) (input, output int, err error)
    GetDailyTokenUsageTotal(date string) (input, output int, err error)
    RecordTokenUsage(date, subsystem string, input, output int) error
}

func NewBudgetedClient(inner Client, store BudgetStore, dailyLimit int) *BudgetedClient

// Complete checks remaining budget, calls inner.Complete, records usage.
// High priority always proceeds (budget is advisory, not hard-block).
func (b *BudgetedClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)

// CompleteWithPriority allows callers to specify priority and subsystem.
func (b *BudgetedClient) CompleteWithPriority(ctx context.Context, req CompletionRequest, priority Priority, subsystem string) (*CompletionResponse, error)

var ErrBudgetExhausted = errors.New("llm: daily token budget exhausted")

// Allowed subsystem values — reject unknown subsystems at construction time.
var AllowedSubsystems = map[string]bool{
    "wiki_engine":     true,
    "evolution_loop":  true,
    "guardian":        true,
    "synthesizer":     true,
    "product_intel":   true,
    "unknown":         true,  // fallback for untagged Complete() calls
}
```

### SubsystemClient adapter

```go
type SubsystemClient struct {
    inner     *BudgetedClient
    subsystem string
    priority  Priority
}

func NewSubsystemClient(inner *BudgetedClient, subsystem string, priority Priority) *SubsystemClient

// Satisfies llm.Client and wiki_engine.LLMClient interfaces
func (s *SubsystemClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
func (s *SubsystemClient) Provider() string
func (s *SubsystemClient) Model() string
```

## Prompt Library

### Package: `internal/insight/prompts/`

```go
package prompts

import _ "embed"

//go:embed obsidian_markdown.md
var ObsidianMarkdown string

const (
    WikiPageGeneration = `You are a technical wiki author for a software project knowledge base.
Generate well-structured markdown wiki pages. Use Obsidian-compatible syntax.`

    WikiSynthesis = `You are a knowledge synthesizer. Given wiki pages, produce a markdown answer
with inline citations using [source_type:source_id] format.`

    HypothesisGeneration = `You are an autonomous improvement engine for a software development workflow system.
Analyze the provided metrics and patterns to generate testable hypotheses for system optimization.
Return a JSON array of hypothesis objects.`

    ExperimentEvaluation = `You are a scientific evaluator for A/B experiment results.
Compare the baseline and proposed configurations, considering statistical significance
and practical impact. Return a JSON object with your assessment.`
)

func Compose(parts ...string) string {
    return strings.Join(parts, "\n\n")
}
```

### Obsidian-skills content

File `internal/insight/prompts/obsidian_markdown.md` contains content from https://github.com/kepano/obsidian-skills `obsidian-markdown/SKILL.md` (MIT license). Key sections:
- Wikilink syntax: `[[page name]]`, `[[page name|display text]]`
- YAML frontmatter rules
- Callout blocks: `> [!note]`, `> [!warning]`, etc.
- Tag syntax: `#tag` in content, `tags:` in frontmatter

### Prompt composition mapping

| Callsite | Current prompt | New prompt |
|----------|---------------|------------|
| `WikiEngine.GeneratePageFromData()` (engine.go:258) | `"You are a technical wiki author..."` | `prompts.Compose(prompts.WikiPageGeneration, prompts.ObsidianMarkdown)` |
| `Synthesizer.SynthesizeAnswer()` (synthesizer.go:64) | `"You are a knowledge synthesizer..."` | `prompts.Compose(prompts.WikiSynthesis, prompts.ObsidianMarkdown)` |
| `Synthesizer.GeneratePageContent()` (synthesizer.go:101) | `"You are a technical documentation writer..."` | `prompts.Compose(prompts.WikiPageGeneration, prompts.ObsidianMarkdown)` |

## Guardian LLM Client Unification

### Adapter: `guardian/llm_adapter.go`

```go
package guardian

type llmAdapter struct {
    client llm.Client
}

func newLLMAdapter(client llm.Client) *llmAdapter

func (a *llmAdapter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
    resp, err := a.client.Complete(ctx, llm.CompletionRequest{
        SystemPrompt: systemPrompt,
        Messages:     []llm.Message{{Role: "user", Content: userPrompt}},
    })
    if err != nil {
        return "", fmt.Errorf("guardian llm adapter: %w", err)
    }
    return resp.Content, nil
}
```

**Migration path:**
1. `Guardian` struct accepts optional `llm.Client` in constructor
2. In `runChecks()`, prefer injected client via adapter; fall back to creating `llmClient` from config fields if not injected
3. Mark `guardian.NewLLMClient()` and `guardian.TestLLMEndpoint()` as deprecated

## Evolution Loop LLM Integration

### Constructor changes

```go
// Current:
func NewEvolutionLoop(store EvolutionStore, configFn func() config.EvolutionConfig) *EvolutionLoop

// New:
func NewEvolutionLoop(store EvolutionStore, configFn func() config.EvolutionConfig, llmClient llm.Client) *EvolutionLoop
```

Same pattern for `NewHypothesisGenerator(store, llmClient)` and `NewExperimentRunner(llmClient)`.

### HypothesisGenerator with LLM

When LLM is available:
1. Gather context: recent run results, current config values, recent event patterns
2. Send to LLM with `prompts.HypothesisGeneration` system prompt
3. Parse JSON response into `[]db.EvolutionHypothesis`
4. Merge with seed hypotheses (deduplication by description similarity)
5. On LLM error: log warning, fall back to `seedHypotheses` map

### ExperimentRunner with LLM

When LLM is available and category is `prompt_tuning`:
1. Generate test scenario relevant to the hypothesis
2. Run baseline prompt through LLM, score the result
3. Run proposed prompt through LLM, score the result
4. Return `ExperimentResult` with real metric comparison

For non-prompt-tuning categories: continue using simulated baselines.

### Wiring in `insight/engine.go`

```go
func (e *Engine) initEvolutionLoop() {
    evoStore := evolution_loop.NewDBEvolutionStore(e.database)
    evoCfg := e.evoCfg
    e.evolutionLoop = evolution_loop.NewEvolutionLoop(evoStore, func() config.EvolutionConfig { return evoCfg }, e.llmClient)
}
```

## Sequence Diagrams

### LLM Client Resolution at Startup

```
Main → config.Load() → Config{LLM, Insight, Guardian, ...}
Main → llm.NewClient(cfg.LLM) → baseClient
Main → NewBudgetedClient(baseClient, db, dailyBudget) → budgetedClient
Main → NewEngineWithConfig(db, cfg, budgetedClient) → passes to WikiEngine, Synthesizer, EvolutionLoop
Main → guardian.New(db, ..., budgetedClient) → uses adapter instead of guardian.llmClient
```

### Evolution Loop with LLM

```
API → Engine.RunEvolutionCycle() → Loop.Run()
  → HypothesisGenerator.Generate()
    → [LLM available?] LLM.Complete(hypothesis prompt) → parse JSON → merge with seeds
    → [LLM unavailable?] seedHypotheses map (current behavior)
  → for each hypothesis:
    → ExperimentRunner.Execute()
      → [prompt_tuning + LLM?] run baseline & proposed prompts, compare
      → [else] categoryBaselines lookup (current behavior)
    → Evaluator.Evaluate() → decision + confidence
    → DB.UpdateHypothesis()
```

## Error Handling

| Error | Source | Handling |
|-------|--------|----------|
| `ErrBudgetExhausted` | `BudgetedClient` | Evolution: hypothesis marked `inconclusive`. Wiki: ingest skipped (fail-open). |
| LLM connection failure | `llm.Client` | Wrapped with context. Evolution: hypothesis skipped. Wiki: returns error. Guardian: FTS-only fallback. |
| JSON parse error from LLM | `HypothesisGenerator` | Falls back to seed hypotheses |
| Budget DB write failure | `BudgetStore` | Logged with full context (`fmt.Errorf("record token usage date=%s subsystem=%s: %w", ...)`) at WARN level. Not propagated — LLM call still succeeds. |
| Invalid config | `llm.Config.Validate()` | Server refuses to start with clear error |

## Test Strategy

**Coverage target:** >= 80% line coverage per `.claude/rules/tdd-requirements.md`.

### Test Doubles Required

| Double | Type | Used By |
|--------|------|---------|
| `mockLLMClient` | `llm.Client` interface mock | `BudgetedClient`, `SubsystemClient`, `llmAdapter`, `HypothesisGenerator`, `ExperimentRunner`, `WikiEngine`, `Synthesizer` |
| `mockBudgetStore` | `BudgetStore` interface mock | `BudgetedClient` tests |
| `mockWikiStore` | `WikiStore` interface mock | Wiki engine tests |
| `mockEvolutionStore` | `EvolutionStore` interface mock | Evolution loop tests |

### Mandatory Test Scenarios

**BudgetedClient (critical path — budget enforcement):**
- `TestBudgetedClient_UnderBudget` — call proceeds, usage recorded
- `TestBudgetedClient_BudgetExhausted_LowPriority` — returns `ErrBudgetExhausted`
- `TestBudgetedClient_BudgetExhausted_HighPriority` — call proceeds (advisory budget)
- `TestBudgetedClient_InnerError` — error wrapped with context
- `TestBudgetedClient_RecordUsageFailure` — LLM call succeeds, error logged

**SubsystemClient (critical path — delegation):**
- `TestSubsystemClient_InvalidSubsystem` — panics or returns error at construction
- `TestSubsystemClient_DelegatesToBudgeted` — correct priority and subsystem forwarded

**llmAdapter (critical path — gateway):**
- `TestLLMAdapter_Complete` — translates (system, user) to CompletionRequest
- `TestLLMAdapter_Error` — error wrapped with "guardian llm adapter:" context
- `TestLLMAdapter_NotConfigured` — returns descriptive error

**HypothesisGenerator with LLM:**
- `TestHypothesisGenerator_WithLLM` — parses LLM JSON response into hypotheses
- `TestHypothesisGenerator_LLMError_FallsBackToSeeds` — graceful degradation
- `TestHypothesisGenerator_WithoutLLM` — uses seed hypotheses (existing behavior)

**ExperimentRunner with LLM:**
- `TestExperimentRunner_PromptTuning_WithLLM` — real A/B comparison
- `TestExperimentRunner_NonPromptTuning` — simulated baselines (existing behavior)

**Config validation:**
- `TestLLMConfig_Validate_TemperatureOutOfRange` — rejects temperature > 2.0 or < 0
- `TestLLMConfig_Validate_InvalidProvider` — rejects unknown provider
- `TestLLMConfig_Validate_MaxTokensBounds` — rejects 0 or > 131072

**API endpoints:**
- `TestGetLLMUsage_InvalidDays` — returns 400
- `TestGetLLMStatus_NoAPIKeyInResponse` — api_key never exposed

## Implementation Order

1. Create `internal/insight/prompts/` package with embedded obsidian-markdown content
2. Add `llm_token_usage` table to `db/schema.go` + DB methods
3. Implement `BudgetedClient` and `SubsystemClient` in `internal/insight/llm/`
4. Add top-level `Config.LLM` field with resolution logic
5. Create guardian adapter (`guardian/llm_adapter.go`)
6. Update `evolution_loop` constructors to accept `llm.Client`
7. Update `wiki_engine` system prompts to use `prompts.Compose()`
8. Update `insight/engine.go` wiring
9. Add `/api/llm/*` endpoints
10. Add tests at each step (TDD per project rules)

## Breaking Changes

1. `evolution_loop.NewEvolutionLoop()` — adds `llm.Client` parameter (1 callsite: `insight/engine.go:233`)
2. `evolution_loop.NewHypothesisGenerator()` — adds `llm.Client` parameter (1 callsite: `loop.go:147`)
3. `evolution_loop.NewExperimentRunner()` — adds `llm.Client` parameter (1 callsite: `loop.go:148`)
4. `guardian.New()` — add optional `llm.Client` via functional options or `WithLLMClient()` method
5. `config.Config` — adds `LLM LLMConfig` field (additive, backward-compatible)
6. No API breaking changes — all new endpoints are additive

## License

obsidian-skills content is MIT licensed (https://github.com/kepano/obsidian-skills). Attribution included in embedded file.

## Key Files

- `internal/insight/llm/client.go:36-40` — canonical `Client` interface
- `internal/insight/llm/config.go` — `Config` struct with `Validate()`, `EffectiveBaseURL()`
- `internal/insight/llm/openai.go` — OpenAI-compatible implementation
- `config/config.go:20-29` — `LLMConfig` struct
- `config/config.go:40-54` — `GuardianConfig` with flat LLM fields
- `guardian/llm.go` — duplicate client to deprecate
- `internal/insight/wiki_engine/engine.go:258` — hardcoded system prompt to replace
- `internal/insight/wiki_engine/synthesizer.go:64,101` — hardcoded system prompts
- `internal/insight/evolution_loop/loop.go:34` — constructor to change
- `internal/insight/evolution_loop/hypothesis.go:20-77` — static seeds to augment
- `internal/insight/evolution_loop/experiment.go:26-31` — simulated metrics
- `insight/engine.go:222-234` — wiring for WikiEngine and EvolutionLoop
