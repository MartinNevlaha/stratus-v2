# Technical Design: Wiki Knowledge Graph — Typed Edges & Rebuild

**Status:** Proposed
**Date:** 2026-04-19
**Workflow type:** spec (complex)
**Karpathy principles enforced:** P2 (Simplicity First), P3 (Surgical Changes)

---

## 1. Goal / Success Criteria

**Goal.** Make the Wiki Knowledge Graph actually populated with the edge types the UI legend already advertises (`related | parent | child | contradicts | supersedes | cites`), and give the user a manual trigger to re-run the linker over all existing pages. Close the gap between what the dashboard promises (6 edge colours) and what the DB stores (`related` only).

**Measurable success criteria.** Verified by running the new `POST /api/wiki/links/rebuild` endpoint against the current local DB (`/home/martin/.stratus/data/54a303554276/stratus.db`, 38 pages, 33 links — all `related`, 13 orphans):

| # | Criterion | Verification |
|---|-----------|-------------|
| SC-1 | Orphan pages drop from 13 to **≤ 3** | `SELECT COUNT(*) FROM wiki_pages p WHERE NOT EXISTS (SELECT 1 FROM wiki_links WHERE from_page_id = p.id OR to_page_id = p.id);` |
| SC-2 | `wiki_links.link_type` distribution contains **≥ 2 distinct types** (not just `related`) | `SELECT link_type, COUNT(*) FROM wiki_links GROUP BY link_type;` |
| SC-3 | Rebuild endpoint returns JSON with per-type counts and completes in **< 60 s** on N=38 pages | Inspect response body + latency |
| SC-4 | `POST /api/wiki/links` rejects `link_type` outside the allowlist with **400** | Integration test |
| SC-5 | `DELETE /api/wiki/links/{id}` removes a single edge without cascading | Integration test |
| SC-6 | Clicking "Rebuild Graph" in the Wiki tab triggers the endpoint and re-renders | Manual smoke test |

**Non-goals for SC.** We are not requiring that every page type get an edge; evolution `concept` pages about isolated findings may legitimately stay orphaned. SC-1 allows up to 3 residual orphans.

---

## 2. Component Overview

Components involved (all pre-existing; we add methods/routes but no new packages):

| Component | File | Role in this change |
|-----------|------|--------------------|
| `wiki_engine.Linker` | `internal/insight/wiki_engine/linker.go` | Already performs cross-ref, shared-source, contradiction detection. Reused as-is. |
| `wiki_engine.LinkSuggester` | `internal/insight/wiki_engine/link_suggester.go:98-104` | Modified to consume typed link suggestions (not just `related`). |
| `prompts.WikiLinkSuggestion` | `internal/insight/prompts/prompts.go:36` | Rewritten to ask the LLM for `link_type` per suggestion. |
| `db.SaveWikiLink` / `db.DeleteWikiLinks` | `db/wiki.go:311, db/wiki.go:458` | Reused. We add one new method: `DeleteWikiLinkByID`. |
| `api/routes_wiki.go` | `api/routes_wiki.go:418` | Add `handleRebuildWikiLinks`, `handleCreateWikiLink`, `handleDeleteWikiLink`. |
| `api/server.go` | `api/server.go:441` | Register three new routes next to the existing `GET /api/wiki/graph`. |
| `frontend/src/routes/Wiki.svelte` | line 139 (`loadGraph`), line 245 (`edgeColor`) | Add a "Rebuild Graph" button. Legend stays as-is — it will now be honest. |
| `frontend/src/lib/api.ts` | line 319 | Add `rebuildWikiLinks()` client function. |

**No new packages. No new DB tables. No new schema columns.** Per P2.

---

## 3. API Contract

### 3.1 `POST /api/wiki/links/rebuild`

Runs the full linker pass over **all** wiki pages in the DB (not just pages from a single onboarding run). Synchronous; no background job — this is a manual admin trigger and N is bounded (see §8 risks).

**Request body** (optional — all fields have defaults):

```json
{
  "include_suggester": true,
  "max_pages": 500
}
```

| Field | Type | Default | Validation |
|-------|------|---------|-----------|
| `include_suggester` | bool | `true` | — |
| `max_pages` | int | `500` | `1 ≤ v ≤ 2000`. If there are more pages than this, return 400 `"dataset too large — pass a higher max_pages or reduce dataset"`. Do not silently truncate. |

**Response 200:**

```json
{
  "pages_scanned": 38,
  "orphans_before": 13,
  "orphans_after": 2,
  "links_saved": 47,
  "by_type": {
    "related": 38,
    "contradicts": 1,
    "parent": 5,
    "child": 3,
    "cites": 0,
    "supersedes": 0
  },
  "suggester_invoked_for": 13,
  "suggester_errors": 0,
  "duration_ms": 12480
}
```

Counts are **after upsert** — they include both freshly-inserted and strength-updated rows (the `ON CONFLICT` upsert at `db/wiki.go:320` does not distinguish). This is acceptable per P2 (do not add a distinguishing mechanism purely for metrics).

**Errors:**

- `400 max_pages out of range`
- `400 dataset too large`
- `409 wiki link rebuild already in progress` (mutex held)
- `500 rebuild wiki links: <wrapped error>`

The endpoint is **not** guarded by workflow state — it is an idempotent admin operation, consistent with the existing `POST /api/wiki/cluster/run` at `api/server.go:445`.

### 3.2 `POST /api/wiki/links`

Manual edge creation. User supplies both endpoints and the edge type.

**Request body:**

```json
{
  "from_page_id": "abc-123",
  "to_page_id":   "def-456",
  "link_type":    "parent",
  "strength":     0.8
}
```

**Validation:**

| Field | Rule | Error |
|-------|------|-------|
| `from_page_id` | non-empty; page must exist | `400 from_page_id is required` / `404 from_page_id not found` |
| `to_page_id`   | non-empty; page must exist; `≠ from_page_id` | `400 to_page_id is required` / `404 to_page_id not found` / `400 self-loops not allowed` |
| `link_type`    | must be in allowlist `{"related","parent","child","contradicts","supersedes","cites"}` | `400 invalid link_type: <value>` |
| `strength`     | if provided, `0 ≤ v ≤ 1`; default `0.5` | `400 strength must be between 0 and 1` |

**Response 201:**

```json
{
  "id": "7a1f...",
  "from_page_id": "abc-123",
  "to_page_id": "def-456",
  "link_type": "parent",
  "strength": 0.8,
  "created_at": "2026-04-19T10:15:30.123Z"
}
```

If the `UNIQUE(from_page_id, to_page_id, link_type)` constraint (`db/schema.go:834`) fires, `SaveWikiLink` upserts the strength (`db/wiki.go:320`). In that case we return **200** (not 201) with the existing row re-read. This keeps the handler idempotent.

**Implementation note on page existence checks.** We call `db.GetWikiPage(id)` twice; this adds 2 SELECTs but prevents dangling edges that bypass the FK (SQLite FKs require `PRAGMA foreign_keys=ON`; the schema at `db/schema.go:829-830` declares them but we defend in-app for better error messages). See §8 for the tradeoff.

### 3.3 `DELETE /api/wiki/links/{id}`

Remove a single edge by primary key.

**Response 200:**

```json
{ "deleted": true }
```

If the ID is unknown: `404 link not found`.
On SQL error: `500 delete wiki link: <wrapped error>`.

This differs from existing `DeleteWikiLinks(pageID)` (`db/wiki.go:458`), which deletes all links for a page and is called from `DeletePage`. We need by-ID delete, so we add a new DB method (see §4).

---

## 4. Data Model

**No schema changes.** The `wiki_links` table (`db/schema.go:826-838`) already has:

```sql
CREATE TABLE wiki_links (
    id           TEXT PRIMARY KEY,
    from_page_id TEXT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    to_page_id   TEXT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    link_type    TEXT NOT NULL DEFAULT 'related',
    strength     REAL NOT NULL DEFAULT 0.5,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(from_page_id, to_page_id, link_type)
);
```

`link_type` is already `TEXT` with no CHECK constraint, so parent/child/cites/supersedes can be inserted without any DDL change. We do **not** add a CHECK constraint — validation lives in Go (same pattern as the existing `GeneratedBy` field). This keeps P3 (no schema churn).

### New DB method

```go
// DeleteWikiLinkByID removes a single link by its primary key.
// Returns (false, nil) if no row matched, (true, nil) on success.
func (d *DB) DeleteWikiLinkByID(id string) (bool, error)
```

Added in `db/wiki.go` after line 466 (next to `DeleteWikiLinks`).

---

## 5. Component-Level Design

### 5.1 Deliverable A — `POST /api/wiki/links/rebuild`

New file, kept isolated to keep diffs reviewable: `api/routes_wiki_links.go`.

```go
// handleRebuildWikiLinks re-runs the Linker + LinkSuggester pass over all
// wiki pages and persists the detected links. Synchronous; bounded by
// req.MaxPages.
func (s *Server) handleRebuildWikiLinks(w http.ResponseWriter, r *http.Request)

// rebuildResult is the response body shape.
type rebuildResult struct {
    PagesScanned       int            `json:"pages_scanned"`
    OrphansBefore      int            `json:"orphans_before"`
    OrphansAfter       int            `json:"orphans_after"`
    LinksSaved         int            `json:"links_saved"`
    ByType             map[string]int `json:"by_type"`
    SuggesterInvokedFor int           `json:"suggester_invoked_for"`
    SuggesterErrors    int            `json:"suggester_errors"`
    DurationMs         int64          `json:"duration_ms"`
}
```

**Concurrency control.** A new `rebuildMu sync.Mutex` on `Server` (added next to existing `onboardingMu` / `updateMu` at `api/server.go:57-62`) serialises rebuild runs. The handler attempts `rebuildMu.TryLock()` at entry; if it returns false, respond **409 Conflict** `"wiki link rebuild already in progress"`. Same pattern already used for onboarding at `api/server.go:57-62`. No change to the read endpoint (`GET /api/wiki/graph`) — reads proceed concurrently with an ongoing rebuild; the upsert behaviour of `SaveWikiLink` keeps reads consistent at row granularity.

**Algorithm** (direct reuse of existing Linker methods — no new logic in `linker.go`):

```
1. Validate request. rebuildMu.TryLock() — if held, 409.
   Load all wiki pages via s.db.ListWikiPages({Limit: req.MaxPages}).
   If total > req.MaxPages → 400. (ListWikiPages returns (pages, total, err); no +1 probe needed.)
2. Count orphans_before:
     SELECT COUNT(*) FROM wiki_pages p
     WHERE NOT EXISTS (SELECT 1 FROM wiki_links WHERE from_page_id=p.id OR to_page_id=p.id)
   (add helper db.CountOrphanWikiPages() — ~10 LOC)
3. Build store := wiki_engine.NewDBWikiStore(s.db); linker := NewLinker(store).
4. Cross-references (O(N²) title-substring scan, N ≤ 500 → ≤ 250k comparisons):
     for each page: links = linker.DetectCrossReferences(&page, allPages)
                    save all via linker.SaveDetectedLinks(links)
5. Shared-source links (O(N²) but cheap after refs are loaded once):
     refs[pageID] = s.db.ListWikiPageRefs(pageID)
     for each unordered pair (A,B):
         if link := linker.DetectSharedSourceLinks(&A, refs[A.ID], &B, refs[B.ID]); link != nil:
             save via linker.SaveDetectedLinks([]db.WikiLink{*link})
6. Contradictions (single pass over all pages):
     contradictionLinks := linker.FindContradictions(allPages)
     save via linker.SaveDetectedLinks(contradictionLinks)
7. If include_suggester AND s.guardianLLM != nil:
     for each orphan page (re-query after step 6):
         suggester := NewLinkSuggester(store, s.guardianLLM)
         suggester.SuggestAndCreateStubs(ctx, &page)   // extended — see 5.2
     // Errors from suggester are logged + counted, not fatal.
8. Count orphans_after; group-by link_type for response ByType.
9. Return rebuildResult.
```

**LLM client selection.** `s.guardianLLM` (`api/server.go:51`) is a shared `insightllm.Client` already used across the server. If nil (LLM not configured), skip step 7 silently and set `SuggesterInvokedFor = 0`. Fail-open, same pattern as `WikiEngine.RunIngest` at `engine.go:69-72`.

**Context.** Use `r.Context()` for cancellation. LLM calls inside the suggester are per-page; cancellation between pages is cooperative (no goroutines — keep it simple, P2).

**LOC estimate:** ~180 lines for the handler + helpers.

### 5.2 Deliverable B — Typed LinkSuggester

Modify `internal/insight/wiki_engine/link_suggester.go`:

```go
// LinkSuggestion is what the LLM returns per candidate edge.
// Extends the previous StubSuggestion (which we keep as a type alias for
// back-compat within the package, since the Suggest API is used only
// internally).
type LinkSuggestion struct {
    Title     string   `json:"title"`
    Rationale string   `json:"rationale"`
    PageType  string   `json:"page_type"`         // concept | entity
    Tags      []string `json:"tags"`
    LinkType  string   `json:"link_type"`         // NEW: related|parent|child|cites
    Strength  float64  `json:"strength"`          // NEW: 0..1, default 0.5
    ToTitle   string   `json:"to_title"`          // NEW: target title (existing page)
}
```

**Dual mode.** The suggester now distinguishes two cases in the LLM output:

1. **Stub-creation mode** (existing behaviour): `title + rationale + page_type` present, `to_title` empty → create a new stub page and link to it with `link_type` (default `related`).
2. **Existing-page link mode** (new): `to_title` present → look up the existing page by case-insensitive title match; if found, save a typed link from the current page to it. If not found, fall back to stub creation.

Both are persisted via the existing `store.SaveLink` and `store.SavePage`. No new store methods needed.

**Validation before save.** The allowlist is defined **once** in `db/wiki.go` next to `WikiLink` and imported by both the handler and the suggester (no duplicated maps, P2):

```go
// db/wiki.go — single source of truth for valid edge types
var AllowedWikiLinkTypes = map[string]struct{}{
    "related": {}, "parent": {}, "child": {},
    "contradicts": {}, "supersedes": {}, "cites": {},
}

func IsValidWikiLinkType(t string) bool {
    _, ok := AllowedWikiLinkTypes[strings.ToLower(strings.TrimSpace(t))]
    return ok
}
```

The suggester normalises via:

```go
// wiki_engine/link_suggester.go
func normalizeLinkType(t string) string {
    trimmed := strings.ToLower(strings.TrimSpace(t))
    if db.IsValidWikiLinkType(trimmed) {
        return trimmed
    }
    slog.Warn("link suggester: invalid link_type from LLM, falling back to related", "got", t)
    return "related"
}
```

The hardcoded `LinkType: "related"` at `link_suggester.go:102` is replaced with `normalizeLinkType(sug.LinkType)`. The handler `handleCreateWikiLink` validates via `db.IsValidWikiLinkType` — same function, no drift.

**Title collision handling.** Wiki page titles are NOT unique in the schema (`db/schema.go:781-797` has no UNIQUE on `title`). When the LLM returns `to_title: "X"` and multiple pages share that title, the suggester picks the most-recently-updated match (`ORDER BY updated_at DESC LIMIT 1`) and logs `slog.Info("link suggester: ambiguous to_title, picked newest", "title", t, "id", id)`. Deterministic, documented, no silent "first match" behaviour.

**LOC estimate:** ~80 net-new lines in the suggester, ~20 lines churn.

### 5.3 Deliverable C — Manual edge CRUD

Same new file `api/routes_wiki_links.go`:

```go
func (s *Server) handleCreateWikiLink(w http.ResponseWriter, r *http.Request)
func (s *Server) handleDeleteWikiLink(w http.ResponseWriter, r *http.Request)
```

Implementation is straight validate → `s.db.GetWikiPage` × 2 → `s.db.SaveWikiLink` / `s.db.DeleteWikiLinkByID`. All error paths use `jsonErr` with wrapped messages per `.claude/rules/error-handling.md`.

**LOC estimate:** ~90 lines combined.

### 5.4 Deliverable D — Frontend "Rebuild Graph" button

Changes are confined to two files:

**`frontend/src/lib/api.ts`** — add one function after line 324:

```typescript
export const rebuildWikiLinks = (maxPages = 500) =>
  post<RebuildLinksResult>('/wiki/links/rebuild', { max_pages: maxPages, include_suggester: true })

export const createWikiLink = (body: {
  from_page_id: string; to_page_id: string;
  link_type: 'related'|'parent'|'child'|'contradicts'|'supersedes'|'cites';
  strength?: number;
}) => post<WikiLink>('/wiki/links', body)

export const deleteWikiLink = (id: string) =>
  del<{deleted: boolean}>(`/wiki/links/${encodeURIComponent(id)}`)
```

Add matching types in `frontend/src/lib/types.ts` (if that file exists in the project pattern — otherwise inline).

**`frontend/src/routes/Wiki.svelte`** — inside the graph view header (near line 139 `loadGraph`):

- Add a `rebuilding` state bool.
- Add a button rendered only when `activeView === 'graph'`:
  ```html
  <button onclick={doRebuild} disabled={rebuilding}>
    {rebuilding ? 'Rebuilding…' : 'Rebuild Graph'}
  </button>
  ```
- Handler:
  ```ts
  async function doRebuild() {
      rebuilding = true
      try {
          const r = await rebuildWikiLinks(500)
          rebuildMessage = `Saved ${r.links_saved} links (${Object.keys(r.by_type).length} types). Orphans: ${r.orphans_before} → ${r.orphans_after}.`
          await loadGraph()
      } catch (e) {
          error = e instanceof Error ? e.message : 'Rebuild failed'
      } finally {
          rebuilding = false
      }
  }
  ```

**Legend unchanged.** The existing `edgeColor`/`edgeDash` functions at `Wiki.svelte:245-258` already handle all 6 link types. They will simply start receiving non-`related` types from the API.

**LOC estimate:** ~40 lines TS/Svelte.

### 5.5 Deliverable E — Tests

See §7 for full test plan.

**Total LOC estimate (non-test, non-comment):**

| Deliverable | LOC |
|-------------|-----|
| A. rebuild endpoint + orphan helper | ~180 |
| B. typed link suggester | ~80 net + ~20 churn |
| C. create/delete link endpoints | ~90 |
| D. frontend button + api client | ~40 |
| Route wiring in `api/server.go` | ~5 |
| **Total** | **~415 LOC** |

Under the 500-LOC budget. Per P2.

---

## 6. Prompt Design for Typed Link Suggestion

**Current prompt** (`internal/insight/prompts/prompts.go:36`): asks for stub candidates only, hardcodes `related`.

**New prompt** (replaces the existing `WikiLinkSuggestion` constant). Must be parseable by the existing `llm.ParseJSONResponse` helper (already used at `link_suggester.go:60`), and work with `ResponseFormat: "json"` (existing at `link_suggester.go:53`).

```go
WikiLinkSuggestion = `You are analyzing a wiki page in a developer knowledge graph. Your job is to suggest typed edges to OTHER wiki pages.

Return a strict JSON object with one key "links" whose value is an array of at most 5 suggestions. No preamble, no code fences.

Each suggestion must have:
- "to_title"  (string) — the exact title of another page this one should link to. Leave empty ("") if you instead propose creating a NEW stub page (use "title" for the stub name).
- "title"     (string) — only when "to_title" is empty: the canonical name for a NEW stub page.
- "link_type" (string) — one of: "related", "parent", "child", "cites".
- "strength"  (number 0..1) — your confidence that this edge is meaningful.
- "rationale" (string) — one sentence justifying the edge.
- "page_type" (string) — only when proposing a new stub: "concept" or "entity".
- "tags"      (array of strings) — optional.

link_type semantics:
- "parent"  — the current page is a CHILD of to_title (to_title is the broader topic)
- "child"   — the current page is a PARENT of to_title (to_title is a specialization)
- "cites"   — the current page references or discusses to_title as a source / dependency
- "related" — neither containment nor citation applies; use sparingly (we already auto-detect this)

Examples (illustrative):

Example 1 — current page "Coordinator":
{"links":[
  {"to_title":"Orchestration Module","link_type":"parent","strength":0.9,
   "rationale":"Coordinator is a component of the Orchestration module."},
  {"to_title":"Phase State Machine","link_type":"cites","strength":0.7,
   "rationale":"Coordinator enforces transitions defined by the state machine."}
]}

Example 2 — current page "HTTP Routing Layer" (no existing target worth linking):
{"links":[
  {"to_title":"","title":"Middleware Chain","link_type":"child","page_type":"concept",
   "strength":0.6,"rationale":"Middleware chain is a specialization worth its own page.",
   "tags":["routing","middleware"]}
]}

Rules:
- Do NOT suggest the current page as its own link target.
- Do NOT output link_type values outside the four listed.
- Do NOT output more than 5 suggestions.
- If nothing is worth linking, return {"links":[]}.`
```

**Why JSON object instead of bare array?** The old prompt returned `[...]` directly. gemma-class models tend to wrap typed-output responses in an object more reliably (see user memory re: gemma4 JSON mode). The new prompt uses `{"links":[...]}` — parsing becomes:

```go
var wrapper struct {
    Links []LinkSuggestion `json:"links"`
}
if err := llm.ParseJSONResponse(resp.Content, &wrapper); err != nil {
    return nil, fmt.Errorf("link suggester: parse: %w", err)
}
return filterSuggestions(wrapper.Links), nil
```

**Supersedes/contradicts not requested from LLM.** These are detected deterministically (contradictions by `linker.FindContradictions`; supersedes could be added later by version metadata). Asking an LLM to declare "page A supersedes page B" is error-prone and out of scope for this iteration.

---

## 7. Sequence Diagrams

### 7.1 Rebuild flow

```
User                Wiki.svelte          api/routes_wiki_links.go          Linker            LinkSuggester         DB
 |                      |                          |                          |                     |                |
 |-- click Rebuild ---->|                          |                          |                     |                |
 |                      |-- POST /wiki/links/rebuild ------------------------->|                     |                |
 |                      |                          |-- ListWikiPages(limit=500+1) --------------------------------->|
 |                      |                          |<------------------------ pages[] -----------------------------|
 |                      |                          |-- CountOrphanWikiPages ---------------------------------------->|
 |                      |                          |<-- orphans_before -------------------------------------------- |
 |                      |                          |-- for each page: DetectCrossReferences -->|                    |
 |                      |                          |                          |-- SaveWikiLink (related) -----------> DB (upsert)
 |                      |                          |-- for each pair: DetectSharedSourceLinks >|                    |
 |                      |                          |                          |-- SaveWikiLink (related) -----------> DB
 |                      |                          |-- FindContradictions(allPages) --------->|                    |
 |                      |                          |                          |-- SaveWikiLink (contradicts) ------> DB
 |                      |                          |-- for orphan p: SuggestAndCreateStubs --------->|              |
 |                      |                          |                          |                     |-- LLM.Complete ...
 |                      |                          |                          |                     |<- JSON {"links":[...]}
 |                      |                          |                          |                     |-- SavePage (stub) --> DB
 |                      |                          |                          |                     |-- SaveLink(typed) --> DB
 |                      |                          |-- group-by link_type ---------------------------------------->|
 |                      |                          |<-- by_type counts -------------------------------------------|
 |                      |<---- 200 rebuildResult --|                          |                     |                |
 |                      |-- GET /wiki/graph ------>|                          |                     |                |
 |                      |<---- nodes + edges ------|                          |                     |                |
 |<-- rerender graph ---|                          |                          |                     |                |
```

### 7.2 Manual create flow

```
Client                      handleCreateWikiLink                DB
  |                                  |                            |
  |-- POST /api/wiki/links --------->|                            |
  |   {from,to,link_type,strength}   |                            |
  |                                  |-- validate JSON body       |
  |                                  |-- link_type ∈ allowlist?   |
  |                                  |-- strength ∈ [0,1]?        |
  |                                  |-- from != to?              |
  |                                  |-- GetWikiPage(from) ------>|
  |                                  |<-- 404? → 404 to client    |
  |                                  |-- GetWikiPage(to) -------->|
  |                                  |<-- 404? → 404 to client    |
  |                                  |-- SaveWikiLink ----------->|  (upsert on unique)
  |                                  |<-- saved link              |
  |<-- 201 or 200 (if upsert) -------|                            |
```

---

## 8. Error Handling

Per `.claude/rules/error-handling.md` — every error returned from DB or LLM calls is wrapped with `fmt.Errorf("...: %w", err)` and surfaced via `jsonErr` at the API boundary with HTTP-appropriate status codes.

| Condition | Response |
|-----------|----------|
| Invalid JSON body | `400 invalid request body: <err>` |
| `max_pages` out of range | `400 max_pages must be between 1 and 2000` |
| Dataset larger than max | `400 dataset has N pages, exceeds max_pages=M` |
| `link_type` not in allowlist | `400 invalid link_type: <value>` |
| `strength` out of [0,1] | `400 strength must be between 0 and 1` |
| Self-loop | `400 self-loops not allowed` |
| Missing `from_page_id`/`to_page_id` page | `404 <field> not found` |
| DB error on list/save | `500 rebuild wiki links: <wrapped err>` |
| LLM error during suggester | **Not surfaced as 500.** Counted in `suggester_errors`, logged with `slog.Warn`. The rebuild succeeds with whatever deterministic links it produced. |
| Link ID unknown on DELETE | `404 link not found` |

**Fail-open LLM.** If `s.guardianLLM == nil`, the suggester step is skipped entirely (`SuggesterInvokedFor=0`) and a log line notes it. Consistent with `engine.go:69-72`.

---

## 9. Implementation Notes

1. **Route ordering in `api/server.go`.** Add the three new routes immediately after line 441 (`GET /api/wiki/graph`). Go's `http.ServeMux` pattern matching handles `POST /api/wiki/links/rebuild` and `POST /api/wiki/links` as distinct patterns, and `DELETE /api/wiki/links/{id}` uses the `{id}` path-param syntax already in use at line 438.

2. **Linker pass ordering matters.** Run cross-references and shared-source **before** contradictions. The contradiction detector returns at most one edge per title-prefix pair, but cross-refs can produce edges in both directions for the same pair — running contradictions last lets the upsert preserve contradiction strength (0.7) over the `related` strength (0.5) only if we insert `related` first. Since upsert updates strength unconditionally (`db/wiki.go:320`), insertion order matters: last write wins per `(from, to, link_type)` tuple. Different link_types are stored as separate rows (different UNIQUE key), so `related` and `contradicts` for the same pair coexist — good.

3. **Orphan detection uses a COUNT query, not a full scan in Go.** Adding `db.CountOrphanWikiPages() (int, error)` to `db/wiki.go` keeps the handler clean. One SQL query, ~10 LOC, tested.

4. **Suggester stub creation budget.** Currently `SuggestAndCreateStubs` creates up to 5 stubs per page (`filterSuggestions` caps at 5 in `link_suggester.go:149`). The rebuild iterates over **orphans only** (say 13 pages on current DB), so the worst-case stub-creation count is 65. Acceptable; no cap change needed.

5. **Wiki page existence check in `handleCreateWikiLink`.** We deliberately call `GetWikiPage` twice (once per endpoint) before calling `SaveWikiLink`. The alternative — letting the FK constraint fire and translating the SQL error — produces opaque messages. Two extra SELECTs on a small table are fine (P2 — readable error messages are worth 2ms).

6. **No background scheduling.** Rebuild is fully manual. The handler blocks until done. Frontend shows a `Rebuilding…` state. This matches the existing pattern for `POST /api/wiki/cluster/run` (`api/server.go:445`). Concurrency is serialised via `s.rebuildMu` (see §5.1 Concurrency control) — second concurrent request receives 409.

7. **Context cancellation.** `r.Context()` cancels LLM calls inside the suggester loop. Between LLM calls, check `ctx.Err()` at the top of each iteration and return early with the counts accumulated so far. No partial rollback — saved links stay saved (P2; upsert makes re-runs idempotent).

8. **Adheres to `.claude/rules/api-parameter-passthrough.md`.** `max_pages` and `include_suggester` are validated AND passed through to the linker loop. They are not validated-then-discarded.

---

## 10. Test Plan

Per `.claude/rules/tdd-requirements.md` — tests are written alongside implementation and use the existing naming conventions (`Test<Function>_<Scenario>` for Go, existing `TestHandle...` pattern in `api/routes_wiki_test.go`).

### 10.1 `api/routes_wiki_test.go` (new tests)

```go
func TestRebuildLinks_AllRelatedIncreases(t *testing.T)
// Seed DB with 3 pages where page A's content contains page B's title.
// Before: 0 links. After POST /wiki/links/rebuild: ≥1 link of type "related".

func TestRebuildLinks_OrphansDecrease(t *testing.T)
// Seed 5 pages, 3 orphaned, 2 linked. Assert rebuildResult.OrphansBefore == 3
// and OrphansAfter < OrphansBefore after cross-ref detection hits.

func TestRebuildLinks_DetectsContradictions(t *testing.T)
// Seed two pages with same 3-word title prefix and different content hashes.
// Assert by_type["contradicts"] >= 1.

func TestRebuildLinks_MaxPagesExceeded_Returns400(t *testing.T)
// Seed 10 pages, post {max_pages: 5} → 400.

func TestRebuildLinks_MaxPagesOutOfRange_Returns400(t *testing.T)
// Post {max_pages: 0} or {max_pages: 5000} → 400.

func TestRebuildLinks_NoLLMClient_SkipsSuggester(t *testing.T)
// Server with guardianLLM=nil. Post with include_suggester=true.
// Expect 200, SuggesterInvokedFor == 0, deterministic linker still ran.

func TestRebuildLinks_ConcurrentRequests_SecondReturns409(t *testing.T)
// Hold rebuildMu via a blocking fixture. Issue second POST — expect 409.
// Release lock; second retry succeeds.

func TestCreateWikiLink_ValidatesLinkType(t *testing.T)
// POST {link_type:"related"} → 201. POST {link_type:"invalid"} → 400.

func TestCreateWikiLink_RejectsUnknownType(t *testing.T)
// Sub-test above — "friend", "x", "" all rejected with 400 "invalid link_type".

func TestCreateWikiLink_RejectsSelfLoop(t *testing.T)
// POST {from:A, to:A} → 400 "self-loops not allowed".

func TestCreateWikiLink_RejectsInvalidStrength(t *testing.T)
// strength = -0.1 → 400. strength = 1.1 → 400. strength missing → defaults to 0.5.

func TestCreateWikiLink_FromPageNotFound_Returns404(t *testing.T)
func TestCreateWikiLink_ToPageNotFound_Returns404(t *testing.T)

func TestCreateWikiLink_DuplicateReturnsUpsert(t *testing.T)
// POST the same link twice with different strength. Second POST returns 200,
// DB strength equals the second value.

func TestDeleteWikiLink_Success(t *testing.T)
func TestDeleteWikiLink_UnknownID_Returns404(t *testing.T)
```

### 10.2 `internal/insight/wiki_engine/link_suggester_test.go` (new tests)

```go
func TestLinkSuggester_ParsesTypedEdges(t *testing.T)
// Mock LLM returns {"links":[{"to_title":"Existing Page","link_type":"parent",...}]}.
// Seed memStore with "Existing Page". SuggestAndCreateStubs links to existing,
// does NOT create a stub. Saved link.LinkType == "parent".

func TestLinkSuggester_ParsesChildAndCites(t *testing.T)
// Mock LLM returns a mix of "child" and "cites". All types preserved in saved links.

func TestLinkSuggester_FallsBackToRelatedOnInvalidType(t *testing.T)
// Mock returns {"link_type":"friend"} → saved link has LinkType="related".

func TestLinkSuggester_StubModeStillWorks(t *testing.T)
// Mock returns {"to_title":"", "title":"New Concept", "link_type":"related",...}.
// Expect 1 new page created + 1 link saved (current behaviour preserved).

func TestLinkSuggester_ParsesObjectWrappedResponse(t *testing.T)
// Confirm prompt-level contract: {"links":[...]} parses correctly.

func TestLinkSuggester_AmbiguousToTitle_PicksNewest(t *testing.T)
// Seed two pages with identical title, different updated_at. LLM returns
// {"to_title":"Shared Title", ...}. Expect link.ToPageID == newer page.
```

### 10.2.1 `internal/insight/prompts/prompts_test.go` (churn)

Existing prompt snapshot tests that assert the exact `WikiLinkSuggestion` string MUST be updated to match the new prompt (see §6). Tracked as part of Deliverable B.

### 10.3 `db/wiki_test.go` (additions)

```go
func TestCountOrphanWikiPages(t *testing.T)
// Seed 3 pages, 1 with outgoing link, 1 with incoming link, 1 isolated.
// Expect count == 1.

func TestDeleteWikiLinkByID_Success(t *testing.T)
// Save link, delete by ID, assert (true, nil) and row gone.

func TestDeleteWikiLinkByID_NotFound(t *testing.T)
// Delete unknown ID, expect (false, nil).
```

Backwards-compat: all existing `db/wiki_test.go` tests must pass unchanged. The schema is not modified.

### 10.4 Manual smoke test (post-implementation)

On the active DB (`/home/martin/.stratus/data/54a303554276/stratus.db`, 38 pages, 13 orphans):

```bash
curl -X POST http://localhost:41777/api/wiki/links/rebuild \
     -H 'content-type: application/json' \
     -d '{"max_pages":500,"include_suggester":true}' | jq
```

Expect `orphans_after ≤ 3`, `by_type` with ≥ 2 non-zero entries. Load `http://localhost:41777/#/wiki` → Graph tab → verify edges render in multiple colours.

---

## 11. Risk / Tradeoffs

| Risk | Impact | Mitigation |
|------|--------|-----------|
| LLM produces invalid `link_type` values | Graph has wrong-coloured edges | `normalizeLinkType` fallback to `related`. Tested in `TestLinkSuggester_FallsBackToRelatedOnInvalidType`. |
| Cross-reference detection is O(N²) — N × title-substring scan | At N=500 that's 250k `strings.Contains` on content lengths up to 10k chars → could approach seconds | Cap `max_pages` at 2000. Document in response that larger datasets need a future background job. Current DB has N=38; practical headroom is large. |
| LLM latency × orphan count | With 13 orphans and 2–5 s/call → 30–60 s total for rebuild | Acceptable for manual trigger. UI shows `Rebuilding…`. Future work: background job. |
| Duplicate edges on re-run | Strength overwrites from previous run | Desired behaviour — upsert (existing) means re-running refines strengths without duplicating rows. |
| Strength update on re-run clobbers user-set value | User manually sets strength=0.9, rebuild runs and linker writes 0.5 | Accepted tradeoff. Manual CRUD is an advanced feature; users who curate edges can re-curate after rebuild. Logging the pre-rebuild strengths would add complexity without clear demand (P2). |
| SQLite FKs require `PRAGMA foreign_keys=ON` | Dangling edges possible if pragma is off | Defensive existence checks in `handleCreateWikiLink` bypass the issue. For rebuild, the Linker reads `allPages` in-memory and never forges a reference to a missing ID. |
| Prompt changes regress stub creation | Existing onboarding flow that uses `SuggestAndCreateStubs` may produce fewer/different stubs | `TestLinkSuggester_StubModeStillWorks` explicitly asserts the stub-creation path still functions. |
| Non-deterministic LLM output | Rebuild is not idempotent across runs | Acceptable — the UI re-fetches after rebuild. Upsert means duplicate edges do not proliferate. |
| New `POST /api/wiki/links` enables spam | User can flood edges via API | Out of scope — same API surface as all other stratus endpoints (no auth layer currently). Document as future hardening. |

---

## 12. Governance Notes

- **`.claude/rules/api-parameter-passthrough.md`** — `max_pages` and `include_suggester` are validated then forwarded into the rebuild routine. Neither is discarded.
- **`.claude/rules/config-validation.md`** — `strength ∈ [0,1]`, `max_pages ∈ [1, 2000]`, `link_type ∈ allowlist` all return 400 on violation with specific messages.
- **`.claude/rules/error-handling.md`** — all DB/LLM errors wrapped with `fmt.Errorf("<context>: %w", err)`. No bare returns. `slog.Warn` used for non-fatal suggester errors.
- **`.claude/rules/tdd-requirements.md`** — tests listed in §10 are written first. Coverage target ≥ 80 % on new code.
- **`.claude/rules/karpathy-principles.md`**:
  - **P2 (Simplicity):** Reuses `Linker`, `LinkSuggester`, existing DB methods. No new tables. Single new DB helper (`CountOrphanWikiPages`) and one new delete-by-ID method. Total ≤ 500 non-test LOC.
  - **P3 (Surgical):** Changes only wiki-related files. No refactors of `api/routes_wiki.go` or `link_suggester.go` beyond what the feature requires. Legend in `Wiki.svelte` is untouched.
  - P1 and P4 apply implicitly to the implementation phase (planning complete here, verification in §10 SC-1..SC-6).

---

## 13. Breaking Changes

**None for end users.** The `wiki_links.link_type` column already accepts any TEXT value; existing `related` rows remain valid.

**Internal API surface:**

- `wiki_engine.StubSuggestion` (type) is extended with new fields: `LinkType`, `Strength`, `ToTitle`. Existing consumers (`SuggestAndCreateStubs`) still work because the new fields are optional and have safe defaults. External consumers: none (the type is used only within the `wiki_engine` package, confirmed via grep).
- `prompts.WikiLinkSuggestion` (string constant) content changed. Any test that asserts on the exact prompt string would break. `grep -r WikiLinkSuggestion` shows uses at `link_suggester.go:49` and `prompts/prompts_test.go` — the prompts test would need to be updated (a 5-line churn) to accept the new schema. Flagged for the implementer.

No wire-format breakage, no DB migration, no consumer migration.

---

## 14. Out-of-Scope Followups

Documented here explicitly so they are not silently re-introduced into this spec:

- **Embedding-based similarity** (vexor/wiki page embeddings → cosine > threshold → related edge). Bigger design; would duplicate code-search infra for wiki. Defer.
- **Background scheduler** for periodic `rebuild`. Would need a job queue, progress endpoint, WS broadcast for completion. Defer until manual trigger shows it is needed.
- **UI edge editor** (drag-to-create, right-click-delete). Manual CRUD in this spec is API-only; the frontend only gains the Rebuild button.
- **`supersedes` detection.** Requires page versioning / supersession metadata that does not exist yet. Defer.
- **Prompt A/B per model.** The gemma4 JSON-mode preference is documented in user memory; if another default LLM is adopted, the prompt may need tuning. Not in scope now.
- **Per-page re-link endpoint** (`POST /api/wiki/pages/{id}/relink`). Would be cheaper than full rebuild. Trivially derivable from this design if demand appears.

---

## 15. Key Files (absolute paths)

- `/home/martin/Documents/projects/stratus-v2/db/schema.go` (lines 826–838) — wiki_links DDL, unchanged
- `/home/martin/Documents/projects/stratus-v2/db/wiki.go` (lines 311–466) — add `DeleteWikiLinkByID`, `CountOrphanWikiPages`
- `/home/martin/Documents/projects/stratus-v2/internal/insight/wiki_engine/linker.go` — reused, unchanged
- `/home/martin/Documents/projects/stratus-v2/internal/insight/wiki_engine/link_suggester.go` — modified (deliverable B)
- `/home/martin/Documents/projects/stratus-v2/internal/insight/prompts/prompts.go` (line 36) — prompt rewrite
- `/home/martin/Documents/projects/stratus-v2/api/routes_wiki_links.go` — **new file** (deliverables A + C)
- `/home/martin/Documents/projects/stratus-v2/api/server.go` (line 441) — 3 new route registrations
- `/home/martin/Documents/projects/stratus-v2/api/routes_wiki_test.go` — add tests from §10.1
- `/home/martin/Documents/projects/stratus-v2/internal/insight/wiki_engine/link_suggester_test.go` — add tests from §10.2
- `/home/martin/Documents/projects/stratus-v2/db/wiki_test.go` — add tests from §10.3
- `/home/martin/Documents/projects/stratus-v2/frontend/src/lib/api.ts` (line 324) — 3 new client functions
- `/home/martin/Documents/projects/stratus-v2/frontend/src/routes/Wiki.svelte` (line 139) — Rebuild Graph button

