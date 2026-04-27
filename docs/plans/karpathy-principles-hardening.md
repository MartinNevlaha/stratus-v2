# Plan: Karpathy Principles Hardening

**Workflow ID:** `karpathy-principles-hardening`
**Type:** spec (complex)
**Date:** 2026-04-17
**Design doc:** [karpathy-principles-hardening-design.md](./karpathy-principles-hardening-design.md)

## Goal

Harden Karpathy principle (2 Simplicity / 3 Surgical) enforcement across the Stratus v2 agent / skill / rules surface so that violations surface during review rather than after merge. Ship as docs/prompt edits only — no Go code, no orchestration, no API changes.

## Approach

4 deliverables (D1–D4) per the TDD. All edits target Markdown files. Each file edit is mechanical and verifiable by grep. No new abstractions, no new state transitions, no new endpoints.

## Budget (Principle 2)

| Field | Value |
|---|---|
| Estimated LOC added | ~500 (revised from TDD's 400 to account for 4-location agent sync) |
| New files | 0 |
| New abstractions | 0 |
| Out-of-scope (explicit) | `db/governance.go` (already indexed), `orchestration/state.go` (no new transitions), bundled `Plan` subagent (cannot be modified), separate plan-template file (inline in SKILL.md instead), API/hook additions, OpenCode parity-skip |

## File-location reality (4-way sync)

In this dev repo, Stratus assets exist in **4 locations**:
- `cmd/stratus/agents/` — embedded source (Claude Code base)
- `cmd/stratus/agents-opencode/` — embedded source (OpenCode variant)
- `.claude/agents/` — local copy (loaded by current Claude Code session — already user-customized so `stratus refresh` would skip)
- `.opencode/agents/` — local copy (same as above for OpenCode)

Skills are shared (only `.claude/skills/` + embedded). Rules are dual-location (embedded + `.claude/rules/`).

## Task List

| # | Task | Files | Agent | Parallelizable |
|---|------|-------|-------|----------------|
| 0 | **D1 — Reviewer hardening** — insert "Karpathy Enforcement" section into `delivery-code-reviewer.md` (×4 locations) per TDD §D1 | 4 | delivery-implementation-expert | ⚡ with #1, #2, #3 |
| 1 | **D2 — Coordinator hardening** — edit `spec-complex/SKILL.md` (×2 locations): add `## Budget (Principle 2)` spec to Phase 4, add Karpathy citation + Step 1a retrospective spec to Phase 7 per TDD §D2 | 2 | delivery-implementation-expert | ⚡ with #0, #2, #3 |
| 2 | **D3 — Producer agent cross-links** — add one-liner reference to `karpathy-principles.md` in 11 producer agents × 4 locations = 44 files. Excludes `code-reviewer` (D1 covers it), `governance-checker` (meta), `skill-creator` (meta) per TDD §D3 | 44 | delivery-implementation-expert | ⚡ with #0, #1, #3 |
| 3 | **D4 — Canonical rules alignment** — update `karpathy-principles.md` Enforcement section in `cmd/stratus/rules/` + `.claude/rules/` to match new reviewer wording per TDD §D4 | 2 | delivery-implementation-expert | ⚡ with #0, #1, #2 |
| 4 | **Verify** — run grep acceptance recipes from TDD §Test Strategy: confirm 12 agent files reference karpathy-principles in each of 4 dirs; SKILL.md contains both Budget heading and Step 1a retrospective heading; karpathy-principles.md Enforcement matches reviewer prompt | n/a | delivery-code-reviewer | sequential after #0–#3 |

Total tasks: **5**. Estimated LOC: ~500. All agent assignments use `delivery-implementation-expert` for the 4 mechanical edit tasks (single agent context preserved across all docs work; respects Principle 3 — no agent-hopping for cohesive change set), then `delivery-code-reviewer` for the verify pass.

## Sequencing

- Tasks 0–3 are **fully parallel-safe** (disjoint file sets). Sequential execution is also fine; ordering does not matter.
- Task 4 (verify) runs **after all of 0–3 complete**.
- No merge conflicts expected since each task touches its own file set.

## Critical Files (canonical references for implementers)

- TDD: `docs/plans/karpathy-principles-hardening-design.md` — exact markdown blocks to insert
- Reviewer base: `cmd/stratus/agents/delivery-code-reviewer.md`
- Reviewer OpenCode: `cmd/stratus/agents-opencode/delivery-code-reviewer.md`
- Skill base: `cmd/stratus/skills/spec-complex/SKILL.md`
- Rules base: `cmd/stratus/rules/karpathy-principles.md`
- 11 producer agents: backend, database, debugger, devops, frontend, implementation-expert, mobile, qa, strategic-architect, system-architect, ux-designer (× 4 location dirs)

## Risks (delta from TDD risk register)

- **R-LOC**: Budget revised upward to ~500 LOC (was 400) due to 4-location sync. Justification: editing only embedded sources would leave the current dev session unchanged, defeating dogfooding.
- **R-DRIFT**: Future `stratus refresh` will skip the locally-edited `.claude/` and `.opencode/` files. Mitigation: after release, re-test by running `stratus init --target both` into a clean tmp dir to verify embedded sources match local edits.

## Out of Scope (explicit, repeated for clarity)

- No `db/governance.go` changes
- No `orchestration/state.go` changes
- No new hooks, no new API endpoints
- No separate plan-template file under `.claude/templates/`
- No modification of bundled `Plan` subagent
- No release/tag (separate workflow when ready)

## Open Questions

None blocking implementation. TDD is fully specified.
