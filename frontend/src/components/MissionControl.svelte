<script lang="ts">
  import { onMount } from 'svelte'
  import { appState } from '$lib/store'
  import { listAllAgents } from '$lib/api'
  import { formatDuration, formatTokens } from '$lib/ui'
  import type { MissionAgent } from '$lib/types'

  let open = $state(false)
  let agents = $state<MissionAgent[]>([])
  let status = $state('')
  let agentType = $state('')
  let allTypes = $state<string[]>([])
  let now = $state(Date.now())
  let loadSeq = 0

  async function load() {
    const seq = ++loadSeq
    try {
      const params: Record<string, string> = {}
      if (status) params.status = status
      if (agentType) params.type = agentType
      const res = await listAllAgents(params)
      if (seq !== loadSeq) return
      agents = res.agents
      if (!status && !agentType) allTypes = [...new Set(res.agents.map(a => a.agent_type))].sort()
    } catch (e) {
      console.error('[mission-control] load failed', e)
    }
  }

  onMount(() => {
    load()
    const tick = setInterval(() => { now = Date.now() }, 10_000)
    return () => clearInterval(tick)
  })

  let debounce: ReturnType<typeof setTimeout> | undefined
  $effect(() => {
    const counter = appState.workflowActivityCounter
    if (counter === 0 || !open) return
    clearTimeout(debounce)
    debounce = setTimeout(load, 500)
    return () => clearTimeout(debounce)
  })

  // Reload when a filter changes.
  $effect(() => {
    void status
    void agentType
    load()
  })

  let running = $derived(agents.filter(a => a.status === 'running' && !a.idle).length)
</script>

<div class="mc">
  <button class="head" onclick={() => (open = !open)}>
    <span>Mission control</span>
    <span class="meta">{agents.length} agent{agents.length === 1 ? '' : 's'} · {running} running</span>
    <span class="caret">{open ? '▲' : '▼'}</span>
  </button>
  {#if open}
    <div class="filters">
      <select bind:value={status} aria-label="Filter by status">
        <option value="">all statuses</option>
        <option value="running">running</option>
        <option value="idle">idle</option>
        <option value="done">done</option>
        <option value="failed">failed</option>
      </select>
      <select bind:value={agentType} aria-label="Filter by agent type">
        <option value="">all agents</option>
        {#each allTypes as t}<option value={t}>{t.replace('delivery-', '')}</option>{/each}
      </select>
    </div>
    {#if agents.length === 0}
      <div class="empty">No agents match.</div>
    {:else}
      <table>
        <thead><tr><th>Agent</th><th>Workflow</th><th>Phase</th><th>Status</th><th>Calls</th><th>Tokens</th><th>Time</th></tr></thead>
        <tbody>
          {#each agents as a (a.id)}
            <tr class:idle={a.idle}>
              <td>{a.agent_type.replace('delivery-', '')}</td>
              <td><a href="#/workflow/{encodeURIComponent(a.workflow_id)}" title={a.workflow_title}>{a.workflow_id}</a></td>
              <td>{a.phase}</td>
              <td>{a.idle ? 'idle' : a.status}{a.verdict ? ` · ${a.verdict}` : ''}</td>
              <td>{a.tool_calls}</td>
              <td>{a.tokens_in + a.tokens_out > 0 ? formatTokens(a.tokens_in + a.tokens_out) : '–'}</td>
              <td>{(void now, formatDuration(a.started_at, a.status === 'running' ? undefined : a.ended_at))}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  {/if}
</div>

<style>
  .mc { background: #161b22; border: 1px solid #30363d; border-radius: 8px; }
  .head { display: flex; align-items: center; gap: 10px; width: 100%; background: none; border: none; color: #c9d1d9; font-size: 13px; font-weight: 600; padding: 10px 14px; cursor: pointer; text-align: left; }
  .meta { color: #8b949e; font-weight: 400; font-size: 12px; flex: 1; }
  .caret { color: #484f58; font-size: 10px; }
  .filters { display: flex; gap: 8px; padding: 0 14px 8px; }
  select { background: #21262d; color: #c9d1d9; border: 1px solid #30363d; border-radius: 5px; font-size: 12px; padding: 3px 6px; }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th { text-align: left; color: #8b949e; font-weight: 600; padding: 4px 14px; }
  td { padding: 4px 14px; color: #c9d1d9; border-top: 1px solid #21262d; }
  tr.idle td { color: #d29922; }
  td a { color: #58a6ff; text-decoration: none; }
  td a:hover { text-decoration: underline; }
  .empty { font-size: 12px; color: #484f58; padding: 4px 14px 12px; }
</style>
