<p align="center">
  <img src="docs/assets/stratus.png" alt="Stratus" width="400" />
</p>

<h1 align="center">stratus v2</h1>

<p align="center">A lightweight Claude Code extension framework — persistent memory, intelligent retrieval, spec-driven orchestration, and an embedded terminal dashboard.</p>

Single binary (~15 MB), zero runtime dependencies.

## vs stratus v1 (Python)

|            | Python               | Go v2                   |
| ---------- | -------------------- | ----------------------- |
| Backend    | 29 k LOC, 164 files  | ~4.5 k LOC, ~25 files   |
| Frontend   | 3.9 k LOC vanilla JS | ~2 k LOC Svelte 5       |
| Databases  | 4 SQLite, 18+ tables | 1 SQLite, 13 tables     |
| Deployment | Python + pip + venv  | Single binary           |
| State      | JSON files on disk   | DB-backed state machine |

## Features

- **Memory** — FTS5 event store with deduplication, TTL, timeline, and scoped search
- **Retrieval** — Dual-backend: Vexor (code embeddings) + FTS5 governance docs, auto-routed
- **Orchestration** — Pure state machine for spec and bug workflows with task tracking
- **Learning** — Pattern candidate detection → proposals → accept/reject decisions
- **Terminal** — PTY terminal embedded side-by-side with Overview (50/50 split) via xterm.js + WebSocket
- **STT** — Microphone button in terminal header; records audio → Whisper transcription → text injected into terminal input. Powered by [speaches](https://github.com/speaches-ai/speaches) (faster-whisper, Docker). Container lifecycle managed automatically by `stratus serve`.
- **Swarm** — Multi-agent parallel execution: isolated git worktrees, domain-based dispatch, merge queue, inter-agent signals
- **MCP** — 12 tools: `search`, `timeline`, `get_observations`, `save_memory`, `retrieve`, `index_status`, `delivery_dispatch`, `swarm_heartbeat`, `swarm_signals`, `swarm_ticket_update`, `swarm_submit_merge`, `swarm_send_signal`
- **Hooks** — `phase_guard`, `delegation_guard`, `workflow_enforcer`, `watcher` (auto-reindex on file write)

## Install

**Requirements:** Go 1.21+, Docker

```bash
go install github.com/MartinNevlaha/stratus-v2/cmd/stratus@latest
```

> If `@latest` installs an old version (Go module proxy cache), bypass it with:
> ```bash
> GOPROXY=direct go install github.com/MartinNevlaha/stratus-v2/cmd/stratus@latest
> ```
> Or pin a specific version:
> ```bash
> go install github.com/MartinNevlaha/stratus-v2/cmd/stratus@v0.3.3
> ```

Or build from source:

```bash
git clone https://github.com/MartinNevlaha/stratus-v2
cd stratus-v2
make install     # builds frontend + Go binary → GOPATH/bin
```

Available `make` targets:

| Target | Description |
|--------|-------------|
| `make install` | Build frontend + Go binary, install to `GOPATH/bin` |
| `make build` | Build frontend + Go binary, output `./stratus` |
| `make dev` | Run Vite dev server on `:5173` (hot-reload UI) |
| `make clean` | Remove `./stratus` build artifact |

Cross-compile (after `npm run build` in `frontend/`):

```bash
GOOS=linux  GOARCH=amd64 go build -o stratus-linux-amd64  ./cmd/stratus
GOOS=darwin GOARCH=arm64 go build -o stratus-darwin-arm64 ./cmd/stratus
```

## Quick Start

**Requirements:** Go 1.21+, Docker (for STT voice input)

```bash
# 1. Initialize project
#    Writes .stratus.json, .mcp.json, .claude/{skills,agents,rules,settings.json}
#    Pulls speaches Docker image for STT (~700 MB, one-time)
cd your-project
stratus init

# 2. Start server
#    Dashboard + API on :41777
#    Starts speaches STT container automatically (port 8011)
#    Ctrl+C stops server and STT container
stratus serve

# 3. Open dashboard
open http://localhost:41777
```

Hooks are registered automatically by `stratus init` — no manual `.claude/settings.json` edits needed.

## Skills

`stratus init` writes coordinator skills to `.claude/skills/`:

| Skill           | Description                                                    |
| --------------- | -------------------------------------------------------------- |
| `/spec`         | Spec-driven development: plan → implement → verify → learn     |
| `/bug`          | Bug-fixing workflow: analyze → fix → review → complete         |
| `/swarm`        | Multi-agent swarm: parallel workers in git worktrees           |
| `/learn`        | Pattern learning: detect candidates, generate proposals        |
| `/sync-stratus` | Installation health check: audits agents, skills, rules, hooks |

## API

### Memory

```
POST   /api/events              Save memory event (with deduplication)
GET    /api/events/search       FTS5 full-text search
GET    /api/events/{id}/timeline Chronological context around an event
POST   /api/events/batch        Batch fetch events by IDs
```

### Orchestration

```
POST   /api/workflows                         Start workflow (spec|bug)
GET    /api/workflows/{id}                    Get state
PUT    /api/workflows/{id}/phase              Transition phase
POST   /api/workflows/{id}/delegate           Record agent delegation
POST   /api/workflows/{id}/tasks              Set task list
POST   /api/workflows/{id}/tasks/{n}/start    Mark task in-progress
POST   /api/workflows/{id}/tasks/{n}/complete Mark task done
DELETE /api/workflows/{id}                    Abort workflow
GET    /api/workflows/{id}/dispatch           Dispatch info for MCP
```

### Retrieval

```
GET    /api/retrieve            Semantic search (code + governance, auto-routed)
GET    /api/retrieve/status     Index freshness and Vexor availability
POST   /api/retrieve/index      Trigger re-index of governance docs
```

### Learning

```
GET    /api/learning/candidates             List pattern candidates
POST   /api/learning/candidates             Save a candidate
GET    /api/learning/proposals              List proposals
POST   /api/learning/proposals/{id}/decide  Accept / reject / snooze / ignore
```

### Swarm

```
POST   /api/swarm/missions                       Create mission
GET    /api/swarm/missions                       List missions
GET    /api/swarm/missions/{id}                  Get mission (+ workers, tickets, forge)
PUT    /api/swarm/missions/{id}/status           Update mission status
DELETE /api/swarm/missions/{id}                  Cleanup + delete mission

POST   /api/swarm/missions/{id}/workers          Spawn worker (creates git worktree)
GET    /api/swarm/missions/{id}/workers          List workers
POST   /api/swarm/workers/{id}/heartbeat         Worker heartbeat
PUT    /api/swarm/workers/{id}/status            Update worker status

POST   /api/swarm/missions/{id}/tickets          Create ticket
POST   /api/swarm/missions/{id}/tickets/batch    Batch create tickets
GET    /api/swarm/missions/{id}/tickets          List tickets
PUT    /api/swarm/tickets/{id}/status            Update ticket status + result

POST   /api/swarm/missions/{id}/dispatch         Run dispatch algorithm
POST   /api/swarm/signals                        Send signal between workers
GET    /api/swarm/workers/{id}/signals           Poll unread signals

POST   /api/swarm/forge/submit                   Submit worker branch to forge
GET    /api/swarm/missions/{id}/forge            List forge entries
```

### Other

```
GET    /api/dashboard/state     Aggregated state for the dashboard
POST   /api/stt/transcribe      Whisper proxy (multipart audio)
GET    /api/stt/status          STT endpoint availability
GET    /api/health              Health check
WS     /api/ws                  Real-time dashboard updates
WS     /api/terminal/ws         PTY terminal I/O
```

## Configuration

`.stratus.json` in project root (created by `stratus init`):

```json
{
  "port": 41777,
  "data_dir": "~/.stratus/data",
  "project_root": ".",
  "vexor": {
    "binary_path": "vexor",
    "model": "nomic-embed-text-v1.5",
    "timeout_sec": 15
  },
  "stt": {
    "endpoint": "http://localhost:8011",
    "model": "Systran/faster-whisper-small"
  }
}
```

`stt.model` controls which faster-whisper model the speaches container loads. Larger models are more accurate but slower to start:

| Model | Size | Notes |
|-------|------|-------|
| `Systran/faster-whisper-small` | ~244 MB | Default, fast |
| `Systran/faster-whisper-medium` | ~769 MB | Better accuracy |
| `Systran/faster-whisper-large-v3` | ~3 GB | Best accuracy |

Environment overrides: `STRATUS_PORT`, `STRATUS_DATA_DIR`.

## Architecture

```
cmd/stratus/        CLI entry point, go:embed for skills
config/             Config loading (.stratus.json + env)
db/                 SQLite: memory events, governance docs, learning, workflows, swarm
orchestration/      Pure state machine (spec + bug workflows)
swarm/              Multi-agent swarm: worktree management, dispatch, signals
api/                HTTP server, REST routes, WebSocket hub
mcp/                MCP stdio server (JSON-RPC, 12 tools)
hooks/              Claude Code hook handlers
terminal/           PTY session management + WebSocket I/O
vexor/              Vexor CLI wrapper for code embeddings
frontend/           Svelte 5 dashboard
```

### Database schema (1 SQLite, 13 tables)

| Table           | Purpose                        |
| --------------- | ------------------------------ |
| `events`        | Memory event store             |
| `events_fts`    | FTS5 index on events           |
| `sessions`      | Claude Code session tracking   |
| `docs`          | Governance document chunks     |
| `docs_fts`      | FTS5 index on docs             |
| `candidates`    | Learning pattern candidates    |
| `proposals`     | Learning proposals             |
| `workflows`     | Orchestration state            |
| `missions`      | Swarm missions                 |
| `workers`       | Swarm workers + worktree info  |
| `tickets`       | Swarm tickets with deps graph  |
| `signals`       | Inter-worker messaging         |
| `forge_entries` | Merge queue entries            |

## Swarm (Multi-Agent Parallel Execution)

The `/swarm` skill spawns multiple Claude Code agents in isolated git worktrees, enabling truly parallel implementation without file conflicts. Progress is tracked in the **Teams** tab of the dashboard.

### How it works

1. **Plan** — You describe a feature (`/swarm implement auth system`). The lead agent explores the codebase, breaks work into tickets with domains and dependencies, and presents the plan for approval.
2. **Spawn** — One worker per domain is spawned. Each worker gets its own git worktree (`.stratus/worktrees/`) and branch (`swarm/<mission>/<worker>`).
3. **Dispatch** — Tickets are assigned to workers by domain matching (backend, frontend, database, tests, infra, architecture). Workers execute in parallel via Claude Code's Task tool.
4. **Forge** — Workers submit completed branches to the merge queue.
5. **Verify** — Code review agents check the combined result.
6. **Complete** — Cleanup, learn, done.

### Terminology

| Term         | Description                                                              |
| ------------ | ------------------------------------------------------------------------ |
| **Mission**  | Group of related tickets for coordinated delivery. Tied to a workflow.   |
| **Ticket**   | Atomic work unit. Has domain, priority, dependencies.                    |
| **Worker**   | Agent process with its own git worktree. Isolated, heartbeat-monitored.  |
| **Lead**     | Coordinating agent (the `/swarm` skill). Creates missions, dispatches.   |
| **Sentinel** | Background health monitor (marks stale/failed workers).                  |
| **Forge**    | Merge queue — processes worker branches into an integration branch.      |
| **Signal**   | Persistent message between agents (TICKET_DONE, HELP, CONFLICT, etc.).  |
| **Dispatch** | Intelligent ticket-to-worker assignment by domain + priority + deps.     |

### Worker domains

Each ticket has a `domain` that determines which agent type handles it:

| Domain         | Agent type                        |
| -------------- | --------------------------------- |
| `backend`      | `delivery-backend-engineer`       |
| `frontend`     | `delivery-frontend-engineer`      |
| `database`     | `delivery-database-engineer`      |
| `tests`        | `delivery-qa-engineer`            |
| `infra`        | `delivery-devops-engineer`        |
| `architecture` | `delivery-system-architect`       |
| `general`      | `delivery-implementation-expert`  |

### Worker lifecycle

```
pending → active → done
                 → failed
                 → stale (no heartbeat)
                 → killed (aborted)
```

Workers communicate with the server via 5 MCP tools: `swarm_heartbeat`, `swarm_signals`, `swarm_ticket_update`, `swarm_submit_merge`, `swarm_send_signal`.

### Git worktrees

Each worker operates in an isolated git worktree:

```
.stratus/worktrees/
  swarm-<mission>-<worker-1>/    ← branch: swarm/<mission>/<worker-1>
  swarm-<mission>-<worker-2>/    ← branch: swarm/<mission>/<worker-2>
```

Worktrees are created automatically when workers spawn and cleaned up when the mission completes or is deleted.

### Usage

```bash
# In Claude Code, invoke the swarm skill:
/swarm implement user authentication with JWT

# The lead agent will:
# 1. Explore your codebase
# 2. Break work into tickets
# 3. Present the plan — wait for your approval
# 4. Spawn workers and execute in parallel
# 5. Verify and complete
```

### Dashboard

The **Teams** tab shows active missions with:
- Worker status indicators (colored dots)
- Ticket progress bars
- Forge merge queue status
- Real-time updates via WebSocket

## STT (Voice Input)

`stratus serve` automatically manages a [speaches](https://github.com/speaches-ai/speaches) Docker container that runs faster-whisper locally. No cloud API keys required.

- Click the microphone button in the terminal header to record
- Recording stops on second click → audio transcribed → text inserted at the terminal cursor
- The container (`stratus-stt`) is stopped and removed when `stratus serve` exits

**Manual container management** (if needed):

```bash
# Start manually
docker run -d --name stratus-stt -p 8011:8000 \
  -e WHISPER__MODEL=Systran/faster-whisper-small \
  -v stratus-whisper-cache:/root/.cache/huggingface \
  ghcr.io/speaches-ai/speaches:latest-cpu

# Stop
docker stop stratus-stt && docker rm stratus-stt
```

## Frontend Development

```bash
cd frontend
npm install
npm run dev      # hot-reload dev server on :5173 (proxies API to :41777)
npm run build    # builds to ../cmd/stratus/static/ (required before go build)
```

## Publishing a Release

The frontend is committed to the repo as pre-built static files so that `go install` works without Node.js on the user's machine. Every release must follow this exact order:

```bash
# 1. Make your changes (Go, Svelte, agents, skills, …)

# 2. Build frontend and install binary locally to test
make install

# 3. Test the changes
stratus serve   # smoke-test at http://localhost:41777

# 4. Commit everything — including the built static files
git add cmd/stratus/static/ <other changed files>
git commit -m "feat: your message"

# 5. Tag the release (semver — go install @latest picks the highest tag)
git tag v0.X.Y

# 6. Push branch + tag
git push origin main --tags
```

> **Why commit `cmd/stratus/static/`?**
> `go install` only runs `go build` — it never runs `npm`. The frontend must be
> pre-built and committed so the embedded `go:embed static` picks it up at compile time.

> **Why tag?**
> Without a semver tag, the Go module proxy serves a cached pseudo-version.
> A new tag guarantees `go install @latest` resolves to the new release immediately.

## MCP Tools

Register in `.mcp.json` (created by `stratus init`):

```json
{
  "mcpServers": {
    "stratus": {
      "type": "stdio",
      "command": "stratus",
      "args": ["mcp-serve"]
    }
  }
}
```

Available tools:

| Tool | Description |
|------|-------------|
| `search` | FTS5 full-text search across memory events |
| `timeline` | Chronological context around a memory event |
| `get_observations` | Batch fetch full event details by IDs |
| `save_memory` | Save a memory event for future search |
| `retrieve` | Semantic search across code (Vexor) and governance docs |
| `index_status` | Check index freshness and backend availability |
| `delivery_dispatch` | Get delivery phase briefing and delegation instructions |
| `swarm_heartbeat` | Worker heartbeat signal (keeps worker marked active) |
| `swarm_signals` | Poll unread signals for a worker |
| `swarm_ticket_update` | Update ticket status (in_progress / done / failed) |
| `swarm_submit_merge` | Submit worker branch to the Forge merge queue |
| `swarm_send_signal` | Send a signal to another worker or broadcast |
