<script lang="ts">
  import { appState } from '$lib/store'
  import { reportError } from '$lib/errors'
  import { listGuardianAlerts, dismissGuardianAlert, dismissAllGuardianAlerts, deleteGuardianAlert, killSwarmWorker, runGuardianScan, startWorkflow, recordDelegation, listAgents } from '$lib/api'
  import type { GuardianAlert, AgentDef } from '$lib/types'

  let guardianAlerts = $state<GuardianAlert[]>([])
  let guardianExpanded = $state(false)
  let guardianScanning = $state(false)
  let copiedFilePath = $state<string | null>(null)
  let delegateAlertId = $state<number | null>(null)
  let availableAgents = $state<AgentDef[]>([])
  let delegating = $state(false)

  async function loadGuardianAlerts() {
    try {
      guardianAlerts = await listGuardianAlerts()
      appState.guardianAlertCount = guardianAlerts.length
    } catch (e) { reportError('Failed to load guardian alerts', e) }
  }

  async function dismissAlert(id: number) {
    try {
      await dismissGuardianAlert(id)
      guardianAlerts = guardianAlerts.filter(a => a.id !== id)
      appState.guardianAlertCount = Math.max(0, guardianAlerts.length)
    } catch (e) { reportError('Failed to dismiss alert', e) }
  }

  async function deleteAlert(id: number) {
    try {
      await deleteGuardianAlert(id)
      guardianAlerts = guardianAlerts.filter(a => a.id !== id)
      appState.guardianAlertCount = Math.max(0, guardianAlerts.length)
    } catch (e) { reportError('Failed to delete alert', e) }
  }

  async function dismissAll() {
    try {
      await dismissAllGuardianAlerts()
      guardianAlerts = []
      appState.guardianAlertCount = 0
    } catch (e) { reportError('Failed to dismiss all alerts', e) }
  }

  async function killWorker(workerId: string, alertId: number) {
    try {
      await killSwarmWorker(workerId)
      await dismissGuardianAlert(alertId)
      guardianAlerts = guardianAlerts.filter(a => a.id !== alertId)
      appState.guardianAlertCount = Math.max(0, guardianAlerts.length)
    } catch (e) { reportError('Failed to kill worker', e) }
  }

  function copyFilePath(filePath: string) {
    navigator.clipboard.writeText(filePath)
    copiedFilePath = filePath
    setTimeout(() => { copiedFilePath = null }, 2000)
  }

  async function openDelegateMenu(alertId: number) {
    if (delegateAlertId === alertId) {
      delegateAlertId = null
      return
    }
    delegateAlertId = alertId
    if (availableAgents.length === 0) {
      try {
        const resp = await listAgents()
        availableAgents = resp.claude_code ?? []
      } catch (e) { console.warn('Failed to load agents for delegation', e) }
    }
  }

  async function sendToAgent(alert: GuardianAlert, agentName: string) {
    delegating = true
    try {
      const wfId = `guardian-${alert.id}-${Date.now()}`
      await startWorkflow(wfId, 'bug' as const, `[Guardian] ${alert.message}`)
      await recordDelegation(wfId, agentName)
      await dismissGuardianAlert(alert.id)
      guardianAlerts = guardianAlerts.filter(a => a.id !== alert.id)
      appState.guardianAlertCount = Math.max(0, guardianAlerts.length)
      delegateAlertId = null
    } catch (e) { reportError('Failed to send alert to agent', e) }
    delegating = false
  }

  async function triggerScan() {
    guardianScanning = true
    try {
      await runGuardianScan()
      setTimeout(() => loadGuardianAlerts(), 3000)
    } catch (e) { reportError('Failed to trigger guardian scan', e) }
    finally { guardianScanning = false }
  }

  function severityIcon(sev: string): string {
    if (sev === 'critical') return '🔴'
    if (sev === 'warning') return '⚠'
    return 'ℹ'
  }

  function relativeTime(ts: string): string {
    const diff = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
    if (diff < 5) return 'just now'
    if (diff < 60) return `${diff}s ago`
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
    return `${Math.floor(diff / 3600)}h ago`
  }

  $effect(() => {
    const _ = appState.guardianAlertCount
    if (_ === 0) return
    loadGuardianAlerts()
  })
</script>

<div class="guardian-widget">
  <div class="guardian-header" role="button" tabindex="0"
    onclick={() => { guardianExpanded = !guardianExpanded; if (guardianExpanded && guardianAlerts.length === 0) loadGuardianAlerts() }}
    onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { guardianExpanded = !guardianExpanded } }}
  >
    <span class="guardian-title">
      Guardian
      {#if guardianAlerts.length > 0}
        <span class="alert-badge">{guardianAlerts.length}</span>
      {/if}
    </span>
    <span class="guardian-actions">
      <button class="scan-btn" onclick={(e) => { e.stopPropagation(); triggerScan() }} disabled={guardianScanning} title="Run scan now">
        {guardianScanning ? '⟳' : '↻'}
      </button>
      <span class="expand-arrow">{guardianExpanded ? '▲' : '▼'}</span>
    </span>
  </div>
  {#if guardianExpanded}
    <div class="guardian-body">
      {#if guardianAlerts.length === 0}
        <p class="no-alerts">No active alerts. Codebase looks healthy.</p>
      {:else}
        <div class="guardian-toolbar">
          <button class="dismiss-all-btn" onclick={() => dismissAll()} title="Dismiss all alerts">
            Dismiss all
          </button>
        </div>
        {#each guardianAlerts as alert}
          <div class="alert-row" class:warning={alert.severity === 'warning'} class:critical={alert.severity === 'critical'}>
            <span class="alert-icon">{severityIcon(alert.severity)}</span>
            <div class="alert-content">
              <div class="alert-message">{alert.message}</div>
              <div class="alert-meta">
                <span class="alert-type">{alert.type.replace('_', ' ')}</span>
                <span class="alert-time">{relativeTime(alert.created_at)}</span>
              </div>
            </div>
            <div class="alert-btns">
              {#if alert.type === 'governance_violation' && alert.metadata?.file}
                <button class="alert-action" onclick={() => copyFilePath(String(alert.metadata.file))} title="Copy file path">
                  {copiedFilePath === String(alert.metadata.file) ? 'Copied!' : 'Copy path'}
                </button>
              {/if}
              {#if alert.type === 'stale_workflow' && alert.metadata?.workflow_id}
                <button class="alert-action" onclick={() => navigator.clipboard.writeText(`/resume ${alert.metadata.workflow_id}`)} title="Copy resume command">
                  Copy /resume
                </button>
              {/if}
              {#if alert.type === 'stale_worker' && alert.metadata?.worker_id}
                <button class="alert-action alert-action-danger" onclick={() => killWorker(String(alert.metadata.worker_id), alert.id)} title="Kill stale worker">
                  Kill worker
                </button>
              {/if}
              <button class="alert-action" onclick={() => openDelegateMenu(alert.id)} title="Send to agent">
                {delegateAlertId === alert.id ? 'Cancel' : 'Send to agent'}
              </button>
              <button class="alert-dismiss" onclick={() => dismissAlert(alert.id)} title="Dismiss">✕</button>
            </div>
            {#if delegateAlertId === alert.id}
              <div class="delegate-dropdown">
                {#if availableAgents.length === 0}
                  <span class="delegate-empty">No agents configured</span>
                {:else}
                  {#each availableAgents as agent}
                    <button class="delegate-agent" disabled={delegating} onclick={() => sendToAgent(alert, agent.name)}>
                      <span class="agent-name">{agent.name}</span>
                      <span class="agent-desc">{agent.description}</span>
                    </button>
                  {/each}
                {/if}
              </div>
            {/if}
          </div>
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  .guardian-widget {
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 6px;
    margin-bottom: 12px;
    overflow: hidden;
  }

  .guardian-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    cursor: pointer;
    color: #c9d1d9;
    font-size: 13px;
    font-weight: 600;
    user-select: none;
  }

  .guardian-header:hover { background: #1f2428; }

  .guardian-title {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .alert-badge {
    background: #da3633;
    color: #fff;
    font-size: 10px;
    font-weight: 700;
    padding: 1px 6px;
    border-radius: 10px;
    min-width: 18px;
    text-align: center;
  }

  .guardian-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    color: #8b949e;
    font-size: 12px;
  }

  .scan-btn {
    background: none;
    border: none;
    cursor: pointer;
    color: #58a6ff;
    font-size: 14px;
    padding: 0 2px;
    line-height: 1;
  }

  .scan-btn:disabled { opacity: 0.5; cursor: default; }

  .expand-arrow { font-size: 10px; }

  .guardian-body {
    padding: 4px 12px 12px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .no-alerts { font-size: 12px; color: #8b949e; margin: 4px 0; }

  .alert-row {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    background: #0d1117;
    border: 1px solid #21262d;
    border-radius: 4px;
    padding: 8px 10px;
  }

  .alert-row.warning { border-color: #d29922; }
  .alert-row.critical { border-color: #f85149; }

  .alert-icon { font-size: 13px; flex-shrink: 0; padding-top: 1px; }

  .alert-content { flex: 1; min-width: 0; }
  .alert-message { font-size: 12px; color: #c9d1d9; line-height: 1.4; }
  .alert-meta { display: flex; gap: 8px; margin-top: 4px; }
  .alert-type { font-size: 10px; background: #21262d; color: #8b949e; padding: 1px 6px; border-radius: 3px; }
  .alert-time { font-size: 10px; color: #6e7681; }

  .alert-btns { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }

  .alert-action {
    background: #21262d;
    border: 1px solid #30363d;
    color: #58a6ff;
    font-size: 10px;
    padding: 2px 8px;
    border-radius: 4px;
    cursor: pointer;
  }

  .alert-action:hover { background: #2a3040; }

  .alert-dismiss {
    background: none;
    border: none;
    color: #6e7681;
    cursor: pointer;
    font-size: 12px;
    padding: 2px;
    line-height: 1;
  }

  .alert-dismiss:hover { color: #f85149; }

  .guardian-toolbar {
    display: flex;
    justify-content: flex-end;
    padding: 4px 8px;
    border-bottom: 1px solid #21262d;
  }

  .dismiss-all-btn {
    background: #21262d;
    border: 1px solid #30363d;
    color: #8b949e;
    font-size: 11px;
    padding: 3px 10px;
    border-radius: 4px;
    cursor: pointer;
  }

  .dismiss-all-btn:hover { background: #2a3040; color: #c9d1d9; }

  .alert-action-danger {
    color: #f85149 !important;
    border-color: #f8514933 !important;
  }

  .alert-action-danger:hover { background: #3d1c1e !important; }

  .delegate-dropdown {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px 8px;
    margin-top: 4px;
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 6px;
  }

  .delegate-agent {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 1px;
    background: none;
    border: 1px solid transparent;
    padding: 6px 8px;
    border-radius: 4px;
    cursor: pointer;
    text-align: left;
    color: #c9d1d9;
  }

  .delegate-agent:hover {
    background: #21262d;
    border-color: #30363d;
  }

  .delegate-agent:disabled { opacity: 0.5; cursor: wait; }

  .agent-name {
    font-size: 12px;
    font-weight: 600;
    color: #58a6ff;
  }

  .agent-desc {
    font-size: 10px;
    color: #6e7681;
    line-height: 1.3;
  }

  .delegate-empty {
    font-size: 11px;
    color: #6e7681;
    padding: 4px;
  }
</style>
