# ADR-0004: Onboarding Full-Project Scan

**Status:** Accepted
**Date:** 2026-04-14
**Deciders:** @MartinNevlaha

## Context

The onboarding ingest pipeline had a hard-coded ceiling of **200 pages** per run (`clampPages` in `internal/insight/onboarding/auto_depth.go`). On non-trivial codebases this truncated coverage and left large swaths of the project undocumented, undermining Wiki's role as project second brain.

## Decision

1. **Full-project scan by default.**
   `clampPages` now accepts a `maxCap` parameter. `maxCap == 0` means **unlimited** — no ceiling. Scanning still respects the existing `skipDirs` list (`.git`, `node_modules`, `vendor`, build outputs, etc.) and per-file content caps (`configFileCap=4KB`, `readmeCap=2000 chars`).
2. **Config bounds + 0-unlimited sentinel.**
   `WikiConfig.MaxPagesPerIngest`, `OnboardingMaxPages`, and the new `IngestTokenBudget` are validated via `ValidateWikiConfig`:
   - `0` = unlimited (accepted)
   - positive = accepted
   - negative = rejected with `ErrInvalidWikiConfig`
3. **LLM budget guard.**
   `OnboardingOpts.IngestTokenBudget` caps total LLM tokens per run. When exceeded mid-ingest, the orchestrator stops generating new pages, appends a warning to `result.Errors`, and returns **partial success** (not an error). Pages written so far are preserved.
4. **No hard upper ceiling.**
   Removed the previous 1000/500 arbitrary limits from the wiki config validator. The only constraint is now `IngestTokenBudget`, which is operator-configurable.

## Consequences

- Wiki DB can grow significantly on large repos. Explicitly accepted — the DB is SQLite with FTS5, and staleness scoring already exists for eviction pressure.
- First-time onboarding on a large monorepo can consume substantial LLM budget. `IngestTokenBudget=0` (unlimited) is risky; operators should set a positive bound in production (e.g., 2M tokens/run).
- Partial-success semantics (`result.Errors` non-empty, but no Go `error` returned) require UI handling: Settings/Onboarding views must surface warnings distinctly from failures.

## Related

- ADR-0003 (wiki as second brain)
- Plan: `docs/plans/spec-wiki-second-brain.md`
- Code: `internal/insight/onboarding/auto_depth.go`, `internal/insight/onboarding/orchestrator.go`, `config/config.go`
