<script lang="ts">
  import { appState } from '$lib/store'
  import PhaseTimeline from '../components/PhaseTimeline.svelte'

  let workflows = $derived(appState.dashboard?.workflows ?? [])
  let recentEvents = $derived(appState.dashboard?.recent_events ?? [])
  let vexorOk = $derived(appState.dashboard?.vexor_available ?? false)
  let govStats = $derived(appState.dashboard?.governance)
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

  <!-- Active workflows -->
  {#if workflows.length === 0}
    <div class="empty">No active workflows. Use <code>/spec</code> or <code>/bug</code> to start one.</div>
  {:else}
    {#each workflows as wf}
      <div class="workflow-card">
        <div class="workflow-header">
          <span class="wf-type">{wf.type}</span>
          <span class="wf-id">{wf.id}</span>
          <span class="wf-phase">{wf.phase}</span>
        </div>
        {#if wf.title}
          <div class="wf-title">{wf.title}</div>
        {/if}
        <PhaseTimeline type={wf.type} currentPhase={wf.phase} />

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
  .workflow-header { display: flex; align-items: center; gap: 8px; }
  .wf-type { font-size: 11px; text-transform: uppercase; font-weight: 700; color: #58a6ff; background: #1f3056; padding: 2px 6px; border-radius: 4px; }
  .wf-id { font-size: 13px; font-weight: 600; color: #c9d1d9; flex: 1; }
  .wf-phase { font-size: 12px; color: #8b949e; }
  .wf-title { font-size: 14px; color: #c9d1d9; }

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

  .section-title { font-size: 13px; font-weight: 600; color: #8b949e; text-transform: uppercase; letter-spacing: 0.05em; }

  .events-list { display: flex; flex-direction: column; gap: 2px; }
  .event-row { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 4px; font-size: 13px; }
  .event-row:hover { background: #161b22; }
  .event-type { font-size: 11px; background: #21262d; color: #8b949e; padding: 1px 6px; border-radius: 4px; min-width: 80px; text-align: center; }
  .event-title { flex: 1; color: #c9d1d9; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .event-ts { font-size: 11px; color: #8b949e; }
</style>
