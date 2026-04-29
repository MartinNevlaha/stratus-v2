---
name: learn
description: "Trigger insight pattern analysis and proposal generation; review pending proposals in the dashboard."
disable-model-invocation: true
---

# Learning Workflow

You trigger the Stratus insight engine to scan the codebase for patterns, generate proposals (rules, ADRs, templates, skills), and surface them for user review.

The insight engine runs server-side. You do NOT generate candidates or proposals manually via curl — the engine does that work and persists results in the database.

## API Base

```bash
BASE=http://127.0.0.1:$(stratus port)
```

---

## Steps

### 1. Trigger insight analysis

Scans codebase, detects patterns, anti-patterns, and inconsistencies. Async — returns immediately.

```bash
curl -sS -X POST $BASE/api/insight/trigger
```

### 2. Trigger proposal generation

Promotes high-confidence patterns into proposals. Async.

```bash
curl -sS -X POST $BASE/api/insight/proposals/generate
```

### 3. List pending proposals

```bash
curl -sS "$BASE/api/insight/proposals?status=pending"
```

Show the user a short summary (count, types, top titles).

### 4. Direct user to the dashboard

Tell the user proposals are ready in the **Learning tab** of the dashboard. There they can accept, reject, or snooze each one. Accepted `asset.*` proposals are auto-applied (file written, governance re-indexed).

---

## When to use /learn

- After completing a `/spec` or `/bug` workflow (the workflow's own learn pipeline runs automatically on `learn → complete`; `/learn` is for ad-hoc full-codebase scans)
- When the user asks "what have we learned?" or wants a fresh pass
- Before major refactoring to capture current patterns
