# Spec: Unified Dev Loop with Hot Reload

**Workflow ID:** `spec-dev-setup-hotreload`
**Type:** spec (simple)
**Date:** 2026-04-10

## Goal

One command (`make dev`) starts both the Go backend with hot reload and the Svelte/Vite frontend with HMR. Ctrl+C cleans up both processes. Developer opens `http://localhost:5173` and edits `.go` or `.svelte` files freely — both hot reload.

## Approach

1. Add `air` (github.com/air-verse/air) as Go hot-reload tool via `.air.toml`.
2. Rewrite Vite proxy to object form with `ws: true` + `changeOrigin: true` for WebSocket support (`/api/ws`, `/api/terminal/ws`).
3. Overhaul `Makefile` `dev` target to run both processes concurrently with bash trap for clean shutdown.
4. Add `STRATUS_DEV` env var parsed in `config.Load()`; when set, `api/server.go` SPA handler returns 404 for non-`/api` paths (prevents stale embedded UI confusion).
5. Unit tests for the config + SPA handler branches (per TDD rule).
6. ADR-0002 documenting the decision + rejected alternatives (reflex, modd, gow, docker-compose).
7. README updates.

## Task List

1. **ADR-0002** — Write `docs/adr/0002-dev-loop.md` documenting decision, alternatives, constraints. *(delivery-strategic-architect)*
2. **Config.DevMode field** — Add `DevMode bool` to `Config` struct; parse `STRATUS_DEV` in `config.Load()`. *(delivery-backend-engineer)*
3. **Config test** — Table-driven test for `STRATUS_DEV` env var parsing. *(delivery-qa-engineer)*
4. **Dev-mode SPA handler branch** — `api/server.go`: when `DevMode=true`, return 404 for non-`/api` paths. Thread `cfg.DevMode` through `cmdServe`. *(delivery-backend-engineer)*
5. **SPA handler test** — `api/server_test.go` with dev-mode + non-dev-mode cases. *(delivery-qa-engineer)*
6. **Vite proxy WS support** — Rewrite `frontend/vite.config.ts` proxy to object form with `ws: true` + `changeOrigin: true`. *(delivery-frontend-engineer)*
7. **.air.toml** — Create config with explicit `exclude_dir` for `cmd/stratus/static`, `frontend`, `node_modules`, `.git`, `swarm`, `swarm-worktrees`, `tmp`. *(delivery-devops-engineer)*
8. **.gitignore** — Ensure `tmp/` is ignored (air build output). *(delivery-devops-engineer)*
9. **Makefile dev target** — Overhaul `dev`: detect missing `air`, export `STRATUS_DEV=1 STRATUS_PORT=41777`, run air + vite with `trap 'kill 0' INT TERM EXIT`. Preserve old behavior as `dev-frontend`. *(delivery-devops-engineer)*
10. **README updates** — Document new `make dev` in Make targets table + Development section; document `STRATUS_DEV` env var. *(delivery-devops-engineer)*
11. **Smoke verification** — 9-point manual checklist: both processes start, proxy works, WS works, hot reload works, Ctrl+C cleans up, non-dev regression. *(delivery-qa-engineer)*

## Sequencing

- Sequential: 2 → 3 → 4 → 5 (Config.DevMode must land before server.go uses it)
- Parallel-safe: 1, 6, 7, 8 (independent files)
- Depends on 6 + 7: 9 (Makefile needs config files in place)
- Depends on 9: 10, 11

## Risks & Open Questions

1. **Air not vendored** — contributors must `go install github.com/air-verse/air@latest` once. Makefile prints install hint on missing binary.
2. **Port collision on 41777** — `make dev` hard-codes port for Vite proxy determinism; collision surfaces as an air crash-loop. Document in README troubleshooting.
3. **Terminal WS under Vite proxy** — needs smoke verification; fallback would be dev-only direct connection.
4. **`trap 'kill 0'`** — requires bash (not dash). `bash -c` wrapper forces bash.
5. **Stale embedded UI** — dev-mode 404 guard + `STRATUS_DEV=1` env export prevents accidental serving.

## Critical Files

- `Makefile`
- `.air.toml` (new)
- `config/config.go`
- `api/server.go`
- `frontend/vite.config.ts`
- `docs/adr/0002-dev-loop.md` (new)
- `README.md`
