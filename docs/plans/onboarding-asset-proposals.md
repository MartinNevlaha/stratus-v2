# Implementation Plan: Onboarding Asset Proposals

Design doc: `docs/plans/onboarding-asset-proposals-design.md`

## Tasks (ordered, bottom-up)

### Task 0: Asset templates
- New file: `internal/insight/onboarding/asset_templates.go`
- Go string constants for each asset type:
  - Rule templates: go-conventions, ts-conventions, python-conventions, docker-conventions, monorepo-conventions, ci-conventions
  - Skill templates: run-tests (jest/pytest/go), docker-build, ci-check
  - Agent templates CC: frontend-specialist, cli-specialist, devops-specialist
  - Agent templates OC: same agents in OpenCode format
  - Command templates OC: build, test
- Use `text/template` for variable substitution (project name, framework, test command)
- Agent: delivery-implementation-expert

### Task 1: Asset deduplication
- New file: `internal/insight/onboarding/asset_dedup.go`
- `DeduplicateProposals(proposals []AssetProposal, projectRoot string, existingPaths map[string]bool) []AssetProposal`
- Checks `os.Stat` for each proposal path
- Checks against existingPaths map (from DB query)
- Agent: delivery-backend-engineer

### Task 2: Asset proposal generator + tests
- New file: `internal/insight/onboarding/asset_proposals.go`
- `AssetProposal` struct
- `GenerateAssetProposals(profile *ProjectProfile, projectRoot string, existingPaths map[string]bool) []AssetProposal`
- Maps ProjectProfile signals to proposals using templates from Task 0
- Confidence: `min(lang.Percentage/100, 0.95)` for lang proposals, 0.8 for pattern, 0.85 for CI
- New file: `internal/insight/onboarding/asset_proposals_test.go`
- Tests: Go project, multi-language, docker pattern, monorepo, web-app, cli-app, CI, jest, empty profile, confidence calc, dedup
- Agent: delivery-backend-engineer

### Task 3: DB prefix matching for ListInsightProposals
- Modified: `db/insight.go`
- When type param ends with `*`, use `LIKE` with `%` suffix
- Test: `TestListInsightProposals_PrefixMatch`
- Agent: delivery-backend-engineer

### Task 4: Apply hook on proposal approval
- Modified: `api/routes_insight.go`
- In `handleUpdateInsightProposalStatus`: when status → "approved" and type starts with "asset.", write file
- `applyAssetProposal()` method: extract path/content from recommendation JSON, validate no path traversal, MkdirAll, WriteFile
- On failure: rollback status, return 500
- Tests: write success, path traversal rejection, rollback on failure
- Agent: delivery-backend-engineer

### Task 5: Standalone API endpoint
- Modified: `api/routes_onboarding.go` — add `handleProposeAssets`
- Modified: `api/server.go` — register route `POST /api/onboarding/propose-assets`
- Scans project, queries existing proposals, generates, deduplicates, saves to DB
- Returns `{proposals, count, skipped}`
- Test: `TestHandleProposeAssets_Success`
- Agent: delivery-backend-engineer

### Task 6: Integration with RunOnboarding
- Modified: `internal/insight/onboarding/orchestrator.go`
- Add `SaveAssetProposals func([]AssetProposal) error` to `OnboardingOpts`
- Add `AssetProposals int` to `OnboardingResult`
- After page generation, call generator + callback
- Modified: `api/routes_onboarding.go` — wire callback in handleOnboard goroutine
- Agent: delivery-backend-engineer

### Task 7: Verify all tests pass and binary compiles
- Run: `go test ./internal/insight/onboarding/... ./api/... ./db/...`
- Run: `go build ./cmd/stratus`
- Agent: delivery-qa-engineer
