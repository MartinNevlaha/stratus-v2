# Implementation Plan: Wiki Knowledge Graph — Typed Edges & Rebuild

**Workflow ID:** `spec-wiki-graph-edges`
**Source design:** `docs/plans/spec-wiki-graph-edges-design.md` (PASSED governance)
**Verified ground truth:** `/home/martin/.stratus/data/54a303554276/stratus.db` — 38 pages, 33 edges (all `related`), 13 orphans
**Karpathy principles:** P2 (Simplicity First), P3 (Surgical Changes)

---

## 1. Ordered Tasks

### Task 1 — DB layer: allowlist + orphan helper + delete-by-ID

**Agent:** `delivery-database-engineer`
**LOC:** ~50 impl + ~40 tests = ~90

**Files:**
- `db/wiki.go`
- `db/wiki_test.go`

**Work:**
1. Add `AllowedWikiLinkTypes` map + `IsValidWikiLinkType(t string) bool` (case-insensitive, trim).
2. Add `(d *DB) CountOrphanWikiPages() (int, error)` — `SELECT COUNT(*) FROM wiki_pages p WHERE NOT EXISTS (SELECT 1 FROM wiki_links WHERE from_page_id=p.id OR to_page_id=p.id)`.
3. Add `(d *DB) DeleteWikiLinkByID(id string) (bool, error)` — `Exec` + `RowsAffected()`.
4. Tests: `TestCountOrphanWikiPages`, `TestDeleteWikiLinkByID_Success`, `TestDeleteWikiLinkByID_NotFound`, `TestIsValidWikiLinkType`.

### Task 2 — Typed LinkSuggester + prompt rewrite

**Agent:** `delivery-backend-engineer`
**LOC:** ~100 impl + ~80 tests = ~180
**Blocked by:** Task 1

**Files:**
- `internal/insight/wiki_engine/link_suggester.go`
- `internal/insight/prompts/prompts.go` (line 36)
- `internal/insight/prompts/prompts_test.go`
- `internal/insight/wiki_engine/link_suggester_test.go`

**Work:**
1. Rewrite `prompts.WikiLinkSuggestion` per design §6 (object-wrapped `{"links":[...]}`, 4 LLM-side types: `related|parent|child|cites`, examples).
2. Extend `StubSuggestion` / add `LinkSuggestion` with optional `LinkType`, `Strength`, `ToTitle`.
3. Parse `{"links":[...]}` wrapper instead of bare array.
4. `normalizeLinkType(t)` → lowercase + trim, fall back to `"related"` + `slog.Warn` on invalid.
5. Dual mode: if `to_title` → look up existing page (case-insensitive, on collision pick newest by `updated_at DESC`, `slog.Info`); else stub creation path uses `normalizeLinkType(sug.LinkType)`.
6. Replace hardcoded `"related"` at `link_suggester.go:102`.
7. Update prompt snapshot test.
8. Tests: `TestLinkSuggester_ParsesTypedEdges`, `TestLinkSuggester_ParsesChildAndCites`, `TestLinkSuggester_FallsBackToRelatedOnInvalidType`, `TestLinkSuggester_StubModeStillWorks`, `TestLinkSuggester_ParsesObjectWrappedResponse`, `TestLinkSuggester_AmbiguousToTitle_PicksNewest`.

### Task 3 — Rebuild endpoint + mutex + manual CRUD (A+C merged)

**Agent:** `delivery-backend-engineer`
**LOC:** ~270 impl + ~220 tests = ~490
**Blocked by:** Tasks 1, 2

**Files:**
- `api/routes_wiki_links.go` (**NEW**)
- `api/server.go` (mutex at L57–62, routes at L441)
- `api/routes_wiki_test.go`

**Work:**
1. Add `rebuildMu sync.Mutex` to `Server` struct.
2. Register three routes:
   ```go
   mux.HandleFunc("POST /api/wiki/links/rebuild", s.handleRebuildWikiLinks)
   mux.HandleFunc("POST /api/wiki/links",          s.handleCreateWikiLink)
   mux.HandleFunc("DELETE /api/wiki/links/{id}",   s.handleDeleteWikiLink)
   ```
3. `handleRebuildWikiLinks` — algorithm from design §5.1: `TryLock` (409 on conflict) → `ListWikiPages` → `CountOrphanWikiPages` → `DetectCrossReferences` per page → pairwise `DetectSharedSourceLinks` → `FindContradictions` → suggester on orphans (skip if `guardianLLM == nil`) → group-by `link_type` → return `rebuildResult`.
4. `handleCreateWikiLink` — validate (`IsValidWikiLinkType`, strength ∈ [0,1], no self-loop) → verify both pages exist → `SaveWikiLink`.
5. `handleDeleteWikiLink` — extract `{id}` → `DeleteWikiLinkByID` → 200/404.
6. Tests: `TestRebuildLinks_AllRelatedIncreases`, `TestRebuildLinks_OrphansDecrease`, `TestRebuildLinks_DetectsContradictions`, `TestRebuildLinks_MaxPagesExceeded_Returns400`, `TestRebuildLinks_MaxPagesOutOfRange_Returns400`, `TestRebuildLinks_NoLLMClient_SkipsSuggester`, `TestRebuildLinks_ConcurrentRequests_SecondReturns409`, `TestCreateWikiLink_ValidatesLinkType`, `TestCreateWikiLink_RejectsUnknownType`, `TestCreateWikiLink_RejectsSelfLoop`, `TestCreateWikiLink_RejectsInvalidStrength`, `TestCreateWikiLink_FromPageNotFound_Returns404`, `TestCreateWikiLink_ToPageNotFound_Returns404`, `TestCreateWikiLink_DuplicateReturnsUpsert`, `TestDeleteWikiLink_Success`, `TestDeleteWikiLink_UnknownID_Returns404`.

### Task 4 — Frontend: api.ts client + Wiki.svelte Rebuild button

**Agent:** `delivery-frontend-engineer`
**LOC:** ~40
**Blocked by:** Task 1 (for types); can run parallel with Task 3

**Files:**
- `frontend/src/lib/api.ts` (after line 324)
- `frontend/src/routes/Wiki.svelte` (line 139 area)

**Work:**
1. Add `rebuildWikiLinks(maxPages = 500)`, `createWikiLink(body)`, `deleteWikiLink(id)` + `RebuildLinksResult` type.
2. Wiki.svelte: `rebuilding = $state(false)` + `rebuildMessage = $state('')` + `<button onclick={doRebuild} disabled={rebuilding}>` in the graph-view header; `doRebuild()` calls `rebuildWikiLinks(500)`, sets status, calls `loadGraph()` to re-fetch.
3. **Leave `edgeColor`/`edgeDash` untouched** — legend already handles all 6 types.

### Task 5 — E2E smoke verification

**Agent:** `delivery-qa-engineer`
**LOC:** 0
**Blocked by:** Tasks 1–4

**Work:**
1. `go test ./db/... ./internal/insight/... ./api/...`
2. `cd frontend && npm run build`
3. Start dev server on the active DB (38 pages, 13 orphans).
4. Capture before-state SQL (page count, link count, `link_type` distribution, orphan count).
5. `POST /api/wiki/links/rebuild` — inspect response.
6. Capture after-state SQL.
7. Browser smoke: Wiki → Graph tab → verify multi-coloured edges.
8. Additional: `POST /wiki/links` with invalid type → 400; `DELETE` round-trip → 200/404; concurrent rebuild → 409.
9. Report mapping SC-1..SC-6.

---

## 2. Dependency Graph

```
Task 1 (DB)  ──┬──► Task 3 (Rebuild + CRUD endpoints) ──┐
               │                                         │
Task 2 (Suggester) ─────────────────────────────────────►├──► Task 5 (QA smoke)
                                                         │
Task 4 (Frontend, parallel with Task 3 after Task 1) ───►┘
```

---

## 3. Success Criteria (design §1)

- **SC-1** — Orphan count drops from 13 to ≤ 3.
- **SC-2** — ≥ 2 distinct `link_type` values in `wiki_links`.
- **SC-3** — `POST /wiki/links/rebuild` completes in < 60 s with populated `by_type`.
- **SC-4** — `POST /wiki/links` with invalid `link_type` → 400.
- **SC-5** — `DELETE /wiki/links/{id}` → 200 single-row removal.
- **SC-6** — Graph tab renders multi-coloured edges after Rebuild.

---

## Budget (Principle 2)

- **Estimated LOC:** 415 non-test (180 rebuild + 100 suggester + 90 CRUD + 40 frontend + 5 route wiring) + ~340 test LOC. Total touched ~755.
- **New files:** 1 (`api/routes_wiki_links.go`)
- **New abstractions:** 4 (`db.AllowedWikiLinkTypes` constant, `db.IsValidWikiLinkType` validator, `db.CountOrphanWikiPages` helper, `db.DeleteWikiLinkByID` helper). `LinkSuggestion` is an extension of existing `StubSuggestion` (backwards-compatible per design §13). `rebuildMu` is a struct field.
- **Out of scope (explicit):**
  - Embedding-based wiki page similarity (defer — duplicates code-search infra).
  - Background scheduler for periodic rebuild (defer — manual trigger suffices at current N).
  - UI edge editor (drag/right-click) — manual CRUD is API-only.
  - `supersedes` detection — requires page versioning metadata that doesn't exist.
  - Per-page re-link endpoint — derivable later if demand appears.
  - Per-model prompt A/B tuning — revisit when changing default LLM.
  - Auth / rate-limit on new endpoints — same surface as all other Stratus endpoints.
  - DDL changes — validation in Go per existing pattern.
