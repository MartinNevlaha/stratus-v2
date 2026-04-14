# ADR-0001: LLM client unification

**Status:** Accepted
**Date:** 2026-04-10

## Context

Stratus v2 shipped two separate LLM client paths:

1. **Insight** — the `internal/insight/llm` package, a multi-provider client supporting `zai`, `anthropic`, `openai`, and `ollama`. Configured by a nested `config.LLMConfig` struct at `insight.llm` in `.stratus.json`. Consumed by `ProductIntelligence`, `WikiEngine`, `WikiSynthesizer`, and the Evolution loop.

2. **Guardian** — a bespoke OpenAI-compatible HTTP client living in `guardian/llm.go`. Configured by five flat fields (`llm_endpoint`, `llm_api_key`, `llm_model`, `llm_temperature`, `llm_max_tokens`) on `guardian.*` in `.stratus.json`. Consumed by `checkGovernanceViolations` and the orchestration risk-analysis endpoint.

This produced duplication and drift:

- Two different config shapes for the same concept.
- Two separate Settings cards in the dashboard.
- Two distinct transport-layer test suites, with the bespoke client missing coverage for trailing-slash normalization and string-shaped error bodies (both were latent bugs surfaced during this migration).
- Guardian was locked to a single provider while Insight was not.
- `main.go` had a dual wiring path (`ResolveGuardianLLMConfig` + fallback client creation in `runChecks`).

## Decision

Unify on `internal/insight/llm.Client` as the single LLM abstraction across all subsystems.

1. **One struct** — `config.LLMConfig` (nested, provider/model/base_url/api_key/timeout/max_tokens/temperature/max_retries) is the config shape everywhere. `GuardianConfig.LLM` is a `LLMConfig` field; the flat legacy fields are retained only as read-path shims.

2. **One resolver** — `config.ResolveLLMConfig(topLevel, subsystem)` merges the top-level `cfg.LLM` with a subsystem override, with zero-value fields inheriting from the global. `ResolveGuardianLLMConfig` is deleted.

3. **One client construction site** — `cmd/stratus/main.go` builds the shared client via `llm.NewClient(resolved)` and injects it into both `Guardian.SetLLMClient` and `api.Server.SetGuardianLLM`. No subsystem creates its own client.

4. **One adapter** — `guardian/llm_adapter.go` bridges the shared `llm.Client` to the `(systemPrompt, userPrompt) -> string` calling convention that the guardian governance checks already use. The adapter is nil-safe: when no client is injected, `configured()` returns false and LLM-dependent checks fall back to their FTS-only path.

5. **One settings UI** — a single **Global LLM** card in the dashboard plus per-subsystem "Override global LLM" toggles. Zero-value subsystem `LLMConfig` in the PUT payload means "inherit global"; any non-zero field is a field-level override.

6. **One validation helper** — `api/llm_validation.go` exports `validateLLMConfig` (Provider enum, Temperature ∈ [0,2], MaxTokens ∈ [0,65536], Timeout ∈ [0,600], MaxRetries ∈ [0,10]), `maskLLMConfig` (APIKey → `"***"`), and `restoreLLMAPIKey` (preserves stored key when incoming is `""` or `"***"`). All three LLM config PUT handlers (`/api/llm/config`, `/api/insight/config`, `/api/guardian/config`) use the same helpers.

## Consequences

**Gains**

- Guardian now supports all providers Insight does (`zai`, `anthropic`, `openai`, `ollama`) for free.
- One code path to test, one code path to secure, one code path to evolve.
- Two latent bugs in the shared OpenAI client were discovered and fixed while porting regression coverage (trailing-slash URL normalization at `internal/insight/llm/openai.go`; string-shaped `error` field parsing).
- The dashboard no longer asks users "which of these two identical LLM cards should I fill in?".
- Bounds validation is now uniformly enforced — all three PUT endpoints reject out-of-range temperature, max_tokens, timeout, max_retries, and invalid provider values.

**Costs**

- Breaking `.stratus.json` shape change: `guardian.llm_*` flat fields are replaced by `guardian.llm.*`. Mitigated by a load-time migration shim in `config.Load()` that detects legacy fields, populates the nested shape, and zeroes the legacy fields so the next save writes only the new format. Covered by `config/migration_test.go` (four cases: legacy-only, nested-only, both, neither).
- `guardian/llm.go` (bespoke HTTP client) is deleted. Transport-layer regression cases have been ported to `internal/insight/llm/openai_regressions_test.go` so coverage is preserved.
- Callers of the deleted public symbols `guardian.NewLLMClient` and `guardian.TestLLMEndpoint` must migrate. The only in-tree caller (`api/routes_orchestration.go`) has been cut over.

## Migration window

- **v0.9.21** — release containing this change. Reads both the legacy flat `guardian.llm_*` fields and the nested `guardian.llm` shape, writes only the nested shape.
- **v0.10.0** — the `Legacy*` shadow fields and `migrateGuardianLegacyLLM` helper will be removed. Users MUST run v0.9.21 at least once before upgrading to v0.10.0 so their config is migrated in place.

## Rejected alternatives

1. **Keep Guardian's flat fields, change nothing.** Rejected — every bug fix and provider addition would need to land twice, and the dashboard would remain confusing.

2. **Introduce a new top-level `global/llm` package separate from `internal/insight/llm`.** Rejected — `internal/insight/llm` is already provider-agnostic and well-tested. The name is a historical accident, not a design statement; renaming it would be churn for no gain.

3. **Keep the Guardian client but route it through `internal/insight/llm` under the hood.** Rejected — this would preserve the duplicate config shape and API surface indefinitely. The whole point is to collapse the shapes.

4. **Require every subsystem to have its own fully-specified LLM config (no global inheritance).** Rejected — forces users to copy-paste the same provider/model across three cards. The "zero value inherits global" semantic of `ResolveLLMConfig` is the right default.
