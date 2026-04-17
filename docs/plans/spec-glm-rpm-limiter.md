# Spec: Min-request-interval spacing for LLM clients

## Problem

Z.ai GLM Coding package enforces a Fair Usage Policy that blocks models (observed on GLM-5.1 and GLM-4.7) when cumulative traffic over time is too high — not just parallel concurrency. The existing `concurrency: 1` semaphore (`internal/insight/llm/semaphore.go`) correctly caps parallelism, but sequential bursts (e.g. onboarding looping through 100+ wiki pages, insight hourly tick firing 10–40 subsystem analyses) still fire one request right after another at full speed. That tight serial flow triggered an account-level block on GLM-5.1 / GLM-4.7. The user does not know their exact RPM ceiling from z.ai, so they need a simple way to bound throughput without having to calibrate a number they cannot verify.

## Goal

Add a `min_request_interval_ms` field to `LLMConfig` that enforces a minimum delay between the **start** of consecutive LLM requests, shared process-wide per `(provider, baseURL)` — the same interning key as the concurrency semaphore. This yields a predictable upper bound on throughput: `~1 request / interval`. `0` = disabled (default, backward compatible).

## Non-goals

- Token-bucket / RPM limiter with burst parameter (more code, requires user to know their limit).
- Cross-process coordination (single `stratus serve` owns all LLM traffic; MCP is a thin HTTP proxy per `CLAUDE.md`).
- TPM (tokens-per-minute) gating.

## Success criteria (Principle 4)

1. `go test ./internal/insight/llm/...` passes, including new tests.
2. `go build ./cmd/stratus` succeeds.
3. With `min_request_interval_ms: 3000` set globally, 5 back-to-back calls from two different `llm.Client` instances (same `provider|baseURL`) take **≥ 12 seconds** (first call immediate, then 4× ≥ 3s spacing). Verifiable via real-time test with tolerance.
4. `min_request_interval_ms: 0` (or absent) preserves current behavior — 5 back-to-back calls complete in < 100ms against a fake inner client (no delay added).
5. Context cancellation during a spacing wait returns the context error and does **not** consume the inner call.
6. API validation rejects values outside `[0, 60000]` with HTTP 400.

## Budget (Principle 2)

- **~80 LOC production code** across 4 files (+ wrapper changes).
- **~100 LOC tests.**
- **No new dependencies.**
- One input field per existing LLM config block in `Settings.svelte`.

## Design

### Wrapper integration

Embed the spacing logic inside the existing `semaphoreClient` (not a new wrapper). Reason: the spacing state (`lastCallUnixNano`) logically co-owns the same `(provider, baseURL)` key as the semaphore, and sharing one struct avoids two map lookups per call. Karpathy Principle 2 and 3 — minimum new surface, surgical change to one existing file.

```go
type semaphoreClient struct {
    inner       Client
    sem         *semaphore.Weighted  // existing; nil when Concurrency <= 0
    minInterval time.Duration        // NEW; 0 = disabled
    lastCall    *atomic.Int64        // NEW; unix nano of last request start; shared pointer (see below)
}
```

`lastCall` must be shared across all `semaphoreClient` instances that share the same `(provider, baseURL)` — otherwise each subsystem's wrapper would track its own `lastCall` independently and spacing would not compose. Implementation: extend the interning map from `*semaphore.Weighted` to a small struct `providerGate { sem *semaphore.Weighted; lastCall *atomic.Int64 }`. The map `providerSemaphores` is renamed to `providerGates` and both handles are looked up together.

### Config field

- `config/config.go` → `LLMConfig`: add `MinRequestIntervalMs int \`json:"min_request_interval_ms,omitempty"\`` after `Concurrency`.
- `internal/insight/llm/config.go` → `Config`: add `MinRequestIntervalMs int` (same name, no JSON tag needed since `llm.Config` is internal).
- `WithEnv()` — no env override needed; not a secret.

### Propagation

Every site that converts `config.LLMConfig → llm.Config` must pass `MinRequestIntervalMs`:
1. `cmd/stratus/main.go:218` (guardian)
2. `cmd/stratus/main.go:1495` (onboard CLI)
3. `insight/engine.go:163` (main insight client)
4. `insight/engine.go:350` (code analyst)
5. `api/routes_onboarding.go:133`
6. `api/routes_guardian.go:132`
7. `api/routes_llm.go:130`

Seven one-line additions. Grep confirms this list is complete.

### Behaviour

In `semaphoreClient.Complete(ctx, req)`:

1. `Acquire(ctx, 1)` on semaphore (unchanged).
2. `defer sem.Release(1)`.
3. If `c.minInterval > 0` and `c.lastCall != nil`:
   - Load `last := c.lastCall.Load()`.
   - `elapsed := time.Now().UnixNano() - last`.
   - If `elapsed < minInterval.Nanoseconds()`: sleep `minInterval - elapsed` with `ctx.Done()` early-exit.
4. `c.lastCall.Store(time.Now().UnixNano())` **before** calling inner — guarantees monotonic progress even when calls overlap in flight (concurrency > 1 would benefit).
5. Return `c.inner.Complete(ctx, req)`.

Retry semantics unchanged: retries happen **inside** the semaphore slot (retry wrapper is inner), so each retry sees the spacing logic only if we restructure — but we don't need that. The `lastCall.Store` before the inner call means a retry loop inside will not re-consume spacing (retries happen within a single `Complete()` execution). That's fine because the retry wrapper does its own backoff already; we don't need to double-space retries.

### API validation

`api/llm_validation.go:validateLLMConfig`:
- Add `MinRequestIntervalMs` to the `isZero` short-circuit check (so empty configs still pass).
- Add `Concurrency` to the same check (pre-existing gap, documented in original exploration).
- Add bounds: `0 <= MinRequestIntervalMs <= 60000` (max 60s between requests — any higher is almost certainly a misconfiguration).

### Frontend

`frontend/src/routes/Settings.svelte` — add one `<input type="number" min="0" max="60000" step="500">` labelled `Min request interval (ms)` next to the existing `Concurrency` field in each of the 3 LLM config blocks (global, insight, guardian). Help text:
`"Minimum milliseconds between consecutive requests (0 = disabled). Use to stay under provider rate limits."`

TypeScript types in `frontend/src/lib/types.ts`: add `min_request_interval_ms?: number` to the `LLMConfig` shape if one exists.

### No-op when disabled

When `MinRequestIntervalMs <= 0`: `lastCall` stays nil (or we simply skip the check branch). Zero extra cost on the hot path — single pointer comparison.

## Tasks

Zero-indexed, execution order:

0. **Add field + propagate.**
   - `config/config.go:95`: `MinRequestIntervalMs int` with JSON tag.
   - `internal/insight/llm/config.go`: same field in `llm.Config`.
   - All 7 call sites (listed above): add one-line `MinRequestIntervalMs: cfg.MinRequestIntervalMs` (or equivalent local name).

1. **Extend semaphore wrapper.**
   - Rename `providerSemaphores` → `providerGates`; change value type to `*providerGate { sem *semaphore.Weighted; lastCall *atomic.Int64 }`.
   - Extend `newSemaphoreClient(inner, cfg)` to populate both `sem` and `lastCall` (allocated once per key via `LoadOrStore`).
   - Extend `Complete` with the spacing wait (context-aware).
   - Keep the `Concurrency <= 0 && MinRequestIntervalMs <= 0` fast path as a no-op (no wrapping needed).

2. **Tests** (`internal/insight/llm/semaphore_test.go` — extend existing):
   - `TestSpacing_Zero_NoDelay` — `MinRequestIntervalMs=0`, 5 calls < 100ms.
   - `TestSpacing_EnforcesMinInterval` — `MinRequestIntervalMs=200`, 3 calls, assert elapsed ≥ 400ms.
   - `TestSpacing_SharedAcrossClients` — two clients same `(provider, baseURL)`, interleaved calls, both share `lastCall`.
   - `TestSpacing_ContextCancelled_DuringWait` — cancel ctx while waiting, assert `ctx.Err()` returned and inner not called.
   - Use real time with small intervals (200ms) and tolerance; no fake clock needed.

3. **API validation.**
   - `api/llm_validation.go:validateLLMConfig`: add `Concurrency` + `MinRequestIntervalMs` to `isZero` check; add bounds `MinRequestIntervalMs ∈ [0, 60000]`.

4. **Frontend.**
   - `frontend/src/routes/Settings.svelte`: add input in global / insight / guardian LLM blocks (and code-analysis if it has one).
   - `frontend/src/lib/types.ts`: add `min_request_interval_ms?: number` to `LLMConfig` if type exists.

5. **Build + verify.**
   - `go test ./internal/insight/llm/...`
   - `go build ./cmd/stratus`
   - `cd frontend && npm run check && npm run build`

## Rollout

After shipping: user sets `"min_request_interval_ms": 3000` in each LLM block of `.stratus.json`. Result: max ~20 requests/min globally across all subsystems → safely under any reasonable GLM package RPM ceiling.

## Risks & mitigations

- **Monotonic-clock edge case on very fast sequential calls**: `atomic.Int64` of unix-nano is fine on modern Linux (clock monotonic).
- **Shared `lastCall` with concurrency > 1**: multiple concurrent holders all update `lastCall` at start — the latest write wins, which is exactly the desired semantic (don't double-delay).
- **Retry inside spacing slot**: retries skip spacing (already explained above). Correct — retry wrapper's own backoff handles 429s.
