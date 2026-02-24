import type { DashboardState } from './types'
import { getDashboardState } from './api'
import { wsClient } from './ws'

interface AppState {
  dashboard: DashboardState | null
  connected: boolean
  loading: boolean
  error: string | null
}

// Svelte 5 reactive state — must live in .svelte.ts to use $state rune.
export const appState: AppState = $state({
  dashboard: null,
  connected: false,
  loading: true,
  error: null,
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

export function initStore() {
  wsClient.connect()

  wsClient.on('connected', () => {
    appState.connected = true
  })

  wsClient.on('disconnected', () => {
    appState.connected = false
  })

  const updateTypes = ['workflow_updated', 'workflow_aborted', 'event_saved', 'learning_update', 'governance_indexed']
  for (const type of updateTypes) {
    wsClient.on(type, () => { refreshDashboard() })
  }

  refreshDashboard()
  setInterval(() => wsClient.ping(), 30_000)
}
