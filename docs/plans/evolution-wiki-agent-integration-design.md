# Technical Design: Evolution Wiki Agent Integration

## Context & Problem

The evolution loop (`insight/engine.go`) generates wiki pages containing codebase findings
(patterns, anti-patterns, architectural insights) but these pages are invisible to delivery
agents for two reasons:

1. **Status is "draft"** — pages are created with `Status: "draft"` (insight/engine.go:272),
   which signals they are not finalized. While `SearchWikiPages` does not filter by status
   today, the semantic meaning of "draft" discourages surfacing them as authoritative.

2. **No agent prompts mention wiki retrieval** — the 7 implementation-oriented delivery agents
   only call `mcp__stratus__retrieve` with `corpus: "code"`. Evolution findings stored in wiki
   are never consulted during task execution.

3. **No scoring signal** — evolution-generated pages are scored identically to all other wiki
   pages. Given that evolution findings represent validated, system-generated insights, they
   deserve a slight ranking advantage over user-written or ingested pages.

## Design

Three coordinated changes, all additive, no breaking changes.

### Change 1: Wiki Page Status — "draft" to "published"

**File:** `insight/engine.go:272`

```diff
 page := &db.WikiPage{
     ID:          uuid.NewString(),
     PageType:    "concept",
     Title:       fmt.Sprintf("Evolution Finding: %s", h.Description),
     Content:     content,
-    Status:      "draft",
+    Status:      "published",
     Tags:        []string{"evolution", h.Category},
     GeneratedBy: "evolution",
     Version:     1,
 }
```

**Rationale:** Evolution findings have already passed LLM validation in the evolution loop
before `wikiFn` is called. They are not provisional — they represent confirmed hypotheses.
Publishing them immediately makes them first-class wiki content.

**Impact:** Existing draft evolution pages in databases will remain "draft". This is acceptable —
only new findings get the change. A one-time migration (`UPDATE wiki_pages SET status = 'published' WHERE generated_by = 'evolution' AND status = 'draft'`) can be run manually if needed.

### Change 2: Retrieve Scoring Boost for Evolution Pages

**File:** `api/routes_retrieval.go:82-98`

The `WikiPage` struct already carries `GeneratedBy string` (db/wiki.go:25), and
`SearchWikiPages` (db/wiki.go:221) returns it. The scoring loop in `runRetrieve` currently
applies position decay and a staleness penalty but has no source-quality signal.

```diff
 for i, p := range wikiPages {
     score := 1.0 - float64(i)*0.1
     if score < 0.1 {
         score = 0.1
     }
     if p.StalenessScore > 0.7 {
         score *= 0.5
     }
+    // Evolution findings boost — validated system-generated insights
+    // rank slightly higher than user-written pages.
+    if p.GeneratedBy == "evolution" {
+        score *= 1.2
+    }
     results = append(results, retrieveResult{
         Source:         "wiki",
         Title:          p.Title,
```

**Boost factor:** 1.2x is conservative. A page at position 0 goes from 1.0 to 1.2; a stale
evolution page (staleness > 0.7) goes from 0.5 to 0.6 — still below a fresh non-evolution
page at 1.0. The boost cannot cause evolution pages to dominate results.

**Score ceiling consideration:** With the boost, the maximum possible wiki score is 1.2 (position 0,
fresh, evolution). Code and governance results use their own scoring, so cross-source ranking is
unaffected — results are already grouped by source in the response.

### Change 3: Agent Prompt Updates — Wiki Retrieve Instruction

**Files (7 agents):**

| File | Line to modify |
|------|---------------|
| `cmd/stratus/agents/delivery-backend-engineer.md` | line 22 (Understand step) |
| `cmd/stratus/agents/delivery-frontend-engineer.md` | line 22 (Understand step) |
| `cmd/stratus/agents/delivery-database-engineer.md` | line 21 (Understand step) |
| `cmd/stratus/agents/delivery-implementation-expert.md` | line 21 (Understand step) |
| `cmd/stratus/agents/delivery-system-architect.md` | line 25 (Read the codebase step) |
| `cmd/stratus/agents/delivery-strategic-architect.md` | line 29 (Assess step) |
| `cmd/stratus/agents/delivery-debugger.md` | line 25 (Trace step) |

**Change pattern:** After each agent's existing `mcp__stratus__retrieve` with `corpus: "code"` line,
append:

```
   Use `mcp__stratus__retrieve` with `corpus: "wiki"` to check for evolution findings and existing knowledge relevant to this task.
```

**Example diff for delivery-backend-engineer.md:**

```diff
 ## Workflow

-1. **Understand** — Read the task and explore existing backend code. Use `mcp__stratus__retrieve` MCP tool with `corpus: "code"` to find existing patterns.
+1. **Understand** — Read the task and explore existing backend code. Use `mcp__stratus__retrieve` MCP tool with `corpus: "code"` to find existing patterns. Use `mcp__stratus__retrieve` with `corpus: "wiki"` to check for evolution findings and existing knowledge relevant to this task.
 2. **Test first** — Write a failing test that captures the expected behavior (TDD).
```

**Agents NOT changed (and why):**
- `delivery-code-reviewer.md` — reviews code, does not need wiki context for implementation
- `delivery-qa-engineer.md` — writes tests from specs, not from wiki findings
- `delivery-devops-engineer.md` — infrastructure work, evolution findings are code-level
- `delivery-mobile-engineer.md` — mobile-specific, evolution loop targets backend patterns
- `delivery-ux-designer.md` — design-focused, not code pattern consumption
- `delivery-skill-creator.md` — skill authoring, separate concern
- `delivery-governance-checker.md` — uses governance corpus, not wiki

## File Map

| File | Change Type | Description |
|------|------------|-------------|
| `insight/engine.go:272` | Modify | `"draft"` to `"published"` |
| `api/routes_retrieval.go:89` | Add | 3-line evolution boost block after staleness penalty |
| `api/routes_retrieval_test.go` | Add | New test `TestHandleRetrieve_EvolutionBoost` |
| `cmd/stratus/agents/delivery-backend-engineer.md:22` | Modify | Add wiki retrieve instruction |
| `cmd/stratus/agents/delivery-frontend-engineer.md:22` | Modify | Add wiki retrieve instruction |
| `cmd/stratus/agents/delivery-database-engineer.md:21` | Modify | Add wiki retrieve instruction |
| `cmd/stratus/agents/delivery-implementation-expert.md:21` | Modify | Add wiki retrieve instruction |
| `cmd/stratus/agents/delivery-system-architect.md:25` | Modify | Add wiki retrieve instruction |
| `cmd/stratus/agents/delivery-strategic-architect.md:29` | Modify | Add wiki retrieve instruction |
| `cmd/stratus/agents/delivery-debugger.md:25` | Modify | Add wiki retrieve instruction |

## Sequence Diagram

```
sequenceDiagram
    participant EvoLoop as Evolution Loop
    participant DB as SQLite (wiki_pages)
    participant Agent as Delivery Agent
    participant API as Retrieve API

    EvoLoop->>DB: SavePage(status="published", generated_by="evolution")
    Note over DB: Page is immediately searchable via FTS5

    Agent->>API: GET /api/retrieve?q=pattern&corpus=wiki
    API->>DB: SearchWikiPages("pattern", "", topK)
    DB-->>API: []WikiPage (includes evolution pages)
    API->>API: Score: position decay → staleness penalty → evolution boost (1.2x)
    API-->>Agent: [{source:"wiki", title:"Evolution Finding: ...", score: 1.2}]
    Agent->>Agent: Incorporate finding into implementation
```

## Test Plan

### Test 1: Evolution Scoring Boost (api/routes_retrieval_test.go)

```go
func TestHandleRetrieve_EvolutionBoost(t *testing.T) {
    database := setupTestDB(t)
    defer database.Close()

    server := newRetrievalServer(t, database)

    // Two pages with identical content relevance, different generated_by
    evoPage := &db.WikiPage{
        ID: "evo-boost-1", PageType: "concept",
        Title: "Evolution Pattern", Content: "error handling retry pattern",
        Status: "published", GeneratedBy: "evolution", StalenessScore: 0.0,
    }
    normalPage := &db.WikiPage{
        ID: "evo-boost-2", PageType: "concept",
        Title: "Normal Pattern", Content: "error handling retry pattern",
        Status: "published", GeneratedBy: "ingest", StalenessScore: 0.0,
    }

    for _, p := range []*db.WikiPage{evoPage, normalPage} {
        if err := database.SaveWikiPage(p); err != nil {
            t.Fatalf("SaveWikiPage %s: %v", p.ID, err)
        }
    }

    req := httptest.NewRequest("GET",
        "/api/retrieve?q=error+handling+retry&corpus=wiki&top_k=10", nil)
    w := httptest.NewRecorder()
    server.handleRetrieve(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
    }

    var resp map[string]any
    json.NewDecoder(w.Body).Decode(&resp)
    results := resp["results"].([]any)

    var evoScore, normalScore float64
    for _, r := range results {
        result := r.(map[string]any)
        title := result["title"].(string)
        score := result["score"].(float64)
        if title == "Evolution Pattern" {
            evoScore = score
        }
        if title == "Normal Pattern" {
            normalScore = score
        }
    }

    // Evolution page must score higher (1.2x boost)
    if evoScore <= normalScore {
        t.Errorf("evolution page score (%v) should be > normal page score (%v)",
            evoScore, normalScore)
    }
}
```

**Note:** FTS5 rank ordering may place the two pages at different positions, which affects the
position-decay component. If both pages match identically, they will be at adjacent positions
(e.g., 0 and 1), giving scores of 1.2 and 0.9. If FTS5 does not return both, the test should
skip (same pattern used in `TestHandleRetrieve_StalePenalty`).

### Test 2: Agent Prompt Verification

Verify all 7 agent files contain the wiki retrieve instruction. This can be validated via grep:

```bash
for agent in delivery-backend-engineer delivery-frontend-engineer \
  delivery-database-engineer delivery-implementation-expert \
  delivery-system-architect delivery-strategic-architect delivery-debugger; do
    grep -q 'corpus: "wiki"' "cmd/stratus/agents/${agent}.md" || echo "MISSING: ${agent}"
done
```

A Go test is not necessary for embedded markdown files — grep verification during code review
is sufficient.

### Test 3: Evolution Status (existing test coverage)

The existing `insight/engine_wiki_evo_test.go` tests the evolution wiki flow. After the change,
verify that new pages are created with `status: "published"` by checking the assertion in that
test file.

## Error Handling

No new error paths are introduced:

- **Change 1** is a string literal change — no error possible.
- **Change 2** adds a conditional score multiplier — no error possible. The `GeneratedBy` field
  is always populated (defaults to empty string if not set; only `"evolution"` triggers the boost).
- **Change 3** is prompt text — no runtime behavior.

## Risks & Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| Evolution findings contain incorrect insights | Medium | Evolution loop already validates hypotheses via LLM before creating wiki pages. Staleness detection (existing) will degrade stale findings over time. The 1.2x boost is conservative — it does not override position or staleness signals. |
| Agents make unnecessary wiki calls (latency) | Low | Wiki search is FTS5 on local SQLite — sub-millisecond. No network call. If no wiki pages exist, `SearchWikiPages` returns empty slice immediately. |
| Score inflation pushes irrelevant evolution pages above relevant non-evolution pages | Low | 1.2x boost only matters when pages are at the same FTS5 rank position. A non-evolution page at position 0 (score 1.0) still beats an evolution page at position 1 (score 1.08). The boost is a tiebreaker, not a ranking override. |
| Existing draft evolution pages remain invisible to status-aware consumers | Low | No current consumer filters by status. If a future consumer does, a one-time SQL migration can backfill: `UPDATE wiki_pages SET status = 'published' WHERE generated_by = 'evolution' AND status = 'draft'` |
| Agent prompts become too long with added instruction | Negligible | One sentence added per agent. Well within token budgets. |

## Breaking Changes

None. All changes are additive:
- Status change: "published" is an existing valid status value (db/wiki.go:20)
- Score boost: only increases scores, never decreases
- Agent prompts: additive instruction, no removal of existing behavior
