# Technical Design: Project Onboarding for Non-Greenfield Projects

## Overview

When Stratus is used in an existing (non-greenfield) project, the onboarding feature auto-generates documentation: wiki markdown pages documenting architecture, modules, conventions, dependencies, and build guides. Pages are stored in the wiki DB, optionally synced to an Obsidian vault, and optionally exported as standalone markdown.

## Component Overview

| Component | Location | Responsibility |
|-----------|----------|----------------|
| **Scanner** | `internal/insight/onboarding/scanner.go` | Deterministic codebase analysis producing a `ProjectProfile` struct. No LLM calls. Respects .gitignore, skips secrets and binary files. |
| **Non-Greenfield Detector** | `internal/insight/onboarding/detector.go` | Weighted heuristic that scores project maturity. Called by `cmdInit` for suggestion and by `stratus onboard` as precondition. |
| **Orchestrator** | `internal/insight/onboarding/orchestrator.go` | Drives page generation in dependency order, cross-references pages, triggers VaultSync. Reports progress via callback. |
| **Prompts** | `internal/insight/prompts/prompts.go` | Four new prompt constants for onboarding page types. |
| **CLI command** | `cmd/stratus/main.go` | New `stratus onboard` subcommand. |
| **API handler** | `api/routes_onboarding.go` | `POST /api/onboard`, `GET /api/onboard/status` endpoints. |
| **Config** | `config/config.go` | New fields on `WikiConfig`. |
| **Existing reused** | `internal/insight/wiki_engine/` | `WikiEngine.GeneratePageFromData()`, `Linker`, `VaultSync`, `WikiStore` interface. |
| **Existing reused** | `internal/insight/llm/` | `SubsystemClient`, `BudgetedClient` with budget tracking. |
| **Existing reused** | `db/wiki.go` | `SaveWikiPage`, `SaveWikiLink`, `SaveWikiPageRef`, `ListWikiPages`. |

---

## ADR-001: Separate CLI command, not embedded in init

**Status:** Proposed  
**Date:** 2026-04-09

**Context:** `stratus init` is designed to be fast and offline (writes config files, indexes governance docs). Onboarding requires LLM calls, which are slow (minutes) and depend on provider configuration. Embedding onboarding in init would break the fast-offline contract.

**Decision:** Onboarding is a separate `stratus onboard` command and a `POST /api/onboard` API endpoint. `cmdInit` calls `IsNonGreenfield()` and prints a suggestion message if the project is non-greenfield. No LLM calls during init.

**Alternatives Considered:**
- Embed in `stratus init` with `--skip-onboard` flag -- rejected because it inverts the default and couples offline and online operations.
- Automatic trigger on first `stratus serve` -- rejected because it creates a surprising side effect that consumes LLM budget without user intent.

**Consequences:**
- Positive: init stays fast/offline; onboarding is explicit and controllable.
- Negative: Users must run a second command; risk of forgetting.
- Mitigation: init prints clear suggestion text when non-greenfield is detected.

## ADR-002: Hybrid scanning (deterministic scan + LLM prose only)

**Status:** Proposed  
**Date:** 2026-04-09

**Context:** Raw file contents sent to LLM risks leaking secrets and wastes tokens on boilerplate. Deterministic analysis can extract structure reliably.

**Decision:** Scanner produces a structured `ProjectProfile` with extracted metadata. Only the `ProjectProfile` fields are passed to LLM prompts -- never raw file contents.

**Alternatives Considered:**
- Send raw files to LLM with redaction -- rejected because redaction is fragile and token-expensive.
- Pure deterministic (no LLM) -- rejected because prose quality for architecture descriptions would be poor.

**Consequences:**
- Positive: No secret leakage risk; predictable token usage per page.
- Negative: LLM has less context than raw code; module descriptions are based on extracted structure.

## ADR-003: Wiki DB as canonical store

**Status:** Proposed  
**Date:** 2026-04-09

**Context:** Stratus already has `wiki_pages`, `wiki_links`, `wiki_page_refs` tables with FTS5 search, graph queries, and Obsidian VaultSync export.

**Decision:** Onboarding pages are stored as regular wiki pages with `generated_by = "onboarding"` and tagged with `["onboarding"]`. VaultSync exports them to Obsidian vault. Optional `--output-dir` flag writes standalone markdown copies.

**Consequences:**
- Positive: Full reuse of wiki search, graph, VaultSync, staleness tracking, and dashboard UI.
- Negative: Onboarding pages mix with other wiki pages (mitigated by tag and `generated_by` filtering).

## ADR-004: One LLM call per page

**Status:** Proposed  
**Date:** 2026-04-09

**Decision:** One LLM call per page via existing `WikiEngine.GeneratePageFromData()`. Architecture overview first, then module pages reference it.

**Consequences:**
- Positive: Graceful degradation, per-page progress tracking, budget tracking works naturally.
- Negative: More latency (mitigated by sequential generation respecting rate limits); lower cross-page coherence (mitigated by passing architecture overview as context).

---

## Data Model

### ProjectProfile (`internal/insight/onboarding/scanner.go`)

```go
type ProjectProfile struct {
    RootPath         string         `json:"root_path"`
    ProjectName      string         `json:"project_name"`
    Languages        []LanguageStat `json:"languages"`
    EntryPoints      []EntryPoint   `json:"entry_points"`
    DirectoryTree    string         `json:"directory_tree"`
    ReadmeContent    string         `json:"readme_content"`
    ConfigFiles      []ConfigFile   `json:"config_files"`
    GitStats         *GitStats      `json:"git_stats"`
    TestStructure    TestStructure  `json:"test_structure"`
    DetectedPatterns []string       `json:"detected_patterns"`
    CIProvider       string         `json:"ci_provider"`
    ScannedAt        time.Time      `json:"scanned_at"`
}

type LanguageStat struct {
    Language   string  `json:"language"`
    Extension  string  `json:"extension"`
    FileCount  int     `json:"file_count"`
    LineCount  int     `json:"line_count"`
    Percentage float64 `json:"percentage"`
}

type EntryPoint struct {
    Path        string `json:"path"`
    Type        string `json:"type"`
    Description string `json:"description"`
}

type ConfigFile struct {
    Path    string `json:"path"`
    Type    string `json:"type"`
    Content string `json:"content"` // capped at 4KB
}

type GitStats struct {
    CommitCount    int       `json:"commit_count"`
    Contributors   int       `json:"contributors"`
    FirstCommit    time.Time `json:"first_commit"`
    LastCommit     time.Time `json:"last_commit"`
    AgeInDays      int       `json:"age_in_days"`
    DefaultBranch  string    `json:"default_branch"`
}

type TestStructure struct {
    TestDirs  []string `json:"test_dirs"`
    TestFiles int      `json:"test_files"`
    Framework string   `json:"framework"`
}
```

### OnboardingResult (`internal/insight/onboarding/orchestrator.go`)

```go
type OnboardingResult struct {
    PagesGenerated int           `json:"pages_generated"`
    PagesFailed    int           `json:"pages_failed"`
    PagesSkipped   int           `json:"pages_skipped"`
    LinksCreated   int           `json:"links_created"`
    VaultSynced    bool          `json:"vault_synced"`
    OutputDir      string        `json:"output_dir,omitempty"`
    Duration       time.Duration `json:"duration"`
    TokensUsed     int           `json:"tokens_used"`
    Errors         []string      `json:"errors"`
    PageIDs        []string      `json:"page_ids"`
}
```

### OnboardingProgress (WebSocket/status polling)

```go
type OnboardingProgress struct {
    JobID       string   `json:"job_id"`
    Status      string   `json:"status"`       // scanning|generating|linking|syncing|complete|failed|idle
    CurrentPage string   `json:"current_page"`
    Generated   int      `json:"generated"`
    Total       int      `json:"total"`
    Errors      []string `json:"errors"`
}
```

### DB Schema Changes

No new tables. Existing `wiki_pages.generated_by` column (no CHECK constraint) accepts `"onboarding"` as new value. Existing `tags_json` includes `["onboarding"]`. `ListWikiPages` Tag filter already works.

### LLM Subsystem Registration

Add `"onboarding"` to `AllowedSubsystems` in `internal/insight/llm/budget.go`.

---

## API Contract

### CLI

```
stratus onboard [flags]

Flags:
  --depth string       Scanning depth: shallow|standard|deep (default "standard")
  --output-dir string  Also write standalone markdown to this directory
  --dry-run            Scan only, print ProjectProfile, do not generate pages
  --max-pages int      Override max pages (default from config: 20)
```

### HTTP API

**POST /api/onboard** — Trigger onboarding (async, returns job ID)

```json
Request:
{
  "depth": "standard",
  "output_dir": "",
  "max_pages": 0
}

Response: 202 Accepted
{
  "job_id": "onboard-1712678400",
  "status": "scanning",
  "message": "Onboarding started"
}

Validation:
  - depth: must be one of "shallow", "standard", "deep" → 400 if invalid
  - max_pages: min=1, max=50 → 400 if out of range (0 = use config default)
  - output_dir: resolved absolute path must be under project root → 400 on path traversal

Errors: 400 (invalid depth/max_pages/path traversal), 409 (already running), 503 (no LLM)
```

**GET /api/onboard/status** — Progress

```json
Response: 200
{
  "job_id": "onboard-1712678400",
  "status": "generating",
  "current_page": "Module: api",
  "generated": 3,
  "total": 8,
  "errors": [],
  "result": null
}
```

**WebSocket broadcasts** via `Hub.BroadcastJSON`:
- Type: `"onboarding_progress"`, Payload: `OnboardingProgress`

### Config Additions

```go
type WikiConfig struct {
    // existing fields...
    OnboardingDepth    string `json:"onboarding_depth"`     // default "standard"; validated: {shallow, standard, deep}
    OnboardingMaxPages int    `json:"onboarding_max_pages"` // default 20; validated: min=1, max=50
}
```

**Depth levels:**

| Depth | Pages | Content |
|-------|-------|---------|
| shallow | 3-5 | Architecture overview, build guide, conventions |
| standard | 8-15 | + module pages for top-level packages |
| deep | 15-25 | + sub-module pages, dependency deep-dive, test strategy |

---

## Sequence Diagram

```
User → CLI/API → Scanner.ScanProject(root) → ProjectProfile
                    ↓
              Orchestrator.RunOnboarding(profile)
                    ↓
              1. WikiEngine.GeneratePageFromData("Architecture Overview", profileSummary)
                    → LLM call → SaveWikiPage(generated_by="onboarding")
                    ↓
              2. For each top-level module:
                    WikiEngine.GeneratePageFromData("Module: X", moduleProfile)
                    → LLM call → SaveWikiPage
                    ↓
              3. WikiEngine.GeneratePageFromData("Conventions", ...)
                 WikiEngine.GeneratePageFromData("Build Guide", ...)
                 WikiEngine.GeneratePageFromData("Dependencies", ...)
                    ↓
              4. Linker.DetectCrossReferences(page, allPages)
                    → SaveWikiLink per detected link
                    ↓
              5. VaultSync.SyncAll() (if vault_path configured)
                    ↓
              6. writeStandaloneMarkdown (if --output-dir)
                    ↓
              → OnboardingResult
```

---

## Component Design Details

### Scanner (`internal/insight/onboarding/scanner.go`)

```go
func ScanProject(rootPath string, depth string) (*ProjectProfile, error)
```

- **Language detection:** Walk directory tree, count files and lines per extension. Map extensions to names. Skip: `.git`, `node_modules`, `vendor`, `__pycache__`, `.venv`, `dist`, `build`, `.next`. Respect `.gitignore`.
- **Entry point detection:** `main.go`, `cmd/*/main.go`, `index.ts`, `src/index.*`, `main.py`, `app.py`, `manage.py`.
- **Directory tree:** `filepath.Walk` with depth limit (3/4/5 for shallow/standard/deep).
- **Config file extraction:** Read known config files capped at 4KB: `go.mod`, `package.json`, `tsconfig.json`, `pyproject.toml`, `Cargo.toml`, `Dockerfile`, `docker-compose.yml`, `Makefile`, `.github/workflows/*.yml`.
- **Git stats:** `git rev-list --count HEAD`, `git shortlog -sn HEAD`, first/last commit dates. Nil if not a git repo.
- **Test structure:** Scan for `*_test.go`, `*.test.ts`, `*.spec.ts`, `test_*.py`, `tests/`, `__tests__/`.
- **Security:** Skip `.env*`, `*.pem`, `*.key`, `*.p12`, `*.pfx`, `credentials*`, `secrets*`, `*.secret`, `id_rsa`, `id_ed25519`.

### Non-Greenfield Detector (`internal/insight/onboarding/detector.go`)

```go
func IsNonGreenfield(rootPath string) (bool, float64)
```

| Factor | Weight | Scoring |
|--------|--------|---------|
| Git history depth | 0.30 | >50 commits=1.0, >10=0.6, >3=0.3, else 0 |
| Source file count | 0.25 | >100 files=1.0, >30=0.6, >10=0.3, else 0 |
| Project markers | 0.20 | go.mod/package.json/etc present=1.0 |
| README exists | 0.10 | README.md present=1.0 |
| CI config exists | 0.15 | .github/workflows or .gitlab-ci.yml=1.0 |

Returns `(true, score)` when score >= 0.4.

### Orchestrator (`internal/insight/onboarding/orchestrator.go`)

```go
type OnboardingOpts struct {
    Depth      string
    MaxPages   int
    OutputDir  string
    ProgressFn func(OnboardingProgress)
}

func RunOnboarding(
    ctx context.Context,
    store wiki_engine.WikiStore,
    llmClient wiki_engine.LLMClient,
    linker *wiki_engine.Linker,
    vaultSync *wiki_engine.VaultSync,
    profile *ProjectProfile,
    opts OnboardingOpts,
) (*OnboardingResult, error)
```

**Page generation order:**
1. Architecture Overview (summary) -- uses full ProjectProfile
2. Module pages (entity) -- one per top-level source directory
3. Conventions (concept) -- uses config files, patterns, test structure
4. Dependencies (summary) -- uses go.mod/package.json content
5. Build Guide (summary) -- uses Makefile, Dockerfile, CI configs

**Fail-open per page:** Log warning, increment `PagesFailed`, continue. Goroutine errors are captured into `OnboardingProgress.Errors` and broadcast via WebSocket `"onboarding_progress"` events — never swallowed silently.

**GeneratedBy override:** The orchestrator sets `page.GeneratedBy = "onboarding"` after `GeneratePageFromData()` returns (before `SaveWikiPage`). Alternatively, `GeneratePageFromData` should accept a `generatedBy` parameter. The implementation should choose whichever approach minimizes changes to the existing WikiEngine interface.

**Idempotency:** Check `ListWikiPages` with Tag="onboarding" and matching title before generating. Skip if published, regenerate if stale.

### New Prompts (`internal/insight/prompts/prompts.go`)

```go
const (
    OnboardingArchitecture = `You are a technical documentation author...`
    OnboardingModule       = `You are a technical documentation author...`
    OnboardingConventions  = `You are a technical documentation author...`
    OnboardingBuildGuide   = `You are a technical documentation author...`
)
```

Each composed with `prompts.ObsidianMarkdown` via `prompts.Compose()`.

---

## Init Integration

In `cmdInit()`, after `governanceIndex(wd)` and before summary output:

```go
if isNonGreenfield, confidence := onboarding.IsNonGreenfield(wd); isNonGreenfield {
    fmt.Printf("\n  Detected existing project (confidence: %.0f%%).\n", confidence*100)
    fmt.Println("  Run `stratus onboard` to auto-generate documentation wiki pages.")
}
```

No LLM calls. Detector runs in <100ms.

---

## Error Handling

| Error | Handling |
|-------|----------|
| LLM client nil | Return error immediately, 503 |
| LLM budget exhausted | Stop, return partial result |
| Single page LLM failure | Log, increment PagesFailed, continue |
| Git not available | Set GitStats=nil, continue |
| VaultSync failure | Non-fatal, set VaultSynced=false |
| Output dir path traversal | Return 400 |
| Onboarding already running | Return 409 |
| Context cancelled | Return partial result |

All errors wrapped with context per project convention.

---

## Security Considerations

1. **Secret file skip list** in scanner (`.env*`, `*.pem`, `*.key`, etc.)
2. **Config file content cap** at 4KB per file
3. **No raw source code to LLM** -- only structural metadata
4. **Output dir traversal prevention** -- must be under project root
5. **LLM budget** -- uses `SubsystemClient` with `"onboarding"` at `PriorityMedium`

---

## Testing Strategy

### Scanner (`internal/insight/onboarding/scanner_test.go`)
- `TestScanProject_GoProject` -- fixture dir with go.mod, main.go, assert languages/entry points
- `TestScanProject_MultiLanguage` -- .go/.ts/.py files, assert all detected
- `TestScanProject_SkipsSecrets` -- .env, credentials.json not in profile
- `TestScanProject_RespectsGitignore` -- vendor/ excluded
- `TestScanProject_DepthLimitsTree` -- deep nesting respects limit
- `TestScanProject_NoGitRepo` -- GitStats nil, no error

### Detector (`internal/insight/onboarding/detector_test.go`)
- `TestIsNonGreenfield_MatureProject` -- (true, >0.8)
- `TestIsNonGreenfield_EmptyDir` -- (false, <0.1)
- `TestIsNonGreenfield_SmallProject` -- (true, ~0.4-0.6)

### Orchestrator (`internal/insight/onboarding/orchestrator_test.go`)
- `TestRunOnboarding_Standard` -- mock LLM, correct SavePage calls
- `TestRunOnboarding_PageFailure` -- partial result
- `TestRunOnboarding_NilLLM` -- immediate error
- `TestRunOnboarding_Idempotent` -- skips existing pages

### API (`api/routes_onboarding_test.go`)
- `TestHandleOnboard_Success` -- 202 with job_id
- `TestHandleOnboard_InvalidDepth` -- 400 for "invalid", empty string
- `TestHandleOnboard_MaxPagesBounds` -- 400 for negative, 0 passthrough, >50 rejected
- `TestHandleOnboard_NoLLM` -- 503
- `TestHandleOnboard_AlreadyRunning` -- 409
- `TestHandleOnboard_PathTraversal` -- 400 for `../`, absolute paths outside root

---

## Implementation Notes

1. Adding `"onboarding"` to `AllowedSubsystems` is additive and safe.
2. VaultSync subdirectories (`summaries/`, `entities/`, `concepts/`) already exist for onboarding page types.
3. Architecture overview MUST be generated first -- module pages reference it.
4. API handler runs orchestrator in goroutine with `sync.Mutex` to prevent concurrent runs.
5. Standalone markdown uses `PageToObsidian()` for consistent formatting.

## Breaking Changes

None. Entirely additive: new CLI command, new API endpoints, new config fields with defaults, new subsystem entry, new prompt constants.

## Key File References

| File | Relevance |
|------|-----------|
| `db/wiki.go:14-28` | WikiPage struct with GeneratedBy field |
| `db/wiki.go:61-111` | SaveWikiPage |
| `db/wiki.go:172-217` | ListWikiPages with Tag filter |
| `db/schema.go:772-786` | wiki_pages DDL |
| `internal/insight/wiki_engine/engine.go:248-300` | GeneratePageFromData |
| `internal/insight/wiki_engine/linker.go:25-49` | DetectCrossReferences |
| `internal/insight/wiki_engine/vault_sync.go:42-92` | SyncAll |
| `internal/insight/wiki_engine/obsidian.go:12-21` | PageToObsidian |
| `internal/insight/llm/budget.go:24-31` | AllowedSubsystems |
| `internal/insight/llm/subsystem_client.go:9-29` | SubsystemClient |
| `internal/insight/prompts/prompts.go:22-25` | Compose() |
| `config/config.go:57-65` | WikiConfig |
| `cmd/stratus/main.go:440-502` | cmdInit |
| `api/server.go:382-389` | Wiki route registration pattern |
