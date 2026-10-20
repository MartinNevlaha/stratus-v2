<script lang="ts">
  import { onMount } from 'svelte'
  import { appState } from '$lib/store'
  import { getWorkflowAgents, getWorkflowLogs, getWorkflowDenials, setWorkflowAgentStatus, resetTask, skipTask } from '$lib/api'
  import { relativeTime } from '$lib/ui'
  import AgentCard from './AgentCard.svelte'
  import TaskBoard from './TaskBoard.svelte'
  import ReviewPanel from './ReviewPanel.svelte'
  import TaskGraph from './TaskGraph.svelte'
  import type { WorkflowState, WorkflowAgent, WorkflowLog, WorkflowDenial, ReviewState } from '$lib/types'

  let { wf }: { wf: WorkflowState } = $props()

  let agents = $state<WorkflowAgent[]>([])
  let review = $state<ReviewState | undefined>()
  let conflicts = $state<string[]>([])
  let logs = $state<WorkflowLog[]>([])
  let denials = $state<WorkflowDenial[]>([])
  let feedOpen = $state(false)
  // Complex specs plan with dependencies, so open on the graph; everything else on the list.
  let viewChoice = $state<'list' | 'graph' | null>(null)
  let view = $derived(viewChoice ?? (wf.complexity === 'complex' ? 'graph' : 'list'))
  // The graph only means something when the plan declared dependencies between tasks.
  let hasDeps = $derived(wf.tasks.some(t => (t.depends_on?.length ?? 0) > 0))
  let now = $state(Date.now())
  let loadSeq = 0

  async function load() {
    const seq = ++loadSeq
    try {
      const [a, l, d] = await Promise.all([getWorkflowAgents(wf.id), getWorkflowLogs(wf.id), getWorkflowDenials(wf.id)])
      if (seq !== loadSeq) return
      agents = a.agents
      review = a.review
      conflicts = a.conflicts
      logs = l
      denials = d.denials
    } catch (e) {
      console.error('[workflow] load activity failed for', wf.id, e)
    }
  }

  async function markFailed(agentId: string) {
    try {
      await setWorkflowAgentStatus(agentId, 'failed')
      await load()
    } catch (e) {
      console.error('[workflow] mark agent failed', agentId, e)
    }
  }

  let active = $derived(!wf.aborted && wf.phase !== 'complete')

  async function skipOne(index: number) {
    try {
      await skipTask(wf.id, index)
      await load()
    } catch (e) {
      console.error('[workflow] skip task', index, e)
    }
  }

  async function retryTask(index: number) {
    try {
      await resetTask(wf.id, index)
      await load()
    } catch (e) {
      console.error('[workflow] reset task', index, e)
    }
  }

  onMount(() => {
    load()
    const tick = setInterval(() => { now = Date.now() }, 10_000)
    return () => clearInterval(tick)
  })

  // Re-fetch on WS activity, coalesced so a burst of tool calls is one request.
  let debounce: ReturnType<typeof setTimeout> | undefined
  $effect(() => {
    const counter = appState.workflowActivityCounter
    if (counter === 0) return
    clearTimeout(debounce)
    debounce = setTimeout(load, 400)
    return () => clearTimeout(debounce)
  })

  let running = $derived(agents.filter(a => a.status === 'running').length)
  let delegatedOnly = $derived(
    Object.entries(wf.delegated_agents).filter(([, list]) => list.length > 0),
  )
</script>

{#if wf.tasks.length > 0}
  {#if hasDeps}
    <div class="view-toggle">
      <button class:active={view === 'list'} onclick={() => (viewChoice = 'list')}>List</button>
      <button class:active={view === 'graph'} onclick={() => (viewChoice = 'graph')}>Graph</button>
    </div>
  {/if}
  {#if hasDeps && view === 'graph'}
    <TaskGraph tasks={wf.tasks} />
  {:else}
    <TaskBoard tasks={wf.tasks} {agents} {now} label={wf.type === 'bug' ? 'Fix attempts' : 'Tasks'}
      onretry={active ? retryTask : undefined} onskip={active ? skipOne : undefined} />
  {/if}
{/if}

{#if agents.length > 0}
  <ReviewPanel {agents} {review} />
  <div class="section">
    <div class="label">Agents ({running} running / {agents.length})</div>
    <div class="agents">
      {#each agents as agent (agent.id)}
        <AgentCard {agent} {now} onfail={markFailed} />
      {/each}
    </div>
    {#if conflicts.length > 0}
      <div class="conflict" title={conflicts.join('\n')}>
        ⚠ {conflicts.length} file{conflicts.length === 1 ? '' : 's'} touched by more than one agent: {conflicts.slice(0, 3).join(', ')}{conflicts.length > 3 ? '…' : ''}
      </div>
    {/if}
  </div>
{:else if delegatedOnly.length > 0}
  <div class="delegated">
    {#each delegatedOnly as [phase, list]}
      <div class="phase-agents">
        <span class="phase-label">{phase}:</span>
        {#each list as agent}<span class="agent-chip">{agent}</span>{/each}
      </div>
    {/each}
  </div>
{/if}

{#if denials.length > 0}
  <div class="section">
    <div class="label">Blocked by guards ({denials.length})</div>
    {#each denials.slice(0, 5) as d}
      <div class="denial" title={d.reason}>
        <span class="hook">{d.hook ?? 'guard'}</span>
        <span class="tool">{d.tool_name ?? ''}{d.agent_type ? ' · ' + d.agent_type.replace('delivery-', '') : ''}</span>
        <span class="reason">{d.reason ?? ''}</span>
      </div>
    {/each}
  </div>
{/if}

{#if logs.length > 0}
  <div class="section">
    <button class="feed-toggle" onclick={() => (feedOpen = !feedOpen)}>
      Activity ({logs.length}) {feedOpen ? '▲' : '▼'}
    </button>
    {#if feedOpen}
      <div class="feed">
        {#each [...logs].reverse().slice(0, 50) as log (log.id)}
          <div class="log">
            <span class="log-tool">{log.tool_name}</span>
            <span class="log-summary">{log.summary}</span>
            <span class="log-ts">{relativeTime(log.ts)}</span>
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/if}

<style>
  .view-toggle { display: flex; gap: 4px; }
  .view-toggle button { background: #21262d; border: 1px solid #30363d; border-radius: 5px; color: #8b949e; font-size: 11px; padding: 2px 10px; cursor: pointer; }
  .view-toggle button.active { background: #1f6feb22; border-color: #1f6feb; color: #58a6ff; }
  .section { display: flex; flex-direction: column; gap: 6px; }
  .label { font-size: 12px; color: #8b949e; font-weight: 600; }
  .agents { display: flex; flex-direction: column; gap: 4px; }
  .conflict { font-size: 11px; color: #d29922; background: #2d2200; padding: 4px 8px; border-radius: 4px; }
  .delegated { display: flex; flex-direction: column; gap: 4px; }
  .phase-agents { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .phase-label { font-size: 12px; color: #8b949e; }
  .agent-chip { font-size: 11px; background: #21262d; color: #c9d1d9; padding: 2px 8px; border-radius: 12px; }
  .denial { display: flex; gap: 8px; align-items: baseline; font-size: 11px; background: #3d1f1f; border-radius: 4px; padding: 3px 8px; }
  .denial .hook { color: #f85149; font-weight: 600; flex-shrink: 0; }
  .denial .tool { color: #8b949e; flex-shrink: 0; }
  .denial .reason { color: #c9d1d9; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .feed-toggle {
    align-self: flex-start; background: none; border: none; color: #8b949e;
    font-size: 12px; cursor: pointer; padding: 0;
  }
  .feed-toggle:hover { color: #c9d1d9; }
  .feed { display: flex; flex-direction: column; gap: 2px; max-height: 220px; overflow-y: auto; }
  .log { display: flex; gap: 8px; align-items: baseline; font-size: 11px; color: #8b949e; }
  .log-tool { color: #58a6ff; flex-shrink: 0; min-width: 44px; }
  .log-summary { flex: 1; font-family: monospace; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .log-ts { color: #484f58; flex-shrink: 0; }
</style>
