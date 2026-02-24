# TDD Requirements

Test-Driven Development is mandatory for all implementation work.

## Workflow

1. **Write a failing test** that captures the expected behavior
2. **Confirm it fails** (and not from syntax errors — the test must be valid)
3. **Write minimal code** to make the test pass
4. **Run all tests** — confirm green
5. **Refactor** while keeping tests green

## Test Naming

Follow this convention per language:

- **Python**: `test_<function>_<scenario>_<expected>`
  - Example: `test_login_invalid_password_returns_401`
- **Go**: `Test<Function>_<Scenario>`
  - Example: `TestLogin_InvalidPassword`
- **TypeScript/JavaScript**: `describe("<function>") → it("should <expected> when <scenario>")`
  - Example: `it("should return 401 when password is invalid")`

## Coverage

- Target: **>= 80%** line coverage
- All new public functions/methods MUST have tests
- Critical paths (auth, payments, data mutations) MUST have tests regardless of coverage

## What to Test

- Happy path (expected input → expected output)
- Edge cases (empty, nil/null, boundary values, max length)
- Error paths (invalid input, missing data, permission denied)
- Integration points (API calls, database queries)

## What NOT to Test

- Private/internal helpers that are tested through public interfaces
- Framework-generated boilerplate
- Trivial getters/setters with no logic
