# Workflow Governance

## Mandatory Workflow Registration

**FORBIDDEN:** Delegating to delivery agents (via Agent tool; `Task` legacy alias) without an active workflow.

### Why This Matters

- **Phase Guards:** Without workflow registration, phase guards cannot enforce review/verify restrictions
- **Audit Trail:** All changes must be tracked through the workflow state machine
- **Governance:** Workflow transitions are validated against the state machine (`orchestration.ValidateTransition`)
- **Institutional Memory:** Decisions and context are lost without workflow tracking

### Required Pattern

1. **Register workflow BEFORE first delivery-agent delegation:**
   ```
   mcp__stratus__register_workflow
   - id: "<type>-<slug>"
   - type: "spec" | "bug" | "e2e"
   - title: "<human-readable title>"
   - session_id: "<session id>"
   ```

   The workflow skills (`/spec`, `/bug`, `/e2e`, `/swarm`) fill the session id in with
   `${CLAUDE_SESSION_ID}`; this rule file is not substituted, so register workflows through them.

2. **Transition phases via MCP tools:**
   ```
   mcp__stratus__transition_phase
   - workflow_id: "<id>"
   - phase: "<next-phase>"
   ```

3. **Record agent delegations:**
   ```
   mcp__stratus__delegate_agent
   - workflow_id: "<id>"
   - agent_id: "delivery-<role>"
   ```

4. **Pass workflow identity as structured data:**
   ```
   workflow_id: "<id>"
   phase: "<current-phase>"
   task_index: <zero-based task index>
   session_id: "<session id>"
   ```

   `workflow_id` is authoritative. A prompt line such as `Workflow ID: <id>` is
   only a compatibility fallback and audit hint. If structured `workflow_id` and
   prompt text disagree, Stratus must block with `WORKFLOW_ID_MISMATCH` rather
   than choosing either value.

### Enforcement

The guards below are hooks of the `stratus` Claude Code plugin (`.claude/skills/stratus/hooks/hooks.json`);
disabling the plugin turns them all off.

- `WorkflowExistenceGuard`: Blocks delivery-agent delegation without active workflow (fail-closed outside the Stratus self-repo development escape hatch)
- `DelegationGuard`: Requires a resolvable workflow for delivery-agent delegation
- Explicit workflow IDs are resolved before session or global active workflow fallback
- Violations block the tool call; the agent receives the reason and keeps working
- Every denial is recorded: a JSONL line in `<data_dir>/hook_denials.jsonl` and an event of
  type `hook_denial` in the timeline (searchable via `mcp__stratus__search`). Check it when
  a delegated agent works around a guard or reports it could not finish — the block reason
  goes to the agent, not to you.

## Agent Choice Per Phase

Hooks do NOT restrict which delivery agent may run in which phase — the coordinator
picks the agent that fits the work. The phase-agent allowlist was removed because it
denied legitimate delegations mid-phase.

Phase discipline is still enforced where it matters: `PhaseGuard` blocks write tools
for delivery agents during `spec/verify` and `bug/review`, so a reviewer cannot edit
what it reviews.

Role discipline: `PhaseGuard` also holds `delivery-system-architect` and
`delivery-strategic-architect` to Markdown files under a `docs/` directory (design docs,
ADRs), in the project or a swarm worktree; their writes anywhere else are denied.

## Stratus Server Requirement

All guards require the Stratus API server to be running:
```
stratus serve
```

If the API is unreachable, guards will **block** operations (fail-closed) to prevent untracked changes, except while developing this Stratus repository itself.
