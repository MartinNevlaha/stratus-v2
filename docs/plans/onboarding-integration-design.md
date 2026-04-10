# Technical Design: Wiki Onboarding Integration into Retrieve Pipeline

## Overview

Wire onboarding wiki pages into the `retrieve` MCP tool so agents automatically get wiki knowledge during spec/bug/swarm workflows. Add staleness detection tied to git changes and workflow events. Enrich agent skills to query wiki context.

## Component Overview

| Component | Change |
|-----------|--------|
| `api/routes_retrieval.go` | Add `corpus="wiki"` to handleRetrieve; merge wiki FTS5 results |
| `db/wiki.go` | Add `WikiPageCount()`, `FindPagesBySourceFiles()` methods |
| `insight/engine.go` | Startup git-diff staleness check; workflow-complete staleness trigger |
| `cmd/stratus/skills/spec-complex/SKILL.md` | Add wiki retrieval in Discovery phase |
| `cmd/stratus/skills/bug/SKILL.md` | Add wiki retrieval in Analyze phase |
| `cmd/stratus/skills/spec/SKILL.md` | Add wiki retrieval in Plan phase |
| `mcp/tools.go` | Update retrieve tool description to include "wiki" corpus option |

## ADR-1: Extend handleRetrieve rather than new endpoint

**Decision:** Add `corpus="wiki"` as third option to `handleRetrieve`. Auto-mode searches all three sources. MCP tool needs no changes.

**Rationale:** Single retrieve call for agents; zero MCP changes; immediate wiki visibility for all existing `retrieve` callers.

## ADR-2: Use guardian_baselines for HEAD SHA storage

**Decision:** Store last-known git HEAD SHA under key `wiki_last_head_sha` in `guardian_baselines` table (existing KV store). No new tables.

## ADR-3: Staleness score penalty, not filtering

**Decision:** Apply 50% score penalty to stale wiki pages in retrieve results. Pages remain visible but rank lower. Include `staleness_score` and `page_type` metadata.

## ADR-4: File cross-reference via wiki_page_refs

**Decision:** Use `wiki_page_refs` with `source_type='artifact'` and `source_id=<file_path>` for file-to-page mapping. Enables staleness detection when git files change.

## API Contract

### Modified: `GET /api/retrieve`

**Validation:** The `corpus` parameter MUST be validated against the allowed set `{"code", "governance", "wiki", ""}`. Return 400 with `{"error": "invalid corpus value"}` for any other value.

**Error handling:** Wiki FTS5 search failures are fail-open: log the error with context (`fmt.Errorf("retrieve: wiki search: %w", err)`), continue without wiki results, and return code + governance results only. Same pattern as existing Vexor/governance error handling.

**Staleness bounds:** `staleness_score` is always clamped to [0.0, 1.0]. Boost operations use `min(current + boost, 1.0)`.

```
Parameters:
  q       string  (required)
  corpus  string  (optional)  "code" | "governance" | "wiki" | "" (auto: all three); 400 on invalid
  top_k   int     (optional)  default 10

Response (new fields for wiki results):
{
  "results": [
    {
      "source":          "code" | "governance" | "wiki",
      "file_path":       string,
      "title":           string,
      "excerpt":         string,
      "score":           float64,
      "doc_type":        string,          // governance only
      "page_type":       string,          // wiki only
      "staleness_score": float64          // wiki only
    }
  ]
}
```

Auto mode: wiki capped at `topK/3`, stale pages penalized 50%.

### Modified: `GET /api/retrieve/status`

New fields: `wiki_available bool`, `wiki_page_count int`.

## Data Model

No schema changes. New DB methods:
- `WikiPageCount() (int, error)` — count of wiki pages
- `FindPagesBySourceFiles(files []string) ([]string, error)` — page IDs referencing given files via wiki_page_refs

## Staleness Detection

**Tier 1 (startup):** `insight/engine.go` Start() runs git diff between stored HEAD and current HEAD. Cross-references changed files against wiki_page_refs. Boosts staleness by 0.3 for affected pages.

**Tier 2 (runtime):** On `EventWorkflowCompleted`, extract touched files from workflow data, boost staleness by 0.2 for affected pages.

## Skill Enrichment

Add wiki retrieval instructions to:
- `spec-complex` Discovery Step 3: retrieve with wiki corpus after codebase exploration
- `bug` Analyze Step 2: retrieve with wiki corpus for module context
- `spec` Plan Step 2: retrieve with wiki corpus for architecture context

## Testing Strategy

| Test | File |
|------|------|
| `TestHandleRetrieve_WikiCorpus` | `api/routes_retrieval_test.go` |
| `TestHandleRetrieve_AutoMode_IncludesWiki` | `api/routes_retrieval_test.go` |
| `TestHandleRetrieve_StalePenalty` | `api/routes_retrieval_test.go` |
| `TestHandleRetrieve_InvalidCorpus` | `api/routes_retrieval_test.go` |
| `TestHandleRetrieveStatus_WikiFields` | `api/routes_retrieval_test.go` |
| `TestFindPagesBySourceFiles` | `db/wiki_test.go` |
| `TestFindPagesBySourceFiles_Empty` | `db/wiki_test.go` |
| `TestFindPagesBySourceFiles_NoMatches` | `db/wiki_test.go` |
| `TestWikiPageCount` | `db/wiki_test.go` |
| `TestWikiPageCount_Empty` | `db/wiki_test.go` |
| `TestCheckStartupStaleness` | `insight/engine_staleness_test.go` |

## Key File References

- `api/routes_retrieval.go:9-79` — handleRetrieve, primary target
- `db/wiki.go:219-247` — SearchWikiPages FTS5
- `db/wiki.go:260-276` — UpdateWikiPageStaleness
- `db/schema.go:829-840` — wiki_page_refs table
- `db/guardian.go:102-116` — GetBaseline/SetBaseline
- `insight/engine.go:239-296` — Start(), staleness check insertion point
- `insight/engine.go:329-343` — HandleEvent(), workflow trigger
- `cmd/stratus/skills/spec-complex/SKILL.md:64` — Discovery Step 3
- `cmd/stratus/skills/bug/SKILL.md:42` — Analyze Step 2
