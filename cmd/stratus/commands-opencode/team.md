---
description: "Parallel spec workflow — plan all tasks, then implement them all at once for faster delivery. Requires stratus serve."
---

# Team-Based Spec Workflow

> **Experimental**: This workflow implements all tasks in a single pass rather than sequentially.

## API Base

```bash
BASE=http://localhost:41777
```

---

## Phase 1: Explore & Plan

Generate a short slug from `$ARGUMENTS` (kebab-case, max 40 chars).

Create the workflow — title **MUST** start with `[TEAM] `:

```bash
curl -sS -X POST $BASE/api/workflows \
  -H 'Content-Type: application/json' \
  -d '{"id": "<slug>", "type": "spec", "complexity": "simple", "title": "[TEAM] $ARGUMENTS"}'
```

Then:
1. Explore the codebase (read, glob, grep) to understand the relevant context.
2. Use `retrieve` MCP tool (corpus: code) for pattern discovery.
3. Use `retrieve` MCP tool (corpus: governance) for rules and ADRs.
4. Break the work into well-defined tasks with clear boundaries.
5. Set tasks:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks \
  -H 'Content-Type: application/json' \
  -d '{"tasks": ["Task 1", "Task 2", ...]}'
```

Present the task list to the user using the `question` tool. **Wait for explicit approval.**

Transition to implement:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "implement"}'
```

---

## Phase 2: Implement — ALL tasks

Unlike `/spec` (which implements tasks one by one), `/team` implements all tasks in one pass:

1. Mark all tasks as started:

```bash
for i in $(seq 0 $((NUM_TASKS-1))); do
  curl -sS -X POST $BASE/api/workflows/<slug>/tasks/$i/start
done
```

2. Implement all tasks following TDD and project conventions.

3. Mark all tasks as complete:

```bash
for i in $(seq 0 $((NUM_TASKS-1))); do
  curl -sS -X POST $BASE/api/workflows/<slug>/tasks/$i/complete
done
```

Transition to verify:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "verify"}'
```

---

## Phase 3: Verify

Review all changes for correctness, security, and governance compliance.

Run all tests. If `[must_fix]` issues → back to implement, fix, re-verify.

On pass:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "learn"}'
```

---

## Phase 4: Learn & Complete

Save memory events for key decisions:

```bash
save_memory(text="<decision>", type="decision", tags=[...], importance=0.7)
```

Complete:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

Summarize what was implemented.

---

## Rules

- Follow TDD for all implementation.
- Always get user approval after the plan phase.
- The `[TEAM]` title prefix is mandatory — it's how the Teams dashboard tab identifies these workflows.
- Check current state: `curl -sS $BASE/api/workflows/<slug>`

Implement the team spec for: $ARGUMENTS
