# Self-Evolving System — Technical Design Document

**Workflow:** spec-self-evolving-system
**Date:** 2026-04-06
**Status:** Proposed
**Complexity:** Complex

---

## Overview

The Self-Evolving System introduces three interconnected subsystems into Stratus, inspired by Karpathy's LLM-Wiki pattern, Autoresearch, and AI-2027 self-improvement concepts:

1. **Knowledge Wiki** — LLM-generated, interlinked markdown pages in SQLite that synthesize insights from raw events, trajectories, solution patterns, and artifacts
2. **Evolution Loop** — Time-bounded goroutine that generates hypotheses, runs experiments, evaluates results, and auto-applies or proposes improvements
3. **Synthesis Query** — MCP tool + API that searches wiki, produces LLM answers with citations, and optionally persists answers

## Architecture Decisions

### ADR-001: Wiki Storage — Dedicated Tables (not extending events/docs)
- Events are append-only with dedup; wiki pages are mutable (revised on new data)
- Docs are keyed by `(file_path, chunk_index)` assuming filesystem source; wiki pages are generated
- Dedicated `wiki_pages` table with its own FTS5 allows wiki-specific indexes and columns

### ADR-002: Evolution Loop — Lightweight Goroutine (not Swarm)
- Swarm assumes git worktrees with code changes; evolution modifies DB state (prompt diffs, routing scores)
- Guardian pattern (ticker + config refresh + context.WithTimeout) is proven and simple
- Sequential execution: ~1 hypothesis per time-budget window, matching Autoresearch cadence

### ADR-003: Evolution-to-Learning Integration
- User-visible changes (rules, skills, ADRs) → Learning proposal pipeline (human approval)
- Internal changes (routing scores, thresholds, bandit params) → auto-apply above confidence threshold
- Auto-revert if Guardian detects scorecard degradation >10% post-change

---

## 1. Data Models

### WikiPage

```go
// db/wiki.go
type WikiPage struct {
    ID             string         `json:"id"`
    PageType       string         `json:"page_type"`       // summary | entity | concept | answer | index
    Title          string         `json:"title"`
    Content        string         `json:"content"`          // markdown body
    Status         string         `json:"status"`           // draft | published | stale | archived
    StalenessScore float64        `json:"staleness_score"`  // 0.0-1.0
    SourceHashes   []string       `json:"source_hashes"`    // hashes of source data used to generate
    Tags           []string       `json:"tags"`
    Metadata       map[string]any `json:"metadata"`
    GeneratedBy    string         `json:"generated_by"`     // ingest | query | maintenance | evolution
    Version        int            `json:"version"`
    CreatedAt      string         `json:"created_at"`
    UpdatedAt      string         `json:"updated_at"`
}
```

### WikiLink

```go
type WikiLink struct {
    ID         string  `json:"id"`
    FromPageID string  `json:"from_page_id"`
    ToPageID   string  `json:"to_page_id"`
    LinkType   string  `json:"link_type"` // related | parent | child | contradicts | supersedes | cites
    Strength   float64 `json:"strength"`  // 0.0-1.0
    CreatedAt  string  `json:"created_at"`
}
```

### WikiPageRef

```go
type WikiPageRef struct {
    ID         string `json:"id"`
    PageID     string `json:"page_id"`
    SourceType string `json:"source_type"` // event | trajectory | artifact | solution_pattern | problem_stats
    SourceID   string `json:"source_id"`
    Excerpt    string `json:"excerpt"`
    CreatedAt  string `json:"created_at"`
}
```

### EvolutionRun

```go
// db/evolution.go
type EvolutionRun struct {
    ID               string         `json:"id"`
    TriggerType      string         `json:"trigger_type"`      // scheduled | manual | event_driven
    Status           string         `json:"status"`            // running | completed | failed | timeout
    HypothesesCount  int            `json:"hypotheses_count"`
    ExperimentsRun   int            `json:"experiments_run"`
    AutoApplied      int            `json:"auto_applied"`
    ProposalsCreated int            `json:"proposals_created"`
    WikiPagesUpdated int            `json:"wiki_pages_updated"`
    DurationMs       int64          `json:"duration_ms"`
    TimeoutMs        int64          `json:"timeout_ms"`
    ErrorMessage     string         `json:"error_message,omitempty"`
    Metadata         map[string]any `json:"metadata"`
    StartedAt        string         `json:"started_at"`
    CompletedAt      *string        `json:"completed_at,omitempty"`
    CreatedAt        string         `json:"created_at"`
}
```

### EvolutionHypothesis

```go
type EvolutionHypothesis struct {
    ID               string         `json:"id"`
    RunID            string         `json:"run_id"`
    Category         string         `json:"category"`         // prompt_tuning | workflow_routing | agent_selection | threshold_adjustment
    Description      string         `json:"description"`
    BaselineValue    string         `json:"baseline_value"`   // JSON-encoded current state
    ProposedValue    string         `json:"proposed_value"`   // JSON-encoded proposed change
    Metric           string         `json:"metric"`           // success_rate | review_pass_rate | cycle_time | rework_rate
    BaselineMetric   float64        `json:"baseline_metric"`
    ExperimentMetric float64        `json:"experiment_metric"`
    Confidence       float64        `json:"confidence"`
    Decision         string         `json:"decision"`         // auto_applied | proposal_created | rejected | inconclusive
    DecisionReason   string         `json:"decision_reason"`
    WikiPageID       *string        `json:"wiki_page_id,omitempty"`
    Evidence         map[string]any `json:"evidence"`
    CreatedAt        string         `json:"created_at"`
}
```

---

## 2. Database Schema

Append to `db/schema.go` DDL constant:

```sql
-- Wiki: Pages
CREATE TABLE IF NOT EXISTS wiki_pages (
    id               TEXT PRIMARY KEY,
    page_type        TEXT NOT NULL DEFAULT 'summary',
    title            TEXT NOT NULL,
    content          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'draft',
    staleness_score  REAL NOT NULL DEFAULT 0,
    source_hashes_json TEXT NOT NULL DEFAULT '[]',
    tags_json        TEXT NOT NULL DEFAULT '[]',
    metadata_json    TEXT NOT NULL DEFAULT '{}',
    generated_by     TEXT NOT NULL DEFAULT 'ingest',
    version          INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_wiki_pages_type ON wiki_pages(page_type);
CREATE INDEX IF NOT EXISTS idx_wiki_pages_status ON wiki_pages(status);
CREATE INDEX IF NOT EXISTS idx_wiki_pages_staleness ON wiki_pages(staleness_score DESC);
CREATE INDEX IF NOT EXISTS idx_wiki_pages_updated ON wiki_pages(updated_at DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS wiki_pages_fts USING fts5(
    title, content, tags_json,
    content='wiki_pages', content_rowid='rowid',
    tokenize='porter unicode61'
);

CREATE TRIGGER IF NOT EXISTS wiki_pages_ai AFTER INSERT ON wiki_pages BEGIN
    INSERT INTO wiki_pages_fts(rowid, title, content, tags_json)
    VALUES (new.rowid, new.title, new.content, new.tags_json);
END;
CREATE TRIGGER IF NOT EXISTS wiki_pages_au AFTER UPDATE ON wiki_pages BEGIN
    INSERT INTO wiki_pages_fts(wiki_pages_fts, rowid, title, content, tags_json)
    VALUES ('delete', old.rowid, old.title, old.content, old.tags_json);
    INSERT INTO wiki_pages_fts(rowid, title, content, tags_json)
    VALUES (new.rowid, new.title, new.content, new.tags_json);
END;
CREATE TRIGGER IF NOT EXISTS wiki_pages_ad AFTER DELETE ON wiki_pages BEGIN
    INSERT INTO wiki_pages_fts(wiki_pages_fts, rowid, title, content, tags_json)
    VALUES ('delete', old.rowid, old.title, old.content, old.tags_json);
END;

-- Wiki: Links
CREATE TABLE IF NOT EXISTS wiki_links (
    id           TEXT PRIMARY KEY,
    from_page_id TEXT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    to_page_id   TEXT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    link_type    TEXT NOT NULL DEFAULT 'related',
    strength     REAL NOT NULL DEFAULT 0.5,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(from_page_id, to_page_id, link_type)
);

CREATE INDEX IF NOT EXISTS idx_wiki_links_from ON wiki_links(from_page_id);
CREATE INDEX IF NOT EXISTS idx_wiki_links_to ON wiki_links(to_page_id);

-- Wiki: Page References (citations to raw sources)
CREATE TABLE IF NOT EXISTS wiki_page_refs (
    id          TEXT PRIMARY KEY,
    page_id     TEXT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    source_type TEXT NOT NULL,
    source_id   TEXT NOT NULL,
    excerpt     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(page_id, source_type, source_id)
);

CREATE INDEX IF NOT EXISTS idx_wiki_refs_page ON wiki_page_refs(page_id);
CREATE INDEX IF NOT EXISTS idx_wiki_refs_source ON wiki_page_refs(source_type, source_id);

-- Evolution: Runs
CREATE TABLE IF NOT EXISTS evolution_runs (
    id                 TEXT PRIMARY KEY,
    trigger_type       TEXT NOT NULL DEFAULT 'scheduled',
    status             TEXT NOT NULL DEFAULT 'running',
    hypotheses_count   INTEGER NOT NULL DEFAULT 0,
    experiments_run    INTEGER NOT NULL DEFAULT 0,
    auto_applied       INTEGER NOT NULL DEFAULT 0,
    proposals_created  INTEGER NOT NULL DEFAULT 0,
    wiki_pages_updated INTEGER NOT NULL DEFAULT 0,
    duration_ms        INTEGER NOT NULL DEFAULT 0,
    timeout_ms         INTEGER NOT NULL DEFAULT 0,
    error_message      TEXT NOT NULL DEFAULT '',
    metadata_json      TEXT NOT NULL DEFAULT '{}',
    started_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at       TEXT,
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_evolution_runs_status ON evolution_runs(status);
CREATE INDEX IF NOT EXISTS idx_evolution_runs_started ON evolution_runs(started_at DESC);

-- Evolution: Hypotheses
CREATE TABLE IF NOT EXISTS evolution_hypotheses (
    id                TEXT PRIMARY KEY,
    run_id            TEXT NOT NULL REFERENCES evolution_runs(id) ON DELETE CASCADE,
    category          TEXT NOT NULL,
    description       TEXT NOT NULL,
    baseline_value    TEXT NOT NULL DEFAULT '{}',
    proposed_value    TEXT NOT NULL DEFAULT '{}',
    metric            TEXT NOT NULL DEFAULT '',
    baseline_metric   REAL NOT NULL DEFAULT 0,
    experiment_metric REAL NOT NULL DEFAULT 0,
    confidence        REAL NOT NULL DEFAULT 0,
    decision          TEXT NOT NULL DEFAULT 'inconclusive',
    decision_reason   TEXT NOT NULL DEFAULT '',
    wiki_page_id      TEXT,
    evidence_json     TEXT NOT NULL DEFAULT '{}',
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_evolution_hypotheses_run ON evolution_hypotheses(run_id);
CREATE INDEX IF NOT EXISTS idx_evolution_hypotheses_category ON evolution_hypotheses(category);
CREATE INDEX IF NOT EXISTS idx_evolution_hypotheses_decision ON evolution_hypotheses(decision);
```

---

## 3. API Contracts

### Wiki Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/wiki/pages` | List pages (query: type, status, tag, limit, offset) |
| GET | `/api/wiki/pages/{id}` | Get page with links and refs |
| GET | `/api/wiki/search?q=...` | FTS5 search (query: q, type, limit) |
| POST | `/api/wiki/query` | Synthesis query → LLM answer with citations |
| GET | `/api/wiki/graph` | Page link graph for visualization |

### Evolution Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/evolution/runs` | List runs (query: status, limit, offset) |
| GET | `/api/evolution/runs/{id}` | Run details with hypotheses |
| POST | `/api/evolution/trigger` | Manual trigger (body: timeout_ms, categories) |
| GET | `/api/evolution/config` | Get config |
| POST | `/api/evolution/config` | Update config |

### POST /api/wiki/query Request/Response

```json
// Request
{
  "query": "What patterns work best for database migration bugs?",
  "persist": true,
  "max_sources": 10
}

// Response
{
  "answer": "## Database Migration Bug Patterns\n\nBased on 14 resolved incidents...",
  "citations": [
    {"source_type": "solution_pattern", "source_id": "sp-123", "excerpt": "...", "relevance": 0.95},
    {"source_type": "event", "source_id": "evt-456", "excerpt": "...", "relevance": 0.82}
  ],
  "wiki_page_id": "wp-789",
  "tokens_used": 1247
}
```

---

## 4. MCP Tools

4 new tools in `mcp/tools.go`:

| Tool | Description | Key Params |
|------|-------------|------------|
| `wiki_search` | Search knowledge wiki pages | query (required), type, limit |
| `wiki_query` | Synthesis query with LLM-generated answer + citations | query (required), persist, max_sources |
| `evolution_status` | Get recent evolution runs and stats | limit |
| `evolution_trigger` | Manually trigger an evolution cycle | timeout_ms, categories |

---

## 5. Wiki Engine Design

**Package:** `internal/insight/wiki_engine/`

### Error Handling

All LLM calls use the existing `internal/insight/llm/retry.go` pattern with context-wrapped errors:
- `fmt.Errorf("wiki ingest: generate page %q: %w", title, err)` 
- `fmt.Errorf("wiki synthesize: llm completion: %w", err)`
- On LLM failure: log warning, skip page generation, continue with next source (fail-open for ingest, fail-closed for synthesis query returning 503)

### Ingest Workflow

Triggered by insight scheduler tick (after `RunKnowledgeAnalysis`):

1. Get new artifacts/trajectories/solution patterns since last ingest
2. For each source:
   - Compute source hash: SHA256(source_type + source_id + content_hash)
   - Find existing page by source hash → update (version+1) or create new
   - Call LLM to generate/update markdown page content
   - Detect cross-references (lexical title matching + shared source refs)
   - Save links and refs
3. Update index pages

**Page type mapping:**
- Artifact with root_cause + solution_pattern → `entity` page
- Trajectory pattern with optimal sequence → `concept` page
- Solution pattern cluster → `summary` page
- Auto-generated ToC → `index` page

### Maintenance Workflow

**Staleness scoring:**
```
staleness = 0.3 * (days_since_update / 30)
           + 0.3 * (changed_source_count / total_refs)
           + 0.2 * (orphan ? 1 : 0)
           + 0.2 * (contradiction_count > 0 ? 1 : 0)
```

Pages with `staleness_score > 0.7` → status `stale` → re-generated on next ingest.

### Cross-Reference Detection

- **Lexical:** Scan content for mentions of other page titles
- **Semantic:** Pages sharing 2+ source refs to same entity → `related` link
- **Hierarchical:** Entity pages link to summary via `child`/`parent`
- **Contradiction:** Pages asserting different best_agent for same problem_class → `contradicts` link

---

## 6. Evolution Loop Design

**Package:** `internal/insight/evolution_loop/`

### Flow

1. `context.WithTimeout(ctx, config.TimeoutMs)`
2. Save `EvolutionRun{status: running}`
3. Generate hypotheses from wiki + scorecards + trajectories + patterns
4. For each hypothesis (until timeout):
   a. Load historical trajectories matching hypothesis domain
   b. Simulate with modified parameters
   c. Compute confidence: `min(1.0, sqrt(sampleSize/minSampleSize)) * (1-pValue)`
   d. Decision:
      - confidence >= autoApplyThreshold AND internal → auto-apply
      - confidence >= proposalThreshold AND user-visible → create Learning proposal
      - else → rejected/inconclusive
   e. Update wiki with finding
5. Save `EvolutionRun{status: completed, stats}`

### Hypothesis Categories

| Category | What It Tunes | Decision Path |
|----------|--------------|---------------|
| `prompt_tuning` | Agent system prompt fragments | Learning proposal (user approval) |
| `workflow_routing` | Routing confidence thresholds | Auto-apply (internal) |
| `agent_selection` | Problem-class to agent mapping | Auto-apply (internal) |
| `threshold_adjustment` | Insight engine numeric thresholds | Auto-apply (internal) |

### Integration

- Runs as last step in insight scheduler tick
- Uses `sync.Mutex` — only one cycle at a time
- Guardian monitors post-change scorecard trends for auto-revert

---

## 7. Config Schema

```go
// config/config.go
type WikiConfig struct {
    Enabled            bool    `json:"enabled"`
    IngestOnEvent      bool    `json:"ingest_on_event"`
    MaxPagesPerIngest  int     `json:"max_pages_per_ingest"`
    StalenessThreshold float64 `json:"staleness_threshold"`
    MaxPageSizeTokens  int     `json:"max_page_size_tokens"`
}

type EvolutionConfig struct {
    Enabled             bool     `json:"enabled"`
    TimeoutMs           int64    `json:"timeout_ms"`
    MaxHypothesesPerRun int      `json:"max_hypotheses_per_run"`
    AutoApplyThreshold  float64  `json:"auto_apply_threshold"`
    ProposalThreshold   float64  `json:"proposal_threshold"`
    MinSampleSize       int      `json:"min_sample_size"`
    Categories          []string `json:"categories"`
}
```

Added to `Config` struct:
```go
type Config struct {
    // ... existing fields ...
    Wiki      WikiConfig      `json:"wiki"`
    Evolution EvolutionConfig `json:"evolution"`
}
```

**Defaults** (in `Default()`): Wiki enabled=false, Evolution enabled=false, TimeoutMs=120000, AutoApplyThreshold=0.85, ProposalThreshold=0.65, MinSampleSize=10, DailyTokenBudget=100000

### Input Validation (API boundaries)

- `POST /api/wiki/query`: `query` max 2000 chars; `max_sources` capped at 50
- `POST /api/evolution/trigger`: `timeout_ms` capped at 600000 (10 min); `categories` validated against allowed set
- All string inputs sanitized before FTS5 queries via existing `buildFTS5Query()` from `db/memory.go`

---

## 8. Frontend Components

### Wiki Tab (`Wiki.svelte`)

```
Wiki.svelte
  ├── WikiSearch.svelte          // Search bar + FTS results
  ├── WikiPageList.svelte        // Filterable list by type/status
  │   └── WikiPageCard.svelte    // Summary card with type badge, staleness
  ├── WikiPageDetail.svelte      // Full markdown page view
  │   ├── WikiCitations.svelte   // Collapsible citation list
  │   └── WikiLinks.svelte       // Related pages sidebar
  ├── WikiGraph.svelte           // Force-directed graph (SVG/canvas)
  └── WikiQuery.svelte           // Synthesis query + answer display
```

### Evolution Tab (`Evolution.svelte`)

```
Evolution.svelte
  ├── EvolutionRunList.svelte    // Run list with status badges
  │   └── EvolutionRunCard.svelte
  ├── EvolutionRunDetail.svelte  // Hypotheses table + metrics
  │   ├── HypothesisList.svelte
  │   └── HypothesisDetail.svelte
  ├── EvolutionConfig.svelte     // Config editor
  └── EvolutionTrigger.svelte    // Manual trigger button
```

---

## 9. File Map

### New Files (37)

| File | Purpose |
|------|---------|
| `db/wiki.go` | WikiPage, WikiLink, WikiPageRef models + CRUD |
| `db/evolution.go` | EvolutionRun, EvolutionHypothesis models + CRUD |
| `db/wiki_test.go` | Wiki DB tests |
| `db/evolution_test.go` | Evolution DB tests |
| `internal/insight/wiki_engine/engine.go` | WikiEngine: RunIngest(), RunMaintenance() |
| `internal/insight/wiki_engine/synthesizer.go` | SynthesizeAnswer(), GeneratePage() |
| `internal/insight/wiki_engine/linker.go` | DetectCrossReferences(), FindContradictions() |
| `internal/insight/wiki_engine/store.go` | WikiStore interface |
| `internal/insight/wiki_engine/engine_test.go` | Wiki engine tests |
| `internal/insight/wiki_engine/synthesizer_test.go` | Synthesizer tests |
| `internal/insight/evolution_loop/loop.go` | EvolutionLoop: Run() with context.WithTimeout |
| `internal/insight/evolution_loop/hypothesis.go` | GenerateHypotheses() |
| `internal/insight/evolution_loop/experiment.go` | ExecuteExperiment() |
| `internal/insight/evolution_loop/evaluator.go` | Evaluate() + confidence computation |
| `internal/insight/evolution_loop/store.go` | EvolutionStore interface |
| `internal/insight/evolution_loop/loop_test.go` | Evolution loop tests |
| `internal/insight/evolution_loop/hypothesis_test.go` | Hypothesis tests |
| `internal/insight/wiki_engine/linker_test.go` | Cross-reference detection tests |
| `internal/insight/evolution_loop/experiment_test.go` | Experiment execution tests |
| `internal/insight/evolution_loop/evaluator_test.go` | Confidence computation + decision tests |
| `api/routes_wiki.go` | Wiki HTTP handlers |
| `api/routes_wiki_test.go` | Wiki handler tests |
| `api/routes_evolution.go` | Evolution HTTP handlers |
| `api/routes_evolution_test.go` | Evolution handler tests |
| `frontend/src/routes/Wiki.svelte` | Wiki tab |
| `frontend/src/routes/Evolution.svelte` | Evolution tab |
| `frontend/src/lib/components/wiki/*.svelte` | 8 wiki components |
| `frontend/src/lib/components/evolution/*.svelte` | 7 evolution components |

### Modified Files (8)

| File | Change |
|------|--------|
| `db/schema.go` | Append wiki + evolution DDL |
| `config/config.go` | Add WikiConfig, EvolutionConfig structs + defaults |
| `insight/engine.go` | Add wikiEngine, evolutionLoop fields + init + public methods |
| `insight/scheduler.go` | Add wiki ingest/maintenance/evolution to tick loop |
| `api/server.go` | Register wiki + evolution route handlers |
| `mcp/tools.go` | Register 4 new MCP tools |
| `frontend/src/lib/types.ts` | Add TypeScript interfaces |
| `frontend/src/lib/api.ts` | Add wiki + evolution API client functions |

---

## 10. Obsidian Vault Sync

### Design

Wiki pages are synced bidirectionally between SQLite (source of truth) and an Obsidian vault on disk. This enables using Obsidian's graph view, backlinks, Dataview, and Marp alongside the Stratus dashboard.

### Vault Structure

```
<vault_path>/
  _index.md                    # Auto-generated index page
  summaries/
    <slug>.md                  # Summary pages
  entities/
    <slug>.md                  # Entity pages
  concepts/
    <slug>.md                  # Concept pages
  answers/
    <slug>.md                  # Persisted synthesis answers
```

### Page Format (Obsidian-compatible)

```markdown
---
id: wp-abc123
page_type: entity
status: published
staleness_score: 0.15
tags: [go, error-handling, patterns]
generated_by: ingest
version: 3
created_at: 2026-04-06T12:00:00Z
updated_at: 2026-04-06T14:30:00Z
sources:
  - type: solution_pattern
    id: sp-456
  - type: event
    id: evt-789
---

# Error Handling Patterns

Content here with [[wikilinks]] to other pages like [[Database Migration Strategies]].

## Citations

- [solution_pattern:sp-456] Best practice for wrapping errors with context
- [event:evt-789] Discovery from bug-fix-login workflow
```

### Sync Engine (`internal/insight/wiki_engine/vault_sync.go`)

- **DB → Disk**: After each wiki page save/update, write .md file to vault. Convert wiki_links to `[[Title]]` syntax. Add YAML frontmatter from page metadata + refs.
- **Disk → DB**: Watch vault directory for external edits (user editing in Obsidian). Parse frontmatter to identify page, update content in DB. Only sync pages with matching `id` in frontmatter.
- **Conflict resolution**: DB wins on staleness_score/status/version fields. Content merges: if both changed, DB version wins (Obsidian is secondary interface).
- **File watcher**: Use `fsnotify` (already available via guardian/watcher patterns) to detect Obsidian edits.

### Config

```go
type WikiConfig struct {
    // ... existing fields ...
    VaultPath       string `json:"vault_path"`        // empty = vault sync disabled
    VaultSyncOnSave bool   `json:"vault_sync_on_save"` // sync immediately on page save (default true)
}
```

### New Files

| File | Purpose |
|------|---------|
| `internal/insight/wiki_engine/vault_sync.go` | Vault sync engine: DB→disk, disk→DB, file watcher |
| `internal/insight/wiki_engine/vault_sync_test.go` | Vault sync tests |
| `internal/insight/wiki_engine/obsidian.go` | Obsidian format helpers: frontmatter, wikilinks, Dataview |
| `internal/insight/wiki_engine/obsidian_test.go` | Format conversion tests |

### API Additions

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/wiki/vault/sync` | Trigger full vault sync (DB → disk) |
| GET | `/api/wiki/vault/status` | Vault sync status (last sync time, file count, errors) |

---

## 11. Removal: Learning System + Analytics

The Self-Evolving System **replaces** the existing Learning (candidates/proposals) and Analytics systems. The Insight Engine internals (patterns, scorecards, trajectories, agent evolution, workflow synthesis) are **kept** as data sources for Wiki/Evolution.

### Files to Delete

| File | What It Was |
|------|-------------|
| `api/routes_learning.go` | Learning API handlers (candidates, proposals) |
| `db/learning.go` | Candidate/Proposal DB models and queries |
| `frontend/src/routes/Learning.svelte` | Learning tab UI |
| `frontend/src/routes/Analytics.svelte` | Analytics tab UI |

### DB Tables to Drop (from schema.go)

- `candidates` — replaced by wiki pages (entity/concept pages capture patterns)
- `proposals` — replaced by evolution hypotheses (auto-apply or wiki findings)

### Code Sections to Remove

| File | What to Remove |
|------|---------------|
| `api/server.go` | Learning route registrations (5 routes) |
| `api/routes_dashboard.go` | `pending_candidates`/`pending_proposals` queries and response fields |
| `db/schema.go` | `candidates` and `proposals` table DDL |
| `frontend/src/App.svelte` | Learning/Analytics imports, tab definitions, proposal badge logic |
| `frontend/src/lib/store.svelte.ts` | `analyticsUpdateCounter`, `learning_update` from updateTypes |
| `frontend/src/lib/types.ts` | `Candidate`, `Proposal` interfaces, `pending_proposals` from DashboardState |
| `frontend/src/lib/api.ts` | `listCandidates()`, `listProposals()`, `decideProposal()`, `saveProposal()`, analytics metric functions |
| `insight/events/types.go` | `EventProposalCreated`, `EventProposalAccepted`, `EventProposalRejected` |

### Evolution Loop Change

Previously: Evolution created Learning proposals for user-visible changes.
Now: Evolution creates **wiki answer pages** tagged `generated_by: "evolution"` with `page_type: "concept"` for user-visible findings. No more proposal pipeline — wiki IS the proposal surface. Users review findings in the Wiki tab or Obsidian.

For auto-applicable internal changes (routing scores, thresholds): unchanged — still auto-applied above confidence threshold.

---

## Breaking Changes

**Removals (intentional):**
- Learning API (`/api/learning/*`) removed — replaced by Wiki + Evolution
- Analytics tab removed — replaced by Evolution tab
- `candidates` and `proposals` DB tables dropped
- Learning-related TypeScript types and API functions removed

**Additive (no breaking):**
- New tables only (IF NOT EXISTS)
- New endpoints only
- New MCP tools only
- New config fields with `enabled: false` defaults
- New frontend tabs (existing tabs unmodified)

---

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| LLM cost explosion from evolution loop | Default to local LLM; per-day token budget in config |
| Wiki quality degradation | Maintenance linting; staleness scoring; contradiction detection |
| Silent regression from auto-applied changes | Guardian monitors scorecards; auto-revert on >10% drop |
| SQLite write contention | Batch wiki writes at cycle end; WAL mode handles concurrent reads |
