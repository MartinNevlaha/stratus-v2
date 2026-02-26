<script lang="ts">
  import { onMount } from 'svelte'
  import { appState, dismissUpdate } from '$lib/store'
  import { listWorkflows, deleteWorkflow, listMissions, getMission } from '$lib/api'
  import PhaseTimeline from '../components/PhaseTimeline.svelte'
  import type { WorkflowState, SwarmMission, SwarmMissionDetail } from '$lib/types'

  let allWorkflows = $state<WorkflowState[]>([])
  let missions = $state<SwarmMission[]>([])
  let expandedMission = $state<string | null>(null)
  let missionDetail = $state<SwarmMissionDetail | null>(null)
  let missionLoading = $state(false)
  let confirmDelete = $state<string | null>(null)
  let copiedId = $state<string | null>(null)
  let expandedTasks = $state<Set<string>>(new Set())
  let confirmTimer: ReturnType<typeof setTimeout> | null = null

  let activeWfs = $derived(allWorkflows.filter(w => !w.aborted && w.phase !== 'complete'))
  let pastWfs   = $derived(allWorkflows.filter(w => w.aborted || w.phase === 'complete'))

  function displayType(wf: WorkflowState): string {
    if (wf.title?.startsWith('[SWARM]')) return 'swarm'
    if (wf.type === 'spec' && wf.complexity === 'complex') return 'spec-complex'
    return wf.type
  }

  let activeMissions = $derived(missions.filter(m => m.status !== 'complete' && m.status !== 'failed' && m.status !== 'aborted'))
  let pastMissions = $derived(missions.filter(m => m.status === 'complete' || m.status === 'failed' || m.status === 'aborted'))

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

  let _missionLoadId = 0
  async function toggleMission(id: string) {
    if (expandedMission === id) {
      expandedMission = null
      missionDetail = null
      missionLoading = false
      return
    }
    expandedMission = id
    missionDetail = null
    missionLoading = true
    const loadId = ++_missionLoadId
    try {
      const detail = await getMission(id)
      // Guard against race: only apply if this is still the latest request
      if (loadId === _missionLoadId) {
        missionDetail = detail
      }
    } catch {
      if (loadId === _missionLoadId) {
        missionDetail = null
      }
    } finally {
      if (loadId === _missionLoadId) {
        missionLoading = false
      }
    }
  }

  function workerStatusColor(status: string): string {
    switch (status) {
      case 'active': return '#3fb950'
      case 'pending': return '#8b949e'
      case 'stale': return '#d29922'
      case 'done': return '#58a6ff'
      case 'failed': case 'killed': return '#f85149'
      default: return '#8b949e'
    }
  }

  function ticketIcon(status: string): string {
    switch (status) {
      case 'done': return '\u2713'
      case 'in_progress': return '\u25B6'
      case 'assigned': return '\u25CB'
      case 'failed': return '\u2717'
      case 'blocked': return '\u2758'
      default: return '\u25CB'
    }
  }

  function relativeTime(ts: string): string {
    const diff = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
    if (diff < 5) return 'just now'
    if (diff < 60) return `${diff}s ago`
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
    return `${Math.floor(diff / 3600)}h ago`
  }

  function workerTicketCounts(workerId: string): string {
    if (!missionDetail) return ''
    const assigned = missionDetail.tickets.filter(t => t.worker_id === workerId)
    if (assigned.length === 0) return ''
    const done = assigned.filter(t => t.status === 'done').length
    return `${done}/${assigned.length}`
  }

  onMount(loadWorkflows)

  $effect(() => {
    const _ = appState.dashboard
    loadWorkflows()
  })

  // Swarm real-time refresh — re-fetch on every WS event
  $effect(() => {
    const _ = appState.swarmUpdateCounter
    if (_ === 0) return // skip initial
    listMissions().then(m => {
      missions = m
      // Auto-expand if exactly 1 active mission and none expanded
      const active = m.filter(mi => mi.status !== 'complete' && mi.status !== 'failed' && mi.status !== 'aborted')
      if (active.length === 1 && !expandedMission) {
        toggleMission(active[0].id)
      }
    }).catch(() => {})
    if (expandedMission) {
      getMission(expandedMission).then(d => missionDetail = d).catch(() => {})
    }
  })

  // Tick heartbeat display every 5s so relative times update
  let _heartbeatTick = $state(0)
  let _heartbeatInterval: ReturnType<typeof setInterval> | undefined
  onMount(() => {
    _heartbeatInterval = setInterval(() => _heartbeatTick++, 5000)
    return () => clearInterval(_heartbeatInterval)
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

  <!-- Active Missions -->
  {#if activeMissions.length > 0}
    <div class="section-title">Active Missions</div>
    {#each activeMissions as mission}
      <div class="mission-card" class:expanded={expandedMission === mission.id}>
        <button
          class="mission-header"
          onclick={() => toggleMission(mission.id)}
          aria-expanded={expandedMission === mission.id}
          aria-controls="mission-detail-{mission.id}"
        >
          <span class="mission-status-dot" style="background: {workerStatusColor(mission.status === 'active' ? 'active' : 'pending')}"></span>
          <span class="mission-title">{mission.title}</span>
          <span class="mission-phase">{mission.status}</span>
          <span class="expand-icon">{expandedMission === mission.id ? '\u25BC' : '\u25B6'}</span>
        </button>

        {#if expandedMission === mission.id && missionLoading}
          <div class="mission-detail">
            <div class="detail-loading">Loading…</div>
          </div>
        {:else if expandedMission === mission.id && missionDetail}
          <div class="mission-detail">
            {#if missionDetail.workers.length > 0}
              <div class="detail-section">
                <div class="detail-label">Workers</div>
                <div class="workers-grid">
                  {#each missionDetail.workers as worker}
                    <div class="worker-node" class:failed={worker.status === 'failed' || worker.status === 'killed'} class:stale={worker.status === 'stale'}>
                      <span class="worker-dot" class:active={worker.status === 'active'} style="background: {workerStatusColor(worker.status)}"></span>
                      <span class="worker-type">{worker.agent_type.replace('delivery-', '')}</span>
                      <span class="worker-status">{worker.status}</span>
                      {#if workerTicketCounts(worker.id)}
                        <span class="worker-tickets">{workerTicketCounts(worker.id)}</span>
                      {/if}
                      {#if worker.status === 'active'}
                        <span class="worker-heartbeat">{(() => { const _ = _heartbeatTick; const hb = appState.lastHeartbeats[worker.id]; return hb ? relativeTime(new Date(hb).toISOString()) : relativeTime(worker.last_heartbeat) })()}</span>
                      {/if}
                    </div>
                  {/each}
                </div>
              </div>
            {/if}

            {#if missionDetail.tickets.length > 0}
              <div class="detail-section">
                <div class="detail-label">
                  Tickets ({missionDetail.tickets.filter(t => t.status === 'done').length}/{missionDetail.tickets.length})
                  <div class="progress-bar">
                    <div
                      class="ticket-progress-fill"
                      class:active={missionDetail.tickets.some(t => t.status === 'in_progress' || t.status === 'assigned')}
                      style="width: {missionDetail.tickets.length > 0 ? (missionDetail.tickets.filter(t => t.status === 'done').length / missionDetail.tickets.length) * 100 : 0}%"
                    ></div>
                  </div>
                </div>
                {#each missionDetail.tickets as ticket}
                  <div class="ticket" class:done={ticket.status === 'done'} class:active={ticket.status === 'in_progress'} class:failed={ticket.status === 'failed'}>
                    <span class="ticket-icon" class:spinning={ticket.status === 'in_progress'}>{ticketIcon(ticket.status)}</span>
                    <span class="ticket-title">{ticket.title}</span>
                    {#if ticket.worker_id}
                      {@const worker = missionDetail.workers.find(w => w.id === ticket.worker_id)}
                      {#if worker}
                        <span class="ticket-worker-chip">{worker.agent_type.replace('delivery-', '')}</span>
                      {/if}
                    {/if}
                    <span class="ticket-domain">{ticket.domain}</span>
                  </div>
                {/each}
              </div>
            {/if}

            {#if missionDetail.forge.length > 0}
              <div class="detail-section">
                <div class="detail-label">Forge (merge queue)</div>
                {#each missionDetail.forge as entry}
                  <div class="forge-entry" class:merged={entry.status === 'merged'} class:conflict={entry.status === 'conflict'}>
                    {#if entry.status === 'merged'}
                      <span class="forge-check">&#10003;</span>
                    {/if}
                    <span class="forge-branch">{entry.branch_name}</span>
                    <span class="forge-status">{entry.status}</span>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/if}
      </div>
    {/each}
  {/if}

  <!-- Active workflows -->
  {#if activeWfs.length === 0 && activeMissions.length === 0}
    <div class="empty">No active workflows. Use <code>/spec</code>, <code>/bug</code>, or <code>/swarm</code> to start one.</div>
  {:else}
    {#each activeWfs as wf}
      <div class="workflow-card">
        <div class="workflow-header">
          <span class="wf-type" class:swarm={displayType(wf) === 'swarm'} class:bug={wf.type === 'bug'}>{displayType(wf)}</span>
          <span class="wf-id">{wf.id}</span>
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
      </div>
    {/each}
  {/if}

  <!-- Past workflows + missions -->
  {#if pastWfs.length > 0 || pastMissions.length > 0}
    <div class="section-title">Past</div>
    {#each pastMissions as mission}
      <div class="mission-card past">
        <div class="mission-header-static">
          <span class="mission-title">{mission.title}</span>
          <span class="mission-phase" class:failed={mission.status === 'failed'}>{mission.status}</span>
          <span class="wf-ts">{new Date(mission.updated_at).toLocaleDateString()}</span>
        </div>
      </div>
    {/each}
    {#each pastWfs as wf}
      <div class="workflow-card past">
        <div class="workflow-header">
          <span class="wf-type" class:swarm={displayType(wf) === 'swarm'} class:bug={wf.type === 'bug'}>{displayType(wf)}</span>
          <span class="wf-id">{wf.id}</span>
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
      </div>
    {/each}
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
  .wf-id { font-size: 13px; font-weight: 600; color: #c9d1d9; flex: 1; }
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

  .mission-header {
    display: flex; align-items: center; gap: 8px; padding: 12px 16px;
    background: transparent; border: none; color: #c9d1d9; cursor: pointer;
    font-size: 14px; width: 100%; text-align: left; transition: background 0.1s;
  }
  .mission-header:hover { background: #1c2129; }

  .mission-header-static {
    display: flex; align-items: center; gap: 8px; padding: 12px 16px;
    font-size: 14px; color: #c9d1d9;
  }

  .mission-status-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
  .mission-title { flex: 1; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mission-phase { font-size: 12px; color: #8b949e; }
  .mission-phase.failed { color: #f85149; }
  .expand-icon { font-size: 10px; color: #8b949e; }

  .mission-detail {
    padding: 0 16px 16px; display: flex; flex-direction: column; gap: 12px;
    border-top: 1px solid #21262d;
  }

  .detail-section { display: flex; flex-direction: column; gap: 6px; }
  .detail-label { font-size: 12px; font-weight: 600; color: #8b949e; display: flex; align-items: center; gap: 8px; margin-top: 8px; }

  .workers-grid { display: flex; flex-wrap: wrap; gap: 8px; }
  .worker-node {
    display: flex; align-items: center; gap: 6px;
    background: #21262d; border-radius: 6px; padding: 4px 10px; font-size: 12px;
  }
  .worker-dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
  .worker-type { color: #c9d1d9; }
  .worker-status { color: #8b949e; font-size: 11px; }

  .ticket {
    display: flex; gap: 8px; align-items: center; font-size: 13px; color: #8b949e; padding: 2px 0;
  }
  .ticket.done { color: #3fb950; }
  .ticket.active { color: #a371f7; }
  .ticket.failed { color: #f85149; }
  .ticket-icon { width: 16px; text-align: center; flex-shrink: 0; }
  .ticket-title { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ticket-domain { font-size: 11px; background: #21262d; padding: 1px 6px; border-radius: 4px; }

  .ticket-progress-fill { height: 100%; background: #a371f7; border-radius: 2px; transition: width 0.3s; }

  .forge-entry {
    display: flex; align-items: center; gap: 8px; font-size: 12px; color: #8b949e; padding: 2px 0;
  }
  .forge-entry.merged { color: #3fb950; }
  .forge-entry.conflict { color: #d29922; }
  .forge-branch { font-family: monospace; font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .forge-status { font-size: 11px; }

  .detail-loading { font-size: 12px; color: #8b949e; padding: 12px 0; }

  /* === Swarm live animations === */

  /* Active worker pulse */
  @keyframes pulse {
    0%, 100% { box-shadow: 0 0 0 0 rgba(63, 185, 80, 0.6); }
    50% { box-shadow: 0 0 0 4px rgba(63, 185, 80, 0); }
  }
  .worker-dot.active { animation: pulse 2s ease-in-out infinite; }

  /* In-progress ticket spinner */
  @keyframes spin { to { transform: rotate(360deg); } }
  .ticket-icon.spinning { display: inline-block; animation: spin 1s linear infinite; }

  /* New element entrance */
  @keyframes fadeSlideIn {
    from { opacity: 0; transform: translateY(-8px); }
    to { opacity: 1; transform: translateY(0); }
  }
  .worker-node, .ticket, .forge-entry { animation: fadeSlideIn 0.3s ease-out; }

  /* Progress bar shimmer while active */
  @keyframes shimmer {
    from { background-position: -200% 0; }
    to { background-position: 200% 0; }
  }
  .ticket-progress-fill.active {
    background: linear-gradient(90deg, #a371f7 30%, #c9a0ff 50%, #a371f7 70%);
    background-size: 200% 100%;
    animation: shimmer 2s linear infinite;
  }

  /* Failed/stale worker attention shake */
  @keyframes shake {
    0%, 100% { transform: translateX(0); }
    25% { transform: translateX(-2px); }
    75% { transform: translateX(2px); }
  }
  .worker-node.failed { animation: shake 0.4s ease-in-out; }
  .worker-node.stale { border: 1px solid #d29922; }

  /* Forge merged checkmark scale-in */
  @keyframes scaleIn {
    from { transform: scale(0); opacity: 0; }
    to { transform: scale(1); opacity: 1; }
  }
  .forge-check { display: inline-block; color: #3fb950; font-weight: bold; animation: scaleIn 0.3s ease-out; }

  /* Worker extra info */
  .worker-tickets { font-size: 10px; color: #58a6ff; background: #1f3056; padding: 1px 5px; border-radius: 3px; }
  .worker-heartbeat { font-size: 10px; color: #8b949e; }

  /* Ticket worker chip */
  .ticket-worker-chip { font-size: 10px; background: #2d1f56; color: #a371f7; padding: 1px 6px; border-radius: 4px; flex-shrink: 0; }

  /* Smooth status transitions on tickets */
  .ticket { transition: color 0.3s, opacity 0.3s; }
</style>
