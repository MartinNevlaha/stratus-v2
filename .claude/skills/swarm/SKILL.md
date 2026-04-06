---
name: swarm
description: "Multi-agent swarm workflow. Spawns isolated workers in git worktrees for truly parallel implementation. Use when the user says /swarm."
disable-model-invocation: true
argument-hint: "<feature description>"
---

# Swarm Workflow

Current session: ${CLAUDE_SESSION_ID}

```bash
BASE=http://localhost:$(stratus port)
```

> **Swarm** runs delivery agents in isolated git worktrees — each worker has its own
> branch and filesystem, enabling truly parallel implementation without file conflicts.
> Progress is tracked in the **Overview** tab of the dashboard.

---

## Phase 1: Explore & Plan

Generate a short slug from `$ARGUMENTS` (kebab-case, max 40 chars).

Create the workflow — title **MUST** start with `[SWARM] `:

```bash
curl -sS -X POST $BASE/api/workflows \
  -H 'Content-Type: application/json' \
  -d "{\"id\": \"<slug>\", \"type\": \"spec\", \"complexity\": \"simple\",
       \"title\": \"[SWARM] $ARGUMENTS\",
       \"session_id\": \"${CLAUDE_SESSION_ID}\"}"
```

### 1a. Explore — built-in Explore agent

**Delegate to the built-in `Explore` agent** (Task tool, `subagent_type: "Explore"`) with thoroughness `"very thorough"`:

Pass the requirement from `$ARGUMENTS` and ask it to:
- Find all files, modules, and patterns relevant to the requirement
- Identify existing conventions, utilities, and abstractions that should be reused
- Map dependencies and integration points
- Surface any architectural constraints or existing design decisions

Do NOT write code during exploration.

### 1b. Architecture — `delivery-system-architect`

Pass the Explore agent's findings as context.

**Delegate to `delivery-system-architect`** (Task tool) with prompt:
- Task breakdown, dependencies, domain assignment
- Component boundaries and API contracts
- Data models and integration points
- Identify which domains (backend/frontend/database/tests/infra) are needed

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-system-architect"}'
```

### 1c. UX Design — `delivery-ux-designer` (if needed)

**Only if the feature has significant UI/UX components**, delegate to `delivery-ux-designer` (Task tool):

- Component hierarchy and design system integration
- User flow and interaction patterns
- Design tokens and styling conventions

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-ux-designer"}'
```

Skip this step for backend-only, infra, or database-focused work.

### 1d. Plan — built-in Plan agent

**Delegate to the built-in `Plan` agent** (Task tool, `subagent_type: "Plan"`):

Pass full context:
- The requirement from `$ARGUMENTS`
- Explore agent's findings
- System architect's component design
- UX designer's component hierarchy (if applicable)

The Plan agent will return:
- Ordered implementation steps
- Ticket breakdown with domains and priorities
- Dependencies between tickets
- Critical files for each ticket

### 1e. Design the ticket breakdown

Each ticket should have:
- **title**: concise name
- **description**: full implementation details, file paths, acceptance criteria
- **domain**: `backend` | `frontend` | `database` | `tests` | `infra` | `architecture` | `general`
- **priority**: 0 = highest (do first), higher = later
- **depends_on**: array of ticket IDs this ticket depends on (use for ordering)

### 1f. Create the mission
   ```bash
   curl -sS -X POST $BASE/api/swarm/missions \
     -H 'Content-Type: application/json' \
     -d '{"workflow_id": "<slug>", "title": "<title>", "base_branch": "main"}'
   ```

### 1g. Create tickets (batch):
   ```bash
   curl -sS -X POST $BASE/api/swarm/missions/<mission-id>/tickets/batch \
     -H 'Content-Type: application/json' \
     -d '{"tickets": [
       {"title": "...", "description": "...", "domain": "backend", "priority": 0},
       {"title": "...", "description": "...", "domain": "frontend", "priority": 1, "depends_on": ["<ticket-0-id>"]}
     ]}'
   ```

### 1h. Present the plan

Present the plan to the user. **Wait for explicit approval** before proceeding.

Transition to implement:
```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "implement"}'
```

Activate the mission:
```bash
curl -sS -X PUT $BASE/api/swarm/missions/<mission-id>/status \
  -H 'Content-Type: application/json' \
  -d '{"status": "active"}'
```

---

## Phase 2: Spawn Workers & Dispatch

### 2a. Spawn workers — one per domain needed

For each domain that has tickets, spawn a worker:

```bash
curl -sS -X POST $BASE/api/swarm/missions/<mission-id>/workers \
  -H 'Content-Type: application/json' \
  -d '{"agent_type": "delivery-backend-engineer"}'
```

The response includes `id`, `worktree_path`, and `branch_name`.

Domain routing:
- Backend / API / handlers / services → `delivery-backend-engineer`
- Frontend / UI / Svelte / components → `delivery-frontend-engineer`
- Database / migrations / schema → `delivery-database-engineer`
- Tests / coverage / QA → `delivery-qa-engineer`
- Infrastructure / CI/CD / Docker → `delivery-devops-engineer`
- Architecture / ADRs → `delivery-system-architect`
- Mixed / unclear → `delivery-implementation-expert`

### 2b. Dispatch tickets

```bash
curl -sS -X POST $BASE/api/swarm/missions/<mission-id>/dispatch
```

**Capture the response** — it contains the ticket-to-worker assignments:
```json
{"assignments": [{"ticket_id": "abc123", "worker_id": "def456"}, ...]}
```

Use this to include the correct tickets in each worker's prompt below.

### 2c. Spawn Task agents — as BACKGROUND tasks (parallel)

For **each worker**, send a Task call with `run_in_background: true` in a **single message**.
This launches all workers in parallel AND keeps you (the lead) free to monitor progress.

Each worker Task prompt MUST include:

1. The `worker_instructions` field from the spawn response (this contains the complete swarm protocol with pre-filled worker ID, worktree, branch, and mission)
2. The list of assigned tickets with full descriptions

**The spawn response includes a ready-to-use `worker_instructions` field.** Just paste it directly into the worker prompt — do NOT construct the instruction block manually. Example worker prompt:

```
<paste worker_instructions from spawn response here>

## Tickets
<list of assigned tickets with full descriptions>

## Dependencies
For tickets with depends_on: poll for TICKET_DONE matching dependency IDs. If not done — skip, work on others, poll later. If dependency FAILED — fail your dependent ticket too.
```

**CRITICAL:** Without the `worker_instructions` block, workers will NOT call `swarm_ticket_update` and ticket progress will be invisible on the dashboard. Always include it.

### 2d. Monitor progress — active polling loop

You MUST actively poll and report to the user. Do NOT go idle.

**Polling intervals:** 0-1min → every 15s | 1-3min → every 30s | 3min+ → every 60s

Each iteration:
1. `sleep <interval>`
2. Fetch status: `curl -sS $BASE/api/swarm/missions/<mission-id>/tickets` and `curl -sS $BASE/api/swarm/missions/<mission-id>/workers`
3. Print progress summary (ticket statuses, worker counts)
4. React: failed/stale worker → report to user; HELP signal → relay; all done → proceed to Phase 3

---

## Phase 3: Verify

Transition to verify:
```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "verify"}'
```

Update mission status:
```bash
curl -sS -X PUT $BASE/api/swarm/missions/<mission-id>/status \
  -H 'Content-Type: application/json' \
  -d '{"status": "verifying"}'
```

Fetch evidence collected by workers:
```bash
curl -sS $BASE/api/swarm/missions/<mission-id>/evidence
```

Delegate to `delivery-code-reviewer` — spawn in background. Pass the evidence as context. They should review the changes across all worker branches using the structured evidence trail.

If `[must_fix]` issues are found → transition back to implement, create fix-up tickets, re-dispatch.
On pass → transition to learn.

---

## Phase 4: Learn & Complete

Transition to learn phase:
```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "learn"}'
```

### Step 1 — Collect worker results

Fetch all swarm data in parallel — tickets carry the actual implementation results:

```bash
curl -sS $BASE/api/swarm/missions/<mission-id>/tickets
curl -sS $BASE/api/swarm/missions/<mission-id>/workers
curl -sS $BASE/api/swarm/missions/<mission-id>/forge
```

Review every ticket's `result` field — this is what each worker reported upon completing (or failing) their work. Also check forge entries for merge conflicts or issues.

### Step 2 — Save memory events

Save one `POST $BASE/api/events` per major decision and per non-trivial worker result. Include `refs` (mission_id, worker_id, ticket_id) for traceability. Use type `"decision"` for architectural choices and conflict resolutions, `"discovery"` for worker results. Skip trivial outcomes.

```bash
curl -sS -X POST $BASE/api/events \
  -H 'Content-Type: application/json' \
  -d '{"title": "<name>", "text": "<details>", "type": "decision|discovery", "importance": 0.7, "tags": ["swarm", "<domain>"], "refs": {"mission_id": "<mission-id>"}, "session_id": "<slug>"}'
```

### Step 3 — Create learning candidates + proposals

For each significant pattern found across workers:

```bash
CANDIDATE_ID=$(curl -sS -X POST $BASE/api/learning/candidates \
  -H 'Content-Type: application/json' \
  -d '{"detection_type": "pattern|decision|anti_pattern", "description": "...", "confidence": 0.85, "files": ["..."], "count": 1}' | jq -r '.id')

curl -sS -X POST $BASE/api/learning/proposals \
  -H 'Content-Type: application/json' \
  -d '{"candidate_id": "'$CANDIDATE_ID'", "type": "rule|adr|template|skill", "title": "...", "description": "...", "proposed_content": "...", "proposed_path": ".claude/rules/<name>.md", "confidence": 0.85, "session_id": "<slug>"}'
```

Focus on: coordination patterns, conflict resolutions, reusable ticket templates, anti-patterns. User reviews proposals in the **Learning tab**.

### Step 4 — Write governance artifacts + re-index

Write rules to `.claude/rules/`, ADRs to `docs/decisions/`, architecture notes to `docs/architecture/`. If files written: `curl -sS -X POST $BASE/api/retrieve/index`

### Step 5 — Complete mission + workflow

```bash
curl -sS -X PUT $BASE/api/swarm/missions/<mission-id>/status \
  -H 'Content-Type: application/json' \
  -d '{"status": "complete"}'
```

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

Summarize to the user: what was implemented, which workers contributed, any issues encountered, and what learning proposals are pending review.

---

## Constraints

- **NEVER** use Write, Edit, or Bash on production source files directly.
- Delegate ALL implementation work to delivery agents via Task tool.
- Always get user approval after the plan phase before spawning workers.
- The `[SWARM]` prefix in the workflow title is mandatory — it's how the Overview dashboard identifies swarm workflows.
- Each worker operates in its own git worktree — do NOT share worktrees between workers.

## Cleanup

If a mission fails or needs to be restarted, clean up resources:

```bash
# Delete the mission (removes all worktrees, workers, tickets, signals, forge entries)
curl -sS -X DELETE $BASE/api/swarm/missions/<mission-id>
```

This removes all git worktrees and associated branches automatically.
