# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build the binary
go build -o stratus ./cmd/stratus

# Cross-compile
GOOS=linux  GOARCH=amd64 go build -o stratus-linux-amd64  ./cmd/stratus
GOOS=darwin GOARCH=arm64 go build -o stratus-darwin-arm64 ./cmd/stratus

# Run the server (dashboard + API on :41777)
./stratus serve

# Run tests
go test ./...

# Run tests for a single package
go test ./api/...
go test ./orchestration/...

# Frontend dev server (hot-reload on :5173)
cd frontend && npm install && npm run dev

# Build frontend into ../static/ (must do before go build to embed latest UI)
cd frontend && npm run build

# Type-check frontend
cd frontend && npm run check
```

## Architecture

Stratus is a single-binary Claude Code extension framework. The binary embeds four things via `go:embed` in `cmd/stratus/main.go`: the Svelte frontend (built to `cmd/stratus/static/`), skills (`cmd/stratus/skills/`), agents (`cmd/stratus/agents/`), and governance rules (`cmd/stratus/rules/`).

**Key architectural points:**

- **Single binary, no runtime deps**: all static assets, skills, agents, and rules are embedded at compile time. The frontend must be built (`cd frontend && npm run build`) before `go build` to include the latest UI.
- **MCP server is a thin HTTP proxy**: `mcp/` does not talk to the database directly — it translates MCP JSON-RPC calls into HTTP requests to the running API server (`http://localhost:41777`).
- **Hooks are stateless processes**: `hooks/` reads JSON from stdin, writes a `{"continue": bool, "reason": string}` decision to stdout, and exits 0 (allow) or 2 (block). They never hold persistent state.
- **Database is shared state**: `db/DB` is the single SQLite connection passed to all subsystems. `db/schema.go` contains the full DDL. FTS5 virtual tables (`events_fts`, `docs_fts`) are kept in sync via SQL triggers defined in the schema.
- **Orchestration is a pure state machine**: `orchestration/state.go` defines `validTransitions` — a map of allowed phase-to-phase moves per workflow type. The coordinator (`orchestration/coordinator.go`) enforces this before any DB write.
- **WebSocket hub**: `api/ws_hub.go` broadcasts real-time updates to all connected dashboard clients. The terminal WebSocket in `terminal/ws_terminal.go` is separate and manages PTY I/O.
- **Config layering**: defaults → `STRATUS_PORT`/`STRATUS_DATA_DIR` env vars → `.stratus.json` in the working directory. `config.Load()` is called at startup; there is no hot-reload.

## Package Map

| Package | Responsibility |
|---------|---------------|
| `cmd/stratus` | CLI entry point; embeds static assets and writes them to disk on `init`/`refresh` |
| `config` | Loads `.stratus.json` + env overrides into `Config` struct |
| `db` | SQLite wrapper: schema, memory events, governance docs, learning, workflows, swarm |
| `orchestration` | Phase state machine for `spec` and `bug` workflow types |
| `swarm` | Multi-agent swarm: worktree management, dispatch engine, signal bus |
| `api` | HTTP server, all REST routes, WebSocket hub, SPA handler |
| `mcp` | MCP stdio server (JSON-RPC); proxies all tool calls to the HTTP API |
| `hooks` | `phase_guard`, `delegation_guard`, `workflow_enforcer` hook handlers |
| `terminal` | PTY session management + WebSocket I/O via `creack/pty` |
| `vexor` | CLI wrapper around the external `vexor` binary for code embeddings |
| `frontend` | Svelte 5 + TypeScript + xterm.js dashboard, built with Vite |

## Workflow Phase Transitions

**Spec workflow** (simple): `plan → implement → verify → learn → complete`
**Spec workflow** (complex): `plan → discovery → design → governance → accept → implement → verify → learn → complete`
**Bug workflow**: `analyze → fix → review → complete` (review can loop back to fix)

Phase transitions are validated by `orchestration.ValidateTransition()` before any state is persisted.

## Frontend Build Contract

The Vite build output goes to `frontend/dist/`, which Vite is configured to output to `cmd/stratus/static/`. The Go binary embeds `cmd/stratus/static` via `//go:embed static`. **If you change frontend code, rebuild the frontend before `go build` or the embedded UI will be stale.**

## Key Conventions

- All HTTP handlers live in `api/routes_*.go` files, grouped by domain.
- The `db.DB` struct wraps the raw SQLite connection; all queries are in the `db/` package.
- Hook errors are silently ignored (fail-open) — hooks call `Allow()` on any parse error to avoid blocking Claude.
- The `retrieve` endpoint auto-routes: queries containing code-like terms go to Vexor; others go to FTS5 governance docs.
