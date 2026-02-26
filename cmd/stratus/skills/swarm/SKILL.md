---
name: swarm
description: "Multi-agent swarm workflow. Spawns isolated workers in git worktrees for truly parallel implementation. Use when the user says /swarm."
disable-model-invocation: true
argument-hint: "<feature description>"
---

# Swarm Workflow

Current session: ${CLAUDE_SESSION_ID}

```bash
BASE=http://localhost:41777
```

> **Swarm** runs delivery agents in isolated git worktrees — each worker has its own
> branch and filesystem, enabling truly parallel implementation without file conflicts.
> Progress is tracked in the **Teams** tab of the dashboard.

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

Then:
1. Explore the codebase (Read, Glob, Grep) to understand the relevant context.
2. Delegate planning to `delivery-system-architect` (Task tool) — task breakdown, dependencies, domain assignment.
3. Design the ticket breakdown. Each ticket should have:
   - **title**: concise name
   - **description**: full implementation details, file paths, acceptance criteria
   - **domain**: `backend` | `frontend` | `database` | `tests` | `infra` | `architecture` | `general`
   - **priority**: 0 = highest (do first), higher = later
   - **depends_on**: array of ticket IDs this ticket depends on (use for ordering)

4. Create the mission:
   ```bash
   curl -sS -X POST $BASE/api/swarm/missions \
     -H 'Content-Type: application/json' \
     -d '{"workflow_id": "<slug>", "title": "<title>", "base_branch": "main"}'
   ```

5. Create tickets (batch):
   ```bash
   curl -sS -X POST $BASE/api/swarm/missions/<mission-id>/tickets/batch \
     -H 'Content-Type: application/json' \
     -d '{"tickets": [
       {"title": "...", "description": "...", "domain": "backend", "priority": 0},
       {"title": "...", "description": "...", "domain": "frontend", "priority": 1, "depends_on": ["<ticket-0-id>"]}
     ]}'
   ```

6. Present the plan to the user. **Wait for explicit approval** before proceeding.

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

### 2c. Spawn Task agents — ALL in a SINGLE message (parallel)

For **each worker**, send a Task call in a **single message** (this is the key — parallel execution).

Each worker Task prompt MUST include:

```
You are a swarm worker operating in an isolated git worktree.

## Your Identity
- Worker ID: <worker-id>
- Worktree: <worktree-path>
- Branch: <branch-name>
- Mission: <mission-id>

## Your Tickets
<list of assigned tickets with full descriptions>

## Rules
1. Work ONLY within your worktree path: <worktree-path>
2. Before starting each ticket: call swarm_ticket_update(ticket_id="<id>", status="in_progress")
3. After completing each ticket: call swarm_ticket_update(ticket_id="<id>", status="done", result="<summary>")
4. If a ticket fails: call swarm_ticket_update(ticket_id="<id>", status="failed", result="<reason>")
5. Commit your changes regularly — small, atomic commits on your branch
6. When ALL tickets are done: call swarm_submit_merge(worker_id="<worker-id>")
7. Call swarm_heartbeat(worker_id="<worker-id>") at the start of your work

## Important
- Do NOT modify files outside your worktree
- Do NOT switch branches
- Your branch was created from the latest main — it has the full codebase
```

Wait for **all** Task calls to complete.

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

Delegate to `delivery-code-reviewer` and `delivery-governance-checker` — spawn them in parallel in a single message. They should review the changes across all worker branches.

If `[must_fix]` issues are found → transition back to implement, create fix-up tickets, re-dispatch.
On pass → transition to learn.

---

## Phase 4: Learn & Complete

Save memory events for key decisions:
```bash
curl -sS -X POST $BASE/api/events \
  -H 'Content-Type: application/json' \
  -d '{"title": "<decision>", "text": "<details>", "type": "decision", "importance": 0.7}'
```

Complete the mission:
```bash
curl -sS -X PUT $BASE/api/swarm/missions/<mission-id>/status \
  -H 'Content-Type: application/json' \
  -d '{"status": "complete"}'
```

Transition to complete:
```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "learn"}'
```

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

Summarize what was implemented, which workers contributed, and any issues encountered.

---

## Constraints

- **NEVER** use Write, Edit, or Bash on production source files directly.
- Delegate ALL implementation work to delivery agents via Task tool.
- Always get user approval after the plan phase before spawning workers.
- The `[SWARM]` prefix in the workflow title is mandatory — it's how the Teams dashboard tab identifies these workflows.
- Each worker operates in its own git worktree — do NOT share worktrees between workers.
