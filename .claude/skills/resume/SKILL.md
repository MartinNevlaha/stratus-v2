---
name: resume
description: "Resume an interrupted spec or bug workflow from where it left off. Use when the user asks to continue, resume, or pick up a paused workflow."
disable-model-invocation: true
argument-hint: "[workflow-id]"
---

# Resume Interrupted Workflow

Current session ID: ${CLAUDE_SESSION_ID}

```bash
BASE=http://localhost:$(stratus port)
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

All phase transitions use: `curl -sS -X PUT $BASE/api/workflows/<id>/phase -H 'Content-Type: application/json' -d '{"phase": "<next>"}'`

### spec — `plan`
Present task list, confirm with user. Transition to `implement`, then continue with implement below.

### spec — `discovery` (complex)
Delegate to `delivery-strategic-architect` (Task tool) — requirements analysis. Record delegation. Transition to `design`.

### spec — `design` (complex)
Delegate to `delivery-system-architect` (Task tool) — component design, API contracts. Produce/update `docs/plans/<id>-design.md`. Record delegation. Transition to `plan`.

### spec — `implement`
Find first `pending`/`in_progress` task. Start it: `POST $BASE/api/workflows/<id>/tasks/<index>/start`. Route to delivery agent (same routing as /spec). Delegate, record, complete, repeat until all done. Transition to `verify`.

### spec — `verify`
Delegate to `delivery-code-reviewer`. If `[must_fix]` → back to `implement`. On pass → `learn`.

### spec — `learn`
Save memory events via `mcp__stratus__save_memory`. Transition to `complete` — the coordinator runs the learn pipeline (artifact build, knowledge update, wiki autodoc) automatically.

### bug — `analyze`
Delegate to `delivery-debugger`. Present diagnosis, get approval. Transition to `fix`.

### bug — `fix`
Delegate to appropriate agent. Record delegation. Transition to `review`.

### bug — `review`
Delegate to `delivery-code-reviewer`. If issues → back to `fix` (max 5 loops). On pass → `complete`.

### `complete` or `aborted`
Inform user: "This workflow is already **<phase>** — nothing to resume."

---

## Constraints

- **NEVER** use Write, Edit, or NotebookEdit on production source files directly.
- Delegate ALL implementation work to delivery agents via Task tool.
- Follow the same agent routing and governance rules as `/spec` and `/bug`.
