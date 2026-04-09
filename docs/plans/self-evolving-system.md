# Self-Evolving System — Implementation Plan

**Workflow:** spec-self-evolving-system
**Date:** 2026-04-06
**Design Doc:** [self-evolving-system-design.md](self-evolving-system-design.md)

---

## Task List (22 tasks, 7 layers)

### Layer 0: Removal (Task 0)

0. **Remove Learning + Analytics systems** — Delete `api/routes_learning.go`, `db/learning.go`, `frontend/src/routes/Learning.svelte`, `frontend/src/routes/Analytics.svelte`. Drop `candidates`+`proposals` tables from `db/schema.go`. Remove learning route registrations from `api/server.go`, pending_candidates/proposals from `api/routes_dashboard.go`, Learning/Analytics imports+tabs from `App.svelte`, related types from `types.ts`, API functions from `api.ts`, `analyticsUpdateCounter` from `store.svelte.ts`, proposal event types from `insight/events/types.go`.

### Layer 1: Database (Tasks 1-4)

1. **Add Wiki + Evolution DDL to schema.go** — Append 6 new tables (wiki_pages, wiki_pages_fts + triggers, wiki_links, wiki_page_refs, evolution_runs, evolution_hypotheses) to `db/schema.go` *(DONE)*
2. **Wiki DB models + CRUD** — Create `db/wiki.go` + `db/wiki_test.go` with WikiPage, WikiLink, WikiPageRef structs and all CRUD/FTS5 search methods
3. **Evolution DB models + CRUD** — Create `db/evolution.go` + `db/evolution_test.go` with EvolutionRun, EvolutionHypothesis structs and CRUD methods
4. **Config structs** — Add WikiConfig (with vault_path, vault_sync_on_save) + EvolutionConfig to `config/config.go` with defaults (both disabled by default) *(DONE)*

### Layer 2: Engines (Tasks 5-11)

5. **WikiStore interface** — Create `internal/insight/wiki_engine/store.go` with WikiStore interface + DBWikiStore implementation
6. **Wiki engine: ingest + maintenance** — Create `internal/insight/wiki_engine/engine.go` + test with RunIngest(), RunMaintenance(), staleness scoring
7. **Wiki synthesizer** — Create `internal/insight/wiki_engine/synthesizer.go` + test with GeneratePage(), SynthesizeAnswer()
8. **Wiki linker** — Create `internal/insight/wiki_engine/linker.go` + test with DetectCrossReferences(), FindContradictions()
9. **EvolutionStore interface** — Create `internal/insight/evolution_loop/store.go` with EvolutionStore interface + DB implementation
10. **Evolution loop core** — Create loop.go, hypothesis.go, experiment.go, evaluator.go + tests in `internal/insight/evolution_loop/`
11. **Insight engine integration** — Wire wiki engine + evolution loop into `insight/engine.go` and `insight/scheduler.go` tick loop

### Layer 3: API (Tasks 12-13)

12. **Wiki HTTP handlers** — Create `api/routes_wiki.go` + test (5 endpoints: list, get, search, query, graph), register in `api/server.go`
13. **Evolution HTTP handlers** — Create `api/routes_evolution.go` + test (5 endpoints: list runs, get run, trigger, get config, update config), register in `api/server.go`

### Layer 4: Obsidian Vault Sync (Tasks 14-16)

14. **Obsidian format helpers** — Create `internal/insight/wiki_engine/obsidian.go` + test with frontmatter serialization, `[[wikilink]]` generation, Dataview metadata conversion
15. **Vault sync engine** — Create `internal/insight/wiki_engine/vault_sync.go` + test with DB→disk sync, disk→DB file watcher (fsnotify), conflict resolution, vault directory management
16. **Vault sync API + MCP** — Add `POST /api/wiki/vault/sync`, `GET /api/wiki/vault/status` endpoints to routes_wiki.go, add vault_sync MCP tool

### Layer 5: MCP (Task 17)

17. **Register 5 MCP tools** — Add wiki_search, wiki_query, evolution_status, evolution_trigger, vault_sync to `mcp/tools.go`

### Layer 6: Frontend (Tasks 18-21)

18. **TypeScript interfaces** — Add WikiPage, WikiLink, WikiPageRef, EvolutionRun, EvolutionHypothesis, VaultStatus, configs to `frontend/src/lib/types.ts`
19. **API client functions** — Add wiki + evolution + vault sync functions to `frontend/src/lib/api.ts`
20. **Wiki tab** — Create Wiki.svelte + WikiSearch, WikiPageList, WikiPageDetail, WikiGraph, WikiQuery, VaultStatus components
21. **Evolution tab** — Create Evolution.svelte + EvolutionRunList, EvolutionRunDetail, EvolutionConfig, EvolutionTrigger components

---

## Dependency Graph

```
Task 1 (DDL) ─────┬──> Task 2 (wiki DB) ──> Task 5 (store) ──> Task 6 (engine) ──┐
                   │                                              Task 7 (synth)  ──┤
                   │                                              Task 8 (linker) ──┤
                   └──> Task 3 (evo DB) ──> Task 9 (store) ──> Task 10 (evo loop) ─┤
                                                                                    │
Task 4 (config) ──────────────────────────────────────────────────────────────────>─┤
                                                                                    v
                                                           Task 11 (integration)
                                                               │           │
                                                               v           v
                                                        Task 12 (wiki)  Task 13 (evo)
                                                               │           │
                                                               v           v
                                                           Task 14 (MCP tools)

Task 15 (types) ──> Task 16 (API client) ──> Task 17 (Wiki UI)
                                          ──> Task 18 (Evo UI)
```

Parallelizable: {1,4}, {2,3}, {5,9}, {6,7,8,10}, {12,13}, {15,16 with backend}, {17,18}

---

## New Files (40)

| # | File |
|---|------|
| 1 | `db/wiki.go` |
| 2 | `db/wiki_test.go` |
| 3 | `db/evolution.go` |
| 4 | `db/evolution_test.go` |
| 5 | `internal/insight/wiki_engine/store.go` |
| 6 | `internal/insight/wiki_engine/engine.go` |
| 7 | `internal/insight/wiki_engine/engine_test.go` |
| 8 | `internal/insight/wiki_engine/synthesizer.go` |
| 9 | `internal/insight/wiki_engine/synthesizer_test.go` |
| 10 | `internal/insight/wiki_engine/linker.go` |
| 11 | `internal/insight/wiki_engine/linker_test.go` |
| 12 | `internal/insight/evolution_loop/store.go` |
| 13 | `internal/insight/evolution_loop/loop.go` |
| 14 | `internal/insight/evolution_loop/hypothesis.go` |
| 15 | `internal/insight/evolution_loop/hypothesis_test.go` |
| 16 | `internal/insight/evolution_loop/experiment.go` |
| 17 | `internal/insight/evolution_loop/experiment_test.go` |
| 18 | `internal/insight/evolution_loop/evaluator.go` |
| 19 | `internal/insight/evolution_loop/evaluator_test.go` |
| 20 | `internal/insight/evolution_loop/loop_test.go` |
| 21 | `api/routes_wiki.go` |
| 22 | `api/routes_wiki_test.go` |
| 23 | `api/routes_evolution.go` |
| 24 | `api/routes_evolution_test.go` |
| 25-40 | Frontend: Wiki.svelte, Evolution.svelte, 6 wiki components, 4 evolution components |

## Modified Files (8)

| File | Change |
|------|--------|
| `db/schema.go` | Append wiki + evolution DDL |
| `config/config.go` | Add WikiConfig, EvolutionConfig + defaults |
| `insight/engine.go` | Add wikiEngine, evolutionLoop fields + methods |
| `insight/scheduler.go` | Add wiki/evolution calls to tick loop |
| `api/server.go` | Register 10 new route handlers |
| `mcp/tools.go` | Register 4 new MCP tools |
| `frontend/src/lib/types.ts` | Add TypeScript interfaces |
| `frontend/src/lib/api.ts` | Add API client functions |
