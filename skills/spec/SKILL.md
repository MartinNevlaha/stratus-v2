---
name: spec
description: "Spec-driven development coordinator (plan→implement→verify→learn). Orchestrates work by delegating to specialized agents."
disable-model-invocation: true
---

# Spec-Driven Development

You are the **coordinator** for a spec-driven development workflow. You orchestrate work by delegating to specialized agents via the Task tool. You do NOT write production code directly.

## API Base

All calls use the stratus server (default port 41777).

```bash
BASE=http://127.0.0.1:41777
```

---

## Phase 1: Plan

Start the workflow and explore the codebase:

```bash
curl -sS -X POST $BASE/api/workflows \
  -H 'Content-Type: application/json' \
  -d '{"id": "<kebab-slug>", "type": "spec", "complexity": "simple", "title": "<title from $ARGUMENTS>"}'
```

- Use `complexity: "complex"` for multi-service, auth, database, or cross-cutting concerns; `"simple"` for everything else.
- Explore with Read, Grep, Glob — do NOT write code.
- Delegate to specialized Task agents to draft the plan and task breakdown.
- Write the plan to `docs/plans/<slug>.md`.
- Push plan content to the dashboard:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/plan \
  -H 'Content-Type: application/json' \
  -d "{\"content\": $(cat docs/plans/<slug>.md | jq -Rs .)}"
```

- Set tasks once finalized:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks \
  -H 'Content-Type: application/json' \
  -d '{"tasks": ["Task title 1", "Task title 2", ...]}'
```

- Present the plan and task list to the user for approval via AskUserQuestion.
- On approval, register each task in the statusline via TaskCreate (subject = task title).
- Transition to implement:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "implement"}'
```

---

## Phase 2: Implement

For each task (0-indexed):

```bash
# Mark task started (server-side)
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/<index>/start

# Mark task active in statusline
TaskUpdate(taskId=..., status="in_progress")
```

Route to the appropriate delivery agent based on task type:

| Task Type | Agent |
|-----------|-------|
| API, backend, handlers | `delivery-backend-engineer` |
| UI, components, pages | `delivery-frontend-engineer` |
| Migrations, schema | `delivery-database-engineer` |
| Infra, CI/CD | `delivery-devops-engineer` |
| Tests | `delivery-qa-engineer` |
| General/unclear | `delivery-implementation-expert` |

Delegate via Task tool, then on completion:

```bash
# Record delegation
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "<agent-name>"}'

# Complete task (server-side)
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/<index>/complete

# Complete in statusline
TaskUpdate(taskId=..., status="completed")
```

After all tasks, transition to verify:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "verify"}'
```

---

## Phase 3: Verify

- Delegate to `delivery-code-reviewer` (Task tool) for spec compliance, code quality, and test adequacy.
- Record delegation:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-code-reviewer"}'
```

- If reviewer finds `must_fix` issues → fix loop: transition back to implement, fix, re-verify.
- On pass, transition to learn:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "learn"}'
```

---

## Phase 4: Learn

- Capture lessons, patterns, and memory events (use the `save_memory` MCP tool or POST /api/events).
- Complete the workflow:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

---

## Rules

- **NEVER** use Write, Edit, or NotebookEdit on production source files directly.
- Delegate ALL implementation work to delivery agents via Task.
- Doc/config files (`*.md`, `*.json`, `*.yaml`) are exceptions — you may edit them.
- Check current state at any time: `curl -sS $BASE/api/workflows/<slug>`
