---
name: e2e
description: "E2E testing coordinator (setup → plan → generate → heal → complete). Orchestrates Playwright Test Agents for autonomous end-to-end test creation and maintenance."
disable-model-invocation: true
allowed-tools: mcp__stratus
---

# E2E Testing Workflow

You are the **coordinator** for an autonomous E2E testing workflow using Playwright Test Agents. You orchestrate work by delegating to specialized Playwright agents via Agent tool. `Task` is a legacy alias only. Delegated agents can run in the background: wait for each one's completion notification before acting on its result, and never `sleep` to wait. You don't write test code directly — agents handle that.

## Prerequisites

Stratus server must be running: `stratus serve`

---

## Execution Protocol

Run the phases in order, and make every MCP call a phase lists before moving to the next one. The phase guards and the workflow record depend on these calls: a skipped call shows up as a blocked delegation or a gap in the audit trail.

---

## Phase 1: Setup

### Step 1 — Register Workflow

Do this first, before reading files or delegating: the guard hooks block delivery-agent delegations until the workflow exists.

Call `mcp__stratus__register_workflow` with:

```
id: "<kebab-slug>"          # lowercase, hyphenated, max 50 chars
type: "e2e"
title: "E2E: <title from $ARGUMENTS>"
session_id: "${CLAUDE_SESSION_ID}"
```

Continue once it returns a workflow ID.

### Step 2 — Environment Checks

Perform ALL of the following:

1. **Check for `package.json`** — if missing, this is not a Node.js project. Stop and inform the user.

2. **Check for `@playwright/test`** in devDependencies:
   ```bash
   cat package.json | grep -q "@playwright/test" || npm install -D @playwright/test
   ```

3. **Check for `playwright.config.ts`** — if missing, create a sensible default.

4. **Create `specs/` directory** with a README if it doesn't exist.

5. **Create `tests/seed.spec.ts`** if missing — ask the user about the app's entry point URL and create a minimal smoke test.

6. **Create `.env.playwright.example`** with placeholder variables.

7. **Install browser:**
   ```bash
   npx playwright install chromium
   ```

### Step 3 — Transition to Plan

After environment setup is complete, call `mcp__stratus__transition_phase` before delegating any planning agent.

```
workflow_id: "<slug>"
phase: "plan"
```

---

## Phase 2: Plan

### Step 4 — Delegate to Planner

Delegate to `delivery-strategic-architect` or `delivery-qa-engineer` (Agent tool) with:
- The user's test scope from `$ARGUMENTS`
- The seed test file location: `tests/seed.spec.ts`
- The base URL from `.env.playwright.example` or `playwright.config.ts`
- Any relevant PRD or requirements docs mentioned by the user

Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-qa-engineer"
```

### Step 5 — Transition to Generate

After planner finishes, call `mcp__stratus__transition_phase` before delegating any test generation agent.

```
workflow_id: "<slug>"
phase: "generate"
```

**Autopilot (Claude Code, optional):** once the transition succeeds, offer the user a goal that keeps this workflow running to completion without a prompt per step. Print it exactly, with the workflow id filled in:

```
/goal Stratus workflow <slug> is complete: the latest mcp__stratus__get_workflow output for <slug> in this conversation shows phase "complete" and every task done, and the last Playwright test run shown passes. Work through the remaining phases in order; if the coordinator instructions for this workflow are not in this conversation, read .claude/skills/e2e/SKILL.md and continue <slug> from its current phase as it describes. Do not skip phases or weaken tests to make them pass. When a step needs my decision, ask me with AskUserQuestion. Stop after 40 turns.
```

Tell the user it starts when they send it, runs unattended only in auto mode, and stops with `/goal clear`; the `/stratus` pane's Autopilot button fills in the same goal. Do not wait for an answer — continue with the next phase.

---

## Phase 3: Generate

### Step 6 — Generate Test Files

Read all spec files from `specs/` and create tasks for each test scenario.

For each scenario:
1. Mark as started with `mcp__stratus__start_task`:

```
workflow_id: "<slug>"
task_index: 0  # zero-based index
```

2. Delegate to `delivery-qa-engineer` or `delivery-frontend-engineer` (Agent tool) with the test plan
3. Record with `mcp__stratus__delegate_agent`
4. Mark complete with `mcp__stratus__complete_task`:

```
workflow_id: "<slug>"
task_index: 0
```

### Step 7 — Transition to Heal

After all tests are generated, call `mcp__stratus__transition_phase` before delegating any healing agent.

```
workflow_id: "<slug>"
phase: "heal"
```

---

## Phase 4: Heal

### Step 8 — Delegate to Debugger

Delegate to `delivery-debugger` or `delivery-qa-engineer` (Agent tool):
- Tell it to run all tests and fix any failures
- It will diagnose and fix issues

Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-debugger"
```

### Step 9 — Evaluate Results

- If all tests pass → transition to complete
- If healer reports tests need regeneration → transition back to generate, then re-generate
- Maximum 3 heal→generate loops before completing with partial results

**Transition to Complete** using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "complete"
```

---

## Phase 5: Complete

Summarize results using `mcp__stratus__save_memory` for key findings.

---

## MCP Tools Reference

| Tool | Purpose |
|------|---------|
| `mcp__stratus__register_workflow` | Create new workflow (first call) |
| `mcp__stratus__transition_phase` | Move to next phase (at each phase boundary) |
| `mcp__stratus__delegate_agent` | Record agent delegation (for every delivery agent) |
| `mcp__stratus__start_task` | Mark task as in_progress (before delegating each task) |
| `mcp__stratus__complete_task` | Mark task as done (after each task completes) |
| `mcp__stratus__get_workflow` | Check current workflow state |
| `mcp__stratus__save_memory` | Save findings for future reference |

---

## Rules

- Never write test code directly — delegate all test writing to agents.
- Call `mcp__stratus__register_workflow` as the very first action.
- Call `mcp__stratus__transition_phase` before starting each new phase.
- Call `mcp__stratus__start_task` before delegating each task.
- Call `mcp__stratus__complete_task` after each task completes successfully.
- Call `mcp__stratus__delegate_agent` for every delivery agent delegation.
- Always get user confirmation of the seed test before proceeding to plan.
- Check current state: `mcp__stratus__get_workflow` with `workflow_id: "<slug>"`
- Maximum 3 heal→generate loops to prevent infinite cycling.
