# Workflow Governance

## Mandatory Workflow Registration

**FORBIDDEN:** Delegating to delivery agents (via Task tool) without an active workflow.

### Why This Matters

- **Phase Guards:** Without workflow registration, phase guards cannot enforce review/verify restrictions
- **Audit Trail:** All changes must be tracked through the workflow state machine
- **Governance:** Workflow transitions are validated against the state machine (`orchestration.ValidateTransition`)
- **Institutional Memory:** Decisions and context are lost without workflow tracking

### Required Pattern

1. **Register workflow BEFORE first Task delegation:**
   ```
   mcp__stratus__register_workflow
   - id: "<type>-<slug>"
   - type: "spec" | "bug" | "e2e"
   - title: "<human-readable title>"
   - session_id: "${CLAUDE_SESSION_ID}"
   ```

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

### Enforcement

- `WorkflowExistenceGuard`: Blocks Task delegation without active workflow (fail-closed)
- `DelegationGuard`: Enforces phase-agent matching
- Violations result in immediate block with error message

## Phase-Agent Matching

Delivery agents are only allowed in specific phases:

| Workflow | Phase | Allowed Agents |
|----------|-------|----------------|
| bug | analyze | delivery-debugger, delivery-strategic-architect |
| bug | fix | delivery-*-engineer, delivery-implementation-expert |
| bug | review | delivery-code-reviewer |
| spec | plan | delivery-strategic-architect, delivery-system-architect |
| spec | implement | delivery-*-engineer, delivery-implementation-expert |
| spec | verify | delivery-code-reviewer |

Attempting to delegate an agent outside its allowed phase will be blocked.

## Stratus Server Requirement

All guards require the Stratus API server to be running:
```
stratus serve
```

If the API is unreachable, guards will **block** operations (fail-closed) to prevent untracked changes.
