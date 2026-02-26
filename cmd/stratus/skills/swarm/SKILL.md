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

**Capture the response** — it contains the ticket-to-worker assignments:
```json
{"assignments": [{"ticket_id": "abc123", "worker_id": "def456"}, ...]}
```

Use this to include the correct tickets in each worker's prompt below.

### 2c. Spawn Task agents — as BACKGROUND tasks (parallel)

For **each worker**, send a Task call with `run_in_background: true` in a **single message**.
This launches all workers in parallel AND keeps you (the lead) free to monitor progress.

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
2. Call swarm_heartbeat(worker_id="<worker-id>") at the start of your work
3. Before starting each ticket: call swarm_ticket_update(ticket_id="<id>", status="in_progress")
4. After completing each ticket:
   a. call swarm_ticket_update(ticket_id="<id>", status="done", result="<summary>")
   b. call swarm_send_signal(from_worker="<worker-id>", type="TICKET_DONE", payload='{"ticket_id":"<id>"}')
      — this broadcasts to all workers so dependents can unblock
5. If a ticket fails:
   a. call swarm_ticket_update(ticket_id="<id>", status="failed", result="<reason>")
   b. call swarm_send_signal(from_worker="<worker-id>", type="TICKET_FAILED", payload='{"ticket_id":"<id>","reason":"<reason>"}')
6. Commit your changes regularly — small, atomic commits on your branch
7. When ALL tickets are done: call swarm_submit_merge(worker_id="<worker-id>")

## Inter-Worker Communication

You can communicate with other workers via signals. Poll and send as needed:

### Poll signals (check for messages from other workers)
Call swarm_signals(worker_id="<worker-id>") periodically — especially:
- **Before starting a ticket with dependencies** — wait for TICKET_DONE signals
  for all dependency ticket IDs. If dependencies are not yet done, work on other
  tickets first and poll again later.
- **After completing a ticket** — check if anyone sent HELP or CONFLICT signals.

### Signal types you can send
- **TICKET_DONE** — broadcast after completing a ticket (required, see Rule 4b)
- **TICKET_FAILED** — broadcast after a ticket fails (required, see Rule 5b)
- **HELP** — request help when stuck. Include context in payload:
  swarm_send_signal(from_worker="<worker-id>", type="HELP", payload='{"ticket_id":"<id>","issue":"<description>"}')
- **STATUS** — share progress update with other workers:
  swarm_send_signal(from_worker="<worker-id>", type="STATUS", payload='{"message":"<update>"}')
- **CONFLICT** — warn about potential file conflicts:
  swarm_send_signal(from_worker="<worker-id>", type="CONFLICT", payload='{"files":["<path>"],"description":"<details>"}')

### Dependency handling
If your ticket has depends_on IDs:
1. Poll signals with swarm_signals(worker_id="<worker-id>")
2. Check for TICKET_DONE signals matching your dependency IDs
3. If a dependency is not done yet — skip that ticket, work on others first
4. If a dependency FAILED — mark your dependent ticket as failed too
5. Poll again after finishing other work

## Important
- Do NOT modify files outside your worktree
- Do NOT switch branches
- Your branch was created from the latest main — it has the full codebase
```

### 2d. Monitor progress — active polling loop

Since workers run in the background, you (the lead) are free to act.
**Do NOT just wait passively.** Actively poll the API and report to the user.

**Polling procedure** — repeat this until all workers are in a terminal state (done/failed/killed).
First check after 5 seconds, then every 10 seconds:

1. Wait before next check (first iteration: 5s, subsequent: 10s):
   ```bash
   sleep 5   # first iteration only — use sleep 10 for all subsequent iterations
   ```

2. Fetch ticket + worker status in parallel:
   ```bash
   curl -sS $BASE/api/swarm/missions/<mission-id>/tickets
   ```
   ```bash
   curl -sS $BASE/api/swarm/missions/<mission-id>/workers
   ```

3. Print a progress summary to the user, e.g.:
   ```
   Swarm progress (1m 30s):
     ✓ "Create store"       — done (frontend worker)
     ▶ "Layout integration" — in_progress (frontend worker)
     ▶ "Unit tests"         — in_progress (qa worker)
     Workers: 2 active, 0 done | Tickets: 1/3 done
   ```

4. React to problems:
   - **Worker failed/stale** → report to user, ask whether to respawn
   - **All tickets done but worker didn't submit merge** → nudge or report
   - **HELP signal received** → relay to user

5. Check if done: if ALL workers are in terminal state → exit loop, proceed to Phase 3.

**CRITICAL**: You MUST keep polling. Do NOT say "I'll notify you when they complete"
and go idle. The user expects live progress updates from the lead coordinator.

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

Save structured memory events for each significant outcome. Include `refs` for traceability:

```bash
# One event per major architectural decision made during planning
curl -sS -X POST $BASE/api/events \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "<short decision name>",
    "text": "<full reasoning and outcome>",
    "type": "decision",
    "importance": 0.8,
    "tags": ["swarm", "<domain>"],
    "refs": {"mission_id": "<mission-id>"},
    "session_id": "<slug>"
  }'

# One event per non-trivial worker result (from ticket.result)
curl -sS -X POST $BASE/api/events \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "<ticket title> — result",
    "text": "<ticket.result content + any notable findings>",
    "type": "discovery",
    "importance": 0.6,
    "tags": ["swarm", "<domain>"],
    "refs": {"mission_id": "<mission-id>", "worker_id": "<worker-id>", "ticket_id": "<ticket-id>"},
    "session_id": "<slug>"
  }'

# If merge conflicts occurred — save how they were resolved
curl -sS -X POST $BASE/api/events \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "Merge conflict resolution: <branch>",
    "text": "<conflict files, resolution strategy, outcome>",
    "type": "decision",
    "importance": 0.7,
    "tags": ["swarm", "merge"],
    "refs": {"mission_id": "<mission-id>", "forge_entry_id": "<id>"},
    "session_id": "<slug>"
  }'
```

Skip trivial results — only save events that carry actionable knowledge.

### Step 3 — Create learning candidates + proposals

Analyze the collected results for patterns worth preserving. For each significant pattern:

```bash
# 3a. Save candidate
CANDIDATE_ID=$(curl -sS -X POST $BASE/api/learning/candidates \
  -H 'Content-Type: application/json' \
  -d '{
    "detection_type": "pattern|decision|anti_pattern",
    "description": "What was found across workers",
    "confidence": 0.85,
    "files": ["path/to/relevant/file.ts"],
    "count": 1
  }' | jq -r '.id')

# 3b. Generate proposal from candidate
curl -sS -X POST $BASE/api/learning/proposals \
  -H 'Content-Type: application/json' \
  -d '{
    "candidate_id": "'$CANDIDATE_ID'",
    "type": "rule|adr|template|skill",
    "title": "Short proposal title",
    "description": "Why this matters — context from swarm execution",
    "proposed_content": "# Full content of the rule/ADR/template",
    "proposed_path": ".claude/rules/<name>.md",
    "confidence": 0.85,
    "session_id": "<slug>"
  }'
```

Focus on swarm-specific learnings:
- **Coordination patterns** — how domain splitting worked, what ticket granularity was effective
- **Conflict resolutions** — which file boundaries caused merge conflicts, how to avoid them
- **Reusable ticket templates** — ticket structures that led to clean worker execution
- **Anti-patterns** — what went wrong (failed workers, blocked tickets, stale heartbeats)

Create a proposal for every insight worth preserving. The user reviews proposals in the **Learning tab**.

### Step 4 — Write governance artifacts directly

For clear, unambiguous decisions that need no review — write directly:

| Artifact type | Write to |
|--------------|----------|
| New coding rule | `.claude/rules/<name>.md` |
| Decision / ADR | `docs/decisions/<slug>-adr.md` |
| Architecture note | `docs/architecture/<slug>.md` |

If you wrote files, re-index governance:
```bash
curl -sS -X POST $BASE/api/retrieve/index
```

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
