---
name: spec-complex
description: "Complex spec-driven development coordinator (7-phase: discovery→design→governance→plan→implement→verify→learn). Use for auth, database, integrations, architecture, multi-service tasks."
disable-model-invocation: true
allowed-tools: mcp__stratus
---

# Spec-Driven Development (Complex)

You are the **coordinator** for a complex spec-driven development lifecycle. You orchestrate work by delegating to specialized agents. Delegated agents can run in the background: wait for each one's completion notification before acting on its result, and never `sleep` to wait. You don't write production code directly.

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

## Execution Protocol

Run the phases in order, and make every MCP call a phase lists before moving to the next one. The phase guards and the workflow record depend on these calls: a skipped call shows up as a blocked delegation or a gap in the audit trail.

Every Agent delegation includes a complete brief: workflow ID, phase, task index/title when applicable, goal, non-goals, relevant files, design document path, plan document path, expected output, and verification command. Include the exact workflow ID in the Agent prompt so guard hooks can disambiguate parallel workflows.

---

## Phase 1: Discovery

> 🎯 **Karpathy — Think Before Coding:** State assumptions explicitly, surface tradeoffs, push back on overcomplication, stop and ask when confused. See `.claude/rules/karpathy-principles.md`.

### Step 1 — Register Workflow

Do this first, before reading files or delegating: the guard hooks block delivery-agent delegations until the workflow exists.

Call `mcp__stratus__register_workflow` with:

```
id: "<kebab-slug>"          # lowercase, hyphenated, max 50 chars
type: "spec"
title: "<title from $ARGUMENTS>"
session_id: "${CLAUDE_SESSION_ID}"
complexity: "complex"
```

Continue once it returns a workflow ID.

### Step 2 — Transition to Discovery

Immediately after registration, call `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "discovery"
```

### Step 3 — Codebase Exploration

Delegate to the `Explore` agent via Agent tool (`subagent_type: "Explore"`) with thoroughness `"very thorough"`. Pass the requirement from `$ARGUMENTS` and ask it to:
- Find all files, modules, and patterns relevant to the requirement
- Identify existing conventions, utilities, and abstractions that should be reused
- Map dependencies and integration points that the implementation will touch
- Surface any architectural constraints or existing design decisions

Don't write code during exploration.

Additionally, call `mcp__stratus__retrieve` with the requirement keywords and `corpus` omitted (auto-routing) to surface any existing wiki knowledge pages about the project architecture, modules, and conventions. Note any results with `staleness_score > 0.7` as potentially outdated.

### Step 4 — Strategic Analysis

Delegate to `delivery-strategic-architect` (Agent tool) — requirements analysis, constraints, technology landscape.

Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-strategic-architect"
```

### Step 5 — Transition to Design

Call `mcp__stratus__transition_phase` before delegating any design agent:

```
workflow_id: "<slug>"
phase: "design"
```

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
- Record delegation for each agent used with `mcp__stratus__delegate_agent`.
- If findings require updates → address them before transitioning.

### Transition to Governance

After design documents are complete, call `mcp__stratus__transition_phase` before delegating the governance reviewer.

```
workflow_id: "<slug>"
phase: "governance"
```

---

## Phase 3: Governance

> 🎯 **Karpathy — Goal-Driven Execution:** Verify against the explicit success criteria, not style preferences. Loop until goals met; don't declare done prematurely. See `.claude/rules/karpathy-principles.md`.

Delegate to `delivery-code-reviewer` (Agent tool) to review design for governance compliance. Include `docs/plans/<slug>-design.md` and explicit governance criteria.

Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-code-reviewer"
```

If checker returns `[must_update]` findings → address them in the design doc before transitioning.

### Transition to Plan

After governance review passes, call `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "plan"
```

---

## Phase 4: Plan

> 🎯 **Karpathy — Think Before Coding:** State assumptions explicitly, surface tradeoffs, push back on overcomplication, stop and ask when confused. See `.claude/rules/karpathy-principles.md`.

Delegate to the `Plan` subagent via Agent tool (`subagent_type: "Plan"`). Pass full context:
- The design document from `docs/plans/<slug>-design.md`
- The original requirement from `$ARGUMENTS`
- Key files and architecture constraints surfaced during discovery and design phases

The Plan agent will return a concrete, ordered implementation plan with individual tasks and critical files.

Use the Plan output to:
1. Write the plan to `docs/plans/<slug>-plan.md`
2. Extract the ordered task list

Present plan, design doc, and task list to the user via AskUserQuestion.

### Transition to Implement

After user approval, call `mcp__stratus__transition_phase` before delegating any implementation tasks.

```
workflow_id: "<slug>"
phase: "implement"
```

**Autopilot (Claude Code, optional):** once the transition succeeds, offer the user a goal that keeps this workflow running to completion without a prompt per step. Print it exactly, with the workflow id filled in:

```
/goal Stratus workflow <slug> is complete: the latest mcp__stratus__get_workflow output for <slug> in this conversation shows phase "complete" and every task done, and the last code review shown has verdict PASS. Work through the remaining phases in order; if the coordinator instructions for this workflow are not in this conversation, read .claude/skills/spec-complex/SKILL.md and continue <slug> from its current phase as it describes. Do not skip phases or weaken tests to make them pass. When a step needs my decision, ask me with AskUserQuestion. Stop after 40 turns.
```

Tell the user it starts when they send it, runs unattended only in auto mode, and stops with `/goal clear`; the `/stratus` pane's Autopilot button fills in the same goal. Do not wait for an answer — continue with the next phase.

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
1. Mark as started with `mcp__stratus__start_task`:

```
workflow_id: "<slug>"
task_index: 0  # zero-based index
```

2. Delegate via Agent tool with full context from `docs/plans/<slug>-design.md` and `docs/plans/<slug>-plan.md`
3. Record with `mcp__stratus__delegate_agent`
4. Mark complete with `mcp__stratus__complete_task`:

```
workflow_id: "<slug>"
task_index: 0
```

### Transition to Verify

After all tasks are complete, call `mcp__stratus__transition_phase` before delegating to the code reviewer.

```
workflow_id: "<slug>"
phase: "verify"
```

---

## Phase 6: Verify

> 🎯 **Karpathy — Goal-Driven Execution:** Verify against the explicit success criteria, not style preferences. Loop until goals met; don't declare done prematurely. See `.claude/rules/karpathy-principles.md`.

Delegate to `delivery-code-reviewer` (Agent tool) — spec compliance, code quality, security, test adequacy. Include design doc, plan doc, completed tasks, and verification results.

Record delegation with `mcp__stratus__delegate_agent`:

```
workflow_id: "<slug>"
agent_id: "delivery-code-reviewer"
```

If reviewer returns `[must_fix]` issues:
1. Transition back to implement: `mcp__stratus__transition_phase` → `phase: "implement"`
2. Fix all `[must_fix]` issues
3. Transition back to verify: `mcp__stratus__transition_phase` → `phase: "verify"`
4. Re-delegate to code reviewer
(max 5 fix loops)

On PASS, transition to learn:

```
workflow_id: "<slug>"
phase: "learn"
```

---

## Phase 7: Learn

**Step 1 — Save memory events** using `mcp__stratus__save_memory`:

```
text: "<key finding>"
type: "decision" | "discovery"
tags: ["<relevant-tags>"]
importance: 0.8
```

**Step 2 — Automatic learn pipeline (runs on learn→complete transition):**

When you transition to complete, the coordinator runs (async, fail-open):
1. **Artifact build** — extracts engineering knowledge from this workflow (agents used, problem class, solution pattern, cycle time). Runs only if insight is enabled in config.
2. **Knowledge update** — updates problem statistics and mines solution patterns for future recommendations. Runs only if step 1 produced an artifact.
3. **Wiki autodoc** — generates a wiki summary page. Always runs when the wiki store is configured.

The coordinator records a `learn_pipeline` memory event with the per-step outcome (`ok` / `skipped` / `failed` / `disabled`) so it shows up in the workflow timeline. Pipeline timeout defaults to 180s and is configurable via `learn.pipeline_timeout_sec`.

You don't need to call these manually.

**Step 3 — Wiki enrichment (optional):**

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

This upserts by `(workflow_id, feature_slug)`. The subsequent auto-write will update the same row. Wiki write failures do not block the complete transition (fail-open).

**Step 4 — Complete workflow** using `mcp__stratus__transition_phase`:

```
workflow_id: "<slug>"
phase: "complete"
```

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
| `mcp__stratus__list_workflows` | See all active workflows |
| `mcp__stratus__save_memory` | Save findings for future reference |

---

## Rules

- Never use Write, Edit, or NotebookEdit on production source files directly.
- Delegate all implementation work to delivery agents via Agent tool.
- Doc/config files (`*.md`, `*.json`, `*.yaml`, `*.toml`) are exceptions — you may edit them.
- Call `mcp__stratus__register_workflow` as the very first action.
- Call `mcp__stratus__transition_phase` before starting each new phase.
- Call `mcp__stratus__start_task` before delegating each task.
- Call `mcp__stratus__complete_task` after each task completes successfully.
- Call `mcp__stratus__delegate_agent` for every delivery agent delegation.
- Always produce a design document before implementing — never skip Phase 2.
- Check current state: `mcp__stratus__get_workflow` with `workflow_id: "<slug>"`

## Workflow API Error Handling

If a workflow MCP tool call returns an error, resolve it before continuing. An API error is not "a limitation" to note and move past.

- Error says "plan not defined" → write the plan to `docs/plans/<slug>-plan.md` and set it via the API, then retry the transition
- Error says "tasks not defined" → create the task list and set it, then retry
- Any other error → read the message, fix the prerequisite, retry

Never proceed after a failed transition: the next phase's guards and the audit trail assume it succeeded.
