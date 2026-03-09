---
name: spec-complex
description: "Complex spec-driven development coordinator (6-phase: discovery→design→plan→implement→verify→learn). Use for auth, database, integrations, architecture, multi-service tasks."
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

## API Base

```bash
BASE=http://127.0.0.1:41777
```

---

## Phase 1: Discovery

Start the workflow:

```bash
curl -sS -X POST $BASE/api/workflows \
  -H 'Content-Type: application/json' \
  -d '{"id": "<kebab-slug>", "type": "spec", "complexity": "complex", "title": "<title from $ARGUMENTS>", "session_id": "${CLAUDE_SESSION_ID}"}'
```

- Transition to discovery first:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "discovery"}'
```

**Codebase exploration — use the built-in Explore agent:**

Delegate to the `Explore` agent via Agent tool (`subagent_type: "Explore"`) with thoroughness `"very thorough"`. Pass the requirement from `$ARGUMENTS` and ask it to:
- Find all files, modules, and patterns relevant to the requirement
- Identify existing conventions, utilities, and abstractions that should be reused
- Map dependencies and integration points that the implementation will touch
- Surface any architectural constraints or existing design decisions

Do NOT write code during exploration.

- Delegate to `delivery-strategic-architect` (Task tool) — requirements analysis, constraints, technology landscape. Pass the Explore agent's findings as context.
- Record delegation:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-strategic-architect"}'
```

- Transition to design:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "design"}'
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
- Push design content to the dashboard:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/design \
  -H 'Content-Type: application/json' \
  -d "{\"content\": $(cat docs/plans/<slug>-design.md | jq -Rs .)}"
```

- Record delegation for each agent used.
- Delegate to `delivery-governance-checker` (Task tool) with prompt: "Review design document at docs/plans/<slug>-design.md for governance compliance."
- Record delegation:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-governance-checker"}'
```

- If checker returns `[must_update]` findings → address them in the design doc before transitioning to plan.
- Transition to plan:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "plan"}'
```

---

## Phase 3: Plan

**Task planning — use the built-in Plan subagent:**

Delegate to the `Plan` subagent via Task tool (`subagent_type: "Plan"`). Pass full context:
- The design document from `docs/plans/<slug>-design.md`
- The original requirement from `$ARGUMENTS`
- Key files and architecture constraints surfaced during discovery and design phases

The Plan agent will return a concrete, ordered implementation plan with individual tasks and critical files.

Use the Plan output to:
1. Write the plan to `docs/plans/<slug>.md`
2. Push plan content to the dashboard:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/plan \
  -H 'Content-Type: application/json' \
  -d "{\"content\": $(cat docs/plans/<slug>.md | jq -Rs .)}"
```

3. Extract the ordered task list

- Set tasks:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks \
  -H 'Content-Type: application/json' \
  -d '{"tasks": ["Task title 1", "Task title 2", ...]}'
```

- Present plan, design doc, and task list to the user via AskUserQuestion.
- On approval, register each task via TaskCreate (subject = task title).
- Transition to implement:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "implement"}'
```

---

## Phase 4: Implement

For each task (0-indexed):

```bash
# Mark task started
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/<index>/start
TaskUpdate(taskId=..., status="in_progress")
```

Route to the appropriate delivery agent (same routing table as /spec — backend/frontend/ux/database/infra/mobile/architecture/tests/general).

Delegate via Task tool with full context from the design doc, then on completion:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "<agent-name>"}'

curl -sS -X POST $BASE/api/workflows/<slug>/tasks/<index>/complete
TaskUpdate(taskId=..., status="completed")
```

After all tasks, transition to verify:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "verify"}'
```

---

## Phase 5: Verify

- Delegate to `delivery-code-reviewer` (Task tool) — spec compliance, code quality, security, test adequacy.
- Record delegation:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-code-reviewer"}'
```

- If reviewer returns `[must_fix]` issues → fix loop: transition back to implement, fix, re-verify (max 5 loops).
- On pass, transition to learn:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "learn"}'
```

---

## Phase 6: Learn

**Step 1 — Save memory events:** `save_memory(text="...", type="decision|discovery|bugfix", tags=[...], importance=0.8)`

**Step 2 — Write governance artifacts** (rules to `.claude/rules/`, ADRs to `docs/decisions/`, architecture to `docs/architecture/`). Only write for insights worth preserving long-term.

**Step 2b — Register pattern candidates** (for human review):

```bash
curl -sS -X POST $BASE/api/learning/candidates \
  -H 'Content-Type: application/json' \
  -d '{"detection_type": "pattern|antipattern|convention", "description": "...", "confidence": 0.8, "files": ["..."]}'
```

**Step 3 — Re-index governance** (if files written): `curl -sS -X POST $BASE/api/retrieve/index`

**Step 4 — Complete workflow**:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

---

## Rules

- **NEVER** use Write, Edit, or NotebookEdit on production source files directly.
- Delegate ALL implementation work to delivery agents via Task.
- Doc/config files (`*.md`, `*.json`, `*.yaml`, `*.toml`) are exceptions — you may edit them.
- Always produce a design document before implementing — never skip Phase 2.
- Check current state at any time: `curl -sS $BASE/api/workflows/<slug>`
