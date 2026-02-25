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
- Delegate to `delivery-governance-checker` (Task tool) with prompt: "Review plan at docs/plans/<slug>.md for governance compliance."
- Record delegation:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-governance-checker"}'
```

- If checker returns `[must_update]` findings → update the plan accordingly before proceeding.
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
| UI/UX design, design system | `delivery-ux-designer` |
| Migrations, schema | `delivery-database-engineer` |
| Infra, CI/CD | `delivery-devops-engineer` |
| Mobile, React Native, iOS/Android | `delivery-mobile-engineer` |
| Architecture, system design, ADRs | `delivery-system-architect` |
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

- Also delegate to `delivery-governance-checker` (Task tool) with prompt: "Review implementation for governance compliance."
- Record delegation:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-governance-checker"}'
```

- If **either** reviewer returns `[must_fix]` issues → fix loop: transition back to implement, fix, re-verify.
- On pass from both, transition to learn:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "learn"}'
```

---

## Phase 4: Learn

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

Only write files for insights worth preserving long-term. Skip if nothing warrants permanent governance.

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
- Doc/config files (`*.md`, `*.json`, `*.yaml`) are exceptions — you may edit them.
- Check current state at any time: `curl -sS $BASE/api/workflows/<slug>`
