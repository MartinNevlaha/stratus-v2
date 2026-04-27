# Plan: Add Pagination to Evolution Runs List

## Problem

`frontend/src/routes/Evolution.svelte:236` — The runs list is hard-limited to 50 results. The UI displays "Showing X of Y run(s)" but provides no way to navigate beyond the first page. The backend API already supports `offset`, and the frontend API function (`api.ts:336`) already passes it through, but the component never uses it.

## Approach

Follow the existing "Load More" pattern from `Overview.svelte` (offset-based accumulation). No backend changes needed.

## Key Files

| File | Change |
|------|--------|
| `frontend/src/routes/Evolution.svelte` | Add load-more state, handler, and button |
| `frontend/src/lib/api.ts` | No changes needed (already supports offset) |

## Tasks

1. **Add load-more pagination to Evolution.svelte** — Add `hasMore`, `loadingMore`, `offset` state; modify `loadRuns()` to accept append mode; add "Load more" button below the table; handle status filter resets (clear offset); add separate loading indicator for incremental fetches.

## Implementation Details

- Add state variables: `loadingMore = false`, `offset = 0`
- Derive `hasMore` from `runs.length < totalCount`
- Modify `loadRuns()` to support append mode (accumulate results when loading more)
- Add "Load more" button next to the "Showing X of Y" label
- On status filter change, reset offset to 0 and reload from scratch
- Follow existing error handling pattern from `Overview.svelte:loadMorePast()`
