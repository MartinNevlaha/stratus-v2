---
name: resume
description: "Resume an interrupted spec or bug workflow from where it left off. Use when the user asks to continue, resume, or pick up a paused workflow."
disable-model-invocation: true
argument-hint: "[workflow-id]"
---

# Resume Interrupted Workflow

Current session ID: ${CLAUDE_SESSION_ID}

```bash
BASE=http://localhost:41777
```

## Step 1 — Identify the workflow

Workflow ID argument: `$ARGUMENTS`

**If no argument was given**, fetch all active workflows and present a numbered list:

```bash
curl -sS $BASE/api/workflows | jq '[.[] | select(.phase != "complete" and .aborted == false) | {id, type, phase, title, updated_at}]'
```

Show the list to the user and ask: "Which workflow do you want to resume? Enter the ID or number."
Wait for their answer before continuing.

**If an argument was given**, use it directly as the workflow ID.

## Step 2 — Load current state

```bash
curl -sS $BASE/api/workflows/<id>/dispatch
```

Display a clear status summary to the user:
```
Workflow: <id> (<type>)
Phase:    <phase>
Tasks:    <done> done / <total> total
Active:   <current task title, or "none">
```

Ask: "Resume from here? (yes/no)"
If no → stop.

## Step 3 — Update session ID

Record this session as the new owner of the workflow:

```bash
curl -sS -X PATCH $BASE/api/workflows/<id>/session \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\": \"${CLAUDE_SESSION_ID}\"}"
```

## Step 4 — Continue from current phase

Based on `type` and `phase` from the dispatch response:

---

### spec — `plan` phase
Tasks exist but implementation hasn't started. Present the task list to the user and confirm.
Transition to implement:
```bash
curl -sS -X PUT $BASE/api/workflows/<id>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "implement"}'
```
Then continue with the implement flow below.

---

### spec — `discovery` phase (complex workflow)
Delegate to `delivery-strategic-architect` (Task tool) — requirements analysis, constraints, technology landscape.
Record delegation. Transition to design:
```bash
curl -sS -X PUT $BASE/api/workflows/<id>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "design"}'
```
Then continue with the design phase below.

---

### spec — `design` phase (complex workflow)
Delegate to `delivery-system-architect` (Task tool) — component design, API contracts, data models.
Produce / update a Technical Design Document at `docs/plans/<id>-design.md`.
Record delegation. Transition to plan:
```bash
curl -sS -X PUT $BASE/api/workflows/<id>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "plan"}'
```
Then continue with the plan phase below.

---

### spec — `implement` phase
Find the first task with status `"pending"` or `"in_progress"`. That is the next task to work on.

Mark it started:
```bash
curl -sS -X POST $BASE/api/workflows/<id>/tasks/<index>/start
```

Route to the correct delivery agent based on task title (same routing as `/spec`):
- Backend, API, handlers → `delivery-backend-engineer`
- Frontend, UI, components → `delivery-frontend-engineer`
- Database, migrations, schema → `delivery-database-engineer`
- Infrastructure, CI/CD → `delivery-devops-engineer`
- Architecture, ADRs → `delivery-system-architect`
- Testing, coverage → `delivery-qa-engineer`
- Unknown / mixed → `delivery-implementation-expert`

Delegate via Task tool, record delegation, complete task, move to next pending task.
Continue until all tasks are done, then transition to verify.

---

### spec — `verify` phase
Delegate to `delivery-code-reviewer` and `delivery-governance-checker` (Task tool).
Record each delegation. If `[must_fix]` issues found → transition back to implement, fix, re-verify.
On pass → transition to learn.

---

### spec — `learn` phase
Save memory events for key decisions made during the workflow.
Create learning candidates via `POST $BASE/api/learning/candidates`.
Transition to complete.

---

### bug — `analyze` phase
Delegate to `delivery-debugger` (Task tool) for root cause analysis.
Record delegation. Present diagnosis to user. Get explicit approval before fixing.
On approval → transition to fix.

---

### bug — `fix` phase
Delegate to the appropriate delivery agent based on bug type.
Record delegation. Transition to review.

---

### bug — `review` phase
Delegate to `delivery-code-reviewer` (Task tool).
Record delegation. If issues → transition back to fix (max 5 loops).
On pass → transition to complete.

---

### `complete` or `aborted`
Inform the user: "This workflow is already **<phase>** — there's nothing to resume."

---

## Constraints

- **NEVER** use Write, Edit, or NotebookEdit on production source files directly.
- Delegate ALL implementation work to delivery agents via Task tool.
- Follow the same agent routing and governance rules as `/spec` and `/bug`.
