<script lang="ts">
  import { onMount } from 'svelte'
  import { appState } from '$lib/store'
  import { getMissionSignals } from '$lib/api'
  import type { SwarmSignal } from '$lib/types'

  let { missionId }: { missionId: string } = $props()

  let signals = $state<SwarmSignal[]>([])
  let expanded = $state(false)
  let filter = $state('')
  let loading = $state(false)

  const signalColors: Record<string, string> = {
    TICKET_DONE: '#3fb950', MERGED: '#3fb950', MISSION_DONE: '#3fb950',
    CONFLICT: '#f85149', ABORT: '#f85149', TICKET_FAILED: '#f85149', GUARDRAIL_BLOCK: '#f85149',
    HELP: '#e3b341', ESCALATE: '#e3b341', PLAN_DRIFT: '#e3b341', GUARDRAIL_WARN: '#e3b341',
    TICKET_ASSIGNED: '#58a6ff', TICKET_STARTED: '#58a6ff', MERGE_READY: '#58a6ff',
  }

  const signalIcons: Record<string, string> = {
    TICKET_DONE: '✓', MERGED: '⊕', MISSION_DONE: '★',
    CONFLICT: '✕', ABORT: '⊘', TICKET_FAILED: '✕', GUARDRAIL_BLOCK: '⛔',
    HELP: '?', ESCALATE: '↑', PLAN_DRIFT: '~', GUARDRAIL_WARN: '⚠',
    TICKET_ASSIGNED: '→', TICKET_STARTED: '▶', MERGE_READY: '⊞',
  }

  async function load() {
    loading = true
    try {
      signals = await getMissionSignals(missionId)
    } catch { /* ignore */ }
    loading = false
  }

  onMount(load)

  $effect(() => {
    const _ = appState.swarmUpdateCounter
    if (expanded) load()
  })

  let filtered = $derived(filter ? signals.filter(s => s.type === filter) : signals)
  let types = $derived([...new Set(signals.map(s => s.type))])

  function relativeTime(ts: string): string {
    const diff = Date.now() - new Date(ts).getTime()
    if (diff < 60000) return `${Math.floor(diff / 1000)}s ago`
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
    return `${Math.floor(diff / 3600000)}h ago`
  }

  function shortId(id: string): string {
    return id.length > 12 ? id.slice(0, 12) + '…' : id
  }
</script>

<div class="signal-bus">
  <div class="sb-header" role="button" tabindex="0"
    onclick={() => { expanded = !expanded; if (expanded && signals.length === 0) load() }}
    onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') expanded = !expanded }}
  >
    <span class="sb-title">
      Signal Bus
      {#if signals.length > 0}
        <span class="sb-count">{signals.length}</span>
      {/if}
      {#if loading}<span class="sb-spinner">⟳</span>{/if}
    </span>
    <span class="sb-arrow">{expanded ? '▲' : '▼'}</span>
  </div>
  {#if expanded}
    <div class="sb-body">
      {#if types.length > 1}
        <div class="sb-filters">
          <button class="sb-filter-btn" class:active={filter === ''} onclick={() => (filter = '')}>All</button>
          {#each types as t}
            <button class="sb-filter-btn" class:active={filter === t}
              onclick={() => (filter = filter === t ? '' : t)}
              style="color: {signalColors[t] ?? '#8b949e'}"
            >{t}</button>
          {/each}
        </div>
      {/if}
      {#if filtered.length === 0}
        <div class="sb-empty">No signals yet.</div>
      {:else}
        {#each filtered as sig}
          <div class="sb-row">
            <span class="sig-icon" style="color: {signalColors[sig.type] ?? '#8b949e'}">
              {signalIcons[sig.type] ?? '·'}
            </span>
            <span class="sig-type" style="color: {signalColors[sig.type] ?? '#8b949e'}">{sig.type}</span>
            <span class="sig-route">{shortId(sig.from_worker)} → {sig.to_worker === '*' ? 'all' : shortId(sig.to_worker)}</span>
            <span class="sig-time">{relativeTime(sig.created_at)}</span>
          </div>
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  .signal-bus {
    border-top: 1px solid #21262d;
    margin-top: 8px;
  }

  .sb-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 0;
    cursor: pointer;
    user-select: none;
  }

  .sb-title {
    font-size: 11px;
    font-weight: 600;
    color: #8b949e;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .sb-count {
    background: #21262d;
    border: 1px solid #30363d;
    color: #8b949e;
    font-size: 10px;
    padding: 0 5px;
    border-radius: 10px;
  }

  .sb-spinner {
    animation: spin 0.7s linear infinite;
    display: inline-block;
  }

  @keyframes spin { to { transform: rotate(360deg); } }

  .sb-arrow { font-size: 10px; color: #484f58; }

  .sb-body {
    padding-bottom: 8px;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .sb-filters {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-bottom: 6px;
  }

  .sb-filter-btn {
    background: none;
    border: 1px solid #30363d;
    border-radius: 4px;
    font-size: 10px;
    padding: 1px 6px;
    cursor: pointer;
    color: #8b949e;
  }

  .sb-filter-btn.active { background: #21262d; }
  .sb-filter-btn:hover { border-color: #58a6ff; }

  .sb-empty { font-size: 11px; color: #484f58; padding: 4px 0; }

  .sb-row {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    padding: 2px 0;
  }

  .sig-icon { font-size: 12px; flex-shrink: 0; width: 14px; text-align: center; }
  .sig-type { font-weight: 600; font-size: 10px; flex-shrink: 0; }
  .sig-route { color: #8b949e; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sig-time { color: #484f58; flex-shrink: 0; font-size: 10px; }
</style>
