# ADR-0003: Wiki as Project Second Brain

**Status:** Accepted
**Date:** 2026-04-14
**Deciders:** @MartinNevlaha

## Context

Until now Wiki was populated only by explicit onboarding runs. `/spec` and `/spec-complex` workflows produced governance proposals (gated) and memory events, but nothing was written back into Wiki. The result: Wiki drifts out of sync with the codebase within days of the initial scan.

The project goal is for Wiki to function as a **second brain** — a living, searchable index of every feature shipped, updated automatically as work completes.

## Decision

1. **Direct-write from Learn phase, no proposal gate.**
   On every `learn → complete` transition, the orchestration coordinator calls `AutodocWorkflow` which upserts a wiki page describing the completed workflow (plan, tasks, delegated agents).
2. **Provenance, not approval, is the trust mechanism.**
   Auto-generated pages carry `status=auto-generated`, `generated_by=workflow`, and `metadata.source = "workflow:<id>"`. The dashboard can filter / surface them for optional human edit; they are never silently laundered into human-authored rules or ADRs.
3. **Upsert key = `(workflow_id, feature_slug)`.**
   Reruns of the same workflow update the same page (version++). Different features under the same workflow get different slugs.
4. **Fail-open.**
   Wiki write failure MUST NOT block the `complete` transition. Errors are logged, workflow proceeds.
5. **Optional enrichment endpoint: `POST /api/wiki/pages`.**
   Operators (or future richer skills) may POST custom markdown before the complete transition for more detailed content. Subsequent autodoc upsert updates the same row, preserving `created_at`.

## Why not proposal-gated?

Proposals make sense for governance files (rules, ADRs) because a wrong rule corrupts future decisions. Wiki pages are read-only descriptions of what already happened — the codebase itself is the source of truth. Gating them adds review backlog friction that would kill the "stays in sync" property. Provenance tagging preserves the audit trail without blocking.

## Consequences

- Wiki DB grows linearly with completed workflows. Acceptable — no size cap policy beyond existing staleness.
- Auto-generated content is minimal by default; quality depends on plan/task hygiene.
- A compromised or malformed workflow could produce low-quality pages. Mitigation: staleness scoring already exists; future work can down-rank auto-generated pages with low confidence.

## Related

- ADR-0004 (onboarding full-scan)
- Plan: `docs/plans/spec-wiki-second-brain.md`
- Code: `orchestration/wiki_autodoc.go`, `db/wiki.go` (`UpsertWikiPageByWorkflow`), `api/routes_wiki.go` (`POST /api/wiki/pages`)
