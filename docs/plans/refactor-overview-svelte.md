# Plan: Refactor Overview.svelte

## Problem
`Overview.svelte` is 1336 lines with 15+ state variables, 20+ functions, 7 async operations, 3 reactive effects, Guardian alerts, swarm details, past items pagination, delegation — all in one monolithic component.

## Strategy
Extract 4 self-contained sub-components, each owning their own state, API calls, and styles. Overview becomes a thin orchestrator (~150 lines).

## Components to Extract

### 1. `UpdatePanel.svelte` (~30 lines)
- **State**: reads from `appState.updateInProgress`, `appState.updateLog`, `appState.updateError`
- **Props**: none (reads store directly, like existing `AnalysisPanel`)
- **Events**: calls `dismissUpdate()`
- **Lines moved**: 338-361 (template) + 860-880 (styles)

### 2. `GuardianWidget.svelte` (~250 lines)
- **State**: `guardianAlerts`, `guardianExpanded`, `guardianScanning`, `copiedFilePath`, `delegateAlertId`, `availableAgents`, `delegating`
- **API calls**: `loadGuardianAlerts`, `dismissAlert`, `deleteAlert`, `dismissAll`, `killWorker`, `triggerScan`, `openDelegateMenu`, `sendToAgent`
- **Helper**: `severityIcon`, `relativeTime`, `copyFilePath`
- **Props**: none (owns all state + reactivity via `$effect` on `appState.guardianAlertCount`)
- **Lines moved**: 29-128 (script) + 367-445 (template) + 1143-1335 (styles)

### 3. `SwarmDetail.svelte` (~200 lines)
- **State**: `swarmDetails`, `swarmLoading`, `swarmViews`, `expandedTickets`, `_heartbeatTick`
- **API calls**: `loadSwarmDetail`, `refreshSwarmDetails` (via `$effect`)
- **Helper**: `workerStatusColor`, `ticketIcon`, `workerTicketCounts`, `relativeTime`
- **Props**: `wf: WorkflowState`, `missions: SwarmMission[]`
- **Lines moved**: 564-653 (template) + 900-1041 (styles)

### 4. `PastWorkflows.svelte` (~120 lines)
- **State**: `pastItems`, `pastTotal`, `pastOffset`, `pastLoading`, `pastLoadSeq`
- **API calls**: `loadPastItems`, `loadMorePast`
- **Props**: none (owns state, `displayType` helper)
- **Lines moved**: 732-781 (template) + 884-897 (styles)
- **Callback**: `onWorkflowsChanged: () => void` to trigger parent refresh after delete

### 5. Slimmed `Overview.svelte` (~150 lines script + ~120 lines template + ~80 lines shared styles)
- Keeps: `allWorkflows`, `missions`, `confirmDelete`, `copiedId`, `expandedTasks`, `expandedPlans`, `expandedDesigns`
- Keeps: `loadWorkflows`, `handleDelete`, `cancelDelete`, `copyToClipboard`, `displayType`
- Keeps: `onMount`, 2 `$effect`s (dashboard refresh, swarm refresh)
- Delegates: Guardian, UpdatePanel, SwarmDetail, PastWorkflows

## Tasks
1. Create `UpdatePanel.svelte` — extract update progress panel
2. Create `GuardianWidget.svelte` — extract guardian alerts with all actions
3. Create `SwarmDetail.svelte` — extract swarm mission detail view
4. Create `PastWorkflows.svelte` — extract past items with pagination
5. Slim down `Overview.svelte` — import and use extracted components
6. Verify: `cd frontend && npm run check` passes
7. Verify: `cd frontend && npm run build` passes
