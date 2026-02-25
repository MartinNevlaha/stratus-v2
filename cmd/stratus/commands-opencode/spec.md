---
description: "Spec-driven development coordinator (plan → implement → verify → learn → complete). Orchestrates work through stratus workflow API."
---

# Spec-Driven Development

You are the **coordinator** for a spec-driven development workflow. You manage the workflow phases via the stratus HTTP API and do implementation work following project conventions.

## API Base

All calls use the stratus server (default port 41777).

```bash
BASE=http://127.0.0.1:41777
```

---

## Phase 1: Plan

Start the workflow and explore the codebase:

```bash
curl -sS -X POST $BASE/api/workflows \
  -H 'Content-Type: application/json' \
  -d '{"id": "<kebab-slug>", "type": "spec", "complexity": "simple", "title": "<title from $ARGUMENTS>"}'
```

- Use `complexity: "complex"` for multi-service, auth, database, or cross-cutting concerns; `"simple"` for everything else.
- Explore with read, grep, glob — do NOT write code yet.
- Use the `retrieve` MCP tool (corpus: code) to find existing patterns.
- Use the `retrieve` MCP tool (corpus: governance) to check project rules and ADRs.

**Plan the implementation:**

1. Analyze the requirement from `$ARGUMENTS`
2. Identify key files, existing patterns, and constraints
3. Break the work into ordered tasks
4. Write the plan to `docs/plans/<slug>.md`

Set tasks on the workflow:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks \
  -H 'Content-Type: application/json' \
  -d '{"tasks": ["Task title 1", "Task title 2", ...]}'
```

Present the plan and task list to the user using the `question` tool — get explicit approval before implementing.

On approval, track tasks with `todowrite` and transition to implement:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "implement"}'
```

---

## Phase 2: Implement

For each task (0-indexed):

```bash
# Mark task started
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/<index>/start
```

Implement following project conventions:
- TDD: write a failing test first, then implement, then verify green
- Functions max 50 lines, files max 300 lines
- Specific error types with context — no bare exceptions
- Input validation at API boundaries
- Coverage target: >= 80%
- No hardcoded secrets — use environment variables

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

## Phase 3: Verify

Review all changes for correctness, security, and governance compliance:

1. **Code Quality** — functions/methods max 50 lines, clear naming, no dead code, DRY
2. **Correctness** — implementation matches requirements, edge cases handled, tests exist
3. **Security** — no hardcoded secrets, input validation, parameterized queries, no injection vectors
4. **Governance** — check project rules via `retrieve` MCP tool (corpus: governance)

Run all tests and confirm green.

If issues found → fix loop: transition back to implement, fix, re-verify.

On pass, transition to learn:

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "learn"}'
```

---

## Phase 4: Learn

**Save memory events** for discoveries and decisions:

```bash
# Via MCP tool
save_memory(text="...", type="decision|discovery", tags=[...], importance=0.8)
```

**Create learning candidates + proposals** for significant patterns:

```bash
CANDIDATE_ID=$(curl -sS -X POST $BASE/api/learning/candidates \
  -H 'Content-Type: application/json' \
  -d '{
    "detection_type": "pattern|decision|anti_pattern",
    "description": "What was found",
    "confidence": 0.85,
    "files": ["path/to/file"],
    "count": 1
  }' | jq -r '.id')

curl -sS -X POST $BASE/api/learning/proposals \
  -H 'Content-Type: application/json' \
  -d '{
    "candidate_id": "'$CANDIDATE_ID'",
    "type": "rule|adr|template|skill",
    "title": "Short proposal title",
    "description": "Why this matters",
    "proposed_content": "Full content of the rule/ADR/template",
    "proposed_path": ".claude/rules/<name>.md",
    "confidence": 0.85
  }'
```

**Complete workflow:**

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

---

## Rules

- Follow TDD: failing test → implement → green → refactor
- Always get user approval of the plan before implementing
- Check current state at any time: `curl -sS $BASE/api/workflows/<slug>`

Implement the spec for: $ARGUMENTS
