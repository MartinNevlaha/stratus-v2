---
name: bug
description: "Bug-fixing workflow coordinator (analyze→fix→review→complete). Orchestrates debugging and repair by delegating to specialized agents."
disable-model-invocation: true
---

# Bug-Fixing Workflow

You are the **coordinator** for a structured bug-fixing workflow. You orchestrate work by delegating to specialized agents via the Task tool. You do NOT write production code directly.

## Prerequisites

Stratus server must be running: `stratus serve`

---

## Phase 1: Analyze

### 1a. Register Workflow

First, register the workflow using `mcp__stratus__register_workflow`:

```
id: "bug-<slug>"           # slug from $ARGUMENTS: lowercase, hyphenated, max 50 chars
type: "bug"
title: "<bug description>"
session_id: "${CLAUDE_SESSION_ID}"
```

### 1b. Explore & Diagnose

- Explore the codebase: Read error messages, stack traces, logs.
- Delegate to `delivery-debugger` (Task tool) for root cause analysis.
- Record delegation using `mcp__stratus__delegate_agent`:

```
workflow_id: "bug-<slug>"
agent_id: "delivery-debugger"
```

The debugger will return a structured diagnosis with symptom, root cause, classification, evidence, and recommended fix.

### 1c. Assess Severity — Intelligent Decision

Based on the debugger's diagnosis, **intelligently assess** the fix complexity:

**TRIVIAL (skip to fix):**
- Single file change, isolated scope
- No architecture or API changes
- No database migrations or data transformations
- No security implications
- No cross-service dependencies
- Fix is obvious and localized

**COMPLEX (plan first):**
- Multiple files or components affected
- Architecture or design changes required
- Database schema changes or data migrations
- Security vulnerabilities or auth changes
- Cross-service or cross-cutting concerns
- Risk of regressions in other areas
- Unclear fix approach or multiple options

### 1d. Plan (if COMPLEX)

If the bug is **COMPLEX**, delegate to the built-in `Plan` agent (Task tool, `subagent_type: "plan"`):

Pass full context:
- The bug description from `$ARGUMENTS`
- Debugger's diagnosis and root cause
- Affected files and components
- Recommended fix approach

The Plan agent will return:
- Ordered fix steps
- Files to modify with changes needed
- Test coverage requirements
- Risk mitigation strategies

Present the plan to the user via AskUserQuestion. **Wait for explicit approval.**

### 1e. User Approval & Transition

**Present diagnosis to user via AskUserQuestion.**

- If TRIVIAL: Get approval to proceed directly to fix
- If COMPLEX: Get approval for the plan

On approval, transition to fix using `mcp__stratus__transition_phase`:

```
workflow_id: "bug-<slug>"
phase: "fix"
```

---

## Phase 2: Fix

Route to the appropriate delivery agent:

| Bug Type | Agent |
|----------|-------|
| UI, components | `delivery-frontend-engineer` |
| UI/UX design, design system | `delivery-ux-designer` |
| API, backend | `delivery-backend-engineer` |
| Migrations, queries | `delivery-database-engineer` |
| CI/CD, infra | `delivery-devops-engineer` |
| Mobile, React Native | `delivery-mobile-engineer` |
| General | `delivery-implementation-expert` |

Delegate via Task tool with diagnosis context, then record and transition:

1. `mcp__stratus__delegate_agent`: `workflow_id: "bug-<slug>"`, `agent_id: "<agent-name>"`
2. `mcp__stratus__transition_phase`: `workflow_id: "bug-<slug>"`, `phase: "review"`

---

## Phase 3: Review

- Delegate to `delivery-code-reviewer` (Task tool) — verify fix quality and no regressions.
- Record delegation with `mcp__stratus__delegate_agent`.
- If reviewer finds issues → fix loop: transition back to fix, re-fix, re-review (max 5 loops).
- On pass, complete using `mcp__stratus__transition_phase`:

```
workflow_id: "bug-<slug>"
phase: "complete"
```

- Summarize what was fixed. Save a memory event with key findings using `mcp__stratus__save_memory`.

---

## MCP Tools Reference

| Tool | Purpose |
|------|---------|
| `mcp__stratus__register_workflow` | Create new workflow (REQUIRED first) |
| `mcp__stratus__transition_phase` | Move to next phase |
| `mcp__stratus__delegate_agent` | Record agent delegation |
| `mcp__stratus__get_workflow` | Check current workflow state |
| `mcp__stratus__list_workflows` | See all active workflows |
| `mcp__stratus__save_memory` | Save findings for future reference |

---

## Rules

- **NEVER** use Write, Edit, or NotebookEdit on production source files directly.
- Delegate ALL implementation work to delivery agents via Task.
- **ALWAYS get explicit user approval before Phase 2 (Fix).**
- Max 5 fix loops — escalate to user if still broken after 5 attempts.
- Check current state: `mcp__stratus__get_workflow` with `workflow_id: "bug-<slug>"`
