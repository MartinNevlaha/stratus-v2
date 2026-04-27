<script lang="ts">
  import { onMount } from 'svelte'
  import { appState } from '$lib/store'
  import { getMission, listMissions } from '$lib/api'
  import SwarmGraph from './SwarmGraph.svelte'
  import SignalBus from './SignalBus.svelte'
  import EvidenceTrail from './EvidenceTrail.svelte'
  import type { WorkflowState, SwarmMission, SwarmMissionDetail } from '$lib/types'

  interface Props {
    wf: WorkflowState
    missions: SwarmMission[]
  }

  let { wf, missions }: Props = $props()

  let swarmDetails = $state<Record<string, SwarmMissionDetail | null>>({})
  let swarmLoading = $state<Record<string, boolean>>({})
  let swarmViews = $state<Record<string, 'list' | 'graph'>>({})
  let expandedTickets = $state<Set<string>>(new Set())
  let _heartbeatTick = $state(0)
  let _pollInterval: ReturnType<typeof setInterval> | undefined

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

  function workerTicketCounts(detail: SwarmMissionDetail, workerId: string): string {
    const assigned = detail.tickets.filter(t => t.worker_id === workerId)
    if (assigned.length === 0) return ''
    const done = assigned.filter(t => t.status === 'done').length
    return `${done}/${assigned.length}`
  }

  function relativeTime(ts: string): string {
    const diff = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
    if (diff < 5) return 'just now'
    if (diff < 60) return `${diff}s ago`
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
    return `${Math.floor(diff / 3600)}h ago`
  }

  function toggleSet(set: Set<string>, id: string): Set<string> {
    const next = new Set(set)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  }

  async function loadSwarmDetail(missionList?: SwarmMission[]) {
    const list = missionList ?? missions
    const mission = list.find(m => m.workflow_id === wf.id)
    if (!mission) { swarmDetails[wf.id] = null; return }
    swarmLoading[wf.id] = true
    try {
      const detail = await getMission(mission.id)
      swarmDetails[wf.id] = detail
    } catch (e) {
      console.error('[swarm] loadSwarmDetail failed for', wf.id, e)
      swarmDetails[wf.id] = null
    } finally {
      swarmLoading[wf.id] = false
    }
  }

  onMount(() => {
    loadSwarmDetail()
    _pollInterval = setInterval(() => {
      _heartbeatTick++
      loadSwarmDetail()
    }, 5000)
    return () => clearInterval(_pollInterval)
  })

  $effect(() => {
    const _ = appState.swarmUpdateCounter
    if (_ === 0) return
    listMissions().then(m => loadSwarmDetail(m)).catch(() => {})
  })
</script>

{#if swarmLoading[wf.id] && !swarmDetails[wf.id]}
  <div class="swarm-inline">
    <div class="detail-loading">Loading mission…</div>
  </div>
{:else if swarmDetails[wf.id]}
  {@const detail = swarmDetails[wf.id]!}
  <div class="swarm-inline">
    {#if detail.tickets.length > 0}
      <div class="mission-view-toggle">
        <button class="view-btn" class:active={(swarmViews[wf.id] ?? 'list') === 'list'} onclick={() => swarmViews[wf.id] = 'list'}>List</button>
        <button class="view-btn" class:active={(swarmViews[wf.id] ?? 'list') === 'graph'} onclick={() => swarmViews[wf.id] = 'graph'}>Graph</button>
      </div>
    {/if}
    {#if (swarmViews[wf.id] ?? 'list') === 'graph' && detail.tickets.length > 0}
      <SwarmGraph detail={detail} />
    {:else}
      {#if detail.workers.length > 0}
        <div class="detail-section">
          <div class="detail-label">Workers</div>
          <div class="workers-grid">
            {#each detail.workers as worker}
              <div class="worker-node" class:failed={worker.status === 'failed' || worker.status === 'killed'} class:stale={worker.status === 'stale'}>
                <span class="worker-dot" class:active={worker.status === 'active'} style="background: {workerStatusColor(worker.status)}"></span>
                <span class="worker-type">{worker.agent_type.replace('delivery-', '')}</span>
                <span class="worker-status">{worker.status}</span>
                {#if workerTicketCounts(detail, worker.id)}
                  <span class="worker-tickets">{workerTicketCounts(detail, worker.id)}</span>
                {/if}
                {#if worker.status === 'active'}
                  <span class="worker-heartbeat">{(() => { const _ = _heartbeatTick; const hb = appState.lastHeartbeats[worker.id]; return hb ? relativeTime(new Date(hb).toISOString()) : relativeTime(worker.last_heartbeat) })()}</span>
                {/if}
              </div>
            {/each}
          </div>
        </div>
      {/if}
      {#if detail.tickets.length > 0}
        <div class="detail-section">
          <div class="detail-label">
            Tickets ({detail.tickets.filter(t => t.status === 'done').length}/{detail.tickets.length})
            <div class="progress-bar">
              <div class="ticket-progress-fill"
                class:active={detail.tickets.some(t => t.status === 'in_progress' || t.status === 'assigned')}
                style="width: {detail.tickets.length > 0 ? (detail.tickets.filter(t => t.status === 'done').length / detail.tickets.length) * 100 : 0}%"
              ></div>
            </div>
          </div>
          {#each detail.tickets as ticket}
            <div class="ticket" class:done={ticket.status === 'done'} class:active={ticket.status === 'in_progress'} class:failed={ticket.status === 'failed'}
              role="button" tabindex="0"
              onclick={() => { expandedTickets = toggleSet(expandedTickets, ticket.id) }}
              onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') expandedTickets = toggleSet(expandedTickets, ticket.id) }}
              style="cursor: pointer"
            >
              <span class="ticket-icon" class:spinning={ticket.status === 'in_progress'}>{ticketIcon(ticket.status)}</span>
              <span class="ticket-title">{ticket.title}</span>
              {#if ticket.worker_id}
                {@const worker = detail.workers.find(w => w.id === ticket.worker_id)}
                {#if worker}
                  <span class="ticket-worker-chip">{worker.agent_type.replace('delivery-', '')}</span>
                {/if}
              {/if}
              <span class="ticket-domain">{ticket.domain}</span>
              <span class="ticket-expand">{expandedTickets.has(ticket.id) ? '▲' : '▼'}</span>
            </div>
            {#if expandedTickets.has(ticket.id)}
              <EvidenceTrail ticketId={ticket.id} />
            {/if}
          {/each}
        </div>
      {/if}
      <SignalBus missionId={detail.mission.id} />
      {#if detail.forge.length > 0}
        <div class="detail-section">
          <div class="detail-label">Forge (merge queue)</div>
          {#each detail.forge as entry}
            <div class="forge-entry" class:merged={entry.status === 'merged'} class:conflict={entry.status === 'conflict'}>
              {#if entry.status === 'merged'}<span class="forge-check">&#10003;</span>{/if}
              <span class="forge-branch">{entry.branch_name}</span>
              <span class="forge-status">{entry.status}</span>
            </div>
          {/each}
        </div>
      {/if}
    {/if}
  </div>
{:else}
  <div class="swarm-inline">
    <div class="swarm-waiting">Awaiting dispatch…</div>
  </div>
{/if}

<style>
  .swarm-inline {
    padding: 0 0 4px 0;
    border-top: 1px solid #21262d;
    margin-top: 8px;
  }
  .swarm-waiting {
    font-size: 12px; color: #484f58; padding: 10px 0 4px;
    font-style: italic;
  }

  .mission-view-toggle {
    display: flex; gap: 4px; padding: 0 0 8px;
  }
  .view-btn {
    background: #21262d; border: 1px solid #30363d; border-radius: 5px;
    color: #8b949e; font-size: 11px; padding: 3px 10px; cursor: pointer; transition: all 0.1s;
  }
  .view-btn:hover { color: #c9d1d9; border-color: #484f58; }
  .view-btn.active { background: #1f6feb22; border-color: #1f6feb; color: #58a6ff; }

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
  .ticket-expand { font-size: 9px; color: #484f58; flex-shrink: 0; margin-left: 4px; }

  .ticket-progress-fill { height: 100%; background: #a371f7; border-radius: 2px; transition: width 0.3s; }

  .forge-entry {
    display: flex; align-items: center; gap: 8px; font-size: 12px; color: #8b949e; padding: 2px 0;
  }
  .forge-entry.merged { color: #3fb950; }
  .forge-entry.conflict { color: #d29922; }
  .forge-branch { font-family: monospace; font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .forge-status { font-size: 11px; }

  .detail-loading { font-size: 12px; color: #8b949e; padding: 12px 0; }

  @keyframes pulse {
    0%, 100% { box-shadow: 0 0 0 0 rgba(63, 185, 80, 0.6); }
    50% { box-shadow: 0 0 0 4px rgba(63, 185, 80, 0); }
  }
  .worker-dot.active { animation: pulse 2s ease-in-out infinite; }

  @keyframes spin { to { transform: rotate(360deg); } }
  .ticket-icon.spinning { display: inline-block; animation: spin 1s linear infinite; }

  @keyframes fadeSlideIn {
    from { opacity: 0; transform: translateY(-8px); }
    to { opacity: 1; transform: translateY(0); }
  }
  .worker-node, .ticket, .forge-entry { animation: fadeSlideIn 0.3s ease-out; }

  @keyframes shimmer {
    from { background-position: -200% 0; }
    to { background-position: 200% 0; }
  }
  .ticket-progress-fill.active {
    background: linear-gradient(90deg, #a371f7 30%, #c9a0ff 50%, #a371f7 70%);
    background-size: 200% 100%;
    animation: shimmer 2s linear infinite;
  }

  @keyframes shake {
    0%, 100% { transform: translateX(0); }
    25% { transform: translateX(-2px); }
    75% { transform: translateX(2px); }
  }
  .worker-node.failed { animation: shake 0.4s ease-in-out; }
  .worker-node.stale { border: 1px solid #d29922; }

  @keyframes scaleIn {
    from { transform: scale(0); opacity: 0; }
    to { transform: scale(1); opacity: 1; }
  }
  .forge-check { display: inline-block; color: #3fb950; font-weight: bold; animation: scaleIn 0.3s ease-out; }

  .worker-tickets { font-size: 10px; color: #58a6ff; background: #1f3056; padding: 1px 5px; border-radius: 3px; }
  .worker-heartbeat { font-size: 10px; color: #8b949e; }
  .ticket-worker-chip { font-size: 10px; background: #2d1f56; color: #a371f7; padding: 1px 6px; border-radius: 4px; flex-shrink: 0; }
  .ticket { transition: color 0.3s, opacity 0.3s; }
</style>
