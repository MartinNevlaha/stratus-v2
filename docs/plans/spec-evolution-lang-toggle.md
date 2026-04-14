# Plan: SK/EN Language Toggle for Evolution & Code Quality Output

## Goal
Let the user pick SK or EN for human-readable output produced by the Evolution loop (hypotheses, descriptions, LLM-generated text) and Code Quality / Guardian (alert messages). Global setting in Settings tab; past results untouched.

## Design

### Single source of truth
Add top-level `Language string` to `config.Config` (default `"en"`, allowed `"sk"|"en"`). Both Evolution and Guardian read from this single field — no duplication.

### Config + API
- `config/config.go`: add `Language` field with JSON tag `language`, default `"en"`.
- Expose via a small `/api/config/language` endpoint (GET + PUT) that validates the enum per `.claude/rules/config-validation.md`.
- Invalid value → 400.

### Evolution output
- `internal/insight/prompts/prompts.go`: add `WithLanguage(lang string)` helper that appends a locale directive (`"Respond in Slovak."` / `"Respond in English."`) to the system prompt. Call sites in `hypothesis.go` and `experiment.go` thread the current language through.
- `internal/insight/evolution_loop/hypothesis.go`: seed descriptions become a `map[string]map[string]string` keyed by `lang → id → description`. Unknown lang falls back to `"en"` (log warning).
- `insight.Engine` / evolution loop receives language from the config at run start (no per-trigger override).

### Guardian alerts
- `guardian/checks.go`: extract alert message templates into a `map[lang]map[alertKey]string`. Missing key → fallback to EN + log.
- Alert generation reads language from config at check time.

### Frontend
- `frontend/src/lib/types.ts`: add `language: "sk"|"en"` field.
- `frontend/src/lib/api.ts`: `getLanguage()` / `setLanguage(lang)` helpers hitting new endpoint.
- `frontend/src/routes/Settings.svelte`: add a small "Output Language" select at the top (General section), saves on change, shows toast.
- `frontend/src/lib/store.svelte.ts`: add `language` to AppState; hydrate on app load.

### Tests (TDD per `.claude/rules/tdd-requirements.md`)
- `config/config_test.go` — default is `"en"`; `"de"` rejected at validation boundary.
- `api/routes_config_language_test.go` — PUT accepts `sk`/`en`, rejects others with 400.
- `internal/insight/prompts/prompts_test.go` — `WithLanguage("sk")` appends Slovak directive; unknown lang → English.
- `internal/insight/evolution_loop/hypothesis_test.go` — seed lookup returns SK descriptions when lang=sk, falls back to EN otherwise.
- `guardian/checks_test.go` — localized message assertion for `stale_workflow` in both languages.
- Frontend: `npm run check` passes.

### Out of scope
- Translating already-stored hypotheses/alerts.
- Locale beyond SK/EN.
- i18n of the dashboard UI chrome itself (only the generated *output* text).

## Tasks
1. Backend: add `config.Language` + validation + `/api/config/language` GET/PUT endpoint with tests.
2. Backend: prompt builder `WithLanguage` + thread language through evolution loop (hypothesis seeds + LLM prompts) with tests.
3. Backend: Guardian alert message templates localized by language with tests.
4. Frontend: Settings UI language selector, store, api client wiring.
5. Verify: run `go test ./...` and `cd frontend && npm run check`; code review.
