---
name: security-review
description: "Audit code for security vulnerabilities — secrets, injection, auth, OWASP Top 10. Produces PASS/FAIL verdict."
context: fork
---

# Security Review

Audit for: "$ARGUMENTS"

Read-only scan — never modifies code.

## Checks

1. **Hardcoded secrets** — `Grep` for `secret`, `password`, `api_key`, `token`, `private_key` in assignments across all source files.

2. **Authentication / authorization** — verify no HTTP endpoint or handler is accessible without identity verification. Check middleware chains.

3. **Input validation** — audit every user-controlled parameter (query strings, request bodies, file paths, headers) for missing validation.

4. **Path traversal** — check all file operations (`open()`, `os.ReadFile`, `fs.readFile`) that accept external input. Verify paths are resolved and constrained to expected directories.

5. **SQL injection** — scan for string-formatted SQL queries. All queries must use parameterized statements / prepared statements.

6. **Dependencies** — review `pyproject.toml`, `package.json`, `go.mod`, or `Cargo.toml` for unpinned versions or packages with known CVE patterns.

7. **OWASP Top 10** — check applicable categories:
   - Injection (SQL, command, LDAP)
   - Broken authentication
   - Sensitive data exposure
   - SSRF / open redirect
   - Insecure deserialization
   - Security misconfiguration

8. **Secrets in version control** — check `.gitignore` for `.env`, credential files; confirm no secrets in git history.

## Output Format

```
## Security Review

**Verdict: PASS** | **Verdict: FAIL**

### Critical Findings (must fix before release)
- `file.go:42` [critical] — hardcoded API key in source
- `handler.go:88` [high] — SQL query built with string concatenation

### Moderate Findings (should fix)
- `config.go:12` [medium] — dependency version unpinned, potential supply chain risk

### Informational
- `server.go:5` [low] — CORS allows all origins, consider restricting in production
```
