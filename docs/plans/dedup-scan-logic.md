# Plan: Deduplicate Scan Logic in db/agent_evolution.go

## Finding
`cf3b9a1a-fef1-4fc0-ad0e-b4abbf82f3a1` — Duplicated scan logic between single and multi-row functions.

## Problem
- `scanAgentCandidate` (line 492) and the loop body of `scanAgentCandidates` (line 518) have identical field-scanning + JSON-unmarshaling logic (~15 lines duplicated).
- `scanAgentExperiment` (line 554) and the loop body of `scanAgentExperiments` (line 598) have identical field-scanning + time-parsing logic (~20 lines duplicated).

## Approach
Extract the shared scan logic into two private helper functions that accept scanned field values:

1. **`scanAgentCandidateValues`** — takes scanned string fields, returns `(*AgentCandidate, error)`. Called by both `scanAgentCandidate` (after `row.Scan`) and `scanAgentCandidates` (inside `rows.Next()` loop).
2. **`scanAgentExperimentValues`** — takes scanned fields including `sql.NullString`, returns `(*AgentExperiment, error)`. Called by both `scanAgentExperiment` and `scanAgentExperiments`.

The `sql.ErrNoRows` check stays in the single-row callers only. Error wrapping context is preserved per caller.

## Key Constraints
- No changes to public API or SQL queries
- Same file (`db/agent_evolution.go`) only
- No new test files needed (private helpers tested via public callers per TDD rules)
- Governance check: COMPLIANT (no ADR conflicts)

## Tasks
1. Extract `scanAgentCandidateValues` and refactor `scanAgentCandidate` + `scanAgentCandidates` to use it
2. Extract `scanAgentExperimentValues` and refactor `scanAgentExperiment` + `scanAgentExperiments` to use it
3. Verify: code review + tests pass
4. Update code quality finding status

## Scope
This workflow scopes to `agent_evolution.go` only. The same pattern exists in other `db/` files (evolution.go, workflow_synthesis.go, etc.) — those can be addressed in follow-up workflows per Karpathy Principle 3 (Surgical Changes).
