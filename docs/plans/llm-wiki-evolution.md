# Implementation Plan: LLM Integration for Wiki Engine & Evolution Loop + Obsidian Skills

**Design doc:** `docs/plans/llm-wiki-evolution-design.md`
**Workflow:** `spec-llm-wiki-evolution`

## Task List

### Wave 1 (no dependencies — parallel)

#### Task 0: Create Prompt Library Package with Embedded Obsidian-Skills
- **Agent:** `delivery-backend-engineer`
- **Create:** `internal/insight/prompts/prompts.go`, `internal/insight/prompts/obsidian_markdown.md`, `internal/insight/prompts/prompts_test.go`
- **Scope:** Constants (`WikiPageGeneration`, `WikiSynthesis`, `HypothesisGeneration`, `ExperimentEvaluation`), `Compose()` helper, embedded obsidian-skills content (MIT, with attribution)

#### Task 1: Add `llm_token_usage` Table and DB Methods
- **Agent:** `delivery-database-engineer`
- **Modify:** `db/schema.go`
- **Create:** `db/llm_usage.go`, `db/llm_usage_test.go`
- **Scope:** `CREATE TABLE llm_token_usage` with upsert pattern, `GetDailyTokenUsage()`, `GetDailyTokenUsageTotal()`, `RecordTokenUsage()`, `GetTokenUsageHistory()`

#### Task 2: Add Top-Level `Config.LLM` with Resolution and Validation
- **Agent:** `delivery-backend-engineer`
- **Modify:** `config/config.go`, `internal/insight/llm/config.go`
- **Create:** `config/llm_resolve.go`, `config/llm_resolve_test.go`, `internal/insight/llm/config_test.go`
- **Scope:** Top-level `LLM` field, resolution order (subsystem > top-level > env), `Validate()` bounds (temperature [0,2], max_tokens [1,131072], timeout [1,600], max_retries [0,10], provider enum)

#### Task 3: Create Guardian LLM Adapter
- **Agent:** `delivery-backend-engineer`
- **Create:** `guardian/llm_adapter.go`, `guardian/llm_adapter_test.go`
- **Modify:** `guardian/guardian.go`
- **Scope:** `llmAdapter` wrapping `llm.Client`, `WithLLMClient()` option, prefer injected client in `runChecks()`, deprecate `NewLLMClient()`

#### Task 4: Update Evolution Loop Constructors to Accept `llm.Client`
- **Agent:** `delivery-backend-engineer`
- **Modify:** `internal/insight/evolution_loop/loop.go`, `hypothesis.go`, `experiment.go`, `*_test.go`, `insight/engine.go`
- **Scope:** Add `llmClient` field, update signatures, pass `nil` everywhere (behavior unchanged), update all test constructor calls

### Wave 2 (after wave 1)

#### Task 5: Implement BudgetedClient Middleware
- **Agent:** `delivery-backend-engineer`
- **Depends on:** Task 1
- **Create:** `internal/insight/llm/budget.go`, `internal/insight/llm/subsystem_client.go`, `internal/insight/llm/budget_test.go`, `internal/insight/llm/subsystem_client_test.go`
- **Scope:** `BudgetedClient`, `SubsystemClient`, `Priority` levels, `AllowedSubsystems`, `ErrBudgetExhausted`, budget check + record usage per call

#### Task 6: Update Wiki Engine Prompts to Use Prompt Library
- **Agent:** `delivery-backend-engineer`
- **Depends on:** Task 0
- **Modify:** `internal/insight/wiki_engine/engine.go` (~line 258), `synthesizer.go` (~lines 64, 101)
- **Scope:** Replace hardcoded system prompts with `prompts.Compose(prompts.WikiPageGeneration, prompts.ObsidianMarkdown)` and `prompts.Compose(prompts.WikiSynthesis, prompts.ObsidianMarkdown)`

#### Task 7: Implement LLM-Powered Hypothesis Generation
- **Agent:** `delivery-backend-engineer`
- **Depends on:** Task 0, Task 4
- **Modify:** `internal/insight/evolution_loop/hypothesis.go`, `hypothesis_test.go`
- **Scope:** `generateWithLLM()` method, JSON parsing of LLM response, merge with seed hypotheses, fallback on LLM error

#### Task 8: Implement LLM-Powered Experiment Runner for `prompt_tuning`
- **Agent:** `delivery-backend-engineer`
- **Depends on:** Task 0, Task 4
- **Modify:** `internal/insight/evolution_loop/experiment.go`, `experiment_test.go`
- **Scope:** A/B prompt comparison via LLM for `prompt_tuning` category, simulated baselines for other categories, `llm_rationale` in `evidence_json`

### Wave 3

#### Task 9: Wire LLM Client Through Engine and Main
- **Agent:** `delivery-backend-engineer`
- **Depends on:** Tasks 2, 3, 4, 5
- **Modify:** `cmd/stratus/main.go`, `insight/engine.go`
- **Scope:** Create `BudgetedClient` at startup, create `SubsystemClient` per subsystem, pass to `NewEngineWithConfig()` and `guardian.New()`

### Wave 4

#### Task 10: Add `/api/llm/*` Endpoints
- **Agent:** `delivery-backend-engineer`
- **Depends on:** Tasks 1, 5, 9
- **Create:** `api/routes_llm.go`, `api/routes_llm_test.go`
- **Modify:** `api/server.go`
- **Scope:** `GET /api/llm/status` (no api_key!), `GET /api/llm/usage` (days validation 1-90), `POST /api/llm/test`

### Wave 5

#### Task 11: Integration Tests and Coverage Validation
- **Agent:** `delivery-qa-engineer`
- **Depends on:** All previous tasks
- **Create:** `internal/insight/llm/integration_test.go`, `insight/engine_llm_integration_test.go`
- **Scope:** Full-path tests (config → BudgetedClient → wiki/evolution with budget tracking), verify >= 80% coverage on all new packages

## Dependency Graph

```
Wave 1 (parallel):  T0  T1  T2  T3  T4
                     |   |   |   |   |
Wave 2:             T6  T5  |   |  T7,T8
                     |   |   |   |   |
Wave 3:             └───T9──┘───┘───┘
                         |
Wave 4:                 T10
                         |
Wave 5:                 T11
```

## Critical Files
- `internal/insight/llm/client.go` — `Client` interface all subsystems consume
- `internal/insight/evolution_loop/loop.go` — breaking constructor change
- `insight/engine.go` — central wiring hub
- `config/config.go` — top-level `Config.LLM` addition
- `guardian/guardian.go` — optional `llm.Client` injection
