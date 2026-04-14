# Spec: Code Quality Finding Status Lifecycle

## Goal
Track lifecycle of code quality findings (pending/rejected/applied), let the "Open in Terminal" agent flow mark an item applied on completion, and add a status filter to the UI.

## Tasks

1. **DB schema + methods** — Add `status` column to `code_findings` (default `pending`, CHECK constraint), extend `CodeFinding` struct, update `SaveCodeFinding`, add `status` filter to `ListCodeFindings`, add `UpdateCodeFindingStatus(ctx, id, status)`.
2. **DB tests** — Default status on insert, status filter in list, UpdateCodeFindingStatus happy + invalid status.
3. **API endpoint** — `PUT /api/code-analysis/findings/{id}/status` (body `{"status":"rejected|applied"}`), extend GET findings with `?status=` filter, register route.
4. **API tests** — PUT status (200/400/404), GET filter by status.
5. **MCP tool** — `code_quality_finding_update(finding_id, status)` proxying to the API endpoint.
6. **Frontend types + slash command** — Add `status` to `CodeFinding`; in `buildSlashCommand` inject `finding_id={id}` and append instruction to call `code_quality_finding_update` with status=applied at end of flow.
7. **Frontend UI** — Status filter dropdown (All/Pending/Rejected/Applied), status badge per row, Reject button (pending only, calls PUT), hide/disable "Open in Terminal" for non-pending items; refresh list after mutation.
8. **Manual smoke test** — Verify default pending, reject transition, terminal-flow applied transition, filter behavior.

## Key files
- db/schema.go, db/code_analysis.go
- api/routes_code_analysis.go
- mcp/tools.go
- frontend/src/lib/types.ts, frontend/src/lib/findingFlow.ts
- frontend/src/routes/CodeQuality.svelte
