<script lang="ts">
  import { onMount } from 'svelte'
  import { appState } from '$lib/store'
  import { listWorkflows, deleteWorkflow } from '$lib/api'
  import PhaseTimeline from '../components/PhaseTimeline.svelte'
  import type { WorkflowState } from '$lib/types'

  let allWorkflows = $state<WorkflowState[]>([])
  let confirmDelete = $state<string | null>(null)
  let copiedId = $state<string | null>(null)
  let confirmTimer: ReturnType<typeof setTimeout> | null = null

  let activeWfs = $derived(allWorkflows.filter(w => !w.aborted && w.phase !== 'complete'))
  let pastWfs   = $derived(allWorkflows.filter(w => w.aborted || w.phase === 'complete'))

  let recentEvents = $derived(appState.dashboard?.recent_events ?? [])
  let vexorOk = $derived(appState.dashboard?.vexor_available ?? false)
  let govStats = $derived(appState.dashboard?.governance)
  let showUpdatePanel = $derived(appState.updateInProgress || appState.updateLog.length > 0)

  async function loadWorkflows() {
    try {
      allWorkflows = await listWorkflows()
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
      await loadWorkflows() // rollback: re-sync with server
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

  onMount(loadWorkflows)

  $effect(() => {
    // Re-fetch when WS events arrive that may change workflow list
    const _ = appState.dashboard
    loadWorkflows()
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
    <div class="update-panel">
      <div class="update-header">
        {appState.updateInProgress ? '⟳ Updating stratus…' : '✓ Update complete'}
      </div>
      <div class="update-log">
        {#each appState.updateLog as line}
          <div class="log-line">{line}</div>
        {/each}
      </div>
      {#if !appState.updateInProgress && appState.updateLog.length > 0}
        <div class="restart-notice">Restart stratus server to apply the new version.</div>
      {/if}
    </div>
  {/if}

  <!-- Active workflows -->
  {#if activeWfs.length === 0}
    <div class="empty">No active workflows. Use <code>/spec</code> or <code>/bug</code> to start one.</div>
  {:else}
    {#each activeWfs as wf}
      <div class="workflow-card">
        <div class="workflow-header">
          <span class="wf-type">{wf.type}</span>
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
            {#each wf.tasks.slice(0, 5) as task}
              <div class="task" class:done={task.status === 'done'} class:active={task.status === 'in_progress'}>
                <span class="task-status">
                  {task.status === 'done' ? '✓' : task.status === 'in_progress' ? '▶' : '○'}
                </span>
                {task.title}
              </div>
            {/each}
            {#if wf.tasks.length > 5}
              <div class="task-more">+{wf.tasks.length - 5} more</div>
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

  <!-- Past workflows (completed + aborted) -->
  {#if pastWfs.length > 0}
    <div class="section-title">Past Workflows</div>
    {#each pastWfs as wf}
      <div class="workflow-card past">
        <div class="workflow-header">
          <span class="wf-type">{wf.type}</span>
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
  .wf-type { font-size: 11px; text-transform: uppercase; font-weight: 700; color: #58a6ff; background: #1f3056; padding: 2px 6px; border-radius: 4px; }
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
  .task-more { font-size: 12px; color: #8b949e; padding-left: 24px; }

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
  .update-header { font-size: 13px; font-weight: 600; color: #ffa657; }
  .update-log { display: flex; flex-direction: column; gap: 2px; }
  .log-line { font-size: 12px; color: #d29922; font-family: monospace; }
  .restart-notice { font-size: 12px; color: #3fb950; font-weight: 600; margin-top: 4px; }

  .section-title { font-size: 13px; font-weight: 600; color: #8b949e; text-transform: uppercase; letter-spacing: 0.05em; }

  .events-list { display: flex; flex-direction: column; gap: 2px; }
  .event-row { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 4px; font-size: 13px; }
  .event-row:hover { background: #161b22; }
  .event-type { font-size: 11px; background: #21262d; color: #8b949e; padding: 1px 6px; border-radius: 4px; min-width: 80px; text-align: center; }
  .event-title { flex: 1; color: #c9d1d9; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .event-ts { font-size: 11px; color: #8b949e; }
</style>
