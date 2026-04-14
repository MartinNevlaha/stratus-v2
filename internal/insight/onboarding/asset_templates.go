package onboarding

import (
	"bytes"
	"fmt"
	"text/template"
)

// TemplateData holds project-specific values used to render asset templates.
type TemplateData struct {
	ProjectName string
	Framework   string // "jest", "pytest", "go-test", etc.
	TestCmd     string // "npm test", "pytest", "go test ./...", etc.
	BuildCmd    string // "npm run build", "go build ./...", etc.
	Language    string // "Go", "TypeScript", "Python"
}

// RenderTemplate renders tmpl with the given TemplateData and returns the result.
func RenderTemplate(tmpl string, data TemplateData) (string, error) {
	t, err := template.New("asset").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("render template: parse: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render template: execute: %w", err)
	}
	return buf.String(), nil
}

// ---------------------------------------------------------------------------
// Rule templates
// ---------------------------------------------------------------------------

const goConventionsRule = `# Go Conventions

## Error Handling

` + "```go" + `
// ALWAYS wrap errors with context
if err != nil {
    return fmt.Errorf("operation description: %w", err)
}
` + "```" + `

- Never return bare errors — every wrapping must include a short description of
  the operation that failed.
- Use ` + "`errors.New`" + ` for sentinel errors; check them with ` + "`errors.Is`" + `.

## Naming

- Exported identifiers: PascalCase (` + "`UserService`" + `, ` + "`ParseConfig`" + `).
- Unexported identifiers: camelCase (` + "`parseToken`" + `, ` + "`maxRetries`" + `).
- Acronyms are all-caps: ` + "`HTTPClient`" + `, ` + "`parseURL`" + `.
- Package names: lowercase, single word, no underscores.

## Interfaces

- Define interfaces in the package that *uses* them, not the package that implements them.
- Keep interfaces small — prefer single-method interfaces where possible.
- Name single-method interfaces after the method: ` + "`type Reader interface { Read(...) }`" + `.

## Tests

- Use table-driven tests for any function with multiple input/output combinations.
- Test names follow ` + "`Test<Function>_<Scenario>`" + ` (e.g., ` + "`TestParseConfig_MissingKey`" + `).
- Subtests via ` + "`t.Run`" + ` for each table row.
- No external test frameworks — use the standard ` + "`testing`" + ` package.

## Formatting & Tooling

- Code must pass ` + "`gofmt`" + ` and ` + "`go vet`" + ` without warnings.
- Run ` + "`go test ./...`" + ` before every commit.
`

const tsConventionsRule = `# TypeScript Conventions

## Strict Mode

All TypeScript code must compile with ` + "`strict: true`" + ` in ` + "`tsconfig.json`" + `.
No ` + "`@ts-ignore`" + ` or ` + "`any`" + ` casts without an accompanying comment explaining why.

## Explicit Types

- Always annotate function parameters and return types explicitly.
- Avoid inferred ` + "`any`" + ` — use ` + "`unknown`" + ` and narrow with type guards instead.
- Prefer ` + "`interface`" + ` for object shapes that may be extended; ` + "`type`" + ` for unions/aliases.

## Async / Await

- Use ` + "`async/await`" + ` over raw Promises and ` + "`.then`" + ` chains.
- Always handle the rejection path — either with ` + "`try/catch`" + ` or propagate.

## Error Handling

` + "```typescript" + `
// Type-narrow in catch blocks
catch (e) {
  if (e instanceof AppError) { /* handle */ }
  throw e;  // re-throw unknown errors
}
` + "```" + `

Never swallow errors in an empty ` + "`catch`" + ` block.

## ESLint

- All files must pass ESLint with the project config before merge.
- Fix lint warnings; do not disable rules without team approval.

## Naming

- Variables and functions: camelCase.
- Classes, interfaces, and type aliases: PascalCase.
- Constants: UPPER_SNAKE_CASE for module-level primitives.
- File names: kebab-case (` + "`user-service.ts`" + `).
`

const pythonConventionsRule = `# Python Conventions

## Type Hints

All function signatures must include type hints (PEP 484).

` + "```python" + `
def get_user(user_id: int) -> User:
    ...
` + "```" + `

Use ` + "`from __future__ import annotations`" + ` for forward references.

## Docstrings

- Public modules, classes, and functions require docstrings (Google style).
- One-line summary; blank line; extended description if needed.

## Naming

- Variables and functions: snake_case.
- Classes: PascalCase.
- Constants: UPPER_SNAKE_CASE.
- Private members: single leading underscore (` + "`_helper`" + `).

## Error Handling

- Raise specific exception types — never ` + "`raise Exception(...)`" + ` directly.
- Catch specific exceptions; avoid bare ` + "`except:`" + `.
- Log errors at the handling site, not at every re-raise level.

## Virtual Environments

- Always develop inside a virtual environment (` + "`venv`" + ` or ` + "`uv`" + `).
- Pin all direct dependencies in ` + "`pyproject.toml`" + ` or ` + "`requirements.txt`" + `.
- Do not commit ` + "`.venv/`" + ` or ` + "`__pycache__/`" + `.

## Formatting

- Format with ` + "`black`" + `; sort imports with ` + "`isort`" + `.
- Maximum line length: 88 characters (black default).
`

const dockerConventionsRule = `# Docker Conventions

## Multi-Stage Builds

Always use multi-stage builds to keep final images small.

` + "```dockerfile" + `
# Stage 1: build
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o /bin/app ./cmd/app

# Stage 2: runtime
FROM alpine:3.20
COPY --from=builder /bin/app /bin/app
ENTRYPOINT ["/bin/app"]
` + "```" + `

## .dockerignore

Include a ` + "`.dockerignore`" + ` file to exclude:
` + "```" + `
.git
node_modules
dist
*.log
` + "```" + `

## Non-Root User

Run containers as a non-root user.

` + "```dockerfile" + `
RUN adduser -D appuser
USER appuser
` + "```" + `

## Layer Caching

Copy dependency manifests before source code to maximise cache reuse.

` + "```dockerfile" + `
COPY go.mod go.sum ./
RUN go mod download
COPY . .
` + "```" + `

## Tagging

Tag images with both ` + "`latest`" + ` and a version/SHA for traceability:
` + "```bash" + `
docker build -t myapp:latest -t myapp:${GIT_SHA} .
` + "```" + `
`

const monorepoConventionsRule = `# Monorepo Conventions

## Workspace Boundaries

- Each package/service is self-contained under its own directory.
- Cross-package imports must go through the package's public API — never import
  internal sub-packages of another package.
- Circular dependencies between packages are forbidden.

## Shared Dependencies

- Shared utilities live in a ` + "`shared/`" + ` or ` + "`internal/`" + ` package.
- Third-party version pins are managed at the workspace root (` + "`go.work`" + `,
  root ` + "`package.json`" + `, etc.).
- A dependency upgrade must be applied to all packages in the same PR.

## Package Naming

- Package names reflect their purpose, not the team that owns them.
- Avoid generic names like ` + "`util`" + ` or ` + "`common`" + ` — be specific.
- Namespace with domain prefix when packages are likely to conflict
  (e.g., ` + "`auth/token`" + ` vs ` + "`billing/token`" + `).

## Tooling

- Use workspace-aware commands (e.g., ` + "`go work`" + `, ` + "`npm workspaces`" + `) for
  cross-package tasks.
- CI must run affected-package detection to avoid rebuilding the full monorepo
  on every change.
`

const ciConventionsRule = `# CI Conventions

## Pipeline Stages

All pipelines must include these ordered stages:

1. **lint** — static analysis and formatting checks (fail fast)
2. **test** — unit and integration tests with coverage report
3. **build** — compile/bundle artefacts

Optional: **deploy** runs only on the default branch after all prior stages pass.

## Fail-Fast

- Enable fail-fast so a broken stage stops subsequent stages immediately.
- Do not allow ` + "`continue-on-error`" + ` on the lint or test stages.

## Caching

Cache dependency directories between runs to reduce build time:

` + "```yaml" + `
# GitHub Actions example
- uses: actions/cache@v4
  with:
    path: ~/.cache/go/pkg/mod
    key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
` + "```" + `

## Environment Variables

- Store secrets in CI secret storage — never hard-code credentials in workflow files.
- Use environment-specific contexts (` + "`staging`" + `, ` + "`production`" + `) for deploy jobs.

## Branch Protection

- Require the full CI pipeline to pass before merging to the default branch.
- Enable required status checks and dismiss stale approvals on force-push.
`

// ---------------------------------------------------------------------------
// Skill templates
// ---------------------------------------------------------------------------

const runTestsSkill = `---
name: run-tests
description: "Run the {{.Framework}} test suite and report results"
context: fork
---
# Run Tests

Run the project test suite using {{.Framework}} and report a clear pass/fail
summary with any failures highlighted.

## Steps

1. Run the tests:
   ` + "```bash" + `
   {{.TestCmd}}
   ` + "```" + `

2. Check the exit code:
   - Exit 0 → all tests passed. Report the total count.
   - Non-zero → tests failed. Show the list of failing test names and their
     error output.

3. If the framework produces a coverage report, include the overall coverage
   percentage in the summary.

## Output Format

` + "```" + `
Tests: PASS | FAIL
Passed:  <n>
Failed:  <n>
Coverage: <n>% (if available)

<failing test names and errors, if any>
` + "```" + `

Do not truncate failure output — show the full error for each failing test.
`

const dockerBuildSkill = `---
name: docker-build
description: "Build and tag a Docker image for the project"
context: fork
---
# Docker Build

Build a Docker image and tag it with both ` + "`latest`" + ` and the current git SHA.

## Steps

1. Determine the current git SHA:
   ` + "```bash" + `
   git rev-parse --short HEAD
   ` + "```" + `

2. Build and tag the image:
   ` + "```bash" + `
   docker build -t {{.ProjectName}}:latest -t {{.ProjectName}}:<sha> .
   ` + "```" + `

3. Verify the image was created:
   ` + "```bash" + `
   docker images {{.ProjectName}}
   ` + "```" + `

4. Report the image ID, tags, and size.

## On Failure

If the build fails, show the full Docker build output. Check for:
- Missing ` + "`.dockerignore`" + ` (large context warning)
- Failed ` + "`COPY`" + ` instructions (missing files)
- Failed ` + "`RUN`" + ` commands (dependency install errors)
`

const ciCheckSkill = `---
name: ci-check
description: "Check CI pipeline status and surface failing logs"
context: fork
---
# CI Check

Inspect the most recent CI pipeline run and surface any failures.

## Steps

1. List recent pipeline runs (GitHub Actions example):
   ` + "```bash" + `
   gh run list --limit 5
   ` + "```" + `

2. Get the status of the latest run:
   ` + "```bash" + `
   gh run view <run-id>
   ` + "```" + `

3. If any job failed, show its logs:
   ` + "```bash" + `
   gh run view <run-id> --log-failed
   ` + "```" + `

4. Report a summary:
   - Overall pipeline status (pass/fail)
   - Names of failed jobs (if any)
   - Relevant log excerpt for each failed job

## Adapting to Other CI Systems

- **GitLab CI**: use ` + "`glab ci view`" + ` and ` + "`glab ci trace`" + `.
- **CircleCI**: use the CircleCI CLI (` + "`circleci pipeline list`" + `).
- If no CLI is available, ask the user to paste the failed job log.
`

// ---------------------------------------------------------------------------
// Agent templates — Claude Code format
// ---------------------------------------------------------------------------

const frontendSpecialistAgentCC = `---
name: delivery-frontend-specialist
description: "{{.Language}} / {{.Framework}} frontend specialist for {{.ProjectName}}"
tools:
  - Read
  - Grep
  - Glob
  - Edit
  - Write
  - Bash
model: claude-sonnet-4-5
skills:
  - run-tests
---
You are a focused {{.Language}} frontend specialist for {{.ProjectName}}.
Your job is to implement, refactor, and fix frontend features cleanly and
completely. You do not speculate — you read the code first.

## Workflow

### 1. Understand
- Read the relevant source files before touching anything.
- Use Grep to locate all call-sites of a function or component before
  modifying its signature.
- Check existing tests so you know what behaviour must be preserved.

### 2. Test
- Write or update tests before changing logic (TDD).
- Run the test suite: ` + "`{{.TestCmd}}`" + `

### 3. Implement
- Follow existing code style and naming conventions.
- Keep functions under 50 lines; files under 300 lines.
- Never add a dependency without checking if the project already has one
  that covers the use case.

### 4. Verify
- Run the full test suite again after implementation.
- Type-check: ensure no new type errors are introduced.
- Report test results (pass/fail counts) and any remaining issues.
`

const cliSpecialistAgentCC = `---
name: delivery-cli-specialist
description: "CLI and command-line tooling specialist for {{.ProjectName}}"
tools:
  - Read
  - Grep
  - Glob
  - Edit
  - Write
  - Bash
model: claude-sonnet-4-5
skills:
  - run-tests
---
You are a CLI specialist for {{.ProjectName}}.
Your job is to implement, debug, and improve command-line interfaces and
developer tooling. You prioritise correct argument parsing, helpful error
messages, and scriptable output.

## Workflow

### 1. Understand
- Read the CLI entry point and existing command definitions.
- Identify the argument parsing library in use (cobra, click, argparse, etc.).
- Review existing tests for command behaviour.

### 2. Test
- Write tests for new commands or flag combinations before implementing.
- Run the test suite: ` + "`{{.TestCmd}}`" + `

### 3. Implement
- Follow the project's existing command structure and help-text style.
- All commands must return non-zero exit codes on error.
- Errors go to stderr; normal output goes to stdout.

### 4. Verify
- Run the full test suite.
- Manually invoke the new command path and capture the output.
- Report results and any edge cases that were not covered.
`

const devopsSpecialistAgentCC = `---
name: delivery-devops-specialist
description: "Docker and CI/CD specialist for {{.ProjectName}}"
tools:
  - Read
  - Grep
  - Glob
  - Edit
  - Write
  - Bash
model: claude-sonnet-4-5
skills:
  - docker-build
  - ci-check
---
You are a DevOps specialist for {{.ProjectName}}.
Your job is to improve build pipelines, Dockerfiles, and CI/CD configuration.
You follow the project's Docker and CI conventions strictly.

## Workflow

### 1. Understand
- Read existing Dockerfiles and CI workflow files before making changes.
- Identify the CI platform in use (GitHub Actions, GitLab CI, etc.).
- Check the current build command: ` + "`{{.BuildCmd}}`" + `

### 2. Implement
- Use multi-stage Docker builds; run containers as non-root.
- Add or update ` + "`.dockerignore`" + ` to exclude build artefacts and secrets.
- Cache dependency layers before copying source in Dockerfiles.
- CI pipelines must have ordered stages: lint → test → build.

### 3. Verify
- Build the Docker image and confirm it runs correctly.
- Trigger or simulate the CI pipeline and check for failures.
- Report image size, build time, and pipeline status.
`

// ---------------------------------------------------------------------------
// Agent templates — OpenCode format
// ---------------------------------------------------------------------------

const frontendSpecialistAgentOC = `---
description: "{{.Language}} / {{.Framework}} frontend specialist for {{.ProjectName}}"
mode: subagent
tools:
  todo: false
---
You are a focused {{.Language}} frontend specialist for {{.ProjectName}}.
Your job is to implement, refactor, and fix frontend features cleanly and
completely. You do not speculate — you read the code first.

## Workflow Guard

Before starting any implementation, confirm there is an active Stratus workflow:

` + "```bash" + `
curl -s http://localhost:41777/api/workflows | jq '.[] | select(.status=="active")'
` + "```" + `

If no active workflow exists, stop and ask the coordinator to register one.

## Workflow

### 1. Understand
- Read the relevant source files before touching anything.
- Use search tools to locate all call-sites of a function or component.
- Check existing tests so you know what behaviour must be preserved.

### 2. Test
- Write or update tests before changing logic (TDD).
- Run the test suite: ` + "`{{.TestCmd}}`" + `

### 3. Implement
- Follow existing code style and naming conventions.
- Keep functions under 50 lines; files under 300 lines.

### 4. Verify
- Run the full test suite again after implementation.
- Report test results (pass/fail counts) and any remaining issues.
`

const cliSpecialistAgentOC = `---
description: "CLI and command-line tooling specialist for {{.ProjectName}}"
mode: subagent
tools:
  todo: false
---
You are a CLI specialist for {{.ProjectName}}.
Your job is to implement, debug, and improve command-line interfaces and
developer tooling.

## Workflow Guard

Before starting any implementation, confirm there is an active Stratus workflow:

` + "```bash" + `
curl -s http://localhost:41777/api/workflows | jq '.[] | select(.status=="active")'
` + "```" + `

If no active workflow exists, stop and ask the coordinator to register one.

## Workflow

### 1. Understand
- Read the CLI entry point and existing command definitions.
- Identify the argument parsing library in use.
- Review existing tests for command behaviour.

### 2. Test
- Write tests for new commands or flag combinations before implementing.
- Run the test suite: ` + "`{{.TestCmd}}`" + `

### 3. Implement
- Follow the project's existing command structure.
- All commands must return non-zero exit codes on error.
- Errors go to stderr; normal output goes to stdout.

### 4. Verify
- Run the full test suite.
- Report results and any edge cases that were not covered.
`

const devopsSpecialistAgentOC = `---
description: "Docker and CI/CD specialist for {{.ProjectName}}"
mode: subagent
tools:
  todo: false
---
You are a DevOps specialist for {{.ProjectName}}.
Your job is to improve build pipelines, Dockerfiles, and CI/CD configuration.

## Workflow Guard

Before starting any implementation, confirm there is an active Stratus workflow:

` + "```bash" + `
curl -s http://localhost:41777/api/workflows | jq '.[] | select(.status=="active")'
` + "```" + `

If no active workflow exists, stop and ask the coordinator to register one.

## Workflow

### 1. Understand
- Read existing Dockerfiles and CI workflow files before making changes.
- Identify the CI platform in use.
- Check the current build command: ` + "`{{.BuildCmd}}`" + `

### 2. Implement
- Use multi-stage Docker builds; run containers as non-root.
- Cache dependency layers before copying source.
- CI pipelines must have ordered stages: lint → test → build.

### 3. Verify
- Build the Docker image and confirm it runs correctly.
- Trigger or simulate the CI pipeline and check for failures.
- Report image size, build time, and pipeline status.
`

// ---------------------------------------------------------------------------
// Command templates — OpenCode format
// ---------------------------------------------------------------------------

const buildCommandOC = `---
description: "Build {{.ProjectName}} using the configured build command"
---
# Build

Run the project build and report the outcome.

## Steps

1. Execute the build command:
   ` + "```bash" + `
   {{.BuildCmd}}
   ` + "```" + `

2. Check the exit code:
   - Exit 0 → build succeeded. Report artefact locations if known.
   - Non-zero → build failed. Show the full error output and identify the
     likely root cause (compile error, missing dependency, bad config, etc.).

3. If the build produced binary or bundle artefacts, report their paths and
   sizes.

Do not silently ignore warnings that are treated as errors in this project.
`

const testCommandOC = `---
description: "Run the {{.ProjectName}} test suite using {{.TestCmd}}"
---
# Test

Run the full test suite and report a clear pass/fail summary.

## Steps

1. Run all tests:
   ` + "```bash" + `
   {{.TestCmd}}
   ` + "```" + `

2. Check the exit code:
   - Exit 0 → all tests passed. Report the total count and coverage percentage
     if available.
   - Non-zero → tests failed. List every failing test name and its error output.

## Output Format

` + "```" + `
Tests: PASS | FAIL
Passed:  <n>
Failed:  <n>
Coverage: <n>% (if available)

<failing test names and full errors, if any>
` + "```" + `

Never truncate failure output — the full error is needed to diagnose the problem.
`
