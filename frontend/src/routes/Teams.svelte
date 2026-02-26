<script lang="ts">
  import { onMount } from 'svelte'
  import { appState } from '$lib/store'
  import { listWorkflows, listMissions, getMission } from '$lib/api'
  import PhaseTimeline from '../components/PhaseTimeline.svelte'
  import type { WorkflowState, SwarmMission, SwarmMissionDetail } from '$lib/types'

  let allWorkflows = $state<WorkflowState[]>([])
  let missions = $state<SwarmMission[]>([])
  let expandedMission = $state<string | null>(null)
  let missionDetail = $state<SwarmMissionDetail | null>(null)
  let copiedId = $state<string | null>(null)

  // Show both [TEAM] and [SWARM] workflows
  let swarmWfs = $derived(allWorkflows.filter(w => w.title?.startsWith('[SWARM]') || w.title?.startsWith('[TEAM]')))
  let activeSwarmWfs = $derived(swarmWfs.filter(w => !w.aborted && w.phase !== 'complete'))
  let pastSwarmWfs = $derived(swarmWfs.filter(w => w.aborted || w.phase === 'complete'))

  let activeMissions = $derived(missions.filter(m => m.status !== 'complete' && m.status !== 'failed' && m.status !== 'aborted'))
  let pastMissions = $derived(missions.filter(m => m.status === 'complete' || m.status === 'failed' || m.status === 'aborted'))

  async function load() {
    try {
      allWorkflows = await listWorkflows()
    } catch { /* ignore */ }
    try {
      missions = await listMissions()
    } catch { /* ignore */ }
  }

  async function toggleMission(id: string) {
    if (expandedMission === id) {
      expandedMission = null
      missionDetail = null
      return
    }
    expandedMission = id
    try {
      missionDetail = await getMission(id)
    } catch {
      missionDetail = null
    }
  }

  function copyToClipboard(text: string, key: string) {
    navigator.clipboard.writeText(text)
    copiedId = key
    setTimeout(() => { if (copiedId === key) copiedId = null }, 2000)
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

  function stripPrefix(title: string): string {
    return title.replace(/^\[(SWARM|TEAM)\]\s*/, '')
  }

  onMount(load)

  $effect(() => {
    const _ = appState.dashboard
    load()
  })
</script>

<div class="teams">
  <div class="header-row">
    <h2>Swarm</h2>
  </div>

  <div class="info-banner">
    <strong>/swarm</strong> spawns delivery agents in isolated git worktrees for truly parallel implementation.
    Each worker has its own branch and filesystem.
  </div>

  {#if activeMissions.length === 0 && activeSwarmWfs.length === 0 && pastMissions.length === 0 && pastSwarmWfs.length === 0}
    <div class="empty">
      No swarm workflows yet. Use <code>/swarm &lt;feature description&gt;</code> to start one.
    </div>
  {/if}

  <!-- Active Missions -->
  {#if activeMissions.length > 0}
    <div class="section-title">Active Missions</div>
    {#each activeMissions as mission}
      <div class="mission-card" class:expanded={expandedMission === mission.id}>
        <button class="mission-header" onclick={() => toggleMission(mission.id)}>
          <span class="mission-status-dot" style="background: {workerStatusColor(mission.status === 'active' ? 'active' : 'pending')}"></span>
          <span class="mission-title">{mission.title}</span>
          <span class="mission-phase">{mission.status}</span>
          <span class="expand-icon">{expandedMission === mission.id ? '\u25BC' : '\u25B6'}</span>
        </button>

        {#if expandedMission === mission.id && missionDetail}
          <div class="mission-detail">
            <!-- Workers -->
            {#if missionDetail.workers.length > 0}
              <div class="detail-section">
                <div class="detail-label">Workers</div>
                <div class="workers-grid">
                  {#each missionDetail.workers as worker}
                    <div class="worker-node">
                      <span class="worker-dot" style="background: {workerStatusColor(worker.status)}"></span>
                      <span class="worker-type">{worker.agent_type.replace('delivery-', '')}</span>
                      <span class="worker-status">{worker.status}</span>
                    </div>
                  {/each}
                </div>
              </div>
            {/if}

            <!-- Tickets -->
            {#if missionDetail.tickets.length > 0}
              <div class="detail-section">
                <div class="detail-label">
                  Tickets ({missionDetail.tickets.filter(t => t.status === 'done').length}/{missionDetail.tickets.length})
                  <div class="progress-bar">
                    <div
                      class="progress-fill"
                      style="width: {missionDetail.tickets.length > 0 ? (missionDetail.tickets.filter(t => t.status === 'done').length / missionDetail.tickets.length) * 100 : 0}%"
                    ></div>
                  </div>
                </div>
                {#each missionDetail.tickets as ticket}
                  <div class="ticket" class:done={ticket.status === 'done'} class:active={ticket.status === 'in_progress'} class:failed={ticket.status === 'failed'}>
                    <span class="ticket-icon">{ticketIcon(ticket.status)}</span>
                    <span class="ticket-title">{ticket.title}</span>
                    <span class="ticket-domain">{ticket.domain}</span>
                  </div>
                {/each}
              </div>
            {/if}

            <!-- Forge -->
            {#if missionDetail.forge.length > 0}
              <div class="detail-section">
                <div class="detail-label">Forge (merge queue)</div>
                {#each missionDetail.forge as entry}
                  <div class="forge-entry" class:merged={entry.status === 'merged'} class:conflict={entry.status === 'conflict'}>
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

  <!-- Active Workflows (legacy [TEAM] + [SWARM] without missions) -->
  {#if activeSwarmWfs.length > 0}
    <div class="section-title">Active Workflows</div>
    {#each activeSwarmWfs as wf}
      <div class="workflow-card">
        <div class="workflow-header">
          <span class="wf-id">{wf.id}</span>
          <span class="wf-phase">{wf.phase}</span>
        </div>
        {#if wf.title}
          <div class="wf-title">{stripPrefix(wf.title)}</div>
        {/if}
        <PhaseTimeline type={wf.type} complexity={wf.complexity} currentPhase={wf.phase} />

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
              title="Copy claude --resume command to clipboard"
            >
              {copiedId === `sess-${wf.id}` ? 'Copied!' : 'claude --resume'}
            </button>
          {/if}
        </div>

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
            {#each wf.tasks as task}
              <div class="task" class:done={task.status === 'done'} class:active={task.status === 'in_progress'}>
                <span class="task-status">
                  {task.status === 'done' ? '\u2713' : task.status === 'in_progress' ? '\u25B6' : '\u25CB'}
                </span>
                {task.title}
              </div>
            {/each}
          </div>
        {/if}

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

  <!-- Past -->
  {#if pastMissions.length > 0 || pastSwarmWfs.length > 0}
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
    {#each pastSwarmWfs as wf}
      <div class="workflow-card past">
        <div class="workflow-header">
          <span class="wf-id">{wf.id}</span>
          <span class="wf-phase" class:aborted={wf.aborted}>{wf.aborted ? 'aborted' : wf.phase}</span>
          <span class="wf-ts">{new Date(wf.updated_at).toLocaleDateString()}</span>
        </div>
        {#if wf.title}
          <div class="wf-title">{stripPrefix(wf.title)}</div>
        {/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  .teams { display: flex; flex-direction: column; gap: 16px; }

  .header-row { display: flex; align-items: center; gap: 10px; }
  h2 { font-size: 16px; font-weight: 600; color: #c9d1d9; }

  .info-banner {
    background: #161b22; border: 1px solid #30363d; border-radius: 6px;
    padding: 10px 14px; font-size: 13px; color: #8b949e; line-height: 1.5;
  }
  .info-banner strong { color: #3fb950; }

  .empty { color: #8b949e; font-size: 14px; padding: 24px; text-align: center; background: #161b22; border-radius: 6px; }
  .empty code { background: #21262d; padding: 1px 4px; border-radius: 4px; }

  .section-title { font-size: 13px; font-weight: 600; color: #8b949e; text-transform: uppercase; letter-spacing: 0.05em; }

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
  .mission-title { flex: 1; font-weight: 500; }
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
  .ticket-title { flex: 1; }
  .ticket-domain { font-size: 11px; background: #21262d; padding: 1px 6px; border-radius: 4px; }

  .forge-entry {
    display: flex; align-items: center; gap: 8px; font-size: 12px; color: #8b949e; padding: 2px 0;
  }
  .forge-entry.merged { color: #3fb950; }
  .forge-entry.conflict { color: #d29922; }
  .forge-branch { font-family: monospace; font-size: 11px; }
  .forge-status { font-size: 11px; }

  /* Legacy workflow cards */
  .workflow-card {
    background: #161b22; border: 1px solid #30363d; border-radius: 8px;
    padding: 16px; display: flex; flex-direction: column; gap: 12px;
  }
  .workflow-card.past { opacity: 0.6; }
  .workflow-card.past:hover { opacity: 1; }

  .workflow-header { display: flex; align-items: center; gap: 8px; }
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

  .tasks { display: flex; flex-direction: column; gap: 4px; }
  .tasks-header { font-size: 12px; color: #8b949e; display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
  .progress-bar { flex: 1; height: 4px; background: #30363d; border-radius: 2px; }
  .progress-fill { height: 100%; background: #a371f7; border-radius: 2px; transition: width 0.3s; }

  .task { display: flex; gap: 8px; align-items: center; font-size: 13px; color: #8b949e; padding: 2px 0; }
  .task.done { color: #3fb950; }
  .task.active { color: #a371f7; }
  .task-status { width: 16px; text-align: center; }

  .delegated { display: flex; flex-direction: column; gap: 4px; }
  .phase-agents { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .phase-label { font-size: 12px; color: #8b949e; }
  .agent-chip { font-size: 11px; background: #21262d; color: #c9d1d9; padding: 2px 8px; border-radius: 12px; }
</style>
