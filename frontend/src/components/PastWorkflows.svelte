<script lang="ts">
  import { onMount } from 'svelte'
  import { listPastItems, deleteWorkflow } from '$lib/api'
  import { reportError } from '$lib/errors'
  import type { PastItem } from '$lib/types'
  import WorkflowCard from './WorkflowCard.svelte'

  interface Props {
    onWorkflowsChanged?: () => void
  }

  let { onWorkflowsChanged }: Props = $props()

  let pastItems = $state<PastItem[]>([])
  let pastTotal = $state(0)
  let pastOffset = $state(0)
  let pastLoading = $state(false)
  let pastLoadSeq = 0
  const PAST_LIMIT = 20

  async function loadPastItems(append = false) {
    if (pastLoading) return
    const seq = ++pastLoadSeq
    try {
      pastLoading = true
      const res = await listPastItems(PAST_LIMIT, append ? pastOffset : 0)
      if (seq !== pastLoadSeq) return
      if (append) {
        pastItems = [...pastItems, ...res.items]
      } else {
        pastItems = res.items
        pastOffset = 0
      }
      pastTotal = res.total
      pastOffset = append ? pastOffset + res.items.length : res.items.length
    } catch (e) { reportError('Failed to load past items', e) } finally {
      if (seq === pastLoadSeq) pastLoading = false
    }
  }

  async function loadMorePast() {
    await loadPastItems(true)
  }

  async function handleDelete(id: string) {
    try {
      await deleteWorkflow(id)
      pastItems = pastItems.filter(item => {
        if (item.kind === 'workflow') return item.data.id !== id
        return true
      })
      pastTotal = Math.max(0, pastTotal - 1)
      onWorkflowsChanged?.()
    } catch (e) {
      console.error('delete workflow failed', e)
      loadPastItems()
    }
  }

  onMount(() => { loadPastItems() })

  export function refresh() {
    loadPastItems()
  }
</script>

{#if pastItems.length > 0}
  <div class="section-title">Past ({pastTotal})</div>
  {#each pastItems as item}
    {#if item.kind === 'mission'}
      {@const mission = item.data}
      <div class="mission-card past">
        <div class="mission-header-static">
          <span class="mission-title">{mission.title}</span>
          <span class="mission-phase" class:failed={mission.status === 'failed'}>{mission.status}</span>
          <span class="wf-ts">{new Date(mission.updated_at).toLocaleDateString()}</span>
        </div>
      </div>
    {:else}
      {@const wf = item.data}
      <WorkflowCard {wf} onDelete={handleDelete} past={true} />
    {/if}
  {/each}
  {#if pastOffset < pastTotal}
    <button class="load-more-btn" onclick={loadMorePast} disabled={pastLoading}>
      {pastLoading ? 'Loading…' : `Load more (${pastTotal - pastOffset} remaining)`}
    </button>
  {/if}
{/if}

<style>
  .section-title { font-size: 13px; font-weight: 600; color: #8b949e; text-transform: uppercase; letter-spacing: 0.05em; }

  .mission-card {
    background: #161b22; border: 1px solid #30363d; border-radius: 8px;
    overflow: hidden;
  }
  .mission-card.past { opacity: 0.6; }
  .mission-card.past:hover { opacity: 1; }
  .mission-header-static {
    display: flex; align-items: center; gap: 8px; padding: 12px 16px;
    font-size: 14px; color: #c9d1d9;
  }
  .mission-title { flex: 1; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mission-phase { font-size: 12px; color: #8b949e; }
  .mission-phase.failed { color: #f85149; }
  .wf-ts { font-size: 11px; color: #8b949e; }

  .load-more-btn {
    background: #21262d; border: 1px solid #30363d; color: #8b949e; cursor: pointer;
    font-size: 12px; padding: 8px 16px; border-radius: 6px; text-align: center;
    transition: background 0.1s, color 0.1s;
  }
  .load-more-btn:hover:not(:disabled) { background: #30363d; color: #c9d1d9; }
  .load-more-btn:disabled { opacity: 0.5; cursor: default; }
</style>
