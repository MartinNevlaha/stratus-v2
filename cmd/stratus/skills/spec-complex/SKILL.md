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

## MANDATORY EXECUTION PROTOCOL

You MUST follow the phases in strict order. Each phase has mandatory MCP tool calls that MUST be executed. Do NOT skip any step. Do NOT proceed to the next phase without completing all mandatory calls in the current phase.

---

## Phase 1: Discovery

> 🎯 **Karpathy — Think Before Coding:** State assumptions explicitly, surface tradeoffs, push back on overcomplication, stop and ask when confused. See `.claude/rules/karpathy-principles.md`.

### STEP 1 — MANDATORY: Register Workflow

**This is the FIRST thing you MUST do. Do NOT delegate to any agent, do NOT read any files, do NOT do anything else until this is complete.**

Call `mcp__stratus__register_workflow` with:

```
id: "<kebab-slug>"          # lowercase, hyphenated, max 50 chars
type: "spec"
title: "<title from $ARGUMENTS>"
session_id: "${CLAUDE_SESSION_ID}"
complexity: "complex"
```

**DO NOT PROCEED until `mcp__stratus__register_workflow` succeeds and returns a workflow ID.**

### STEP 2 — MANDATORY: Transition to Discovery

**Immediately after registration, you MUST call `mcp__stratus__transition_phase`:**

```
workflow_id: "<slug>"
phase: "discovery"
```

### STEP 3 — Codebase Exploration

Delegate to the `Explore` agent via Task tool (`subagent_type: "Explore"`) with thoroughness `"very thorough"`. Pass the requirement from `$ARGUMENTS` and ask it to:
- Find all files, modules, and patterns relevant to the requirement
- Identify existing conventions, utilities, and abstractions that should be reused
- Map dependencies and integration points that the implementation will touch
- Surface any architectural constraints or existing design decisions

Do NOT write code during exploration.

Additionally, call `mcp__stratus__retrieve` with the requirement keywords and `corpus` omitted (auto-routing) to surface any existing wiki knowledge pages about the project architecture, modules, and conventions. Note any results with `staleness_score > 0.7` as potentially outdated.

### STEP 4 — Strategic Analysis

Delegate to `delivery-strategic-architect` (Task tool) — requirements analysis, constraints, technology landscape.

**MANDATORY:** Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-strategic-architect"
```

### STEP 5 — MANDATORY: Transition to Design

**You MUST call `mcp__stratus__transition_phase` before starting design work. DO NOT delegate any design agent until this is done.**

```
workflow_id: "<slug>"
phase: "design"
```

**DO NOT PROCEED to Phase 2 until this transition succeeds.**

---

## Phase 2: Design

> 🎯 **Karpathy — Think Before Coding:** State assumptions explicitly, surface tradeoffs, push back on overcomplication, stop and ask when confused. See `.claude/rules/karpathy-principles.md`.

Delegate based on what the spec requires:

| Design Need | Agent |
|-------------|-------|
| System architecture, ADRs, tech selection | `delivery-strategic-architect` |
| Component design, API contracts, data models | `delivery-system-architect` |
| UI/UX design, component hierarchy, design tokens | `delivery-ux-designer` |

Typically: delegate to `delivery-system-architect` (always), + `delivery-strategic-architect` for technology decisions, + `delivery-ux-designer` for UI-heavy specs.

- Produce a Technical Design Document at `docs/plans/<slug>-design.md`.
- **MANDATORY:** Record delegation for each agent used with `mcp__stratus__delegate_agent`.
- If findings require updates → address them before transitioning.

### MANDATORY: Transition to Governance

**After design documents are complete, you MUST call `mcp__stratus__transition_phase` before delegating the governance reviewer. DO NOT skip this step.**

```
workflow_id: "<slug>"
phase: "governance"
```

**DO NOT PROCEED to Phase 3 until this transition succeeds.**

---

## Phase 3: Governance

> 🎯 **Karpathy — Goal-Driven Execution:** Verify against the explicit success criteria, not style preferences. Loop until goals met; don't declare done prematurely. See `.claude/rules/karpathy-principles.md`.

Delegate to `delivery-code-reviewer` (Task tool) to review design for governance compliance.

**MANDATORY:** Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-code-reviewer"
```

If checker returns `[must_update]` findings → address them in the design doc before transitioning.

### MANDATORY: Transition to Plan

**After governance review passes, you MUST call `mcp__stratus__transition_phase`. DO NOT skip this step.**

```
workflow_id: "<slug>"
phase: "Plan"
```

**DO NOT PROCEED to Phase 4 until this transition succeeds.**

---

## Phase 4: Plan

> 🎯 **Karpathy — Think Before Coding:** State assumptions explicitly, surface tradeoffs, push back on overcomplication, stop and ask when confused. See `.claude/rules/karpathy-principles.md`.

Delegate to the `Plan` subagent via Task tool (`subagent_type: "Plan"`). Pass full context:
- The design document from `docs/plans/<slug>-design.md`
- The original requirement from `$ARGUMENTS`
- Key files and architecture constraints surfaced during discovery and design phases

The Plan agent will return a concrete, ordered implementation plan with individual tasks and critical files.

Use the Plan output to:
1. Write the plan to `docs/plans/<slug>.md`
2. Extract the ordered task list

Present plan, design doc, and task list to the user via AskUserQuestion.

### MANDATORY: Transition to Implement

**After user approval, you MUST call `mcp__stratus__transition_phase` BEFORE delegating any implementation tasks. DO NOT delegate to any engineer until this transition is complete.**

```
workflow_id: "<slug>"
phase: "implement"
```

**DO NOT PROCEED to Phase 5 until this transition succeeds.**

---

## Phase 5: Implement

> 🎯 **Karpathy — Simplicity First + Surgical Changes:** Minimum code that solves the problem. Touch only what the task requires. No speculative abstractions, no "improvements" to adjacent code. See `.claude/rules/karpathy-principles.md`.

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

2. Delegate via Task tool with full context from the design doc
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

**DO NOT PROCEED to Phase 6 until this transition succeeds.**

---

## Phase 6: Verify

> 🎯 **Karpathy — Goal-Driven Execution:** Verify against the explicit success criteria, not style preferences. Loop until goals met; don't declare done prematurely. See `.claude/rules/karpathy-principles.md`.

Delegate to `delivery-code-reviewer` (Task tool) — spec compliance, code quality, security, test adequacy.

**MANDATORY:** Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-code-reviewer"
```

If reviewer returns `[must_fix]` issues:
1. **MANDATORY:** Transition back to implement: `mcp__stratus__transition_phase` → `phase: "implement"`
2. Fix all `[must_fix]` issues
3. **MANDATORY:** Transition back to verify: `mcp__stratus__transition_phase` → `phase: "verify"`
4. Re-delegate to code reviewer
(max 5 fix loops)

On PASS, **MANDATORY:** transition to learn:

```
workflow_id: "<slug>"
phase: "learn"
```

**DO NOT PROCEED to Phase 7 until this transition succeeds.**

---

## Phase 7: Learn

**Step 1 — MANDATORY: Save memory events** using `mcp__stratus__save_memory`:

```
text: "<key finding>"
type: "decision" | "discovery"
tags: ["<relevant-tags>"]
importance: 0.8
```

**Step 2 — MANDATORY: Create learning candidates + proposals** for each significant pattern, rule, or decision:

Use Bash with curl to the local API (`http://localhost:$(stratus port)`):

```bash
# 2a. Save candidate
CANDIDATE_ID=$(curl -sS -X POST http://localhost:$(stratus port)/api/learning/candidates \
  -H 'Content-Type: application/json' \
  -d '{
    "detection_type": "pattern|decision|anti_pattern",
    "description": "Short description of what was found",
    "confidence": 0.85,
    "files": ["path/to/relevant/file.ts"],
    "count": 1
  }' | jq -r '.id')

# 2b. Generate proposal from candidate
curl -sS -X POST http://localhost:$(stratus port)/api/learning/proposals \
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

**Step 3 — Wiki auto-doc (optional enrichment):**

On the `learn → complete` transition below, the coordinator automatically writes a wiki page for this workflow (status=`auto-generated`, upsert by `(workflow_id, feature_slug)`). The auto-generated content is a minimal summary of plan + tasks + delegations.

If you want richer wiki content (architecture notes, diagrams, usage examples), POST directly before transitioning:

```bash
curl -sS -X POST http://localhost:$(stratus port)/api/wiki/pages \
  -H 'Content-Type: application/json' \
  -d '{
    "workflow_id": "<slug>",
    "feature_slug": "<kebab-feature-name>",
    "title": "<human title>",
    "content": "<markdown body — architecture, API shape, examples>",
    "tags": ["feature", "<area>"],
    "confidence": 0.9,
    "source_files": ["path/to/file1.go", "path/to/file2.ts"]
  }'
```

This upserts by `(workflow_id, feature_slug)`. The subsequent auto-write will update the same row. Wiki write failures MUST NOT block the complete transition (fail-open).

**Step 4 — MANDATORY: Complete workflow** using `mcp__stratus__transition_phase`:

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

---

## Rules

- **NEVER** use Write, Edit, or NotebookEdit on production source files directly.
- Delegate ALL implementation work to delivery agents via Task.
- Doc/config files (`*.md`, `*.json`, `*.yaml`, `*.toml`) are exceptions — you may edit them.
- **ALWAYS** call `mcp__stratus__register_workflow` as the very first action.
- **ALWAYS** call `mcp__stratus__transition_phase` before starting each new phase.
- **ALWAYS** call `mcp__stratus__start_task` before delegating each task.
- **ALWAYS** call `mcp__stratus__complete_task` after each task completes successfully.
- **ALWAYS** call `mcp__stratus__delegate_agent` for every delivery agent delegation.
- Always produce a design document before implementing — never skip Phase 2.
- Check current state: `mcp__stratus__get_workflow` with `workflow_id: "<slug>"`

## Workflow API Error Handling

If any workflow MCP tool call returns an error, you MUST resolve it before continuing. **NEVER rationalize away an API error as "a limitation" or "not important" and proceed anyway.**

- Error says "plan not defined" → write the plan to `docs/plans/<slug>.md` and set it via the API, then retry the transition
- Error says "tasks not defined" → create the task list and set it, then retry
- Any other error → read the message, fix the prerequisite, retry

**Proceeding after a failed transition is FORBIDDEN regardless of the reason.**
