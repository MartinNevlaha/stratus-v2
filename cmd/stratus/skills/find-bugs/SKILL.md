---
name: find-bugs
description: "Systematically identify bugs and root causes. Read-only diagnosis — never modifies code. Use when debugging failures or unexpected behavior."
context: fork
---

# Find Bugs

Systematically identify root causes for: "$ARGUMENTS"

Never guess without evidence. Never modify code — diagnosis only.

## Process

### 1. Reproduce First
- Get the exact error message, stack trace, or unexpected output
- Identify the minimal steps to reproduce
- Confirm reproduction before investigating

### 2. Read the Error Completely
- Stack traces point to the ACTUAL failure site (often not the root cause)
- Check line numbers, not just the exception type
- Note what data was present when it failed

### 3. Trace Data Flow
```
Entry point → transformation → storage/output
              ↑ where does expected ≠ actual?
```
- Read the input
- Follow the code path
- Find where the invariant breaks

### 4. Check Recent Changes
```bash
git log --oneline -10
git diff HEAD~1
```
If a previously working feature broke, the bug is almost always in the diff.

### 5. Form a Falsifiable Hypothesis
"The bug is X because Y. If I change Z, the output will be W."

Never make multiple changes at once — one variable at a time.

### 6. Identify Fix + Regression Test

The diagnosis is not complete without:
- A clear explanation of the root cause
- The minimal code change that fixes it
- A test that FAILS before the fix and PASSES after

## Common Bug Patterns

| Pattern | Signal | Where to Look |
|---------|--------|---------------|
| Off-by-one | Wrong count, first/last item missing | Loop bounds, slice indices |
| Nil/null dereference | Panic, NullPointerException | Guard conditions before use |
| Race condition | Intermittent failures | Shared state, goroutines, async |
| Wrong type | TypeError, unexpected coercion | Input validation, type assertions |
| State mutation | Works alone, fails in sequence | Shared objects, append/push |
| Encoding | UnicodeDecodeError, garbled text | `open(..., encoding="utf-8")` |

## Output Format

```
## Bug Report

**Root Cause**: <one sentence>

**Evidence**:
- File: `path/to/file.go:42`
- Data at failure: <value>
- Expected: <value>

**Fix**:
<minimal code change — describe, do not apply>

**Regression Test**:
<test that catches this bug>
```
