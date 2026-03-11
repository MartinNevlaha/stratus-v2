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

## Phase 1: Setup

### 1a. Register Workflow

First, register the workflow using `mcp__stratus__register_workflow`:

```
id: "<kebab-slug>"          # lowercase, hyphenated, max 50 chars
type: "e2e"
title: "E2E: <title from $ARGUMENTS>"
session_id: "${CLAUDE_SESSION_ID}"
```

### 1b. Environment Checks

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

### 1c. Transition to Plan

Use `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "plan"
```

---

## Phase 2: Plan

Delegate test planning to the appropriate agent.

### 2a. Set Task

Set the planning task (use Bash for task API calls if no MCP tool available).

### 2b. Delegate to Planner

Delegate to `delivery-strategic-architect` or `delivery-qa-engineer` (Task tool) with:
- The user's test scope from `$ARGUMENTS`
- The seed test file location: `tests/seed.spec.ts`
- The base URL from `.env.playwright.example` or `playwright.config.ts`
- Any relevant PRD or requirements docs mentioned by the user

Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-qa-engineer"
```

### 2c. Transition to Generate

After planner finishes, transition using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "generate"
```

---

## Phase 3: Generate

Generate test files from the plans.

### 3a. Read Spec Files

Read all spec files from `specs/` and create tasks for each test scenario.

### 3b. For Each Scenario

1. Delegate to `delivery-qa-engineer` or `delivery-frontend-engineer` (Task tool) with the test plan
2. Record with `mcp__stratus__delegate_agent`
3. Mark task complete

### 3c. Transition to Heal

After all tests generated, transition using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "heal"
```

---

## Phase 4: Heal

Run all tests and fix failures.

### 4a. Set Task

Set healing task.

### 4b. Delegate to Debugger

Delegate to `delivery-debugger` or `delivery-qa-engineer` (Task tool):
- Tell it to run all tests and fix any failures
- It will diagnose and fix issues

Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-debugger"
```

### 4c. Evaluate Results

- If all tests pass → transition to complete
- If healer reports tests need regeneration → transition back to generate
- Maximum 3 heal→generate loops before completing with partial results

Use `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "complete"
```

---

## Phase 5: Complete

Transition to complete using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "complete"
```

Summarize results using `mcp__stratus__save_memory` for key findings.

---

## MCP Tools Reference

| Tool | Purpose |
|------|---------|
| `mcp__stratus__register_workflow` | Create new workflow (REQUIRED first) |
| `mcp__stratus__transition_phase` | Move to next phase |
| `mcp__stratus__delegate_agent` | Record agent delegation |
| `mcp__stratus__get_workflow` | Check current workflow state |
| `mcp__stratus__save_memory` | Save findings for future reference |

---

## Rules

- **NEVER** write test code directly — delegate ALL test writing to agents.
- Always get user confirmation of the seed test before proceeding to plan.
- Check current state: `mcp__stratus__get_workflow` with `workflow_id: "<slug>"`
- Maximum 3 heal→generate loops to prevent infinite cycling.
