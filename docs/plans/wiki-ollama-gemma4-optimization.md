# Wiki Synthesizer Context Optimization for Ollama Gemma4

## Problem

The wiki synthesizer's `buildSourceContext` (`internal/insight/wiki_engine/synthesizer.go:155-184`) builds LLM context by simply concatenating full page content in search-result order until a 12K char budget is exhausted. This approach:

1. **Ignores staleness** — stale pages get equal context weight to fresh ones
2. **No summary-first grounding** — the LLM must process full content without a concise overview
3. **No graph expansion** — linked/related pages are ignored even when highly relevant
4. **Generic prompt** — `WikiSynthesis` prompt is a brief two-liner not optimized for Gemma4

## Approach

All changes in `internal/insight/wiki_engine/synthesizer.go` (context building + prompt) and `db/wiki.go` (batch page fetch). Surgical changes only — no interface changes to `WikiStore` (existing `ListLinksFrom`/`ListLinksTo` suffice).

### Task 1: Staleness-Aware Ranking

Sort search results before context assembly:
- Primary results (from FTS5): sort by staleness ascending (fresh first), then by page type priority (concept > entity > summary > topic > raw > answer)
- Stale pages (score > 0.7): included but ranked lower
- Evolution-generated pages: small boost (1.1x) for validated findings

**Files**: `internal/insight/wiki_engine/synthesizer.go` — new `rankPages(pages []db.WikiPage)` helper

### Task 2: Summary-First Context Building

Replace naive concatenation with structured context:
- For each ranked page, extract first section/paragraph (up to 300 chars) as a "summary snippet"
- Include summary snippets for ALL ranked pages first (overview layer)
- Then fill remaining budget with full content of top pages (detail layer)
- Budget split: 30% summary layer, 70% detail layer
- Extract summary by finding first `##` heading content or first 300 chars of content

**Files**: `internal/insight/wiki_engine/synthesizer.go` — refactor `buildSourceContext` into `buildSummaryLayer` + `buildDetailLayer`

### Task 3: Graph-Aware Context Expansion (1-hop)

After ranking, expand context by following wiki_links:
- For each primary result page, fetch linked pages (via `ListLinksFrom` + `ListLinksTo`)
- Deduplicate with primary results (avoid double-including)
- Reserve 15% of total budget for expanded pages (taken from detail layer budget)
- Cap at 5 expanded pages
- No recursive expansion (1-hop only per Karpathy simplicity principle)

**Files**: `internal/insight/wiki_engine/synthesizer.go` — new `expandWithLinkedPages` helper

### Task 4: Improved Synthesis Prompt

Enhance `WikiSynthesis` prompt for Gemma4:
- Structured answer format guidance (TL;DR, Details, Sources)
- Explicit citation instruction reinforcement
- Conciseness directive (prefer focused answers)
- Keep it model-agnostic in the prompt text (no Gemma4-specific references)

**Files**: `internal/insight/prompts/prompts.go` — update `WikiSynthesis` constant

### Task 5: Unit Tests

Table-driven tests for:
- `rankPages`: various staleness/page-type combinations
- `extractSummary`: pages with headings, without headings, empty content
- `buildSourceContext` integration: budget exhaustion, empty input, single page
- `expandWithLinkedPages`: no links, circular links, budget exceeded

**Files**: `internal/insight/wiki_engine/synthesizer_test.go`

## Constraints (from governance)

- Functions max 50 lines, files max 300 lines
- All SQL in `db/` package
- Error wrapping with context
- TDD: tests for all new public functions
- Simplicity first: no speculative features
- Surgical changes: touch only synthesizer.go, prompts.go, synthesizer_test.go
- No new WikiStore interface methods (use existing `ListLinksFrom`/`ListLinksTo`)

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Graph expansion unbounded growth | Hard cap 5 expanded pages, 15% budget reserve |
| Summary extraction fragile | Fallback to first 300 chars when no `##` heading found |
| Model-specific coupling | Keep all optimizations in prompt layer + ranking logic, not in llm.Client |
| 12K char budget too small | Configurable via constant; not exposed to API (no config validation needed) |
