# Implementation Plan: Wiki Onboarding Integration

## Design Reference
See `docs/plans/onboarding-integration-design.md`

## Task List

### Task 1: DB methods — WikiPageCount + FindPagesBySourceFiles (S)
**Files:** `db/wiki.go` (modify), `db/wiki_test.go` (modify)
- `WikiPageCount() (int, error)` — simple COUNT(*) on wiki_pages
- `FindPagesBySourceFiles(files []string) ([]string, error)` — SELECT DISTINCT page_id FROM wiki_page_refs WHERE source_type='artifact' AND source_id IN (...)
- Tests: happy path, empty input, no matches, zero pages
- **Dependencies:** None

### Task 2: Extend handleRetrieve for wiki corpus (M)
**Files:** `api/routes_retrieval.go` (modify), `api/routes_retrieval_test.go` (create/modify)
- Validate corpus against {"code", "governance", "wiki", ""} — 400 on invalid
- Add `useWiki := corpus == "" || corpus == "wiki"` branch
- Call `db.SearchWikiPages(query, "", wikiLimit)` where wikiLimit=topK/3 in auto, topK in explicit
- Map WikiPage results to retrieve result struct with source="wiki", page_type, staleness_score
- Apply 50% score penalty for stale pages (staleness_score > 0.7)
- Update handleRetrieveStatus with wiki_available, wiki_page_count
- Fail-open: log wiki search errors, continue without wiki results
- Tests: wiki corpus, auto mode includes wiki, caps results, stale penalty, invalid corpus 400, status fields
- **Dependencies:** Task 1

### Task 3: Update MCP retrieve tool description (S)
**Files:** `mcp/tools.go` (modify)
- Update retrieve tool description to mention wiki corpus
- Update corpus parameter description: `"Force search corpus: 'code', 'governance', or 'wiki'. Omit for auto-routing."`
- **Dependencies:** None

### Task 4: Startup git-diff staleness check (M)
**Files:** `insight/engine.go` (modify), `insight/engine_wiki_evo_test.go` (modify)
- New method `checkStartupStaleness()` called from Start()
- GetBaseline("wiki_last_head_sha") via db/guardian.go
- Run `git rev-parse HEAD` and `git diff --name-only oldSHA currentSHA`
- Call FindPagesBySourceFiles on changed files
- Boost staleness: UpdateWikiPageStaleness(id, min(current+0.3, 1.0))
- SetBaseline("wiki_last_head_sha", currentSHA)
- All git commands with 5s timeout, errors non-fatal
- Tests: diff detected, no git repo, first run (no stored SHA)
- **Dependencies:** Task 1

### Task 5: Workflow-complete staleness trigger (S)
**Files:** `insight/engine.go` (modify)
- In HandleEvent(), on EventWorkflowCompleted: extract workflow files, call FindPagesBySourceFiles, boost staleness by 0.2
- Non-blocking goroutine, errors logged
- **Dependencies:** Task 1, Task 4 (same file)

### Task 6: Skill enrichment — spec-complex, bug, spec (S)
**Files:** `cmd/stratus/skills/spec-complex/SKILL.md`, `cmd/stratus/skills/bug/SKILL.md`, `cmd/stratus/skills/spec/SKILL.md` (modify)
- spec-complex Discovery Step 3: add sub-step to call retrieve with wiki context
- bug Analyze Step 2: add wiki retrieval instruction
- spec Plan Step 2: add wiki check instruction
- **Dependencies:** None

## Dependency Graph
```
Tasks 1, 3, 6 — parallel (no deps)
  ↓
Task 2 (retrieve) ← 1
Task 4 (startup staleness) ← 1
  ↓
Task 5 (workflow staleness) ← 1, 4
```

## Execution Order
1 → 2 + 3 + 4 + 6 (parallel) → 5
