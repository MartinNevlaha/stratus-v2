---
name: spec
description: "Spec-driven development coordinator (plan→implement→verify→learn). Orchestrates work by delegating to specialized agents."
disable-model-invocation: true
---

# Spec-Driven Development

You are the **coordinator** for a spec-driven development workflow. You orchestrate work by delegating to specialized agents via the Task tool. You do NOT write production code directly.

## Prerequisites

Stratus server must be running: `stratus serve`

---

## Phase 1: Plan

### 1a. Register Workflow

First, register the workflow using `mcp__stratus__register_workflow`:

```
id: "<kebab-slug>"          # lowercase, hyphenated, max 50 chars
type: "spec"
title: "<title from $ARGUMENTS>"
session_id: "${CLAUDE_SESSION_ID}"
complexity: "simple" | "complex"   # complex for multi-service, auth, database, cross-cutting
```

### 1b. Codebase Exploration

Delegate to the `Explore` agent via Task tool (`subagent_type: "explore"`) with thoroughness `"very thorough"`. Pass the requirement from `$ARGUMENTS` and ask it to:
- Find all files, modules, and patterns relevant to the requirement
- Identify existing conventions, utilities, and abstractions that should be reused
- Map dependencies and integration points that the implementation will touch

Do NOT write code during exploration.

### 1c. Governance Check

Delegate to `delivery-strategic-architect` or `delivery-system-architect` (Task tool) to review the requirement against project governance:
- Does this requirement conflict with any accepted ADRs?
- Are there mandatory practices that must be followed?
- Are there architectural constraints to consider?

Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-strategic-architect"
```

If findings require updates → share with user and adjust requirements before proceeding.

### 1d. Task Planning

Delegate to the `Plan` subagent via Task tool (`subagent_type: "plan"`). Pass full context:
- The requirement from `$ARGUMENTS`
- Key files, directories, and patterns discovered by the Explore agent
- Relevant architecture, patterns, and constraints
- Any governance findings

The Plan agent will return a step-by-step implementation plan.

Use the Plan output to:
1. Write the plan to `docs/plans/<slug>.md`
2. Extract the ordered task list

Present the plan and task list to the user for approval via AskUserQuestion.
On approval, transition to implement using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "implement"
```

---

## Phase 2: Implement

Route tasks to appropriate delivery agents:

| Task Type | Agent |
|-----------|-------|
| API, backend, handlers | `delivery-backend-engineer` |
| UI, components, pages | `delivery-frontend-engineer` |
| UI/UX design, design system | `delivery-ux-designer` |
| Migrations, schema | `delivery-database-engineer` |
| Infra, CI/CD | `delivery-devops-engineer` |
| Mobile, React Native | `delivery-mobile-engineer` |
| General/unclear | `delivery-implementation-expert` |

For each task:
1. Delegate via Task tool
2. Record with `mcp__stratus__delegate_agent`
3. Mark complete in workflow

After all tasks, transition to verify using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "verify"
```

---

## Phase 3: Verify

- Delegate to `delivery-code-reviewer` (Task tool) for spec compliance, code quality, and test adequacy.
- Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-code-reviewer"
```

- If reviewer returns `[must_fix]` issues → fix loop: transition back to implement, fix, re-verify.
- On pass, transition to learn using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "learn"
```

---

## Phase 4: Learn

**Step 1 — Save memory events:** Use `mcp__stratus__save_memory`:

```
text: "<key finding>"
type: "decision" | "discovery" | "feature"
tags: ["<relevant-tags>"]
importance: 0.8
```

**Step 2 — Write governance artifacts** (only clear, unambiguous decisions): rules to `.claude/rules/`, ADRs to `docs/decisions/`.

**Step 3 — Complete workflow** using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "complete"
```

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
| `mcp__stratus__retrieve` | Search code and governance docs |

---

## Rules

- **NEVER** use Write, Edit, or NotebookEdit on production source files directly.
- Delegate ALL implementation work to delivery agents via Task.
- Doc/config files (`*.md`, `*.json`, `*.yaml`) are exceptions — you may edit them.
- Check current state: `mcp__stratus__get_workflow` with `workflow_id: "<slug>"`
