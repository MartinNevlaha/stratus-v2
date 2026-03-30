<script lang="ts">
  import { onMount } from 'svelte'
  import { getTicketEvidence } from '$lib/api'
  import type { SwarmEvidence } from '$lib/types'

  let { ticketId }: { ticketId: string } = $props()

  let evidence = $state<SwarmEvidence[]>([])
  let loading = $state(false)
  let expandedItems = $state<Set<number>>(new Set())

  const typeColors: Record<string, string> = {
    diff: '#58a6ff',
    test_result: '#3fb950',
    review: '#a371f7',
    build: '#f0883e',
    note: '#8b949e',
    gate: '#e3b341',
  }

  function verdictIcon(verdict: string): string {
    if (!verdict) return '·'
    const v = verdict.toLowerCase()
    if (v === 'pass' || v === 'approved' || v === 'success') return '✓'
    if (v === 'fail' || v === 'failed' || v === 'rejected') return '✕'
    if (v === 'warn' || v === 'warning') return '⚠'
    return '·'
  }

  function verdictColor(verdict: string): string {
    if (!verdict) return '#484f58'
    const v = verdict.toLowerCase()
    if (v === 'pass' || v === 'approved' || v === 'success') return '#3fb950'
    if (v === 'fail' || v === 'failed' || v === 'rejected') return '#f85149'
    if (v === 'warn' || v === 'warning') return '#e3b341'
    return '#484f58'
  }

  function relativeTime(ts: string): string {
    const diff = Date.now() - new Date(ts).getTime()
    if (diff < 60000) return `${Math.floor(diff / 1000)}s ago`
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
    return `${Math.floor(diff / 3600000)}h ago`
  }

  function toggleItem(i: number) {
    const next = new Set(expandedItems)
    if (next.has(i)) next.delete(i)
    else next.add(i)
    expandedItems = next
  }

  onMount(async () => {
    loading = true
    try {
      evidence = await getTicketEvidence(ticketId)
    } catch { /* ignore */ }
    loading = false
  })
</script>

<div class="evidence-trail">
  {#if loading}
    <div class="ev-loading">Loading evidence…</div>
  {:else if evidence.length === 0}
    <div class="ev-empty">No evidence recorded for this ticket.</div>
  {:else}
    {#each evidence as item, i}
      <div class="ev-item">
        <div class="ev-header" role="button" tabindex="0"
          onclick={() => toggleItem(i)}
          onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') toggleItem(i) }}
        >
          <span class="ev-icon" style="color: {verdictColor(item.verdict)}">{verdictIcon(item.verdict)}</span>
          <span class="ev-type" style="color: {typeColors[item.type] ?? '#8b949e'}">{item.type}</span>
          {#if item.agent}
            <span class="ev-agent">{item.agent}</span>
          {/if}
          {#if item.verdict}
            <span class="ev-verdict" style="color: {verdictColor(item.verdict)}">{item.verdict}</span>
          {/if}
          <span class="ev-time">{relativeTime(item.created_at)}</span>
          <span class="ev-toggle">{expandedItems.has(i) ? '▲' : '▼'}</span>
        </div>
        {#if expandedItems.has(i) && item.content}
          <pre class="ev-content">{item.content}</pre>
        {/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  .evidence-trail {
    padding: 6px 0 4px 12px;
    border-left: 2px solid #21262d;
    margin: 4px 0 4px 4px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .ev-loading, .ev-empty {
    font-size: 11px;
    color: #484f58;
  }

  .ev-item {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .ev-header {
    display: flex;
    align-items: center;
    gap: 6px;
    cursor: pointer;
    font-size: 11px;
  }

  .ev-icon { flex-shrink: 0; font-size: 12px; width: 14px; text-align: center; }
  .ev-type { font-weight: 600; font-size: 10px; text-transform: uppercase; flex-shrink: 0; }
  .ev-agent { color: #8b949e; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ev-verdict { font-size: 10px; font-weight: 600; flex-shrink: 0; }
  .ev-time { color: #484f58; font-size: 10px; flex-shrink: 0; }
  .ev-toggle { color: #484f58; font-size: 9px; flex-shrink: 0; }

  .ev-content {
    font-size: 11px;
    font-family: monospace;
    color: #c9d1d9;
    background: #0d1117;
    border: 1px solid #21262d;
    border-radius: 4px;
    padding: 8px;
    overflow-x: auto;
    max-height: 200px;
    overflow-y: auto;
    white-space: pre-wrap;
    word-break: break-all;
    margin: 0;
  }
</style>
