import type { DashboardState, Language, VersionInfo } from './types'
import { getDashboardState, getLanguage, getVersion, triggerUpdate } from './api'
import { wsClient } from './ws'

export type TabId = 'overview' | 'agents' | 'memory' | 'retrieval' | 'wiki' | 'settings' | 'terminal'

interface AppState {
  dashboard: DashboardState | null
  connected: boolean
  loading: boolean
  error: string | null
  version: VersionInfo | null
  updateInProgress: boolean
  updateLog: string[]
  updateError: string | null
  swarmUpdateCounter: number
  workflowActivityCounter: number
  toasts: Toast[]
  lastHeartbeats: Record<string, number>
  guardianAlertCount: number
  activeTab: TabId
  pendingTerminalInput: string | null
  language: Language
}

export interface Toast {
  id: number
  kind: 'info' | 'warn' | 'error' | 'success'
  text: string
  href?: string
}

let toastSeq = 0
const TOAST_MS = 8000

export function pushToast(kind: Toast['kind'], text: string, href?: string) {
  const id = ++toastSeq
  appState.toasts = [...appState.toasts.slice(-4), { id, kind, text, href }]
  setTimeout(() => dismissToast(id), TOAST_MS)
}

export function dismissToast(id: number) {
  appState.toasts = appState.toasts.filter(t => t.id !== id)
}

function emitSwarmUpdate() { appState.swarmUpdateCounter++ }

const TAB_KEY = 'stratus-active-tab'
const TAB_IDS: TabId[] = ['overview', 'agents', 'memory', 'retrieval', 'wiki', 'settings', 'terminal']

function loadActiveTab(): TabId {
  try {
    const saved = localStorage.getItem(TAB_KEY)
    if (saved && (TAB_IDS as string[]).includes(saved)) return saved as TabId
  } catch {
    // storage unavailable: use the default tab
  }
  return 'overview'
}

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
  workflowActivityCounter: 0,
  toasts: [],
  lastHeartbeats: {},
  guardianAlertCount: 0,
  activeTab: loadActiveTab(),
  pendingTerminalInput: null,
  language: 'en',
})

export function setActiveTab(id: TabId) {
  appState.activeTab = id
  try {
    localStorage.setItem(TAB_KEY, id)
  } catch {
    // not persisted; the tab just resets on reload
  }
}

export function requestTerminalPrefill(text: string) {
  appState.pendingTerminalInput = text
}

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

  const updateTypes = ['workflow_updated', 'workflow_aborted', 'workflow_deleted', 'event_saved', 'governance_indexed']
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
  
  wsClient.on('worker_heartbeat', (msg) => {
    const data = msg.payload as { id?: string; ts?: string } | undefined
    if (data?.id) appState.lastHeartbeats[data.id] = Date.now()
    emitSwarmUpdate()
  })

  // Agent/task activity of normal workflows
  const activityTypes = ['agent_started', 'agent_status', 'agent_activity', 'task_started', 'task_completed', 'workflow_log']
  for (const type of activityTypes) {
    wsClient.on(type, () => { appState.workflowActivityCounter++ })
  }

  // Things worth interrupting for: a failing review, a finished workflow.
  wsClient.on('agent_status', (msg) => {
    const d = msg.payload as { workflow_id?: string; agent_id?: string; status?: string; verdict?: string; agent_type?: string } | undefined
    const href = d?.workflow_id ? `#/workflow/${encodeURIComponent(d.workflow_id)}` : undefined
    const who = (d?.agent_type ?? 'agent').replace('delivery-', '')
    if (d?.verdict === 'FAIL') pushToast('warn', `Review FAILED (${who})`, href)
    else if (d?.status === 'failed') pushToast('error', `${who} marked as failed`, href)
  })
  // Alerts that mean something is stuck or over budget; routine health alerts stay in Guardian.
  const toastAlertTypes = new Set(['stale_agent', 'token_budget', 'stale_worker', 'reviewer_timeout', 'agent_failed', 'review_failed'])
  wsClient.on('guardian_alert', (msg) => {
    const a = msg.payload as { type?: string; severity?: string; message?: string; metadata?: { workflow_id?: string } } | undefined
    if (!a?.type || !toastAlertTypes.has(a.type) || !a.message) return
    const wfId = a.metadata?.workflow_id
    pushToast(a.severity === 'critical' ? 'error' : 'warn', a.message, wfId ? `#/workflow/${encodeURIComponent(wfId)}` : undefined)
  })
  // Claude is waiting for the human: a permission prompt, an idle prompt or a question.
  wsClient.on('workflow_waiting', (msg) => {
    const d = msg.payload as { workflow_id?: string; title?: string; message?: string } | undefined
    if (!d?.workflow_id) return
    const what = d.message ? `: ${d.message}` : ''
    pushToast('info', `${d.title || d.workflow_id} is waiting for you${what}`.slice(0, 160), `#/workflow/${encodeURIComponent(d.workflow_id)}`)
  })
  wsClient.on('workflow_completed', (msg) => {
    const d = msg.payload as { workflow_id?: string } | undefined
    if (d?.workflow_id) pushToast('success', `Workflow ${d.workflow_id} completed`, `#/workflow/${encodeURIComponent(d.workflow_id)}`)
  })

  wsClient.on('guardian_alert', () => {
    appState.guardianAlertCount++
  })

  refreshDashboard()
  getVersion().then(v => { appState.version = v }).catch(() => {})
  getLanguage().then(r => { appState.language = r.language as Language }).catch(() => {})
  setInterval(() => wsClient.ping(), 30_000)
}
