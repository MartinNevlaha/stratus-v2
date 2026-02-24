---
name: code-review
description: "Structured code review — correctness, tests, standards, security, maintainability. Produces PASS/FAIL verdict."
context: fork
---

# Code Review

Systematic review for: "$ARGUMENTS"

Focused on correctness, maintainability, and project standards.

## Review Dimensions

### 1. Correctness
- Does it do what it claims?
- Edge cases: empty input, null/nil, zero, max values
- Error handling: specific exceptions, not bare `catch`/`except`
- Race conditions in async/concurrent code

### 2. Project Standards
- File size: production files < 300 lines (500 hard limit)
- Type hints / types on all public functions
- No unused imports or dead code
- Naming follows language conventions
- Run the linter: `ruff check` (Python), `eslint`/`biome` (TS), `go vet` (Go), `cargo clippy` (Rust)

### 3. Tests
- Every new function/method has a corresponding test
- Tests mock external dependencies
- Tests verify behavior, not implementation details
- Coverage does not drop below project threshold

### 4. Security
- No hardcoded secrets or credentials
- SQL uses parameterized queries, not string interpolation
- Path operations validated against traversal attacks
- User input sanitized at entry points

### 5. Maintainability
- No duplicate logic (DRY)
- Functions do one thing
- No deeply nested conditionals (max 3 levels)
- Comments explain WHY, not WHAT

## Output Format

```
## Code Review

**Verdict: PASS** | **Verdict: FAIL**

### Issues
1. **[must_fix]** `file.go:42` — bare error discard, handle or wrap
2. **[should_fix]** `file.go:88` — duplicate logic, extract helper
3. **[suggestion]** `file.go:120` — consider early return to reduce nesting

### Checks
- [x] Correctness: PASS
- [ ] Tests: FAIL
- [x] Standards: PASS
- [x] Security: PASS
- [x] Maintainability: PASS
```

`[must_fix]` → FAIL verdict. `[should_fix]` → warning only. `[suggestion]` → optional.
