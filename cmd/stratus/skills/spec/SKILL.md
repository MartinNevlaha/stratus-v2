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

## MANDATORY EXECUTION PROTOCOL

You MUST follow the phases in strict order. Each phase has mandatory MCP tool calls that MUST be executed. Do NOT skip any step. Do NOT proceed to the next phase without completing all mandatory calls in the current phase.

---

## Phase 1: Plan

### STEP 1 — MANDATORY: Register Workflow

**This is the FIRST thing you MUST do. Do NOT delegate to any agent, do NOT read any files, do NOT do anything else until this is complete.**

Call `mcp__stratus__register_workflow` with:

```
id: "<kebab-slug>"          # lowercase, hyphenated, max 50 chars
type: "spec"
title: "<title from $ARGUMENTS>"
session_id: "${CLAUDE_SESSION_ID}"
complexity: "simple" | "complex"   # complex for multi-service, auth, database, cross-cutting
```

**DO NOT PROCEED until `mcp__stratus__register_workflow` succeeds and returns a workflow ID.**

### STEP 2 — Codebase Exploration

Delegate to the `Explore` agent via Task tool (`subagent_type: "Explore"`) with thoroughness `"very thorough"`. Pass the requirement from `$ARGUMENTS` and ask it to:
- Find all files, modules, and patterns relevant to the requirement
- Identify existing conventions, utilities, and abstractions that should be reused
- Map dependencies and integration points that the implementation will touch

Do NOT write code during exploration.

### STEP 3 — Governance Check

Delegate to `delivery-strategic-architect` or `delivery-system-architect` (Task tool) to review the requirement against project governance.

**MANDATORY:** Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-strategic-architect"
```

If findings require updates → share with user and adjust requirements before proceeding.

### STEP 4 — Task Planning

Delegate to the `Plan` subagent via Task tool (`subagent_type: "Plan"`). Pass full context:
- The requirement from `$ARGUMENTS`
- Key files, directories, and patterns discovered by the Explore agent
- Relevant architecture, patterns, and constraints
- Any governance findings

Use the Plan output to:
1. Write the plan to `docs/plans/<slug>.md`
2. Extract the ordered task list

Present the plan and task list to the user for approval via AskUserQuestion.

### STEP 5 — MANDATORY: Transition to Implement

**After user approval, you MUST call `mcp__stratus__transition_phase` BEFORE delegating any implementation tasks. DO NOT delegate to any engineer until this transition is complete.**

```
workflow_id: "<slug>"
phase: "implement"
```

**DO NOT PROCEED to Phase 2 until this transition succeeds.**

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

For each task (by index, starting at 0):
1. **MANDATORY:** Mark as started with `mcp__stratus__start_task`:

```
workflow_id: "<slug>"
task_index: 0  # zero-based index
```

2. Delegate via Task tool
3. **MANDATORY:** Record with `mcp__stratus__delegate_agent`
4. **MANDATORY:** Mark complete with `mcp__stratus__complete_task`:

```
workflow_id: "<slug>"
task_index: 0
```

### MANDATORY: Transition to Verify

**After ALL tasks are complete, you MUST call `mcp__stratus__transition_phase` BEFORE delegating to the code reviewer. DO NOT skip this step.**

```
workflow_id: "<slug>"
phase: "verify"
```

**DO NOT PROCEED to Phase 3 until this transition succeeds.**

---

## Phase 3: Verify

- Delegate to `delivery-code-reviewer` (Task tool) for spec compliance, code quality, and test adequacy.
- **MANDATORY:** Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-code-reviewer"
```

- If reviewer returns `[must_fix]` issues:
  1. **MANDATORY:** Transition back to implement: `mcp__stratus__transition_phase` → `phase: "implement"`
  2. Fix all `[must_fix]` issues by delegating to the appropriate engineer
  3. **MANDATORY:** Transition back to verify: `mcp__stratus__transition_phase` → `phase: "verify"`
  4. Re-delegate to code reviewer
- On PASS, **MANDATORY:** transition to learn:

```
workflow_id: "<slug>"
phase: "learn"
```

**DO NOT PROCEED to Phase 4 until this transition succeeds.**

---

## Phase 4: Learn

**Step 1 — MANDATORY: Save memory events** using `mcp__stratus__save_memory`:

```
text: "<key finding>"
type: "decision" | "discovery" | "feature"
tags: ["<relevant-tags>"]
importance: 0.8
```

**Step 2 — MANDATORY: Create learning candidates + proposals** for each significant pattern, rule, or decision:

Use Bash with curl to the local API (`http://localhost:{{STRATUS_PORT}}`):

```bash
# 2a. Save candidate
CANDIDATE_ID=$(curl -sS -X POST http://localhost:{{STRATUS_PORT}}/api/learning/candidates \
  -H 'Content-Type: application/json' \
  -d '{
    "detection_type": "pattern|decision|anti_pattern",
    "description": "Short description of what was found",
    "confidence": 0.85,
    "files": ["path/to/relevant/file.ts"],
    "count": 1
  }' | jq -r '.id')

# 2b. Generate proposal from candidate
curl -sS -X POST http://localhost:{{STRATUS_PORT}}/api/learning/proposals \
  -H 'Content-Type: application/json' \
  -d '{
    "candidate_id": "'$CANDIDATE_ID'",
    "type": "rule|adr|template|skill",
    "title": "Short proposal title",
    "description": "Why this matters",
    "proposed_content": "Full content of the rule/ADR/template",
    "proposed_path": ".claude/rules/<name>.md",
    "confidence": 0.85
  }'
```

Create a proposal for every insight worth preserving. The user will review proposals in the Learning tab. **Do not write governance files directly** — proposals are the gate.

**Step 3 — MANDATORY: Complete workflow** using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "complete"
```

---

## MCP Tools Reference

| Tool | Purpose |
|------|---------|
| `mcp__stratus__register_workflow` | Create new workflow (REQUIRED FIRST — call before anything else) |
| `mcp__stratus__transition_phase` | Move to next phase (REQUIRED at each phase boundary) |
| `mcp__stratus__delegate_agent` | Record agent delegation (REQUIRED for every delivery agent) |
| `mcp__stratus__start_task` | Mark task as in_progress (REQUIRED before delegating each task) |
| `mcp__stratus__complete_task` | Mark task as done (REQUIRED after each task completes) |
| `mcp__stratus__get_workflow` | Check current workflow state |
| `mcp__stratus__list_workflows` | See all active workflows |
| `mcp__stratus__save_memory` | Save findings for future reference |
| `mcp__stratus__retrieve` | Search code and governance docs |

---

## Rules

- **NEVER** use Write, Edit, or NotebookEdit on production source files directly.
- Delegate ALL implementation work to delivery agents via Task.
- Doc/config files (`*.md`, `*.json`, `*.yaml`) are exceptions — you may edit them.
- **ALWAYS** call `mcp__stratus__register_workflow` as the very first action.
- **ALWAYS** call `mcp__stratus__transition_phase` before starting each new phase.
- **ALWAYS** call `mcp__stratus__start_task` before delegating each task.
- **ALWAYS** call `mcp__stratus__complete_task` after each task completes successfully.
- **ALWAYS** call `mcp__stratus__delegate_agent` for every delivery agent delegation.
- Check current state: `mcp__stratus__get_workflow` with `workflow_id: "<slug>"`

## Workflow API Error Handling

If any workflow MCP tool call returns an error, you MUST resolve it before continuing. **NEVER rationalize away an API error as "a limitation" or "not important" and proceed anyway.**

- Error says "plan not defined" → write the plan to `docs/plans/<slug>.md` and set it via the API, then retry the transition
- Error says "tasks not defined" → create the task list and set it, then retry
- Any other error → read the message, fix the prerequisite, retry

**Proceeding after a failed transition is FORBIDDEN regardless of the reason.**
