# Plan: Auto-Refresh for Running Evolution Runs

## Problem

When an evolution run has status `running`, the UI never polls or subscribes to updates. Users must manually click Refresh to see when a run completes. For long-running operations this is a significant UX gap.

## Solution

Add conditional `setInterval` polling in `Evolution.svelte` that automatically refreshes the run list every 3 seconds when any run has status `running`, and stops polling when all runs are in a terminal state (`completed`, `failed`, `timeout`).

## Implementation Details

### File: `frontend/src/routes/Evolution.svelte`

1. **Import `onMount`** from Svelte
2. **Add a `hasRunningRun` derived value** — checks if any run in the list has `status === 'running'`
3. **Add `setInterval` polling** in `$effect` (Svelte 5 style, reactive to `hasRunningRun`):
   - When `hasRunningRun` is true → start polling `loadRuns()` + `loadStatus()` every 3 seconds
   - When `hasRunningRun` is false → stop polling
   - Store interval ref, clear on cleanup
4. **Also auto-poll after triggering** — the `handleTrigger()` function already calls `loadRuns()` + `loadStatus()` once; the polling loop will naturally pick up the running state and continue refreshing
5. **Also auto-poll on initial load** — if a run is already running when the page loads, polling starts immediately

### Pattern alignment

This follows the `Overview.svelte` pattern (line 310-321): `setInterval` in `onMount`/`$effect` with cleanup. The `Wiki.svelte` `setTimeout`-chain pattern is also valid but `setInterval` is simpler for a recurring fixed-interval poll.

### Error handling

Existing `loadRuns()` already surfaces errors via the `error` state variable. The polling calls reuse the same function, so no additional error handling is needed.

### Governance compliance

- No ADR conflicts
- Follows existing codebase polling patterns
- Surgical change — only `Evolution.svelte` is modified
- Frontend build contract: rebuild frontend before go build

## Tasks

1. Add auto-refresh polling for running evolution runs in `Evolution.svelte`
