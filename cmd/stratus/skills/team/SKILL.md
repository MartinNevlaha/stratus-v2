---
name: team
description: "Parallel spec workflow using agent teams. Spawns delivery agents simultaneously for faster implementation. Use when the user says /team."
disable-model-invocation: true
argument-hint: "<feature description>"
---

# Team-Based Spec Workflow

Current session: ${CLAUDE_SESSION_ID}

```bash
BASE=http://localhost:41777
```

> **Experimental**: This workflow uses parallel delivery agents.
> Workflows created here are visible in the **Teams** tab of the dashboard.

---

## Phase 1: Explore & Plan

Generate a short slug from `$ARGUMENTS` (kebab-case, max 40 chars).

Create the workflow — title **MUST** start with `[TEAM] `:

```bash
curl -sS -X POST $BASE/api/workflows \
  -H 'Content-Type: application/json' \
  -d "{\"id\": \"<slug>\", \"type\": \"spec\", \"complexity\": \"simple\",
       \"title\": \"[TEAM] $ARGUMENTS\",
       \"session_id\": \"${CLAUDE_SESSION_ID}\"}"
```

Then:
1. Explore the codebase (Read, Glob, Grep) to understand the relevant context.
2. Delegate planning to `delivery-system-architect` (Task tool) — task breakdown, dependencies.
3. Check governance: `GET $BASE/api/governance/check?query=<topic>`
4. Set tasks on the workflow:
   ```bash
   curl -sS -X PUT $BASE/api/workflows/<slug>/tasks \
     -H 'Content-Type: application/json' \
     -d '[{"title": "<task>", "description": "<desc>"}]'
   ```
5. Present the task list to the user. **Wait for explicit approval** before proceeding.

Transition to implement:
```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "implement"}'
```

---

## Phase 2: Implement — spawn ALL delivery agents in parallel

Group pending tasks by delivery domain. Spawn **all agents simultaneously** using
multiple Task tool calls **in a single message** (parallel execution — this is the
key difference from `/spec` which runs agents one by one).

Domain routing:
- Backend / API / handlers → `delivery-backend-engineer`
- Frontend / UI / Svelte / components → `delivery-frontend-engineer`
- Database / migrations / schema → `delivery-database-engineer`
- Tests / coverage / QA → `delivery-qa-engineer`
- Infrastructure / CI/CD / Docker → `delivery-devops-engineer`
- Architecture / ADRs → `delivery-system-architect`
- Mixed / unclear → `delivery-implementation-expert`

If multiple tasks map to the same domain, assign them all to one agent of that type.

Each agent's spawn prompt MUST include:
1. Their assigned task indices and full descriptions
2. Stratus task API calls:
   - `POST $BASE/api/workflows/<slug>/tasks/<idx>/start` — call before starting each task
   - `POST $BASE/api/workflows/<slug>/tasks/<idx>/complete` — call after finishing each task
3. Session claim: `PATCH $BASE/api/workflows/<slug>/session -H 'Content-Type: application/json' -d '{"session_id": "${CLAUDE_SESSION_ID}"}'`

Wait for **all** Task calls to complete. Then transition to verify:
```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "verify"}'
```

---

## Phase 3: Verify

Delegate to `delivery-code-reviewer` (Task) and `delivery-governance-checker` (Task)
— spawn them in parallel in a single message.

If `[must_fix]` issues are found → transition back to implement, fix issues, re-verify.
On pass → transition to learn:
```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "learn"}'
```

---

## Phase 4: Learn & Complete

Save memory events for key decisions:
```bash
curl -sS -X POST $BASE/api/memory \
  -H 'Content-Type: application/json' \
  -d '{"title": "<decision>", "text": "<details>", "type": "decision", "importance": 0.7}'
```

Transition to complete:
```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

Summarize what was implemented and which agents contributed.

---

## Constraints

- **NEVER** use Write, Edit, or Bash on production source files directly.
- Delegate ALL implementation work to delivery agents via Task tool.
- Always get user approval after the plan phase before spawning agents.
- The `[TEAM]` prefix in the title is mandatory — it's how the Teams dashboard tab identifies these workflows.
