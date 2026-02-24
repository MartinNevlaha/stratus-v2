# Review Verdict Format

All code reviews MUST use this structured verdict format.

## Verdict

Every review ends with exactly one of:

```
Verdict: PASS
Verdict: FAIL
```

## Issue Severity

Each issue is tagged with one severity:

- `[must_fix]` — Bugs, security issues, spec violations, missing critical tests. **FAIL requires at least one.**
- `[should_fix]` — Code smells, style violations, missing edge case coverage. Does not block PASS.
- `[suggestion]` — Optional improvements, alternative approaches. Does not block PASS.

## Format

```
## Review Verdict: PASS | FAIL

### Issues

[must_fix] Description — file:line
[should_fix] Description — file:line
[suggestion] Description — file:line

### Summary
1-3 sentence assessment.
```

## Rules

- A review with zero `[must_fix]` issues MUST be `PASS`
- A review with one or more `[must_fix]` issues MUST be `FAIL`
- Every issue MUST include a file:line reference
- The summary MUST be actionable (what to fix, not just what's wrong)
