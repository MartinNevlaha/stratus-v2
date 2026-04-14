# ADR-0002: Unified dev loop via `air` + Vite

**Status:** Accepted
**Date:** 2026-04-10

## Context

Stratus v2 is a single-binary Go application that embeds its Svelte frontend via `//go:embed static` in `cmd/stratus/main.go`. Until now, the developer loop required two terminals:

1. `cd frontend && npm run dev` — Vite dev server with HMR on `:5173`, proxying `/api` to the Go backend.
2. `stratus serve` — the Go binary on `:41777` serving the **embedded** static assets.

This had three concrete pain points:

1. **No Go hot reload.** Any change to a `.go` file required `Ctrl+C` → `go build` → re-run. The feedback loop for backend iteration was measured in tens of seconds, not hundreds of milliseconds.
2. **Stale embedded UI.** Because the Go binary serves the embedded `cmd/stratus/static/` directory, developers hitting `http://localhost:41777` saw whatever was committed to `static/`, **not** the live Vite build. This caused repeated "why isn't my change showing up?" incidents, particularly when juggling both URLs.
3. **Vite proxy did not forward WebSockets.** The proxy was in string-shorthand form (`'/api': 'http://localhost:41777'`), which `http-proxy-middleware` interprets as HTTP-only. `/api/ws` (real-time hub) and `/api/terminal/ws` (PTY) broke under the Vite dev server.

Additionally, onboarding docs (`README.md`) said "run both" with no single entry point.

## Decision

Ship a one-command dev loop: **`make dev`** starts Go with [`air`](https://github.com/air-verse/air) file-watching hot reload **and** Vite dev server with HMR, wired together so the developer opens a single URL (`http://localhost:5173`) and can edit any `.go` or `.svelte` file freely.

Six coordinated changes:

1. **`air` as the Go file watcher.** `.air.toml` checked into repo, excluding `cmd/stratus/static`, `cmd/stratus/skills`, `cmd/stratus/agents`, `cmd/stratus/rules`, `frontend`, `node_modules`, `swarm`, `swarm-worktrees`, `docs`, `scripts`, `memory`, and `_test.go` files. Air is **not** vendored — developers install it once via `go install github.com/air-verse/air@latest`; the Makefile detects missing `air` and prints the install hint (mirrors the existing `gh` detection pattern in the `release` target).

2. **`STRATUS_DEV` env var.** Parsed in `config.Load()` into `Config.DevMode bool`. Only read from env; never persisted to `.stratus.json` (omitempty + not set in `Default()`).

3. **SPA handler dev-mode 404.** When `Config.DevMode == true`, `api/server.go:spaHandler` returns `404` with body `"dev mode: frontend served by vite on :5173"` for any non-`/api` path. This prevents the "stale embedded UI" footgun entirely: in dev, `:41777` refuses to serve HTML, so the only way to see the UI is via Vite on `:5173`, which always shows live source. API routes and WebSockets (`/api/ws`, `/api/terminal/ws`) are unaffected because they never reach the SPA handler.

4. **Vite proxy rewritten to object form with `ws: true`.** `frontend/vite.config.ts` proxy becomes `{ target, changeOrigin: true, ws: true }` so the WebSocket endpoints work through the dev proxy.

5. **Deterministic port.** `make dev` exports `STRATUS_PORT=41777` explicitly for both children. This disables the port auto-increment logic in `cmdServe` (`cmd/stratus/main.go:273-289`) so Vite's hardcoded proxy target (`http://localhost:41777`) and the actual backend port can never drift.

6. **Bash-trap process cleanup.** The `dev` target runs inside `bash -c 'trap "kill 0" INT TERM EXIT; air & (cd frontend && npm run dev) & wait'`. A single `Ctrl+C` terminates both children and their descendants; `kill 0` signals the entire process group.

Legacy behavior is preserved via two split targets: `make dev-frontend` (Vite only) and `make dev-backend` (air only).

## Alternatives considered

### `reflex` / `modd` / `gow`

- **`reflex`** — general-purpose file watcher, not Go-specific. Less maintained; no config file (CLI flags only), making multi-path includes/excludes awkward. Rejected.
- **`modd`** — YAML-configured watcher, works for Go. Adds a second config format to the repo and is materially slower than `air` on large trees. Rejected.
- **`gow`** — `go run` wrapper. No exclude config, no kill-delay tuning, no restart-on-crash — would constantly rebuild when the frontend Vite build writes into `cmd/stratus/static/`. Rejected because the embed pipeline makes exclusions mandatory; `gow` cannot express them.

`air` wins because `.air.toml` lets us explicitly `exclude_dir` the paths that would otherwise cause restart loops (especially `cmd/stratus/static`).

### `docker-compose` dev environment

Considered and rejected. Would add a container boundary around an inner loop that lives on the developer's laptop; would complicate the PTY/WebSocket path (terminal sessions via `creack/pty`) because PTYs inside containers require `tty: true` and proper signal forwarding; would slow the inner loop by 5-20× depending on filesystem driver. The Stratus single-binary philosophy makes containerization for dev a net negative.

### Serving Vite's output from disk in Go (dev build tag)

Considered: a `//go:build dev` file that replaces `//go:embed static` with a disk-backed `os.DirFS("frontend/dist")`. Rejected because (a) it requires maintaining two parallel file-server implementations, (b) it still doesn't give you Vite HMR — you'd get a file refresh but not module-level hot swap, and (c) the developer still has to run `npm run build` on every change, which defeats the purpose.

## Consequences

**Gains**

- Single command (`make dev`) starts the full loop. One `Ctrl+C` stops it.
- Backend rebuild triggered within ~500ms of saving a `.go` file (air's default debounce).
- Frontend module HMR under 100ms (Vite default).
- WebSockets (`/api/ws`, `/api/terminal/ws`) work through the Vite proxy.
- The stale-UI footgun at `http://localhost:41777` is impossible in dev mode — the SPA handler deliberately 404s.
- Port collision mode is deterministic: if `41777` is in use, air crash-loops visibly rather than the Go server silently picking `41778` and the Vite proxy targeting a dead port.

**Constraints frozen by this ADR**

- `cmd/stratus/static`, `cmd/stratus/skills`, `cmd/stratus/agents`, `cmd/stratus/rules`, `swarm`, `swarm-worktrees`, and `frontend` **must** remain in `.air.toml`'s `exclude_dir`. Re-introducing any of these paths would trigger a Go rebuild loop on every frontend build.
- `STRATUS_DEV` is **env-only**, never persisted to `.stratus.json`. Do not add it to `Default()` or any save path.
- The dev-mode SPA 404 branch (`api/server.go:spaHandler`) is load-bearing. Do not "helpfully" make it serve index.html again — that would re-open the stale-UI footgun.
- `make dev` **must** export `STRATUS_PORT=41777` explicitly. Do not remove this export.

**Costs**

- `air` is an unversioned external dev tool; contributors must `go install` it once. Makefile prints the install hint on first run. This is accepted trade-off vs. vendoring `air` into `go.mod` (rejected because it would pollute the production dependency graph for a dev-only concern).
- `trap 'kill 0'` requires bash, not a POSIX `dash` shell. The `bash -c` wrapper in the Makefile forces bash explicitly.
- Log output from air and Vite is interleaved in a single terminal with no prefixes. Acceptable for now; prefixing is a future enhancement.

**Tests**

- `config/config_test.go::TestLoad_DevMode` covers `STRATUS_DEV` env parsing (9 table rows: unset, empty, "0", "1", "true", "TRUE", "True", "false", "yes").
- `api/server_test.go::TestSpaHandler_DevMode` covers three cases: dev-mode 404, non-dev fallback-to-index, nil-cfg safety.

## References

- `Makefile` — `dev`, `dev-backend`, `dev-frontend` targets
- `.air.toml` — air configuration with exclude list
- `frontend/vite.config.ts` — proxy object form with `ws: true`
- `config/config.go` — `DevMode` field + `STRATUS_DEV` parsing
- `api/server.go` — `spaHandler` dev-mode branch
- `cmd/stratus/main.go` — dev-mode startup log in `cmdServe`
