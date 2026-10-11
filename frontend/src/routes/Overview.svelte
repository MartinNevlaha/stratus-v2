<script lang="ts">
  import { onMount } from 'svelte'
  import { appState, dismissUpdate } from '$lib/store'
  import { listWorkflows, deleteWorkflow, listMissions, listPastItems, listGuardianAlerts, dismissGuardianAlert, dismissAllGuardianAlerts, deleteGuardianAlert, killSwarmWorker, setWorkflowAgentStatus, runGuardianScan, startWorkflow, recordDelegation, listAgents } from '$lib/api'
  import PhaseTimeline from '../components/PhaseTimeline.svelte'
  import AnalysisPanel from '../components/AnalysisPanel.svelte'
  import SwarmDetail from '../components/SwarmDetail.svelte'
  import WorkflowActivity from '../components/WorkflowActivity.svelte'
  import WorkflowDetail from '../components/WorkflowDetail.svelte'
  import AttentionStrip from '../components/AttentionStrip.svelte'
  import MissionControl from '../components/MissionControl.svelte'
  import { relativeTime } from '$lib/ui'
  import type { WorkflowState, SwarmMission, PastItem, GuardianAlert, AgentDef } from '$lib/types'

  let allWorkflows = $state<WorkflowState[]>([])
  let missions = $state<SwarmMission[]>([])
  let confirmDelete = $state<string | null>(null)
  let copiedId = $state<string | null>(null)
  let expandedTasks = $state<Set<string>>(new Set())
  let expandedPlans = $state<Set<string>>(new Set())
  let expandedDesigns = $state<Set<string>>(new Set())
  let confirmTimer: ReturnType<typeof setTimeout> | null = null

  // Deep link: `#/workflow/<id>` focuses one workflow card.
  const WF_HASH = '#/workflow/'
  let focusId = $state<string | null>(readFocusId())

  function readFocusId(): string | null {
    const h = window.location.hash
    return h.startsWith(WF_HASH) ? decodeURIComponent(h.slice(WF_HASH.length)) : null
  }

  let activeWfs = $derived(allWorkflows.filter(w => !w.aborted && w.phase !== 'complete'))
  // A focused workflow may already be finished: its agents and tasks stay inspectable.
  let visibleWfs = $derived(focusId ? allWorkflows.filter(w => w.id === focusId) : activeWfs)

  // Guardian
  let guardianAlerts = $state<GuardianAlert[]>([])
  let guardianExpanded = $state(false)
  let guardianScanning = $state(false)

  async function loadGuardianAlerts() {
    try {
      guardianAlerts = await listGuardianAlerts()
      appState.guardianAlertCount = guardianAlerts.length
    } catch { /* ignore */ }
  }

  async function dismissAlert(id: number) {
    try {
      await dismissGuardianAlert(id)
      guardianAlerts = guardianAlerts.filter(a => a.id !== id)
      appState.guardianAlertCount = Math.max(0, guardianAlerts.length)
    } catch { /* ignore */ }
  }

  async function deleteAlert(id: number) {
    try {
      await deleteGuardianAlert(id)
      guardianAlerts = guardianAlerts.filter(a => a.id !== id)
      appState.guardianAlertCount = Math.max(0, guardianAlerts.length)
    } catch { /* ignore */ }
  }

  async function dismissAll() {
    try {
      await dismissAllGuardianAlerts()
      guardianAlerts = []
      appState.guardianAlertCount = 0
    } catch { /* ignore */ }
  }

  async function killWorker(workerId: string, alertId: number) {
    try {
      await killSwarmWorker(workerId)
      await dismissGuardianAlert(alertId)
      guardianAlerts = guardianAlerts.filter(a => a.id !== alertId)
      appState.guardianAlertCount = Math.max(0, guardianAlerts.length)
    } catch { /* ignore */ }
  }

  async function failAgent(agentId: string, alertId: number) {
    try {
      await setWorkflowAgentStatus(agentId, 'failed')
      await dismissGuardianAlert(alertId)
      guardianAlerts = guardianAlerts.filter(a => a.id !== alertId)
      appState.guardianAlertCount = Math.max(0, guardianAlerts.length)
    } catch { /* ignore */ }
  }

  function viewFile(filePath: string) {
    navigator.clipboard.writeText(filePath)
    copiedFilePath = filePath
    setTimeout(() => { copiedFilePath = null }, 2000)
  }

  let copiedFilePath = $state<string | null>(null)
  let delegateAlertId = $state<number | null>(null)
  let availableAgents = $state<AgentDef[]>([])
  let delegating = $state(false)

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
      } catch { /* ignore */ }
    }
  }

  async function sendToAgent(alert: GuardianAlert, agentName: string) {
    delegating = true
    try {
      const wfId = `guardian-${alert.id}-${Date.now()}`
      const wfType = alert.type === 'stale_worker' ? 'bug' : 'bug'
      const title = `[Guardian] ${alert.message}`
      await startWorkflow(wfId, wfType as 'bug', title)
      await recordDelegation(wfId, agentName)
      await dismissGuardianAlert(alert.id)
      guardianAlerts = guardianAlerts.filter(a => a.id !== alert.id)
      appState.guardianAlertCount = Math.max(0, guardianAlerts.length)
      delegateAlertId = null
      loadWorkflows()
    } catch { /* ignore */ }
    delegating = false
  }

  async function triggerScan() {
    guardianScanning = true
    try {
      await runGuardianScan()
      setTimeout(() => loadGuardianAlerts(), 3000)
    } catch { /* ignore */ }
    finally { guardianScanning = false }
  }

  function severityIcon(sev: string): string {
    if (sev === 'critical') return '🔴'
    if (sev === 'warning') return '⚠'
    return 'ℹ'
  }

  let pastItems = $state<PastItem[]>([])
  let pastTotal = $state(0)
  let pastOffset = $state(0)
  let pastLoading = $state(false)
  let pastLoadSeq = 0
  const PAST_LIMIT = 20

  function displayType(wf: WorkflowState): string {
    if (wf.title?.startsWith('[SWARM]')) return 'swarm'
    if (wf.type === 'spec' && wf.complexity === 'complex') return 'spec-complex'
    return wf.type
  }

  // Map workflow_id → active mission (for swarm workflows)
  let swarmMissionByWfId = $derived(new Map(missions.map(m => [m.workflow_id, m])))

  let recentEvents = $derived(appState.dashboard?.recent_events ?? [])
  let vexorOk = $derived(appState.dashboard?.vexor_available ?? false)
  let govStats = $derived(appState.dashboard?.governance)
  let showUpdatePanel = $derived(appState.updateInProgress || appState.updateLog.length > 0)

  async function loadWorkflows() {
    try {
      allWorkflows = await listWorkflows()
    } catch { /* ignore */ }
    try {
      missions = await listMissions()
    } catch { /* ignore */ }
  }

  async function loadPastItems(append = false) {
    if (pastLoading) return
    const seq = ++pastLoadSeq
    try {
      pastLoading = true
      const res = await listPastItems(PAST_LIMIT, append ? pastOffset : 0)
      if (seq !== pastLoadSeq) return
      if (append) {
        pastItems = [...pastItems, ...res.items]
      } else {
        pastItems = res.items
        pastOffset = 0
      }
      pastTotal = res.total
      pastOffset = append ? pastOffset + res.items.length : res.items.length
    } catch { /* ignore */ } finally {
      if (seq === pastLoadSeq) pastLoading = false
    }
  }

  async function loadMorePast() {
    await loadPastItems(true)
  }

  async function handleDelete(id: string) {
    if (confirmDelete !== id) {
      if (confirmTimer) clearTimeout(confirmTimer)
      confirmDelete = id
      confirmTimer = setTimeout(() => {
        if (confirmDelete === id) confirmDelete = null
        confirmTimer = null
      }, 10_000)
      return
    }
    if (confirmTimer) { clearTimeout(confirmTimer); confirmTimer = null }
    confirmDelete = null
    try {
      await deleteWorkflow(id)
      allWorkflows = allWorkflows.filter(w => w.id !== id)
      loadPastItems()
    } catch (e) {
      console.error('delete workflow failed', e)
      await loadWorkflows()
    }
  }

  function cancelDelete() {
    if (confirmTimer) { clearTimeout(confirmTimer); confirmTimer = null }
    confirmDelete = null
  }

  function copyToClipboard(text: string, key: string) {
    navigator.clipboard.writeText(text)
    copiedId = key
    setTimeout(() => { if (copiedId === key) copiedId = null }, 2000)
  }

  onMount(() => {
    loadWorkflows()
    loadPastItems()
    loadGuardianAlerts()
    const onHash = () => { focusId = readFocusId() }
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  })

  $effect(() => {
    const _ = appState.dashboard
    loadWorkflows()
  })

  $effect(() => {
    const _ = appState.guardianAlertCount
    if (_ === 0) return
    loadGuardianAlerts()
  })

  // Swarm real-time refresh on WS events
  $effect(() => {
    const _ = appState.swarmUpdateCounter
    if (_ === 0) return
    listMissions().then(m => { missions = m }).catch(() => {})
    loadPastItems()
  })

  // Keep the mission list fresh while a swarm workflow is active
  onMount(() => {
    const pollInterval = setInterval(() => {
      const hasSwarm = allWorkflows.some(wf => !wf.aborted && wf.phase !== 'complete' && displayType(wf) === 'swarm')
      if (hasSwarm) {
        listMissions().then(m => { missions = m }).catch(() => {})
      }
    }, 5000)
    return () => clearInterval(pollInterval)
  })
</script>

<div class="overview">
  <!-- Status bar -->
  <div class="status-bar">
    <span class="badge" class:ok={vexorOk} class:warn={!vexorOk}>
      Vexor {vexorOk ? '✓' : '✗'}
    </span>
    {#if govStats}
      <span class="badge ok">{govStats.total_chunks} governance chunks</span>
    {/if}
    <span class="badge ok">WS {appState.connected ? 'live' : 'offline'}</span>
  </div>

  <!-- Update progress panel -->
  {#if showUpdatePanel}
    <div class="update-panel" class:error={!!appState.updateError}>
      <div class="update-panel-header">
        <span class="update-header">
          {#if appState.updateInProgress}
            ⟳ Updating stratus…
          {:else if appState.updateError}
            ✕ Update failed
          {:else}
            ✓ Update complete — server restarting
          {/if}
        </span>
        {#if !appState.updateInProgress}
          <button class="dismiss-btn" onclick={dismissUpdate} title="Dismiss">✕</button>
        {/if}
      </div>
      <div class="update-log">
        {#each appState.updateLog as line}
          <div class="log-line" class:error-line={line.startsWith('Error:')}>{line}</div>
        {/each}
      </div>
    </div>
  {/if}

  <!-- Risk Analyzer -->
  <AnalysisPanel />

  <!-- Guardian alerts -->
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
                  <button class="alert-action" onclick={() => viewFile(String(alert.metadata.file))} title="Copy file path">
                    {copiedFilePath === String(alert.metadata.file) ? 'Copied!' : 'View file'}
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
                {#if alert.type === 'stale_agent' && alert.metadata?.agent_id}
                  <button class="alert-action alert-action-danger" onclick={() => failAgent(String(alert.metadata.agent_id), alert.id)} title="Mark idle agent as failed">
                    Mark failed
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


  <!-- Active workflows -->
  {#if !focusId}
    <AttentionStrip />
    {#if activeWfs.length > 0}<MissionControl />{/if}
  {/if}
  {#if focusId}
    <a class="focus-back" href="#/" onclick={() => { focusId = null }}>← All workflows</a>
  {/if}
  {#if focusId && visibleWfs.length === 1}
    <WorkflowDetail wf={visibleWfs[0]} {missions} isSwarm={displayType(visibleWfs[0]) === 'swarm'} />
  {:else if visibleWfs.length === 0}
    <div class="empty">
      {#if focusId}Workflow <code>{focusId}</code> was not found.{:else}No active workflows. Use <code>/spec</code>, <code>/bug</code>, or <code>/swarm</code> to start one.{/if}
    </div>
  {:else}
    {#each visibleWfs as wf (wf.id)}
      <div class="workflow-card">
        <div class="workflow-header">
          <span class="wf-type" class:swarm={displayType(wf) === 'swarm'} class:bug={wf.type === 'bug'} class:e2e={wf.type === 'e2e'}>{displayType(wf)}</span>
          <a class="wf-id" href="{WF_HASH}{encodeURIComponent(wf.id)}" onclick={() => { focusId = wf.id }} title="Open this workflow only">{wf.id}</a>
          <span class="wf-phase">{wf.phase}</span>
          <div class="wf-actions">
            {#if confirmDelete === wf.id}
              <span class="confirm-label">Delete?</span>
              <button class="btn-confirm" onclick={() => handleDelete(wf.id)}>Yes</button>
              <button class="btn-cancel" onclick={cancelDelete}>No</button>
            {:else}
              <button class="btn-delete" onclick={() => handleDelete(wf.id)} title="Delete workflow">✕</button>
            {/if}
          </div>
        </div>
        {#if wf.title}
          <div class="wf-title">{wf.title}</div>
        {/if}
        <PhaseTimeline type={wf.type} complexity={wf.complexity} currentPhase={wf.phase} />

        <!-- Plan content -->
        {#if wf.plan_content}
          <div class="doc-section">
            <button
              class="doc-toggle"
              onclick={() => {
                const next = new Set(expandedPlans)
                if (next.has(wf.id)) next.delete(wf.id)
                else next.add(wf.id)
                expandedPlans = next
              }}
            >
              <span class="doc-icon">{expandedPlans.has(wf.id) ? '\u25BC' : '\u25B6'}</span>
              Plan
            </button>
            {#if expandedPlans.has(wf.id)}
              <div class="doc-content"><pre>{wf.plan_content}</pre></div>
            {/if}
          </div>
        {/if}

        <!-- Design content -->
        {#if wf.design_content}
          <div class="doc-section">
            <button
              class="doc-toggle"
              onclick={() => {
                const next = new Set(expandedDesigns)
                if (next.has(wf.id)) next.delete(wf.id)
                else next.add(wf.id)
                expandedDesigns = next
              }}
            >
              <span class="doc-icon">{expandedDesigns.has(wf.id) ? '\u25BC' : '\u25B6'}</span>
              Design Document
            </button>
            {#if expandedDesigns.has(wf.id)}
              <div class="doc-content"><pre>{wf.design_content}</pre></div>
            {/if}
          </div>
        {/if}

        <!-- Change summary (completed workflows) -->
        {#if wf.change_summary}
          <div class="change-summary">
            <div class="cs-stats">
              <span class="cs-stat">{wf.change_summary.files_changed} files</span>
              <span class="cs-stat added">+{wf.change_summary.lines_added}</span>
              <span class="cs-stat removed">-{wf.change_summary.lines_removed}</span>
              {#if wf.change_summary.test_coverage_delta}
                <span class="cs-stat">{wf.change_summary.test_coverage_delta}</span>
              {/if}
            </div>
            {#if wf.change_summary.capabilities_added.length > 0}
              <div class="cs-section">
                <span class="cs-label">Added</span>
                {#each wf.change_summary.capabilities_added as cap}
                  <span class="cs-item">{cap}</span>
                {/each}
              </div>
            {/if}
            {#if wf.change_summary.capabilities_modified.length > 0}
              <div class="cs-section">
                <span class="cs-label">Modified</span>
                {#each wf.change_summary.capabilities_modified as cap}
                  <span class="cs-item">{cap}</span>
                {/each}
              </div>
            {/if}
            {#if wf.change_summary.downstream_risks.length > 0}
              <div class="cs-section">
                <span class="cs-label cs-risk">Risks</span>
                {#each wf.change_summary.downstream_risks as risk}
                  <span class="cs-item risk">{risk}</span>
                {/each}
              </div>
            {/if}
            {#if wf.change_summary.governance_compliance.length > 0}
              <div class="cs-section">
                <span class="cs-label">Governance</span>
                {#each wf.change_summary.governance_compliance as g}
                  <span class="cs-tag">{g}</span>
                {/each}
              </div>
            {/if}
          </div>
        {:else if wf.phase === 'complete' && wf.base_commit}
          <div class="cs-pending">Analyzing changes…</div>
        {/if}

        <!-- Swarm mission detail (inline for [SWARM] workflows) -->
        {#if displayType(wf) === 'swarm'}
          <SwarmDetail {wf} {missions} />
        {/if}

        <!-- Resume buttons -->
        <div class="resume-row">
          <button
            class="btn-resume skill"
            onclick={() => copyToClipboard(`/resume ${wf.id}`, `skill-${wf.id}`)}
            title="Copy /resume command to clipboard"
          >
            {copiedId === `skill-${wf.id}` ? 'Copied!' : `/resume ${wf.id}`}
          </button>
          {#if wf.session_id}
            <button
              class="btn-resume session"
              onclick={() => copyToClipboard(`claude --resume ${wf.session_id}`, `sess-${wf.id}`)}
              title="Copy claude --resume command to clipboard (restores full conversation)"
            >
              {copiedId === `sess-${wf.id}` ? 'Copied!' : 'claude --resume'}
            </button>
          {:else}
            <span class="btn-resume disabled" title="Run /spec or /bug first to capture session ID">claude --resume</span>
          {/if}
        </div>

        {#if displayType(wf) === 'swarm'}
          <!-- Tasks -->
          {#if wf.tasks.length > 0}
            <div class="tasks">
              <div class="tasks-header">
                Tasks ({wf.tasks.filter(t => t.status === 'done').length}/{wf.total_tasks})
                <div class="progress-bar">
                  <div
                    class="progress-fill"
                    style="width: {wf.total_tasks > 0 ? (wf.tasks.filter(t => t.status === 'done').length / wf.total_tasks) * 100 : 0}%"
                  ></div>
                </div>
              </div>
              {#each expandedTasks.has(wf.id) ? wf.tasks : wf.tasks.slice(0, 5) as task}
                <div class="task" class:done={task.status === 'done'} class:active={task.status === 'in_progress'}>
                  <span class="task-status">
                    {task.status === 'done' ? '✓' : task.status === 'in_progress' ? '▶' : '○'}
                  </span>
                  {task.title}
                </div>
              {/each}
              {#if wf.tasks.length > 5}
                <button
                  class="task-toggle"
                  onclick={() => {
                    const next = new Set(expandedTasks)
                    if (next.has(wf.id)) next.delete(wf.id)
                    else next.add(wf.id)
                    expandedTasks = next
                  }}
                >
                  {expandedTasks.has(wf.id) ? 'Show less' : `+${wf.tasks.length - 5} more`}
                </button>
              {/if}
            </div>
          {/if}

          <!-- Delegated agents -->
          {#if Object.keys(wf.delegated_agents).length > 0}
            <div class="delegated">
              {#each Object.entries(wf.delegated_agents) as [phase, agents]}
                {#if agents.length > 0}
                  <div class="phase-agents">
                    <span class="phase-label">{phase}:</span>
                    {#each agents as agent}
                      <span class="agent-chip">{agent}</span>
                    {/each}
                  </div>
                {/if}
              {/each}
            </div>
          {/if}
        {:else}
          <WorkflowActivity {wf} />
        {/if}
      </div>
    {/each}
  {/if}

  <!-- Past workflows + missions (paginated) -->
  {#if pastItems.length > 0 && !focusId}
    <div class="section-title">Past ({pastTotal})</div>
    {#each pastItems as item}
      {#if item.kind === 'mission'}
        {@const mission = item.data}
        <div class="mission-card past">
          <div class="mission-header-static">
            <span class="mission-title">{mission.title}</span>
            <span class="mission-phase" class:failed={mission.status === 'failed'}>{mission.status}</span>
            <span class="wf-ts">{new Date(mission.updated_at).toLocaleDateString()}</span>
          </div>
        </div>
      {:else}
        {@const wf = item.data}
        <div class="workflow-card past">
          <div class="workflow-header">
            <span class="wf-type" class:swarm={displayType(wf) === 'swarm'} class:bug={wf.type === 'bug'} class:e2e={wf.type === 'e2e'}>{displayType(wf)}</span>
            <a class="wf-id" href="{WF_HASH}{encodeURIComponent(wf.id)}" onclick={() => { focusId = wf.id }} title="Open agents and tasks of this run">{wf.id}</a>
            <span class="wf-phase" class:aborted={wf.aborted}>{wf.aborted ? 'aborted' : wf.phase}</span>
            <span class="wf-ts">{new Date(wf.updated_at).toLocaleDateString()}</span>
            <div class="wf-actions">
              {#if confirmDelete === wf.id}
                <span class="confirm-label">Delete?</span>
                <button class="btn-confirm" onclick={() => handleDelete(wf.id)}>Yes</button>
                <button class="btn-cancel" onclick={cancelDelete}>No</button>
              {:else}
                <button class="btn-delete" onclick={() => handleDelete(wf.id)} title="Delete workflow">✕</button>
              {/if}
            </div>
          </div>
          {#if wf.title}
            <div class="wf-title">{wf.title}</div>
          {/if}
          {#if wf.change_summary}
            <div class="cs-stats">
              <span class="cs-stat">{wf.change_summary.files_changed} files</span>
              <span class="cs-stat added">+{wf.change_summary.lines_added}</span>
              <span class="cs-stat removed">-{wf.change_summary.lines_removed}</span>
            </div>
          {/if}
        </div>
      {/if}
    {/each}
    {#if pastOffset < pastTotal}
      <button class="load-more-btn" onclick={loadMorePast} disabled={pastLoading}>
        {pastLoading ? 'Loading…' : `Load more (${pastTotal - pastOffset} remaining)`}
      </button>
    {/if}
  {/if}

  <!-- Recent events -->
  {#if recentEvents.length > 0}
    <div class="section-title">Recent Events</div>
    <div class="events-list">
      {#each recentEvents as event}
        <div class="event-row">
          <span class="event-type">{event.type}</span>
          <span class="event-title">{event.title || event.text.slice(0, 80)}</span>
          <span class="event-ts">{new Date(event.ts).toLocaleTimeString()}</span>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .overview { display: flex; flex-direction: column; gap: 16px; }

  .status-bar { display: flex; gap: 8px; flex-wrap: wrap; }
  .badge { padding: 2px 8px; border-radius: 12px; font-size: 12px; background: #21262d; color: #8b949e; }
  .badge.ok { color: #3fb950; }
  .badge.warn { color: #d29922; }

  .empty { color: #8b949e; font-size: 14px; padding: 24px; text-align: center; background: #161b22; border-radius: 6px; }
  code { background: #21262d; padding: 1px 4px; border-radius: 4px; }

  .workflow-card { background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 16px; display: flex; flex-direction: column; gap: 12px; }
  .workflow-card.past { opacity: 0.6; }
  .workflow-card.past:hover { opacity: 1; }
  .workflow-header { display: flex; align-items: center; gap: 8px; }
  .wf-type { font-size: 11px; text-transform: uppercase; font-weight: 700; color: #58a6ff; background: #1f3056; padding: 2px 6px; border-radius: 4px; white-space: nowrap; }
  .wf-type.swarm { color: #a371f7; background: #2d1f56; }
  .wf-type.bug { color: #f0883e; background: #3d2200; }
  .wf-type.e2e { color: #3fb950; background: #0d3226; }
  .wf-id { font-size: 13px; font-weight: 600; color: #c9d1d9; flex: 1; text-decoration: none; }
  a.wf-id:hover { color: #58a6ff; text-decoration: underline; }
  .focus-back { font-size: 12px; color: #58a6ff; text-decoration: none; align-self: flex-start; }
  .focus-back:hover { text-decoration: underline; }
  .wf-phase { font-size: 12px; color: #8b949e; }
  .wf-phase.aborted { color: #f85149; }
  .wf-ts { font-size: 11px; color: #8b949e; }
  .wf-title { font-size: 14px; color: #c9d1d9; }

  .resume-row { display: flex; gap: 6px; align-items: center; }
  .btn-resume { font-size: 11px; padding: 3px 10px; border-radius: 4px; cursor: pointer; border: 1px solid #30363d; font-family: monospace; white-space: nowrap; transition: background 0.1s; }
  .btn-resume.skill { background: #1f2d1f; color: #3fb950; border-color: #238636; }
  .btn-resume.skill:hover { background: #2d3f2d; }
  .btn-resume.session { background: #1a2433; color: #58a6ff; border-color: #1f6feb; }
  .btn-resume.session:hover { background: #1f3056; }
  .btn-resume.disabled { background: #161b22; color: #484f58; border-color: #21262d; cursor: default; }

  .wf-actions { display: flex; align-items: center; gap: 4px; margin-left: auto; }
  .btn-delete { background: none; border: none; color: #8b949e; cursor: pointer; font-size: 12px; padding: 2px 6px; border-radius: 4px; line-height: 1; }
  .btn-delete:hover { color: #f85149; background: #2d1117; }
  .confirm-label { font-size: 12px; color: #f85149; }
  .btn-confirm { background: #f85149; border: none; color: #fff; cursor: pointer; font-size: 11px; padding: 2px 8px; border-radius: 4px; }
  .btn-confirm:hover { background: #da3633; }
  .btn-cancel { background: #21262d; border: none; color: #c9d1d9; cursor: pointer; font-size: 11px; padding: 2px 8px; border-radius: 4px; }
  .btn-cancel:hover { background: #30363d; }

  .tasks { display: flex; flex-direction: column; gap: 4px; }
  .tasks-header { font-size: 12px; color: #8b949e; display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
  .progress-bar { flex: 1; height: 4px; background: #30363d; border-radius: 2px; }
  .progress-fill { height: 100%; background: #58a6ff; border-radius: 2px; transition: width 0.3s; }

  .task { display: flex; gap: 8px; align-items: center; font-size: 13px; color: #8b949e; padding: 2px 0; }
  .task.done { color: #3fb950; }
  .task.active { color: #58a6ff; }
  .task-status { width: 16px; text-align: center; }
  .task-toggle {
    font-size: 12px; color: #58a6ff; padding-left: 24px;
    background: none; border: none; cursor: pointer; text-align: left;
  }
  .task-toggle:hover { color: #79c0ff; text-decoration: underline; }

  .delegated { display: flex; flex-direction: column; gap: 4px; }
  .phase-agents { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .phase-label { font-size: 12px; color: #8b949e; }
  .agent-chip { font-size: 11px; background: #21262d; color: #c9d1d9; padding: 2px 8px; border-radius: 12px; }

  .update-panel {
    background: #1c1700;
    border: 1px solid #9e6a03;
    border-radius: 8px;
    padding: 12px 16px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .update-panel.error {
    background: #2d1117;
    border-color: #f85149;
  }
  .update-panel-header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .update-header { font-size: 13px; font-weight: 600; color: #ffa657; }
  .update-panel.error .update-header { color: #f85149; }
  .dismiss-btn { background: none; border: none; color: #8b949e; cursor: pointer; font-size: 12px; padding: 2px 6px; border-radius: 4px; line-height: 1; }
  .dismiss-btn:hover { color: #c9d1d9; background: #30363d; }
  .update-log { display: flex; flex-direction: column; gap: 2px; }
  .log-line { font-size: 12px; color: #d29922; font-family: monospace; }
  .log-line.error-line { color: #f85149; }

  .section-title { font-size: 13px; font-weight: 600; color: #8b949e; text-transform: uppercase; letter-spacing: 0.05em; }

  .load-more-btn {
    background: #21262d; border: 1px solid #30363d; color: #8b949e; cursor: pointer;
    font-size: 12px; padding: 8px 16px; border-radius: 6px; text-align: center;
    transition: background 0.1s, color 0.1s;
  }
  .load-more-btn:hover:not(:disabled) { background: #30363d; color: #c9d1d9; }
  .load-more-btn:disabled { opacity: 0.5; cursor: default; }

  .events-list { display: flex; flex-direction: column; gap: 2px; }
  .event-row { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 4px; font-size: 13px; }
  .event-row:hover { background: #161b22; }
  .event-type { font-size: 11px; background: #21262d; color: #8b949e; padding: 1px 6px; border-radius: 4px; min-width: 80px; text-align: center; }
  .event-title { flex: 1; color: #c9d1d9; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .event-ts { font-size: 11px; color: #8b949e; }

  /* Mission cards */
  .mission-card {
    background: #161b22; border: 1px solid #30363d; border-radius: 8px;
    overflow: hidden;
  }
  .mission-card.past { opacity: 0.6; }
  .mission-card.past:hover { opacity: 1; }

  .mission-header-static {
    display: flex; align-items: center; gap: 8px; padding: 12px 16px;
    font-size: 14px; color: #c9d1d9;
  }

  .mission-title { flex: 1; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mission-phase { font-size: 12px; color: #8b949e; }
  .mission-phase.failed { color: #f85149; }

  /* === Change summary === */
  .change-summary { display: flex; flex-direction: column; gap: 6px; }
  .cs-pending { font-size: 12px; color: #8b949e; font-style: italic; }
  .cs-stats { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .cs-stat { font-size: 12px; color: #8b949e; background: #21262d; padding: 2px 8px; border-radius: 4px; }
  .cs-stat.added { color: #3fb950; }
  .cs-stat.removed { color: #f85149; }
  .cs-section { display: flex; align-items: flex-start; gap: 6px; flex-wrap: wrap; font-size: 12px; }
  .cs-label { color: #8b949e; font-weight: 600; min-width: 56px; padding-top: 2px; }
  .cs-label.cs-risk { color: #d29922; }
  .cs-item { color: #c9d1d9; background: #21262d; padding: 1px 8px; border-radius: 4px; }
  .cs-item.risk { color: #d29922; background: #2d2200; }
  .cs-tag { font-size: 11px; background: #1f2d1f; color: #3fb950; border: 1px solid #238636; padding: 1px 8px; border-radius: 12px; }

  /* Plan/Design doc sections */
  .doc-section { display: flex; flex-direction: column; gap: 0; }
  .doc-toggle {
    display: flex; align-items: center; gap: 6px;
    background: none; border: none; color: #8b949e; cursor: pointer;
    font-size: 12px; font-weight: 600; padding: 4px 0; text-align: left;
  }
  .doc-toggle:hover { color: #c9d1d9; }
  .doc-icon { font-size: 10px; width: 12px; }
  .doc-content {
    max-height: 400px; overflow-y: auto;
    background: #0d1117; border: 1px solid #21262d; border-radius: 6px;
    padding: 12px; margin-top: 4px;
  }
  .doc-content pre {
    margin: 0; font-size: 12px; color: #c9d1d9; white-space: pre-wrap;
    word-break: break-word; font-family: monospace; line-height: 1.5;
  }

  /* === Guardian widget === */
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
