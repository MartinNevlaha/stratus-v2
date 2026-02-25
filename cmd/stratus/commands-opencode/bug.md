---
description: "Bug-fixing workflow (analyze → fix → review → complete). Structured debugging and repair via stratus workflow API."
---

# Bug-Fixing Workflow

You are the **coordinator** for a structured bug-fixing workflow. You manage phases via the stratus HTTP API.

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

Diagnose the root cause:

1. **Reproduce** — Understand symptoms. Find error messages, stack traces, logs.
2. **Trace** — Follow the execution path from symptom to root cause. Use `retrieve` MCP tool (corpus: code) to find related code.
3. **Classify** the bug type:

| Type | Description |
|------|-------------|
| **Logic** | Wrong condition, off-by-one, incorrect algorithm |
| **Integration** | API contract mismatch, wrong endpoint, data format |
| **Concurrency** | Race condition, deadlock, missing synchronization |
| **Data** | Corrupt data, missing validation, encoding issue |
| **Configuration** | Wrong env var, missing config, path issue |
| **Dependency** | Library bug, version incompatibility |

4. **Report** — Document the diagnosis:

```
## Bug Diagnosis

### Symptom
<What the user observes>

### Root Cause
<Exact cause with file:line references>

### Classification
<Bug type>

### Evidence
- <file:line — problematic code>

### Recommended Fix
<What needs to change>
```

**Present diagnosis to user using the `question` tool. Get explicit approval before fixing.**

On approval, transition to fix:

```bash
curl -sS -X PUT $BASE/api/workflows/bug-<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "fix"}'
```

---

## Phase 2: Fix

Implement the fix following project conventions:

- Write a failing test that reproduces the bug first (TDD)
- Fix the root cause — not just the symptom
- Ensure all existing tests still pass
- No hardcoded values — use proper error types and validation

Then transition to review:

```bash
curl -sS -X PUT $BASE/api/workflows/bug-<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "review"}'
```

---

## Phase 3: Review

Review the fix for quality and regressions:

1. **Correctness** — does the fix address the root cause? Edge cases?
2. **Regression** — run all tests, confirm no new failures
3. **Code quality** — clean code, specific error types, no dead code
4. **Security** — no new injection vectors, secrets, or validation gaps

If issues found → fix loop: transition back to fix, re-fix, re-review (max 5 loops).

On pass, complete:

```bash
curl -sS -X PUT $BASE/api/workflows/bug-<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

Save a memory event with key findings:

```bash
save_memory(text="Bug fix: <summary>", type="bugfix", tags=["bug"], importance=0.7)
```

---

## Rules

- **ALWAYS get explicit user approval before Phase 2 (Fix).**
- Max 5 fix loops — escalate to user if still broken after 5 attempts.
- Write a regression test that proves the bug is fixed.
- Check current state: `curl -sS $BASE/api/workflows/bug-<slug>`

Fix the bug: $ARGUMENTS
