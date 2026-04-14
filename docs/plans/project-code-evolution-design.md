# Project Code Evolution — Technical Design Document

**Workflow:** spec-project-code-evolution
**Date:** 2026-04-12
**Status:** Proposed
**Complexity:** Complex

---

## Overview

Project Code Evolution extends the Stratus self-evolving system to analyze the **host project's source code** for quality issues. A new `CodeAnalyst` subsystem gathers evidence using cheap heuristics (git churn, grep patterns, line counts), ranks files by a composite score, then sends the top-K files through LLM-as-judge analysis. Findings are persisted as per-file wiki pages and queryable via MCP tools.

The system is read-only with respect to the host codebase — it produces findings and wiki pages, never modifies source code or opens PRs.

---

## Architecture Decisions

### ADR-001: CodeAnalyst as Separate Package Feeding into Evolution Loop

**Status:** Proposed

#### Context

The Evolution Loop (`internal/insight/evolution_loop/`) runs Generate-Execute-Evaluate pipelines on 4 internal categories. The loop supports pluggable `wikiFn` and `applyFn` callbacks. We need to add project-level code analysis without polluting the loop's existing concerns.

#### Decision

Create `internal/insight/code_analyst/` as a standalone package. CodeAnalyst runs its own collection/ranking pipeline and produces wiki pages directly. It does NOT feed hypotheses into Evolution Loop — code findings are observations, not experiments with "baseline vs proposed" structure.

#### Rationale

- **Separation of concerns:** File I/O, git invocations, and LLM file analysis are qualitatively different from the loop's internal config-tuning.
- **Testability:** Collector, ranker, and analyzer can each be unit-tested independently.
- **Existing pattern:** Mirrors how Guardian runs its own checks while Insight Engine orchestrates subsystems.

#### Alternatives Considered

- Embed in Evolution Loop hypothesis generation — rejected because code findings don't fit the hypothesis-experiment model.
- Extend Guardian checks — rejected because Guardian emits transient alerts while code analysis needs persistent per-file wiki pages.

---

### ADR-002: File Selection Strategy (Churn-Ranked)

**Status:** Proposed

#### Context

Analyzing every file is prohibitively expensive. We need a ranking function that surfaces files most likely to benefit from analysis.

#### Decision

Composite score: `score = churn_rate * (1 - coverage) * complexity_proxy`.

- `churn_rate`: normalized commit count from `git log --numstat` over configurable depth (default: 100 commits).
- `coverage`: per-file test coverage where available (Go: `go test -coverprofile`). Default 0.5 for unknown languages.
- `complexity_proxy`: `line_count / 100.0` capped at 3.0.

Only top-K files (default: 10) are sent to LLM analysis each run.

#### Rationale

- Git churn is language-agnostic and identifies "hot" files where bugs concentrate.
- Coverage is inversely correlated with risk.
- Line count is a weak but free signal for complexity.

#### Alternatives Considered

- AST-based cyclomatic complexity — rejected for MVP (requires per-language parsers).
- Random sampling — rejected (wastes LLM calls on stable files).

---

### ADR-003: Per-File Wiki Pages as Output Surface

**Status:** Proposed

#### Context

Findings need a persistent, searchable, versionable output surface.

#### Decision

Each analyzed file gets one wiki page (`page_type="code_analysis"`, title=`Code Analysis: <relative_path>`). Created on first analysis, updated on subsequent runs. Category-level summary pages (`page_type="code_summary"`) roll up findings across files.

#### Rationale

- Reuses existing wiki infrastructure: FTS5 search, staleness scoring, vault sync, MCP `retrieve` tool.
- Per-file granularity allows agents to query findings for a specific file they're editing.

#### Alternatives Considered

- Flat findings table only — rejected (not searchable via `retrieve` MCP tool).
- One wiki page per finding — rejected (creates hundreds of pages).

---

## 1. Data Models

### CodeAnalysisRun

```go
// internal/insight/code_analyst/types.go
type CodeAnalysisRun struct {
    ID               string         `json:"id"`
    Status           string         `json:"status"`           // running | completed | failed
    FilesScanned     int            `json:"files_scanned"`
    FilesAnalyzed    int            `json:"files_analyzed"`   // top-K sent to LLM
    FindingsCount    int            `json:"findings_count"`
    WikiPagesCreated int            `json:"wiki_pages_created"`
    WikiPagesUpdated int            `json:"wiki_pages_updated"`
    DurationMs       int64          `json:"duration_ms"`
    TokensUsed       int            `json:"tokens_used"`
    GitCommitHash    string         `json:"git_commit_hash"`  // HEAD at run start
    ErrorMessage     string         `json:"error_message,omitempty"`
    Metadata         map[string]any `json:"metadata"`
    StartedAt        string         `json:"started_at"`
    CompletedAt      *string        `json:"completed_at,omitempty"`
    CreatedAt        string         `json:"created_at"`
}
```

### CodeFinding

```go
type CodeFinding struct {
    ID          string         `json:"id"`
    RunID       string         `json:"run_id"`
    FilePath    string         `json:"file_path"`
    Category    string         `json:"category"`       // anti_pattern | duplication | coverage_gap | error_handling | complexity | dead_code | security
    Severity    string         `json:"severity"`        // critical | warning | info
    Title       string         `json:"title"`
    Description string         `json:"description"`     // LLM-generated explanation
    LineStart   int            `json:"line_start"`      // 0 if file-level
    LineEnd     int            `json:"line_end"`        // 0 if file-level
    Confidence  float64        `json:"confidence"`      // 0.0-1.0 from LLM judge
    Suggestion  string         `json:"suggestion"`      // recommended fix
    WikiPageID  *string        `json:"wiki_page_id,omitempty"`
    Evidence    map[string]any `json:"evidence"`        // supporting data
    CreatedAt   string         `json:"created_at"`
}
```

### FileScore

```go
type FileScore struct {
    FilePath        string  `json:"file_path"`
    ChurnRate       float64 `json:"churn_rate"`         // normalized 0-1
    Coverage        float64 `json:"coverage"`            // 0-1, 0.5 = unknown
    ComplexityProxy float64 `json:"complexity_proxy"`    // line_count / 100, capped at 3.0
    CompositeScore  float64 `json:"composite_score"`     // churn * (1 - coverage) * complexity
    CommitCount     int     `json:"commit_count"`
    LineCount       int     `json:"line_count"`
    TechDebtMarkers int     `json:"tech_debt_markers"`
    LastAnalyzedAt  *string `json:"last_analyzed_at,omitempty"`
    LastGitHash     *string `json:"last_git_hash,omitempty"`
}
```

### CodeQualityMetric

```go
type CodeQualityMetric struct {
    ID                 string         `json:"id"`
    MetricDate         string         `json:"metric_date"`      // YYYY-MM-DD
    TotalFiles         int            `json:"total_files"`
    FilesAnalyzed      int            `json:"files_analyzed"`
    FindingsTotal      int            `json:"findings_total"`
    FindingsBySeverity map[string]int `json:"findings_by_severity"`
    FindingsByCategory map[string]int `json:"findings_by_category"`
    AvgChurnScore      float64        `json:"avg_churn_score"`
    AvgCoverage        float64        `json:"avg_coverage"`
    CreatedAt          string         `json:"created_at"`
}
```

---

## 2. Database Schema

Append to `db/schema.go` DDL constant:

```sql
-- Code Analysis: Runs
CREATE TABLE IF NOT EXISTS code_analysis_runs (
    id                 TEXT PRIMARY KEY,
    status             TEXT NOT NULL DEFAULT 'running',
    files_scanned      INTEGER NOT NULL DEFAULT 0,
    files_analyzed     INTEGER NOT NULL DEFAULT 0,
    findings_count     INTEGER NOT NULL DEFAULT 0,
    wiki_pages_created INTEGER NOT NULL DEFAULT 0,
    wiki_pages_updated INTEGER NOT NULL DEFAULT 0,
    duration_ms        INTEGER NOT NULL DEFAULT 0,
    tokens_used        INTEGER NOT NULL DEFAULT 0,
    git_commit_hash    TEXT NOT NULL DEFAULT '',
    error_message      TEXT NOT NULL DEFAULT '',
    metadata_json      TEXT NOT NULL DEFAULT '{}',
    started_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at       TEXT,
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_code_analysis_runs_status ON code_analysis_runs(status);
CREATE INDEX IF NOT EXISTS idx_code_analysis_runs_started ON code_analysis_runs(started_at DESC);

-- Code Analysis: Findings
CREATE TABLE IF NOT EXISTS code_findings (
    id            TEXT PRIMARY KEY,
    run_id        TEXT NOT NULL REFERENCES code_analysis_runs(id) ON DELETE CASCADE,
    file_path     TEXT NOT NULL,
    category      TEXT NOT NULL,
    severity      TEXT NOT NULL DEFAULT 'info',
    title         TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    line_start    INTEGER NOT NULL DEFAULT 0,
    line_end      INTEGER NOT NULL DEFAULT 0,
    confidence    REAL NOT NULL DEFAULT 0,
    suggestion    TEXT NOT NULL DEFAULT '',
    wiki_page_id  TEXT,
    evidence_json TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_code_findings_run ON code_findings(run_id);
CREATE INDEX IF NOT EXISTS idx_code_findings_file ON code_findings(file_path);
CREATE INDEX IF NOT EXISTS idx_code_findings_category ON code_findings(category);
CREATE INDEX IF NOT EXISTS idx_code_findings_severity ON code_findings(severity);

-- Code Analysis: FTS5 for finding search
CREATE VIRTUAL TABLE IF NOT EXISTS code_findings_fts USING fts5(
    title, description, file_path, suggestion,
    content='code_findings', content_rowid='rowid',
    tokenize='porter unicode61'
);

CREATE TRIGGER IF NOT EXISTS code_findings_ai AFTER INSERT ON code_findings BEGIN
    INSERT INTO code_findings_fts(rowid, title, description, file_path, suggestion)
    VALUES (new.rowid, new.title, new.description, new.file_path, new.suggestion);
END;
CREATE TRIGGER IF NOT EXISTS code_findings_au AFTER UPDATE ON code_findings BEGIN
    INSERT INTO code_findings_fts(code_findings_fts, rowid, title, description, file_path, suggestion)
    VALUES ('delete', old.rowid, old.title, old.description, old.file_path, old.suggestion);
    INSERT INTO code_findings_fts(rowid, title, description, file_path, suggestion)
    VALUES (new.rowid, new.title, new.description, new.file_path, new.suggestion);
END;
CREATE TRIGGER IF NOT EXISTS code_findings_ad AFTER DELETE ON code_findings BEGIN
    INSERT INTO code_findings_fts(code_findings_fts, rowid, title, description, file_path, suggestion)
    VALUES ('delete', old.rowid, old.title, old.description, old.file_path, old.suggestion);
END;

-- Code Analysis: Quality Metrics (time series, one row per day)
CREATE TABLE IF NOT EXISTS code_quality_metrics (
    id                        TEXT PRIMARY KEY,
    metric_date               TEXT NOT NULL,
    total_files               INTEGER NOT NULL DEFAULT 0,
    files_analyzed            INTEGER NOT NULL DEFAULT 0,
    findings_total            INTEGER NOT NULL DEFAULT 0,
    findings_by_severity_json TEXT NOT NULL DEFAULT '{}',
    findings_by_category_json TEXT NOT NULL DEFAULT '{}',
    avg_churn_score           REAL NOT NULL DEFAULT 0,
    avg_coverage              REAL NOT NULL DEFAULT 0,
    created_at                TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(metric_date)
);

CREATE INDEX IF NOT EXISTS idx_code_quality_metrics_date ON code_quality_metrics(metric_date DESC);

-- Code Analysis: File analysis cache (dedup across runs)
CREATE TABLE IF NOT EXISTS code_file_cache (
    file_path        TEXT PRIMARY KEY,
    git_hash         TEXT NOT NULL,
    last_analyzed_at TEXT NOT NULL,
    last_run_id      TEXT NOT NULL,
    composite_score  REAL NOT NULL DEFAULT 0,
    findings_count   INTEGER NOT NULL DEFAULT 0,
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_code_file_cache_score ON code_file_cache(composite_score DESC);
```

---

## 3. Package Design: `internal/insight/code_analyst/`

### Package Structure

```
internal/insight/code_analyst/
    types.go           -- CodeAnalysisRun, CodeFinding, FileScore, CodeQualityMetric structs
    store.go           -- CodeAnalystStore interface + DBCodeAnalystStore
    store_test.go      -- Store CRUD tests (SQL queries, JSON marshaling)
    collector.go       -- git churn, tech debt grep, line count collection
    collector_test.go
    ranker.go          -- composite scoring and top-K selection
    ranker_test.go
    analyzer.go        -- LLM-powered file analysis
    analyzer_test.go
    dedup.go           -- git hash caching, skip unchanged files
    dedup_test.go
    engine.go          -- pipeline orchestrator (collect -> rank -> analyze -> persist)
    engine_test.go
    prompts.go         -- LLM prompt templates
```

### store.go — CodeAnalystStore Interface

```go
type CodeAnalystStore interface {
    // Runs
    SaveRun(r *CodeAnalysisRun) error
    GetRun(id string) (*CodeAnalysisRun, error)
    ListRuns(limit, offset int) ([]CodeAnalysisRun, int, error)
    UpdateRun(r *CodeAnalysisRun) error

    // Findings
    SaveFinding(f *CodeFinding) error
    ListFindings(filters FindingFilters) ([]CodeFinding, int, error)
    SearchFindings(query string, limit int) ([]CodeFinding, error)

    // Metrics
    SaveMetric(m *CodeQualityMetric) error
    ListMetrics(days int) ([]CodeQualityMetric, error)

    // File cache (dedup)
    GetFileCache(path string) (*FileCacheEntry, error)
    SetFileCache(path, gitHash, runID string, score float64, findingsCount int) error
}
```

### collector.go — Signal Gathering

```go
type Collector struct {
    projRoot string
}

type FileSignals struct {
    FilePath        string
    CommitCount     int      // from git log --numstat
    LineCount       int
    TechDebtMarkers int      // TODO/FIXME/HACK grep count
    TestFile        bool     // heuristic: *_test.go, *.test.ts, etc.
    Language        string   // inferred from extension
}

func (c *Collector) CollectAll(ctx context.Context, gitDepth int) ([]FileSignals, error)
func (c *Collector) CollectGitChurn(ctx context.Context, depth int) (map[string]int, error)
func (c *Collector) CollectTechDebt(ctx context.Context) (map[string]int, error)
func (c *Collector) CollectLineCounts(ctx context.Context, files []string) (map[string]int, error)
```

All commands use `exec.CommandContext` with timeout, following the Guardian pattern.

### Error Handling Conventions

All functions in `code_analyst` MUST wrap errors with context per `.claude/rules/error-handling.md`:

```go
// Every error includes the operation that failed
if err != nil {
    return fmt.Errorf("code analyst: collect git churn: %w", err)
}
if err != nil {
    return fmt.Errorf("code analyst: analyze file %q: %w", filePath, err)
}
if err != nil {
    return fmt.Errorf("code analyst: save finding: %w", err)
}
```

Prefix pattern: `"code analyst: <component>: <operation>: %w"` where component is one of `collect`, `rank`, `analyze`, `dedup`, `engine`, `store`.

### ranker.go — File Scoring

```go
type Ranker struct {
    config RankerConfig
}

type RankerConfig struct {
    MaxFiles        int
    MinChurnScore   float64
    CoverageDefault float64 // 0.5 for unknown languages
    ComplexityCap   float64 // 3.0
}

func (r *Ranker) Rank(signals []FileSignals, coverageMap map[string]float64) []FileScore
```

Pipeline: normalize churn → lookup coverage → compute complexity proxy → compute composite → sort → top-K → filter test files and below-threshold.

### analyzer.go — LLM Analysis

```go
type Analyzer struct {
    llmClient  llm.Client
    projRoot   string
    categories []string
}

func (a *Analyzer) AnalyzeFile(ctx context.Context, file FileScore, governanceRules string) (*AnalysisResult, error)
```

Reads file content (capped at 8000 tokens / ~32KB), constructs prompt with file content + metrics + governance rules, calls `llm.Client.Complete()`, parses JSON response into `[]CodeFinding`, filters below confidence threshold.

### dedup.go — Change Detection

```go
type Deduplicator struct {
    store CodeAnalystStore
}

func (d *Deduplicator) FilterUnchanged(files []FileScore, currentHashes map[string]string) []FileScore
func (d *Deduplicator) UpdateCache(file string, gitHash, runID string, score float64, findings int) error
```

Uses `code_file_cache` table. Compares `git hash-object <file>` against cached hash. Skips matching files unless last analysis > 7 days old.

### engine.go — Pipeline Orchestrator

```go
type Engine struct {
    store     CodeAnalystStore
    collector *Collector
    ranker    *Ranker
    analyzer  *Analyzer
    dedup     *Deduplicator
    wikiFn    func(ctx context.Context, finding *CodeFinding, filePath string) (string, error)
    configFn  func() CodeAnalysisConfig
    mu        sync.Mutex
    running   bool
}

func NewEngine(store CodeAnalystStore, llmClient llm.Client, projRoot string, configFn func() CodeAnalysisConfig, opts ...EngineOption) *Engine
func (e *Engine) Run(ctx context.Context, triggerType string) (*RunResult, error)
func (e *Engine) IsRunning() bool
```

Run pipeline:
1. Acquire mutex, create `CodeAnalysisRun`
2. Get HEAD commit hash
3. `collector.CollectAll()` — gather signals
4. `ranker.Rank()` — score and select top-K
5. `dedup.FilterUnchanged()` — skip unchanged files
6. For each file: `analyzer.AnalyzeFile()`, save findings, call `wikiFn`
7. Persist daily `code_quality_metrics`
8. Update `code_file_cache`
9. Persist `CodeAnalysisRun` with final counts

---

## 4. Integration Points

### Insight Engine Orchestration

Add to `insight/engine.go`:
- New field: `codeAnalyst *code_analyst.Engine`
- New init: `initCodeAnalyst()` — wires `wikiFn` callback for per-file wiki pages
- New method: `RunCodeAnalysis(ctx, triggerType)` — public entry point
- Scheduler tick at `ScanInterval` minutes (default: 60), separate from evolution loop tick

### Guardian Cross-Feed

CodeAnalyst reads Guardian baselines (`guardian_baselines` table) for tech_debt_count and coverage values. No changes to Guardian itself.

### Vexor Integration (Optional)

For duplication category: embed file content via `vexor.Client.Search()`, flag files with similarity > 0.85. Gated behind `config.Vexor.BinaryPath != ""`.

---

## 5. API Contracts

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/code-analysis/runs` | List analysis runs (query: limit, offset) |
| GET | `/api/code-analysis/runs/{id}` | Run details with findings |
| GET | `/api/code-analysis/findings` | Query findings (filter: run_id, file, category, severity, q, limit, offset) |
| POST | `/api/code-analysis/trigger` | Manual trigger (body: categories[]) |
| GET | `/api/code-analysis/metrics` | Quality metrics time series (query: days) |
| GET | `/api/code-analysis/config` | Get config |
| POST | `/api/code-analysis/config` | Update config (validated per config-validation.md) |

### POST /api/code-analysis/trigger Parameter Passthrough

The `categories` field from the request body is validated against `allowedCodeAnalysisCategories` and then passed through to `Engine.Run(ctx, triggerType, categories)`. If empty/omitted, all enabled categories from config are used. Per `api-parameter-passthrough.md`, validated parameters MUST be forwarded — the handler does not silently discard categories.

### POST /api/code-analysis/config Validation

- `max_files_per_run`: 1-50
- `token_budget_per_run`: 0-5000000
- `min_churn_score`: 0.0-1.0
- `confidence_threshold`: 0.0-1.0
- `scan_interval`: 5-1440 (minutes)
- `git_history_depth`: 10-1000
- `categories`: validated against `allowedCodeAnalysisCategories`:
  ```go
  var allowedCodeAnalysisCategories = map[string]struct{}{
      "anti_pattern":    {},
      "duplication":     {},
      "coverage_gap":    {},
      "error_handling":  {},
      "complexity":      {},
      "dead_code":       {},
      "security":        {},
  }
  ```

Config updates are persisted to `.stratus.json` via `cfg.Save()` (matching the pattern in `handleUpdateWikiConfig`).

### API Error Mapping

Internal errors are mapped to HTTP status codes at the handler boundary. Internal details are never leaked to clients.

| Internal Error | HTTP Status | User-Facing Message |
|---------------|-------------|---------------------|
| Validation failure (bad field value) | 400 | `"invalid <field>: <reason>"` |
| Invalid category in trigger/config | 400 | `"unknown category: <value>. Allowed: anti_pattern, duplication, ..."` |
| `Engine.IsRunning()` == true on trigger | 409 | `"code analysis already running"` |
| CodeAnalyst engine nil / not initialized | 503 | `"code analysis engine not available"` |
| `collector.CollectAll` fails (git not on PATH) | 500 | `"code analysis failed: git command not available"` |
| LLM timeout / budget exhausted mid-run | 200 | Run completes with partial results; `error_message` field populated |
| DB write failure | 500 | `"internal error"` (logged with full context) |
| File read failure (deleted between collect and analyze) | — | Skipped silently; run continues with remaining files |

---

## 6. MCP Tools

| Tool | Description | Key Params |
|------|-------------|------------|
| `code_analysis_trigger` | Trigger code quality analysis run | categories (optional) |
| `code_analysis_findings` | Query findings by file/category/severity | file, category, severity, q, limit |
| `code_quality_summary` | Aggregate metrics over time | days |

---

## 7. Config Schema

```go
type CodeAnalysisConfig struct {
    Enabled             bool      `json:"enabled"`
    MaxFilesPerRun      int       `json:"max_files_per_run"`
    TokenBudgetPerRun   int       `json:"token_budget_per_run"`
    MinChurnScore       float64   `json:"min_churn_score"`
    ConfidenceThreshold float64   `json:"confidence_threshold"`
    ScanInterval        int       `json:"scan_interval"`          // minutes
    IncludeGitHistory   bool      `json:"include_git_history"`
    GitHistoryDepth     int       `json:"git_history_depth"`      // commits
    Categories          []string  `json:"categories"`
    LLM                 LLMConfig `json:"llm"`                    // per-subsystem LLM override
}
```

The `LLM` field follows the same pattern as `GuardianConfig.LLM` and `InsightConfig.LLM` — allows per-subsystem model/provider override. If empty, falls back to global LLM config.

Defaults: `enabled: false`, `max_files_per_run: 10`, `token_budget_per_run: 500000`, `min_churn_score: 0.1`, `confidence_threshold: 0.75`, `scan_interval: 60`, `include_git_history: true`, `git_history_depth: 100`, `categories: []` (empty = all).

---

## 8. Frontend Components

### New "Code Quality" tab

```
frontend/src/routes/CodeQuality.svelte                  -- main tab
frontend/src/lib/components/CodeFindingsList.svelte      -- filterable findings table
frontend/src/lib/components/CodeMetricsChart.svelte      -- trend charts
frontend/src/lib/components/CodeFindingDetail.svelte     -- finding detail view
frontend/src/lib/components/CodeAnalysisConfig.svelte    -- config form
```

Layout:
1. **Header**: title, "Run Analysis" button, last run timestamp
2. **Metrics panel**: sparkline charts (findings over time, by severity/category)
3. **Findings list**: table with File, Category, Severity, Title, Confidence columns
4. **Detail panel**: full description, suggestion, link to wiki page, file:line references
5. **Config section**: collapsible settings panel

---

## 9. File Map

### New Files (23)

| File | Purpose |
|------|---------|
| `internal/insight/code_analyst/types.go` | Go structs |
| `internal/insight/code_analyst/store.go` | Store interface + DB implementation |
| `internal/insight/code_analyst/store_test.go` | Store tests (SQL queries, JSON marshaling, CRUD) |
| `internal/insight/code_analyst/collector.go` | Git churn, tech debt, line count collection |
| `internal/insight/code_analyst/collector_test.go` | Tests with mock git output |
| `internal/insight/code_analyst/ranker.go` | Composite scoring and top-K |
| `internal/insight/code_analyst/ranker_test.go` | Scoring formula tests |
| `internal/insight/code_analyst/analyzer.go` | LLM-powered file analysis |
| `internal/insight/code_analyst/analyzer_test.go` | Tests with mock LLM |
| `internal/insight/code_analyst/dedup.go` | Git hash caching |
| `internal/insight/code_analyst/dedup_test.go` | Cache hit/miss/expiry tests |
| `internal/insight/code_analyst/engine.go` | Pipeline orchestrator |
| `internal/insight/code_analyst/engine_test.go` | Integration test |
| `internal/insight/code_analyst/prompts.go` | LLM prompt templates |
| `db/code_analysis.go` | DB methods for runs, findings, metrics, cache |
| `db/code_analysis_test.go` | DB layer tests |
| `api/routes_code_analysis.go` | HTTP handlers (7 endpoints) |
| `api/routes_code_analysis_test.go` | Handler tests |
| `frontend/src/routes/CodeQuality.svelte` | Main dashboard tab |
| `frontend/src/lib/components/CodeFindingsList.svelte` | Findings table |
| `frontend/src/lib/components/CodeMetricsChart.svelte` | Trend charts |
| `frontend/src/lib/components/CodeFindingDetail.svelte` | Finding detail |
| `frontend/src/lib/components/CodeAnalysisConfig.svelte` | Config form |

### Modified Files (8)

| File | Change |
|------|--------|
| `db/schema.go` | Add 4 new tables + FTS5 + triggers + indexes |
| `config/config.go` | Add `CodeAnalysisConfig` struct + defaults |
| `insight/engine.go` | Add `codeAnalyst` field, `initCodeAnalyst()`, `RunCodeAnalysis()` |
| `api/server.go` | Wire code analysis routes |
| `mcp/tools.go` | Register 3 new MCP tools |
| `frontend/src/lib/api.ts` | Add code analysis API methods |
| `frontend/src/lib/types.ts` | Add TypeScript interfaces |
| `frontend/src/App.svelte` | Add CodeQuality tab to navigation |

---

## 10. Security Considerations

1. **Source code sent to LLM provider**: File content from top-K files is sent to the configured LLM. Config must document this. Frontend shows warning banner when enabling.
2. **Sensitive file exclusion**: Files matching `*.env`, `*credentials*`, `*secret*`, `*.key`, `*.pem` excluded by default in collector. `.gitignore` patterns respected.
3. **Shell command injection**: All commands use `exec.CommandContext` with explicit arg arrays (no string interpolation). File paths validated via `filepath.Rel(projRoot, absPath)`.
4. **Token budget**: `TokenBudgetPerRun` (default 500K) caps per-run spending. Analyzer stops at 80% budget. Daily budget enforced by existing `BudgetedClient`.

---

## 11. Breaking Changes & Risks

### Breaking Changes

None. Entirely additive:
- New tables via `CREATE TABLE IF NOT EXISTS`
- New config field with defaults (`enabled: false`)
- New API endpoints at new paths
- New MCP tools alongside existing ones

### Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| LLM hallucinated findings (false positives) | Medium | `confidence_threshold: 0.75`; per-file wiki pages allow human review |
| Large token consumption | Medium | `token_budget_per_run` cap; `max_files_per_run: 10`; shared daily budget |
| `git log` slow on large repos | Low | `git_history_depth: 100`; `exec.CommandContext` with timeout |
| Coverage parsing only works for Go | Low | Documented; `coverage_default: 0.5` for other languages |
| Wiki page bloat | Low | Consolidated per file; truncated to `max_page_size_tokens` |

### Implementation Ordering

1. DB schema + `db/code_analysis.go`
2. `code_analyst/types.go` + `store.go`
3. `code_analyst/collector.go` + tests
4. `code_analyst/ranker.go` + tests
5. `code_analyst/dedup.go` + tests
6. `code_analyst/analyzer.go` + tests
7. `code_analyst/engine.go` + tests
8. `config/config.go` changes
9. `api/routes_code_analysis.go` + tests
10. `mcp/tools.go` additions
11. `insight/engine.go` integration
12. Frontend components
