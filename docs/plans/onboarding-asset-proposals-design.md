# Technical Design: Onboarding Asset Proposals

## Context & Problem

Stratus onboarding scans projects and generates wiki pages but does not propose project-specific development assets (agents, skills, rules). Users must manually create `.claude/rules/`, `.claude/skills/`, `.claude/agents/`, `.opencode/agents/`, `.opencode/commands/` files.

The `ProjectProfile` already detects languages, patterns, test frameworks, CI providers — enough to deterministically propose useful assets.

## Design

### Approach

- **Deterministic generation** from `ProjectProfile` signals — no LLM required
- **Store in existing `insight_proposals` table** with `type` prefix `asset.*`
- **Dual trigger**: during onboarding + standalone `POST /api/onboarding/propose-assets`
- **Apply via learning proposals UI**: user approves in dashboard, file is written to disk
- **Deduplication**: filesystem check + DB query before proposing

### New Types

| Type | Target | Path Pattern |
|------|--------|-------------|
| `asset.rule` | Claude Code | `.claude/rules/<name>.md` |
| `asset.skill.cc` | Claude Code | `.claude/skills/<name>/SKILL.md` |
| `asset.agent.cc` | Claude Code | `.claude/agents/<name>.md` |
| `asset.agent.oc` | OpenCode | `.opencode/agents/<name>.md` |
| `asset.command.oc` | OpenCode | `.opencode/commands/<name>.md` |

### AssetProposal Struct

```go
type AssetProposal struct {
    Type            string   // asset.rule | asset.skill.cc | asset.agent.cc | asset.agent.oc | asset.command.oc
    Title           string
    Description     string
    ProposedPath    string   // relative path from project root
    ProposedContent string   // full file content
    Confidence      float64  // 0.0-1.0
    Target          string   // "claude-code" | "opencode" | "both"
    Signals         []string // which profile signals triggered this
}
```

### Signal-to-Proposal Mapping

| Signal | Proposals |
|--------|-----------|
| Go language detected | `go-conventions.md` rule |
| TypeScript detected | `ts-conventions.md` rule |
| Python detected | `python-conventions.md` rule |
| `docker` pattern | `docker-conventions.md` rule |
| `monorepo` pattern | `monorepo-conventions.md` rule |
| `web-app` pattern | `delivery-frontend-specialist` agent (CC + OC) |
| `cli-app` pattern | `delivery-cli-specialist` agent (CC + OC) |
| `docker` + CI | `delivery-devops-specialist` agent (CC + OC) |
| CI detected | `ci-conventions.md` rule, `ci-check` skill |
| jest framework | `run-tests` skill (customized for jest) |
| pytest framework | `run-tests` skill (customized for pytest) |
| go-test framework | `run-tests` skill (customized for go test) |

### Deduplication

Before adding a proposal:
1. `os.Stat(filepath.Join(projectRoot, proposedPath))` — skip if file exists
2. DB query: `SELECT proposed_path FROM insight_proposals WHERE type LIKE 'asset.%' AND status IN ('detected','drafted','approved')` — skip if already proposed

### API

**POST /api/onboarding/propose-assets** (standalone, synchronous):
- Scans project, generates proposals, saves to DB
- Returns `{proposals: [...], count: N, skipped: N}`

**PATCH /api/insight/proposals/:id** (existing, extended):
- When `status` → `"approved"` and `type` starts with `"asset."`:
  1. Extract `proposed_path` + `proposed_content` from `recommendation` JSON
  2. Validate path doesn't escape project root (`filepath.Abs` prefix check)
  3. `os.MkdirAll` + `os.WriteFile`
  4. On failure: roll back status, return 500

### Integration with RunOnboarding

Add callback in `OnboardingOpts`:
```go
SaveAssetProposals func([]AssetProposal) error // nil-safe
```

After page generation, call `GenerateAssetProposals()` and invoke callback.
Add `AssetProposals int` to `OnboardingResult`.

### Storage

No schema change. Asset proposals use existing `insight_proposals` table:
- `type` = `"asset.rule"` etc.
- `status` = `"drafted"` (content is fully formed, ready for user approval)
- `recommendation` JSON = `{"proposed_path": "...", "proposed_content": "...", "target": "..."}`
- `source_pattern_id` = `"onboarding"` (synthetic, not a real pattern)

### DB Query Extension

`ListInsightProposals` needs prefix matching: when type ends with `*`, use SQL `LIKE` with `%`.

## File Map

| File | Status | Description |
|------|--------|-------------|
| `internal/insight/onboarding/asset_proposals.go` | New | `AssetProposal`, `GenerateAssetProposals()`, signal mapping |
| `internal/insight/onboarding/asset_templates.go` | New | Template constants for rules, skills, agents |
| `internal/insight/onboarding/asset_dedup.go` | New | `DeduplicateProposals()` |
| `internal/insight/onboarding/asset_proposals_test.go` | New | Tests |
| `internal/insight/onboarding/orchestrator.go` | Modified | Add callback, result field |
| `api/routes_onboarding.go` | Modified | Add `handleProposeAssets` |
| `api/routes_insight.go` | Modified | Add apply hook on approval |
| `api/server.go` | Modified | Register new route |
| `db/insight.go` | Modified | Prefix matching in ListInsightProposals |

## Test Plan

Unit tests: 14 tests covering generation per signal, dedup, confidence, conversion.
Integration tests: 3 tests for API endpoint and apply logic.

## Security

- Path traversal prevention: `filepath.Abs` + prefix check before any write
- Never auto-write: all proposals require explicit user approval
- Never overwrite: dedup + `os.Stat` check before write

## Risks

| Risk | Mitigation |
|------|------------|
| Templates become stale | Go constants, easy to update in releases |
| User accidentally overwrites file | Dedup at generation + overwrite check at apply |
| Large recommendation JSON | ~1-3 KB per proposal, max ~15 proposals = <50 KB total |
| Race condition on concurrent calls | Check-then-insert, acceptable small window |

## Breaking Changes

None. All changes additive.
