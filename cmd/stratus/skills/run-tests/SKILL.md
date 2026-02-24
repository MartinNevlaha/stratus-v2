---
name: run-tests
description: "Run the project test suite. Auto-detects Python/Node/Go/Rust. Use when asked to test or verify changes."
context: fork
---

# Run Tests

Run the test suite for: "$ARGUMENTS"

1. Detect the project type from config files (`pyproject.toml`, `package.json`, `go.mod`, `Cargo.toml`, `bun.lockb`).

2. Run the appropriate test command:
   - **Python (uv)**: `uv run pytest -v` (with coverage: `uv run pytest --cov=src --cov-fail-under=80 -v`)
   - **Python**: `pytest -v`
   - **Node.js (bun)**: `bun test`
   - **Node.js (npm)**: `npm test`
   - **Go**: `go test ./...`
   - **Rust**: `cargo test`

3. If specific files or modules are provided in `$ARGUMENTS`, run only those tests.

4. If tests **fail**:
   - Read the full error output
   - Identify root cause (not just the failing assertion — find why)
   - Report the failure with file path, line number, and suggested fix

5. If tests **pass**, run the project linter:
   - Python: `uv run ruff check src/ tests/` or `ruff check .`
   - Node/TS: `eslint` or `biome check`
   - Go: `golangci-lint run` (if installed), else `go vet ./...`
   - Rust: `cargo clippy`

6. Report final status: `PASS` or `FAIL` with a summary.
