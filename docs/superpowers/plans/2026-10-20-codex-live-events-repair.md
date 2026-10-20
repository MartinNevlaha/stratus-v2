# Codex live-event repair Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Repair the five failures observed on uvo_docs_v2, starting with agent runs and current review status.
**Architecture:** Keep legacy adapters unchanged. Give identified Codex turns their own persistent run rows; keep native identities separate from role labels. Compute current review status once on the server. Normalize host tool names/results and support explicit role binding when native briefs are opaque. Validate worktree edits against Git repository identity.
**Tech Stack:** Existing Go/SQLite, Svelte/TypeScript/Vitest; no new runtime dependency.
**Spec:** User's approved repair request and observed failures recorded below; original adapter specification in ../specs/2026-10-20-codex-adapter-design.md remains the compatibility boundary.

## Global Constraints

- Never install/refresh/restart or mutate workflow/configuration/data in uvo_docs_v2. Never POST to port 41777. Use temporary data and loopback ports for all tests.
- Preserve Claude Code and OpenCode handlers, schemas, default targets and lifecycle behavior.
- Keep duplicates and late events from reopening a completed turn. Missing identity does not prove a new run or a delivery role.
- Do not infer PASS/FAIL from arbitrary prose. A newer unverified review or subsequent work must not inherit an older PASS.
- Do not decode opaque host payloads, parse Codex transcripts, change trust, or invent token usage.
- No release, push, global installation or server replacement in this task.
- Implement natively; one fresh independent whole-branch review at the end.

## Review Focus

1. A reused native agent produces a new turn without SubagentStart; create a running turn while retaining the prior terminal run. Delayed events for the old turn must remain terminal.
2. A new task/reviewer or subsequent repair follows PASS; never display stale PASS, including attention summaries and unsorted input.
3. Role binding arrives before/after start or is repeated with conflicting ownership; use explicit native identity and refuse cross-session/workflow adoption.
4. apply_patch returns structured success or an unknown result and edits a linked worktree; only proven successful files in the same repository are accepted.
5. Native collaboration names and opaque messages are delivered; aliases work without inferring content or blocking read-only exploration.

### Task 1: Separate Codex agent turns
**Files:** integrations/codex/events.go, api/routes_codex.go, db/integration_events.go, db/workflow_agents.go, db/schema.go, db/db.go and corresponding tests.
**Interfaces:** AgentRunKey(session, agent, turn string) string; optional WorkflowAgent native_agent_id and turn_id. Existing AgentKey remains the fallback without turn identity.
- [ ] Write API/DB tests for stop t1 → first tool t2 → late start/tool/stop t1 → stop t2, asserting two independent rows, status, counters and reports. Verify DB reopen and unscoped legacy rows.
- [ ] Run focused tests and observe RED.
- [ ] Implement per-turn IDs; a stable identified tool can create a missing turn row. Preserve terminal records and transactional receipts. Add only additive identity columns.
- [ ] Run go test ./db ./api ./integrations/... -count=1; expect PASS.
- [ ] Commit: fix: separate repeated Codex agent turns.

### Task 2: Current review projection
**Files:** db/codex_review.go and tests; api/routes_workflow_agents.go, api/routes_workflow_insights.go, api/routes_codex.go and tests; frontend/src/components/{ReviewPanel,WorkflowActivity}.svelte, frontend/src/lib/types.ts and DOM tests.
**Interfaces:** db.CodexReview(agents, phase, taskIndex) returns a review projection with status awaiting/running/unverified/outdated/PASS/FAIL, current agent ID and counts. Codex agent-list response includes review. Legacy response and ReviewPanel behavior remain unchanged.
- [ ] Write tests for current task vs old task, new unknown/running review, new implementation after PASS, unsorted runs, explicit FAIL and legacy rendering.
- [ ] Run focused Go and frontend tests; observe RED.
- [ ] Compute a conservative current projection; use it in UI and attention instead of skipping newer unknown reports. Keep historical review counts distinct.
- [ ] Run go test ./db ./api -count=1 and npm --prefix frontend run test; expect PASS.
- [ ] Commit: fix: scope Codex review status to current work.

### Task 3: Native payloads, roles and file edits
**Files:** integrations/codex/{events,tools,policy,hooks}.go and tests; integrations/events.go; mcp/runtime.go and tests; api/routes_codex.go and tests; db/integration_events.go, schema.go and tests; Codex instructions.
**Interfaces:** Native collaboration aliases canonicalize only known tool names. Codex-only bind_agent_role requires workflow_id, session_id, agent_id and delivery_role; persisted binding is owner-scoped and applied to that worker's runs. SubagentStart context supplies native identity so a worker can explicitly bind its readable brief's role. Unknown roles stay honest. PostToolUse recognizes documented structured success shapes without assuming success for unknown output. Same-repository worktree validation is filesystem-backed, never prefix matching.
- [ ] Write failing tests for native alias guard, opaque summary redaction, explicit role binding and conflicts, structured successful/failed/unknown patch responses and linked-worktree paths.
- [ ] Run focused tests; observe RED.
- [ ] Implement normalization/bindings/context; document unavailable identity/opaque brief limits. Keep original tool name for display.
- [ ] Run go test ./integrations/... ./db ./api ./mcp ./cmd/stratus -count=1 and OpenCode executable tests; expect PASS.
- [ ] Commit: fix: adapt Codex native roles and edit payloads.

### Task 4: End-to-end verification and independent review
**Files:** cmd/stratus/codex_e2e_test.go, docs/testing/codex-live-events-repair.md, generated frontend assets.
- [ ] Extend real stdio/API fixture for repeated turns, role binding and current review; verify final state in temporary DB.
- [ ] Run full Go suite, frontend tests/check/build, OpenCode tests and fresh Go build; record baseline warnings and actual limits.
- [ ] Commit tests/docs/static assets; request one independent whole-branch review. Fix Important/Critical findings with RED→GREEN tests, no re-review.
- [ ] Preserve isolated checkout and report exact verification and any deferred findings. Do not deploy into the active user workflow.
