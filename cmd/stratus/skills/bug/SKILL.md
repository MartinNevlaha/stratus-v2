---
name: bug
description: "Bug-fixing workflow coordinator (analyze→fix→review→complete). Orchestrates debugging and repair by delegating to specialized agents."
disable-model-invocation: true
---

# Bug-Fixing Workflow

You are the **coordinator** for a structured bug-fixing workflow. You orchestrate work by delegating to specialized agents via the Task tool. You do NOT write production code directly.

## API Base

```bash
BASE=http://127.0.0.1:41777
```

---

## Phase 1: Analyze

Start the bug workflow:

```bash
SLUG=$(echo "$ARGUMENTS" | tr '[:upper:] ' '[:lower:]-' | sed 's/[^a-z0-9-]//g' | cut -c1-50)
curl -sS -X POST $BASE/api/workflows \
  -H 'Content-Type: application/json' \
  -d "{\"id\": \"bug-$SLUG\", \"type\": \"bug\", \"title\": \"$ARGUMENTS\"}"
```

- Explore the codebase: Read error messages, stack traces, logs.
- Delegate to `delivery-debugger` (Task tool) for root cause analysis.
- Record delegation:

```bash
curl -sS -X POST $BASE/api/workflows/bug-<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-debugger"}'
```

- **Present diagnosis to user via AskUserQuestion. Get explicit approval before fixing.**
- On approval, transition to fix:

```bash
curl -sS -X PUT $BASE/api/workflows/bug-<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "fix"}'
```

---

## Phase 2: Fix

Route to the appropriate delivery agent:

| Bug Type | Agent |
|----------|-------|
| UI, components | `delivery-frontend-engineer` |
| API, backend | `delivery-backend-engineer` |
| Migrations, queries | `delivery-database-engineer` |
| CI/CD, infra | `delivery-devops-engineer` |
| General | `delivery-implementation-expert` |

Delegate via Task tool with diagnosis context, then record and transition:

```bash
curl -sS -X POST $BASE/api/workflows/bug-<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "<agent-name>"}'

curl -sS -X PUT $BASE/api/workflows/bug-<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "review"}'
```

---

## Phase 3: Review

- Delegate to `delivery-code-reviewer` (Task tool) — verify fix quality and no regressions.
- Record delegation.
- If reviewer finds issues → fix loop: transition back to fix, re-fix, re-review (max 5 loops).
- On pass, complete:

```bash
curl -sS -X PUT $BASE/api/workflows/bug-<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

- Summarize what was fixed. Save a memory event with key findings.

---

## Rules

- **NEVER** use Write, Edit, or NotebookEdit on production source files directly.
- Delegate ALL implementation work to delivery agents via Task.
- **ALWAYS get explicit user approval before Phase 2 (Fix).**
- Max 5 fix loops — escalate to user if still broken after 5 attempts.
- Check current state: `curl -sS $BASE/api/workflows/bug-<slug>`
