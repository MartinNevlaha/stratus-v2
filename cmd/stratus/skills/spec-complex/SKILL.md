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
  -d '{"id": "<kebab-slug>", "type": "spec", "complexity": "complex", "title": "<title from $ARGUMENTS>"}'
```

- Explore the codebase with Read, Grep, Glob — do NOT write code.
- Delegate to `delivery-strategic-architect` (Task tool) — requirements analysis, constraints, technology landscape.
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
- Record delegation for each agent used.
- Transition to plan:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "plan"}'
```

---

## Phase 3: Plan

- Based on the design, break work into concrete tasks.
- Delegate to `delivery-system-architect` (Task tool) if task estimates need design input.
- Write the plan to `docs/plans/<slug>.md`.
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

Route to the appropriate delivery agent:

| Task Type | Agent |
|-----------|-------|
| API, backend, handlers | `delivery-backend-engineer` |
| UI, components, pages | `delivery-frontend-engineer` |
| UI/UX design, design system | `delivery-ux-designer` |
| Migrations, schema | `delivery-database-engineer` |
| Infra, CI/CD | `delivery-devops-engineer` |
| Mobile, React Native, iOS/Android | `delivery-mobile-engineer` |
| Architecture docs, ADRs | `delivery-system-architect` |
| Tests | `delivery-qa-engineer` |
| General/unclear | `delivery-implementation-expert` |

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
- Record delegation.
- If reviewer finds `must_fix` issues → fix loop: transition back to implement, fix, re-verify (max 5 loops).
- On pass, transition to learn:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "learn"}'
```

---

## Phase 6: Learn

**Step 1 — Save memory events** (session discoveries, decisions):

```bash
# Via MCP tool (preferred)
save_memory(text="...", type="decision|discovery|bugfix", tags=[...], importance=0.8)

# Or direct API
curl -sS -X POST $BASE/api/events \
  -H 'Content-Type: application/json' \
  -d '{"text": "...", "type": "decision", "title": "...", "tags": ["..."]}'
```

**Step 2 — Write governance artifacts** (permanent, retrievable by future agents):

| Artifact type | Write to |
|--------------|----------|
| New coding rule | `.claude/rules/<name>.md` |
| Decision / ADR | `docs/decisions/<slug>-adr.md` |
| Architecture note | `docs/architecture/<slug>.md` |

Only write files for insights worth preserving long-term.

**Step 2b — Register pattern candidates** (for patterns needing human review before becoming rules):

```bash
curl -sS -X POST $BASE/api/learning/candidates \
  -H 'Content-Type: application/json' \
  -d '{
    "detection_type": "pattern|antipattern|convention",
    "description": "...",
    "confidence": 0.8,
    "files": ["path/to/relevant/file"]
  }'
```

**Step 3 — Re-index governance** (only if you wrote files in Step 2):

```bash
curl -sS -X POST $BASE/api/retrieve/index
```

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
