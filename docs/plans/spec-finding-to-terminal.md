# Plan: Code Quality Finding → Overview Terminal Prefill

## Goal
Clicking a Code Quality finding opens the Overview tab and prefills a `/bug` or `/spec` slash-command in the embedded xterm terminal. **Never auto-submits** — user always presses Enter.

## Design

### Flow-type mapping (`frontend/src/lib/findingFlow.ts`, new)
- `warning`/`error` + `{error_handling, dead_code, anti_pattern}` → `/bug` (precedence wins)
- `info`/`warning` + `{duplication, complexity, coverage_gap}` → `/spec`
- Fallback `/bug`
- Per-card `<select>` override (Auto / bug / spec) in expanded detail
- `buildSlashCommand(flow, finding)` → single line, format: `/<flow> <file>:<lstart>[-<lend>] <title> — <desc>` (desc truncated ~200 chars)
- `sanitizeTerminalPayload(s)` strips `\r`, `\n` as a code invariant (governance)

### State (`frontend/src/lib/store.svelte.ts`)
- Add `activeTab: TabId` (hoist from App.svelte local state)
- Add `pendingTerminalInput: string | null`
- Helpers: `setActiveTab(id)`, `requestTerminalPrefill(text)`

### App.svelte
- Replace local `activeTab` `$state` with `appState.activeTab` reads and `setActiveTab` writes. No behavioural change.

### Terminal.svelte
- `$effect` watches `appState.pendingTerminalInput`. When set AND WS `OPEN`:
  - Sanitize (strip CR/LF defensively)
  - Send via existing `{type:'input', data:{id: sessionId, data: text}}` path (same as `term.onData`)
  - `term.focus()`, clear signal
- If WS not open → leave value; effect re-fires on `connected` change.

### CodeQuality.svelte
- In expanded `.finding-detail` panel (NOT summary row):
  - `<select>` flow override (Auto / bug / spec)
  - Button **"Open in Terminal"** → `requestTerminalPrefill(buildSlashCommand(flow, f))` + `setActiveTab('overview')`

### Tests (Vitest, new `findingFlow.test.ts`)
- Table-driven severity×category mapping
- Default fallback
- Overlap precedence
- `sanitizeTerminalPayload` strips `\r`, `\n`, `\r\n`
- `buildSlashCommand` snapshot: starts `/bug ` or `/spec `, no CR/LF

## Governance
- `workflow-governance.md`: compliant — user presses Enter; no auto-submit, no Task-tool delegation before `register_workflow`.
- `error-handling.md`: WS send catch blocks use `e instanceof Error` narrowing; toast on failure, no silent swallow.
- `tdd-requirements.md`: Vitest covers mapping + sanitizer.
- No backend changes, no DB migrations, no governance files touched.

## Critical Files
- `frontend/src/lib/store.svelte.ts` (modify)
- `frontend/src/App.svelte` (modify — activeTab hoist)
- `frontend/src/components/Terminal.svelte` (modify — $effect consumer)
- `frontend/src/routes/CodeQuality.svelte` (modify — UI + handler)
- `frontend/src/lib/findingFlow.ts` (new)
- `frontend/src/lib/findingFlow.test.ts` (new)

## Risk
- `activeTab` hoist is the only touchy refactor — every existing `activeTab ===` comparison in App.svelte must be updated.
- `$effect` must guard against firing before WS open.
- Manual verification: network frame inspector confirms no `\r`/`\n` in injected payload.

## Ordered Tasks
1. Add `activeTab` + `pendingTerminalInput` + helpers to `store.svelte.ts`
2. Hoist `activeTab` from `App.svelte` to the store
3. Create `frontend/src/lib/findingFlow.ts` (`flowForFinding`, `buildSlashCommand`, `sanitizeTerminalPayload`)
4. Add Vitest suite `findingFlow.test.ts`
5. Add consumer `$effect` in `Terminal.svelte` (sanitize → WS send → focus → clear)
6. Add `<select>` override + "Open in Terminal" button in expanded finding detail in `CodeQuality.svelte`
7. Manual end-to-end verification in dev server + rebuild frontend
