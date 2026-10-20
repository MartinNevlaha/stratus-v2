# Codex live-event repair

This repair targets the failures observed read-only on `uvo_docs_v2` after v0.21.0. Implementation and all write tests run in the isolated Stratus checkout, temporary project directories, temporary databases and dynamically allocated loopback ports. The active project, server on port 41777, workflow, configuration and global installation are not changed. No release or deployment is performed.

## Behavior

- Native worker turns have separate run rows. A first identified tool event starts a reused worker's new turn even without SubagentStart; delayed events cannot reopen its old terminal turn. Missing turn identity retains the previous fallback identity.
- Codex review status comes from the current phase/task's evidence. New reviews are running or unverified until they supply an explicit verdict. A new task waits for its own review. Activity after a review makes its verdict outdated, including activity from an already-running worker. Both the review panel and attention summaries consume this projection. Legacy review rendering is unchanged.
- `bind_agent_role` explicitly binds the native parent session, native worker and workflow to a delivery role. SubagentStart supplies those native IDs as worker context. Binding before or after start updates that worker's runs; conflicting roles, foreign sessions and cross-workflow adoption are rejected. Default workers without a binding remain unclassified. Bound default workers use the existing delivery guard policy, including read-only access without API discovery. Closed workflows retain identity so closing a workflow cannot hide its bound role from the guard.
- Known native collaboration aliases use the same delegation policy. Opaque `gAAAA…` delegation messages are redacted from summaries; the adapter does not decode them or infer a role. It keeps the original tool name for display.
- Patch files are recorded only on recognizable successful results, including structured output and explicit exit status. Explicit failure wins over success text; unknown output does not prove a successful edit. Files outside the selected project require a registered linked Git worktree of the same repository, verified using its common directory and reverse registration. Existing ancestors are resolved to reject symlink escapes. Files in verified linked worktrees retain their absolute location.

## Verification

The red/green regressions cover reused and delayed turns, legacy database migration, scoped/unsorted review evidence, newer unknown reviews, work continuing after PASS, existing receipt-key stability, actual collaboration aliases, explicit role binding and conflicts, bound-role guards, structured patch outcomes, foreign paths and linked worktrees.

`TestCodexE2E_ReusedWorkersAndCurrentReview` runs the built CLI through real MCP stdio, HTTP API, hook processes and SQLite. It checks binding, two reviewer turns, a terminal historical PASS, current running/unverified status, subsequent activity invalidation and a bound native default worker's write guard. Existing mixed-runtime and installation target fixtures remain in the suite. All fixtures are isolated; none use the active user workflow.

Verification commands:

```
go test ./... -count=1
node --test tests/opencode/stratus.test.mjs
npm --prefix frontend run test
npm --prefix frontend run check
npm --prefix frontend run build
go build -o <temporary-path>/stratus ./cmd/stratus
```

Frontend verification: 8 files / 45 tests pass; type/Svelte checking has 0 errors and the existing 52 warnings in 4 files. The production frontend build succeeds with the existing chunk-size warning. OpenCode executable regressions: 4/4 pass. Full Go verification covers 22 packages with tests. Generated embedded frontend assets include the current review panel.

## Host limits and activation

The structured tool fixtures exercise supported output shapes; they are not claimed to be captured raw PostToolUse payloads from the running project. The host can omit identity or opt tools out of hooks. Token usage and unavailable per-tool identity are not invented, and transcripts are not parsed.

A readable brief and explicit role binding are required when the host hides the delegation payload. Historical default workers are not retroactively guessed into delivery roles. Updated worker context/instructions apply after the repaired binary/assets are deliberately installed and activated in a later session; this repair does not refresh or restart the active project. Guardrails cover identifiable supported operations and are not a complete enforcement boundary. Existing indexing still follows the configured project root; recording a linked-worktree edit does not change the external indexer's repository scope.
