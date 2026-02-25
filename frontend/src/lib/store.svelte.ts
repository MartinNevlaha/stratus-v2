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
}

// Svelte 5 reactive state — must live in .svelte.ts to use $state rune.
export const appState: AppState = $state({
  dashboard: null,
  connected: false,
  loading: true,
  error: null,
  version: null,
  updateInProgress: false,
  updateLog: [],
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
  await triggerUpdate()
}

export function initStore() {
  wsClient.connect()

  wsClient.on('connected', () => {
    appState.connected = true
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
    if (data?.msg) appState.updateLog.push(data.msg)
  })

  wsClient.on('update_failed', (msg) => {
    const data = msg.payload as { error?: string } | undefined
    appState.updateInProgress = false
    if (data?.error) appState.updateLog.push(`Error: ${data.error}`)
  })

  refreshDashboard()
  getVersion().then(v => { appState.version = v }).catch(() => {})
  setInterval(() => wsClient.ping(), 30_000)
}
