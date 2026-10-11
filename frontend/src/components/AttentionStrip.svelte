<script lang="ts">
  import { onMount } from 'svelte'
  import { appState } from '$lib/store'
  import { getAttention } from '$lib/api'
  import type { AttentionItem } from '$lib/types'

  let items = $state<AttentionItem[]>([])
  let timer: ReturnType<typeof setTimeout> | undefined

  async function load() {
    try {
      items = (await getAttention()).items
    } catch {
      /* the strip is advisory; stay quiet when the API is unreachable */
    }
  }

  onMount(() => {
    load()
    const poll = setInterval(load, 30_000)
    return () => clearInterval(poll)
  })

  $effect(() => {
    const counter = appState.workflowActivityCounter
    if (counter === 0) return
    clearTimeout(timer)
    timer = setTimeout(load, 600)
    return () => clearTimeout(timer)
  })

  function reasons(it: AttentionItem): string[] {
    const r: string[] = []
    if (it.review_failed) r.push('review failed')
    if (it.stale_agents > 0) r.push(`${it.stale_agents} idle agent${it.stale_agents === 1 ? '' : 's'}`)
    if (it.conflicts.length > 0) r.push(`${it.conflicts.length} shared file${it.conflicts.length === 1 ? '' : 's'}`)
    return r
  }
</script>

{#if items.length > 0}
  <div class="strip">
    <span class="label">Needs attention</span>
    {#each items as it (it.workflow_id)}
      <a class="item" href="#/workflow/{encodeURIComponent(it.workflow_id)}" title={it.title}>
        <span class="id">{it.workflow_id}</span>
        <span class="why">{reasons(it).join(' · ')}</span>
      </a>
    {/each}
  </div>
{/if}

<style>
  .strip { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; background: #2d2200; border: 1px solid #5a4500; border-radius: 6px; padding: 6px 10px; font-size: 12px; }
  .label { color: #d29922; font-weight: 600; }
  .item { display: flex; gap: 6px; background: #161b22; border-radius: 4px; padding: 2px 8px; text-decoration: none; color: #c9d1d9; }
  .item:hover { outline: 1px solid #d29922; }
  .why { color: #d29922; }
</style>
