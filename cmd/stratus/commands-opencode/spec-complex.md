---
description: "Complex spec workflow with discovery and design phases (discovery → design → plan → implement → verify → learn → complete). Use for auth, databases, integrations, architecture."
---

# Spec-Driven Development (Complex)

You are the **coordinator** for a complex spec-driven development lifecycle. This adds discovery and design phases before planning.

## When to Use

Use `/spec-complex` for:
- Authentication, authorization, security changes
- Database migrations, schema design
- New API surface with business logic
- Third-party integrations, webhooks
- Infrastructure, CI/CD changes
- Architecture decisions requiring ADRs
- Multi-file, multi-service, or cross-cutting concerns

For simple, well-understood tasks use `/spec`.

## API Base

```bash
BASE=http://127.0.0.1:41777
```

---

## Phase 1: Discovery

Start the workflow:

```bash
curl -sS -X POST $BASE/api/workflows \
  -H 'Content-Type: application/json' \
  -d '{"id": "<kebab-slug>", "type": "spec", "complexity": "complex", "title": "<title from $ARGUMENTS>"}'

curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "discovery"}'
```

- Explore the codebase with read, grep, glob — do NOT write code.
- Use the `retrieve` MCP tool (corpus: code) for pattern discovery.
- Use the `retrieve` MCP tool (corpus: governance) for existing ADRs and constraints.
- Identify requirements, constraints, existing architecture, and the technology landscape.
- Document findings.

Transition to design:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "design"}'
```

---

## Phase 2: Design

Produce a Technical Design Document at `docs/plans/<slug>-design.md`:

```
## Technical Design: <title>

### Component Overview
<which components are involved and their responsibilities>

### API Contract
<REST/gRPC endpoints with request/response shapes, error codes>

### Data Model
<schema changes, new tables, index requirements>

### Sequence Diagram
<key flows in Mermaid or ASCII>

### Error Handling
<what errors can occur, how they propagate>

### Breaking Changes
<interface changes that require consumer updates>
```

Check governance compliance:
- Use `retrieve` MCP tool (corpus: governance) to find applicable rules and ADRs.
- Verify the design doesn't contradict accepted ADRs.
- Ensure mandatory practices are covered (TDD, error handling, input validation).

If governance issues exist → address them in the design doc before proceeding.

Transition to plan:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "plan"}'
```

---

## Phase 3: Plan

Using the design document, create an ordered implementation plan:

1. Write the plan to `docs/plans/<slug>.md`
2. Break work into ordered tasks
3. Set tasks:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks \
  -H 'Content-Type: application/json' \
  -d '{"tasks": ["Task title 1", "Task title 2", ...]}'
```

Present the plan, design doc, and task list to the user using the `question` tool. **Wait for explicit approval.**

On approval, transition to implement:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "implement"}'
```

---

## Phase 4: Implement

For each task (0-indexed):

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/<index>/start
```

Implement following the design document and project conventions:
- TDD: failing test → implement → green → refactor
- Functions max 50 lines, files max 300 lines
- Specific error types, no bare exceptions
- Input validation at API boundaries
- Coverage >= 80%

On task completion:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/<index>/complete
```

After all tasks, transition to verify:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "verify"}'
```

---

## Phase 5: Verify

Review all changes:

1. **Code Quality** — functions max 50 lines, clear naming, no dead code, DRY
2. **Correctness** — matches design doc and requirements, edge cases, tests exist
3. **Security** — no secrets, input validation, parameterized queries, no injection
4. **Governance** — check via `retrieve` MCP tool (corpus: governance)

Run all tests. If issues found → fix loop (back to implement, max 5 loops).

On pass:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "learn"}'
```

---

## Phase 6: Learn

Save memory events, create learning candidates and proposals (same as `/spec` learn phase).

Complete:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

---

## Rules

- Always produce a design document before implementing — never skip Phase 2.
- Follow TDD for all implementation.
- Always get user approval of the plan before implementing.
- Check current state: `curl -sS $BASE/api/workflows/<slug>`

Implement the complex spec for: $ARGUMENTS
