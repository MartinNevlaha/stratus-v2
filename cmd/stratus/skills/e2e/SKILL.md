---
name: e2e
description: "E2E testing coordinator (setup → plan → generate → heal → complete). Orchestrates Playwright Test Agents for autonomous end-to-end test creation and maintenance."
disable-model-invocation: true
---

# E2E Testing Workflow

You are the **coordinator** for an autonomous E2E testing workflow using Playwright Test Agents. You orchestrate work by delegating to specialized Playwright agents via Task tool. You do NOT write test code directly — agents handle that.

## Prerequisites

Stratus server must be running: `stratus serve`

---

## MANDATORY EXECUTION PROTOCOL

You MUST follow the phases in strict order. Each phase has mandatory MCP tool calls that MUST be executed. Do NOT skip any step. Do NOT proceed to the next phase without completing all mandatory calls in the current phase.

---

## Phase 1: Setup

### STEP 1 — MANDATORY: Register Workflow

**This is the FIRST thing you MUST do. Do NOT delegate to any agent, do NOT read any files, do NOT do anything else until this is complete.**

Call `mcp__stratus__register_workflow` with:

```
id: "<kebab-slug>"          # lowercase, hyphenated, max 50 chars
type: "e2e"
title: "E2E: <title from $ARGUMENTS>"
session_id: "${CLAUDE_SESSION_ID}"
```

**DO NOT PROCEED until `mcp__stratus__register_workflow` succeeds and returns a workflow ID.**

### STEP 2 — Environment Checks

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

### STEP 3 — MANDATORY: Transition to Plan

**After environment setup is complete, you MUST call `mcp__stratus__transition_phase` BEFORE delegating any planning agent. DO NOT skip this step.**

```
workflow_id: "<slug>"
phase: "plan"
```

**DO NOT PROCEED to Phase 2 until this transition succeeds.**

---

## Phase 2: Plan

### STEP 4 — Delegate to Planner

Delegate to `delivery-strategic-architect` or `delivery-qa-engineer` (Task tool) with:
- The user's test scope from `$ARGUMENTS`
- The seed test file location: `tests/seed.spec.ts`
- The base URL from `.env.playwright.example` or `playwright.config.ts`
- Any relevant PRD or requirements docs mentioned by the user

**MANDATORY:** Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-qa-engineer"
```

### STEP 5 — MANDATORY: Transition to Generate

**After planner finishes, you MUST call `mcp__stratus__transition_phase` BEFORE delegating any test generation agent. DO NOT skip this step.**

```
workflow_id: "<slug>"
phase: "generate"
```

**DO NOT PROCEED to Phase 3 until this transition succeeds.**

---

## Phase 3: Generate

### STEP 6 — Generate Test Files

Read all spec files from `specs/` and create tasks for each test scenario.

For each scenario:
1. Delegate to `delivery-qa-engineer` or `delivery-frontend-engineer` (Task tool) with the test plan
2. **MANDATORY:** Record with `mcp__stratus__delegate_agent`
3. Mark task complete

### STEP 7 — MANDATORY: Transition to Heal

**After all tests are generated, you MUST call `mcp__stratus__transition_phase` BEFORE delegating any healing agent. DO NOT skip this step.**

```
workflow_id: "<slug>"
phase: "heal"
```

**DO NOT PROCEED to Phase 4 until this transition succeeds.**

---

## Phase 4: Heal

### STEP 8 — Delegate to Debugger

Delegate to `delivery-debugger` or `delivery-qa-engineer` (Task tool):
- Tell it to run all tests and fix any failures
- It will diagnose and fix issues

**MANDATORY:** Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-debugger"
```

### STEP 9 — Evaluate Results

- If all tests pass → **MANDATORY:** transition to complete
- If healer reports tests need regeneration → **MANDATORY:** transition back to generate, then re-generate
- Maximum 3 heal→generate loops before completing with partial results

**MANDATORY: Transition to Complete** using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "complete"
```

---

## Phase 5: Complete

**MANDATORY:** Summarize results using `mcp__stratus__save_memory` for key findings.

---

## MCP Tools Reference

| Tool | Purpose |
|------|---------|
| `mcp__stratus__register_workflow` | Create new workflow (REQUIRED FIRST — call before anything else) |
| `mcp__stratus__transition_phase` | Move to next phase (REQUIRED at each phase boundary) |
| `mcp__stratus__delegate_agent` | Record agent delegation (REQUIRED for every delivery agent) |
| `mcp__stratus__get_workflow` | Check current workflow state |
| `mcp__stratus__save_memory` | Save findings for future reference |

---

## Rules

- **NEVER** write test code directly — delegate ALL test writing to agents.
- **ALWAYS** call `mcp__stratus__register_workflow` as the very first action.
- **ALWAYS** call `mcp__stratus__transition_phase` before starting each new phase.
- **ALWAYS** call `mcp__stratus__delegate_agent` for every delivery agent delegation.
- Always get user confirmation of the seed test before proceeding to plan.
- Check current state: `mcp__stratus__get_workflow` with `workflow_id: "<slug>"`
- Maximum 3 heal→generate loops to prevent infinite cycling.
