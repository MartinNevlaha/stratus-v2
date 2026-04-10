# Implementation Plan: Project Onboarding for Non-Greenfield Projects

## Design Reference

See `docs/plans/project-onboarding-design.md` for full technical design, ADRs, and data models.

## Task List

### Task 1: Config Additions (S)
**Files:** `config/config.go` (modify), `config/config_onboarding_test.go` (create)
- Add `OnboardingDepth` (string, default "standard", enum: shallow/standard/deep) and `OnboardingMaxPages` (int, default 20, range 1-50) to `WikiConfig`
- Set defaults in `Default()`
- Tests: `TestWikiConfig_OnboardingDepthDefault`, `TestWikiConfig_OnboardingMaxPagesDefault`, `TestWikiConfig_LoadFromJSON`

### Task 2: LLM Subsystem Registration (S)
**Files:** `internal/insight/llm/budget.go` (modify), `internal/insight/llm/budget_test.go` (modify)
- Add `"onboarding": true` to `AllowedSubsystems` map
- Tests: `TestAllowedSubsystems_ContainsOnboarding`, `TestSubsystemClient_Onboarding`

### Task 3: Onboarding Prompts (S)
**Files:** `internal/insight/prompts/prompts.go` (modify), `internal/insight/prompts/prompts_test.go` (create/modify)
- Add `OnboardingArchitecture`, `OnboardingModule`, `OnboardingConventions`, `OnboardingBuildGuide` constants
- Tests: `TestOnboardingPrompts_NonEmpty`, `TestOnboardingPrompts_ComposeWithObsidian`

### Task 4: Scanner (L)
**Files:** `internal/insight/onboarding/scanner.go` (create), `internal/insight/onboarding/scanner_test.go` (create)
- `ScanProject(rootPath, depth) (*ProjectProfile, error)` + all data model types
- Language detection, entry point detection, directory tree, config file extraction, git stats, test structure
- Respects .gitignore, skips secrets/binaries, caps config files at 4KB
- Tests: `TestScanProject_GoProject`, `TestScanProject_MultiLanguage`, `TestScanProject_SkipsSecrets`, `TestScanProject_RespectsGitignore`, `TestScanProject_DepthLimitsTree`, `TestScanProject_NoGitRepo`, `TestScanProject_ConfigFileCap`
- **Dependencies:** None

### Task 5: Non-Greenfield Detector (M)
**Files:** `internal/insight/onboarding/detector.go` (create), `internal/insight/onboarding/detector_test.go` (create)
- `IsNonGreenfield(rootPath) (bool, float64)` — weighted heuristic (git commits 0.30, file count 0.25, project markers 0.20, README 0.10, CI config 0.15), threshold >= 0.4
- Tests: `TestIsNonGreenfield_MatureProject`, `TestIsNonGreenfield_EmptyDir`, `TestIsNonGreenfield_SmallProject`, `TestIsNonGreenfield_NoGit`, `TestIsNonGreenfield_Threshold`
- **Dependencies:** None

### Task 6: Orchestrator (L)
**Files:** `internal/insight/onboarding/orchestrator.go` (create), `internal/insight/onboarding/orchestrator_test.go` (create)
- `RunOnboarding(ctx, store, llmClient, linker, vaultSync, profile, opts) (*OnboardingResult, error)`
- Page order: architecture overview → module pages → conventions → dependencies → build guide
- Uses composited prompts per page type, sets `GeneratedBy = "onboarding"`
- Cross-references via Linker, vault sync, optional standalone markdown output
- Fail-open per page, progress callback, idempotency check
- Tests: `TestRunOnboarding_Standard`, `TestRunOnboarding_PageFailure`, `TestRunOnboarding_NilLLM`, `TestRunOnboarding_Idempotent`, `TestRunOnboarding_ProgressCallback`, `TestRunOnboarding_ContextCancelled`, `TestRunOnboarding_OutputDir`, `TestRunOnboarding_DepthControlsPageCount`, `TestRunOnboarding_VaultSyncFailure`
- **Dependencies:** Tasks 1, 2, 3, 4

### Task 7: Init Integration (S)
**Files:** `cmd/stratus/main.go` (modify)
- In `cmdInit()`, after `governanceIndex(wd)`, call `onboarding.IsNonGreenfield(wd)` and print suggestion if true
- **Dependencies:** Task 5

### Task 8: CLI Command `stratus onboard` (M)
**Files:** `cmd/stratus/main.go` (modify)
- Add `case "onboard"` to CLI switch, implement `cmdOnboard()` with flags: `--depth`, `--output-dir`, `--dry-run`, `--max-pages`
- Load config, open DB, create LLM client, run scanner, run orchestrator, print summary
- Tests: `TestParseOnboardFlags_Defaults`, `TestParseOnboardFlags_AllFlags`
- **Dependencies:** Tasks 1, 2, 4, 6

### Task 9: API Endpoints (M)
**Files:** `api/routes_onboarding.go` (create), `api/routes_onboarding_test.go` (create), `api/server.go` (modify)
- `POST /api/onboard` — async trigger, returns 202 with job_id
- `GET /api/onboard/status` — progress polling
- Server struct gets `onboardingMu sync.Mutex`, `onboardingProgress *OnboardingProgress`
- WebSocket broadcasts via `Hub.BroadcastJSON`
- Validation: depth enum, max_pages 1-50, output_dir traversal check
- Tests: `TestHandleOnboard_Success`, `TestHandleOnboard_InvalidDepth`, `TestHandleOnboard_MaxPagesBounds`, `TestHandleOnboard_NoLLM`, `TestHandleOnboard_AlreadyRunning`, `TestHandleOnboard_PathTraversal`, `TestHandleOnboardStatus_Idle`
- **Dependencies:** Tasks 1, 6

### Task 10: Frontend — Onboarding Trigger (M)
**Files:** `frontend/src/routes/Wiki.svelte` (modify), `frontend/src/lib/api.ts` (modify), `frontend/src/lib/types.ts` (modify)
- "Onboard Project" button with depth selector, progress bar via WebSocket
- `triggerOnboarding(opts)` and `getOnboardingStatus()` API functions
- `OnboardingProgress` and `OnboardingResult` TypeScript types
- **Dependencies:** Task 9

## Dependency Graph

```
Tasks 1, 2, 3, 4, 5 — parallel (no deps)
  ↓
Task 6 (Orchestrator) ← 1, 2, 3, 4
Task 7 (Init) ← 5
  ↓
Task 8 (CLI) ← 1, 2, 4, 6
Task 9 (API) ← 1, 6
  ↓
Task 10 (Frontend) ← 9
```

## Execution Order (serial)
1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10
