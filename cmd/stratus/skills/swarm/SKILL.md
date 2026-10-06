---
name: swarm
description: "Multi-agent swarm workflow. Spawns isolated workers in git worktrees for truly parallel implementation. Use when the user says /swarm."
disable-model-invocation: true
allowed-tools: mcp__stratus
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

> 🎯 **Karpathy — Think Before Coding:** State assumptions explicitly, surface tradeoffs, push back on overcomplication, stop and ask when confused. See `.claude/rules/karpathy-principles.md`.

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

**Delegate to the built-in `Explore` agent** (Agent tool, `subagent_type: "Explore"`) with thoroughness `"very thorough"`:

Pass the requirement from `$ARGUMENTS` and ask it to:
- Find all files, modules, and patterns relevant to the requirement
- Identify existing conventions, utilities, and abstractions that should be reused
- Map dependencies and integration points
- Surface any architectural constraints or existing design decisions

Do NOT write code during exploration.

### 1b. Architecture — `delivery-system-architect`

Pass the Explore agent's findings as context.

**Delegate to `delivery-system-architect`** (Agent tool) with prompt:
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

**Only if the feature has significant UI/UX components**, delegate to `delivery-ux-designer` (Agent tool):

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

**Delegate to the built-in `Plan` agent** (Agent tool, `subagent_type: "Plan"`):

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

**Autopilot (Claude Code, optional):** once the mission is active, offer the user a goal that keeps this workflow running to completion without a prompt per step. Print it exactly, with the workflow id filled in:

```
/goal Stratus workflow <slug> is complete: the latest mcp__stratus__get_workflow output for <slug> in this conversation shows phase "complete" and every task done, and the last code review shown has verdict PASS. Work through the remaining phases in order; if the coordinator instructions for this workflow are not in this conversation, read .claude/skills/swarm/SKILL.md and continue <slug> from its current phase as it describes. Do not skip phases or weaken tests to make them pass. When a step needs my decision, ask me with AskUserQuestion. Stop after 40 turns.
```

Tell the user it starts when they send it, runs unattended only in auto mode, and stops with `/goal clear`; the `/stratus` pane's Autopilot button fills in the same goal. Do not wait for an answer — continue with the next phase.

---

## Phase 2: Spawn Workers & Dispatch

> 🎯 **Karpathy — Simplicity First + Surgical Changes:** Minimum code that solves the problem. Touch only what the task requires. No speculative abstractions, no "improvements" to adjacent code. See `.claude/rules/karpathy-principles.md`.

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

### 2c. Spawn Agent workers — as BACKGROUND tasks (parallel)

For **each worker**, send an Agent call with `run_in_background: true` in a **single message**.
This launches all workers in parallel AND keeps you (the lead) free to monitor progress.

Each worker Agent prompt MUST include:

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

### 2d. Monitor progress — watch, don't sleep

Claude Code blocks a foreground `sleep`, so do NOT poll in a sleep loop. Start ONE watch with the Monitor tool running this script; each line it prints reaches you as an event while you stay free to answer the user:

```bash
BASE=http://localhost:$(stratus port); M=<mission-id>; prev=""
while true; do
  if ! tj=$(curl -fsS "$BASE/api/swarm/missions/$M/tickets") || ! wj=$(curl -fsS "$BASE/api/swarm/missions/$M/workers"); then
    [ "$prev" != "API_UNREACHABLE" ] && echo "API_UNREACHABLE" && prev="API_UNREACHABLE"
    sleep 15; continue
  fi
  t=$(echo "$tj" | grep -o '"status":"[a-z_]*"' | sort | uniq -c | tr -s ' \n' ' ')
  w=$(echo "$wj" | grep -o '"status":"[a-z_]*"' | sort | uniq -c | tr -s ' \n' ' ')
  cur="tickets:$t| workers:$w"
  [ "$cur" != "$prev" ] && echo "$cur" && prev="$cur"
  # Settled only once the mission has tickets and none is still open.
  if [ -n "$t" ] && ! echo "$t" | grep -qE '"(pending|assigned|in_progress|blocked)"'; then
    echo "ALL_TICKETS_SETTLED"; break
  fi
  sleep 15
done
```

Start it with `timeout_ms: 1800000` (30 minutes, the longest a watch may run). On each event:
1. Print a progress summary for the user (ticket statuses, worker counts)
2. React: `failed`/`stale` worker → report to user; check `curl -sS $BASE/api/swarm/missions/<mission-id>/signals` and relay HELP signals
3. `API_UNREACHABLE` → tell the user the Stratus server is not answering (`stratus serve`); the watch keeps trying, so do not move on
4. `ALL_TICKETS_SETTLED` → proceed to Phase 3

A watch ends at its deadline: if tickets are still open when that notice arrives, start the watch again. Workers also notify you when they finish; where the Monitor tool is unavailable (Bedrock, Vertex, Foundry, or telemetry disabled), rely on those notifications and fetch ticket status once per notification.

---

## Phase 3: Verify

> 🎯 **Karpathy — Goal-Driven Execution:** Verify against the explicit success criteria, not style preferences. Loop until goals met; don't declare done prematurely. See `.claude/rules/karpathy-principles.md`.

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

### Step 3 — Automatic learn pipeline (runs on workflow complete)

The coordinator runs the learn pipeline async on `complete` transition: artifact build (insight-gated), knowledge update (only if artifact built), wiki autodoc (wiki-gated). Outcomes land as a `learn_pipeline` memory event on the workflow timeline. You do NOT need to create candidates or proposals manually.

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
- Delegate ALL implementation work to delivery agents via Agent tool. `Task` is a legacy alias only.
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
