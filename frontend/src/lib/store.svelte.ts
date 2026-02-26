import type { DashboardState, VersionInfo } from './types'
import { getDashboardState, getVersion, triggerUpdate } from './api'
import { wsClient } from './ws'

interface AppState {
  dashboard: DashboardState | null
  connected: boolean
  loading: boolean
  error: string | null
  version: VersionInfo | null
  updateInProgress: boolean
  updateLog: string[]
  updateError: string | null  // set when update_failed, cleared on next attempt or success
  // Swarm real-time state
  swarmUpdateCounter: number
  lastHeartbeats: Record<string, number>
}

function emitSwarmUpdate() { appState.swarmUpdateCounter++ }

// Svelte 5 reactive state — must live in .svelte.ts to use $state rune.
export const appState: AppState = $state({
  dashboard: null,
  connected: false,
  loading: true,
  error: null,
  version: null,
  updateInProgress: false,
  updateLog: [],
  updateError: null,
  swarmUpdateCounter: 0,
  lastHeartbeats: {},
})

export async function refreshDashboard() {
  try {
    appState.dashboard = await getDashboardState()
    appState.error = null
  } catch (e) {
    appState.error = e instanceof Error ? e.message : 'Unknown error'
  } finally {
    appState.loading = false
  }
}

export async function startUpdate() {
  appState.updateInProgress = true
  appState.updateLog = []
  appState.updateError = null
  try {
    await triggerUpdate()
  } catch (e) {
    // POST /system/update itself failed (network error, 409 conflict, etc.)
    appState.updateInProgress = false
    appState.updateError = e instanceof Error ? e.message : 'Failed to start update'
  }
}

export function dismissUpdate() {
  appState.updateLog = []
  appState.updateError = null
}

export function initStore() {
  wsClient.connect()

  wsClient.on('connected', () => {
    const wasUpdating = appState.updateInProgress
    appState.connected = true
    // If the WS reconnects while an update was in progress the server restarted
    // (syscall.Exec replaced the process). Treat reconnect as update completion.
    if (wasUpdating) {
      appState.updateInProgress = false
      appState.updateLog.push('Server restarted — update applied.')
    } else {
      // Refresh dashboard on every reconnect so stale data is never shown.
      refreshDashboard()
    }
  })

  wsClient.on('disconnected', () => {
    appState.connected = false
  })

  const updateTypes = ['workflow_updated', 'workflow_aborted', 'workflow_deleted', 'event_saved', 'learning_update', 'governance_indexed']
  for (const type of updateTypes) {
    wsClient.on(type, () => { refreshDashboard() })
  }

  wsClient.on('update_progress', (msg) => {
    const data = msg.payload as { msg?: string } | undefined
    if (data?.msg) appState.updateLog.push(data.msg)
  })

  wsClient.on('update_complete', (msg) => {
    const data = msg.payload as { msg?: string } | undefined
    appState.updateInProgress = false
    appState.updateError = null
    if (data?.msg) appState.updateLog.push(data.msg)
  })

  wsClient.on('update_failed', (msg) => {
    const data = msg.payload as { error?: string } | undefined
    appState.updateInProgress = false
    appState.updateError = data?.error ?? 'Unknown error'
    if (data?.error) appState.updateLog.push(`Error: ${data.error}`)
  })

  // Swarm real-time events — targeted refresh instead of full dashboard reload
  const swarmTypes = ['mission_status', 'worker_spawned', 'worker_status', 'ticket_status', 'forge_update', 'signal_sent']
  for (const type of swarmTypes) {
    wsClient.on(type, () => { emitSwarmUpdate() })
  }

  // Worker heartbeat — track timestamps for live pulse display
  wsClient.on('worker_heartbeat', (msg) => {
    const data = msg.payload as { id?: string; ts?: string } | undefined
    if (data?.id) appState.lastHeartbeats[data.id] = Date.now()
    emitSwarmUpdate()
  })

  refreshDashboard()
  getVersion().then(v => { appState.version = v }).catch(() => {})
  setInterval(() => wsClient.ping(), 30_000)
}
