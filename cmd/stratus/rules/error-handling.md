# Error Handling

Consistent error handling across all languages used in the project.

## Go

```go
// ALWAYS wrap errors with context
if err != nil {
    return fmt.Errorf("create user: %w", err)
}

// NEVER return bare errors
if err != nil {
    return err  // BAD — no context for debugging
}

// Use sentinel errors for expected conditions
var ErrNotFound = errors.New("not found")

// Check sentinel errors with errors.Is()
if errors.Is(err, ErrNotFound) { ... }
```

## Python

```python
# Use specific exception types
raise ValueError("email must contain @")

# NEVER use bare except
except Exception:  # BAD
except:            # WORSE

# Catch specific exceptions
try:
    user = get_user(id)
except UserNotFoundError:
    return 404
except DatabaseError as e:
    log.error("db failure: %s", e)
    raise
```

## TypeScript

```typescript
// Use typed errors
class AppError extends Error {
  constructor(message: string, public code: string) {
    super(message);
  }
}

// NEVER use bare catch
catch (e) { ... }           // BAD — e is unknown

// Type-narrow in catch blocks
catch (e) {
  if (e instanceof AppError) { ... }
  throw e;  // re-throw unknown errors
}
```

## Universal Rules

- Every error MUST include context about what operation failed
- Log errors at the point of handling, not at every level
- Never swallow errors silently (`catch {}` with empty body)
- At API boundaries: map internal errors to user-facing messages (don't leak internals)
