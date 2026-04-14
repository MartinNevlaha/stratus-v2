# Plan — Wiki ako druhý mozog

**Workflow:** `spec-wiki-second-brain` (type=spec, simple)

## Goal

Zmeniť Wiki na „druhý mozog" projektu:
- **A)** Onboarding + wiki ingest skenuje celý projekt (remove hard-cap 200, 0=unlimited sentinel, config-validated).
- **B)** `/spec` a `/spec-complex` v Learn fáze automaticky zapíšu wiki stránku o implementovanej feature (direct-write, provenance-tagged, fail-open).
- **C)** Upsert-by-(workflow_id, feature_slug) — žiadne duplikáty na reruny.
- **D)** LLM budget guard pri full ingeste.
- **E)** ADR-0003 (wiki-as-second-brain) + ADR-0004 (onboarding-full-scan).

## Tasks

1. **Config bounds + 0-unlimited sentinel** — `config/config.go` + validation test; MaxPagesPerIngest/OnboardingMaxPages: 0=unlimited, negatíva reject.
2. **Remove hard cap v auto_depth** — `internal/insight/onboarding/auto_depth.go:115`; clampPages rešpektuje sentinel.
3. **Orchestrator full-scan + LLM budget guard** — `internal/insight/onboarding/orchestrator.go`; partial-success pri prekročení budgetu.
4. **DB upsert by (workflow_id, feature_slug)** — migrácia v `db/schema.go`, nová `UpsertWikiPageByWorkflow` v `db/wiki.go`, unique index.
5. **Learn-phase auto-write hook (fail-open)** — `orchestration/coordinator.go` + nový `orchestration/wiki_autodoc.go`; write zlyhá → workflow stále complete.
6. **Internal wiki write endpoint** — `POST /api/wiki/pages` pre workflow-sourced zápisy (bypass proposal gate).
7. **Wire /spec + /spec-complex skills na autodoc** — add Learn step do `cmd/stratus/commands-opencode/spec.md`, `spec-complex.md`, `.claude/skills/spec/SKILL.md`, `.claude/skills/spec-complex/SKILL.md`.
8. **ADR-0003 + ADR-0004** — `docs/adr/`.

## Non-Goals
- Žiadne proposal-gate pre wiki (direct write so source-tagom je governance gate).
- Žiadne automatické mazanie stale pages (len existujúca staleness logika).
- UI reviewer pre auto-generated pages — out of scope (future).

## Risks
- Full-scan LLM cost → budget guard (Task 3).
- Duplicate pages → upsert (Task 4).
- Workflow complete blocked by wiki write failure → fail-open (Task 5).
