---
name: spec-complex
description: "Complex spec-driven development coordinator (7-phase: discovery→design→governance→plan→implement→verify→learn). Use for auth, database, integrations, architecture, multi-service tasks."
disable-model-invocation: true
---

# Spec-Driven Development (Complex)

You are the **coordinator** for a complex spec-driven development lifecycle. You orchestrate work by delegating to specialized agents. You do NOT write production code directly.

## When to Use

Use `/spec-complex` for:
- Authentication, authorization, security changes
- Database migrations, schema design
- New API surface with business logic
- Third-party integrations, webhooks
- Infrastructure, CI/CD changes
- Architecture decisions requiring ADRs
- Multi-file, multi-service, or cross-cutting concerns
- Unclear or evolving requirements that need discovery first

For simple, well-understood tasks use `/spec`.

## Prerequisites

Stratus server must be running: `stratus serve`

---

## Phase 1: Discovery

### 1a. Register Workflow

First, register the workflow using `mcp__stratus__register_workflow`:

```
id: "<kebab-slug>"          # lowercase, hyphenated, max 50 chars
type: "spec"
title: "<title from $ARGUMENTS>"
session_id: "${CLAUDE_SESSION_ID}"
complexity: "complex"
```

### 1b. Transition to Discovery

Use `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "discovery"
```

### 1c. Codebase Exploration

Delegate to the `Explore` agent via Task tool (`subagent_type: "explore"`) with thoroughness `"very thorough"`. Pass the requirement from `$ARGUMENTS` and ask it to:
- Find all files, modules, and patterns relevant to the requirement
- Identify existing conventions, utilities, and abstractions that should be reused
- Map dependencies and integration points that the implementation will touch
- Surface any architectural constraints or existing design decisions

Do NOT write code during exploration.

### 1d. Strategic Analysis

- Delegate to `delivery-strategic-architect` (Task tool) — requirements analysis, constraints, technology landscape.
- Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-strategic-architect"
```

- Transition to design using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "design"
```

---

## Phase 2: Design

Delegate based on what the spec requires:

| Design Need | Agent |
|-------------|-------|
| System architecture, ADRs, tech selection | `delivery-strategic-architect` |
| Component design, API contracts, data models | `delivery-system-architect` |
| UI/UX design, component hierarchy, design tokens | `delivery-ux-designer` |

Typically: delegate to `delivery-system-architect` (always), + `delivery-strategic-architect` for technology decisions, + `delivery-ux-designer` for UI-heavy specs.

- Produce a Technical Design Document at `docs/plans/<slug>-design.md`.
- Record delegation for each agent used with `mcp__stratus__delegate_agent`.
- If findings require updates → address them before transitioning.
- Transition to governance using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "governance"
```

---

## Phase 3: Governance

- Delegate to `delivery-code-reviewer` (Task tool) to review design for governance compliance.
- Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-code-reviewer"
```

- If checker returns `[must_update]` findings → address them in the design doc before transitioning.
- Transition to plan using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "plan"
```

---

## Phase 4: Plan

Delegate to the `Plan` subagent via Task tool (`subagent_type: "plan"`). Pass full context:
- The design document from `docs/plans/<slug>-design.md`
- The original requirement from `$ARGUMENTS`
- Key files and architecture constraints surfaced during discovery and design phases

The Plan agent will return a concrete, ordered implementation plan with individual tasks and critical files.

Use the Plan output to:
1. Write the plan to `docs/plans/<slug>.md`
2. Extract the ordered task list

Present plan, design doc, and task list to the user via AskUserQuestion.
On approval, transition to implement using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "implement"
```

---

## Phase 5: Implement

Route tasks to appropriate delivery agents (same routing table as /spec — backend/frontend/ux/database/infra/mobile/architecture/tests/general).

For each task:
1. Delegate via Task tool with full context from the design doc
2. Record with `mcp__stratus__delegate_agent`

After all tasks, transition to verify using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "verify"
```

---

## Phase 6: Verify

- Delegate to `delivery-code-reviewer` (Task tool) — spec compliance, code quality, security, test adequacy.
- Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-code-reviewer"
```

- If reviewer returns `[must_fix]` issues → fix loop: transition back to implement, fix, re-verify (max 5 loops).
- On pass, transition to learn using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "learn"
```

---

## Phase 7: Learn

**Step 1 — Save memory events:** Use `mcp__stratus__save_memory`:

```
text: "<key finding>"
type: "decision" | "discovery"
tags: ["<relevant-tags>"]
importance: 0.8
```

**Step 2 — Write governance artifacts** (rules to `.claude/rules/`, ADRs to `docs/decisions/`). Only write for insights worth preserving long-term.

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

---

## Rules

- **NEVER** use Write, Edit, or NotebookEdit on production source files directly.
- Delegate ALL implementation work to delivery agents via Task.
- Doc/config files (`*.md`, `*.json`, `*.yaml`, `*.toml`) are exceptions — you may edit them.
- Always produce a design document before implementing — never skip Phase 2.
- Check current state: `mcp__stratus__get_workflow` with `workflow_id: "<slug>"`
