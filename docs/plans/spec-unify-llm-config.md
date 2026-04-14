# Spec — Unify Insight and Guardian LLM configuration

**Workflow:** `spec-unify-llm-config`
**Target version:** v0.9.21 (reads legacy, writes nested) → v0.10.0 (drops legacy)

## Problem

Stratus v2 dashboard Settings has two separate LLM config sections:

- **Insight LLM** — nested `LLMConfig` struct at `config.InsightConfig.LLM`. Multi-provider (zai, anthropic, openai) via internal `llm.Client`. Used by ProductIntelligence, WikiEngine, WikiSynthesizer, EvolutionLoop.
- **Guardian LLM** — flat fields (`LLMEndpoint`, `LLMAPIKey`, `LLMModel`, `LLMTemperature`, `LLMMaxTokens`) on `GuardianConfig`. Bespoke OpenAI-compatible HTTP client at `guardian/llm.go`. Used for governance violation analysis.

Two separate data shapes, two clients, two Settings cards, duplicate logic.

## Goal

- **One shared `LLMConfig`** struct everywhere (already exists — reuse as-is).
- **Guardian reuses `llm.Client`** (same abstraction Insight uses).
- **UI**: one **Global LLM** card at the top, Insight/Guardian each gain an "Override global LLM" toggle. Remove the Guardian "OpenAI-compatible endpoint" card.
- **Migration shim** reads legacy flat fields on load, writes nested shape on save.

## Design decisions

1. **Global endpoint**: new `GET/PUT /api/llm/config` in `api/routes_llm.go` backed by `cfg.LLM`.
2. **Override semantics**: zero-value subsystem `LLMConfig` = inherit global; any non-zero field = field-level override. Already implemented by `ResolveLLMConfig`.
3. **Guardian construction**: `main.go` calls `ResolveLLMConfig(cfg.LLM, cfg.Guardian.LLM)`, builds `llm.NewClient`, injects via `g.SetLLMClient`. No fallback inside `runChecks`.
4. **Regression tests**: port transport-level cases (string-shaped error, trailing slash, empty choices, bad-gateway parsing) from `guardian/llm_test.go` into `internal/insight/llm/openai_client_test.go`. Delete `guardian/llm.go`.
5. **Migration window**: v0.9.21 reads both shapes and writes nested; v0.10.0 removes the legacy read path.
6. **Secret masking**: `"***"` sentinel on GET across global/insight/guardian; PUT restores stored key when incoming is `""` or `"***"`.
7. **Bounds validation** on every PUT: `Temperature ∈ [0, 2]`, `MaxTokens ∈ (0, 65536]`, `Timeout ∈ (0, 600s]`, `Provider ∈ {"", "zai", "anthropic", "openai", "ollama"}` (empty allowed for zero-value subsystem overrides). Returns 400 with specific field errors.

## Risks

1. `.stratus.json` shape change — mitigated by load-time legacy shim + migration test.
2. `api/routes_orchestration.go:226` still calls `guardian.NewLLMClient`. **Must cut over before deleting `guardian/llm.go`.**
3. Startup ordering — guardian client created after `api.NewServer`; inject via new `SetGuardianLLM` setter (matches existing `SetGuardian` pattern).
4. Sentinel-round-trip edge case: override toggled off must not clear a stored global key. Handler rule: only overwrite stored key on an explicit non-sentinel non-empty value.

## Task list (ordered)

| # | Task | Agent | Depends on |
|---|------|-------|------------|
| 1 | Config struct, resolver, migration shim + tests (TDD) | backend-engineer | — |
| 2 | Port regression cases into `internal/insight/llm` tests; prune `guardian/llm_test.go` | backend-engineer | 1 |
| 3 | API: config bounds validation + nested Insight/Guardian handlers + `maskLLMConfig` helper | backend-engineer | 1 |
| 4 | API: new `GET/PUT /api/llm/config` global endpoint | backend-engineer | 3 |
| 5 | `main.go` wiring + `srv.SetGuardianLLM` + orchestration risk-analysis cutover | backend-engineer | 1, 3, 4 |
| 6 | Delete `guardian/llm.go`, remove fallback in `runChecks`, wrap adapter errors | backend-engineer | 2, 5 |
| 7 | Frontend types + API client (`LLMConfig`, nested `GuardianConfig.llm`) | frontend-engineer | 3, 4 |
| 8 | `Settings.svelte` — Global LLM card + override toggles + remove old Guardian LLM card | frontend-engineer | 7 |
| 9 | ADR-001 (`docs/adr/0001-llm-client-unification.md`) + `docs/adr/README.md` | implementation-expert | — |
| 10 | Code review pass against all governance rules | code-reviewer | all |

**Critical ordering:** Task 6 (delete `guardian/llm.go`) must land **after** Task 5 (orchestration cutover), otherwise the build breaks.

## Governance alignment

- `.claude/rules/config-validation.md` — covered by task 3 (bounds validation on all PUT handlers).
- `.claude/rules/tdd-requirements.md` — covered by TDD-first ordering in tasks 1, 3, 4.
- `.claude/rules/error-handling.md` — covered by task 6 (error wrapping in `llm_adapter.go`).
- `.claude/rules/api-parameter-passthrough.md` — covered by task 3 (test-LLM handler passes all LLMConfig fields through).
- `.claude/rules/workflow-governance.md` — workflow registered as `spec-unify-llm-config`, every delegation recorded.
