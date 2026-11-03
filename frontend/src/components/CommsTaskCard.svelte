<script lang="ts">
  import { updateSharedTask } from '$lib/api'
  import { describeCommsError } from '$lib/comms'
  import type { SharedTask } from '$lib/types'

  interface Props { task: SharedTask; handles: string[]; onchanged: () => void }
  let { task, handles, onchanged }: Props = $props()

  let finishing = $state(false)
  let result = $state('')
  let error = $state<string | null>(null)
  let closed = $derived(task.status === 'done' || task.status === 'cancelled')

  async function patch(p: Parameters<typeof updateSharedTask>[1]) {
    try {
      await updateSharedTask(task.id, p)
      finishing = false
      error = null
      onchanged()
    } catch (e) {
      error = describeCommsError(e)
    }
  }
</script>

<article class:closed>
  <div class="title">#{task.id} {task.title}{#if task.status === 'cancelled'} <span class="cancelled">cancelled</span>{/if}</div>
  {#if task.description}<p class="desc">{task.description}</p>{/if}
  <div class="meta">by {task.created_by_handle}{#if task.assignee_handle} · assigned to {task.assignee_handle}{/if}</div>
  {#if task.result}<p class="result">{task.result}</p>{/if}
  {#if error}<div class="err" role="alert">{error}</div>{/if}
  {#if !closed}
    {#if finishing}
      <form onsubmit={(e) => { e.preventDefault(); patch({ status: 'done', result }) }}>
        <input aria-label="Result" placeholder="Result" bind:value={result} />
        <button type="submit" class="act ok">done</button>
        <button type="button" class="act" onclick={() => (finishing = false)}>back</button>
      </form>
    {:else}
      <div class="actions">
        <button class="act ok" onclick={() => (finishing = true)}>mark done</button>
        <button class="act danger" onclick={() => patch({ status: 'cancelled' })}>cancel</button>
        <select aria-label="Reassign task {task.id}" value="" onchange={(e) => e.currentTarget.value && patch({ assignee: e.currentTarget.value })}>
          <option value="">reassign…</option>
          {#each handles as h}<option value={h}>{h}</option>{/each}
        </select>
      </div>
    {/if}
  {/if}
</article>

<style>
  article { padding: 8px; border: 1px solid #30363d; border-radius: 6px; background: #0d1117; }
  article.closed { opacity: 0.7; }
  .title { font-weight: 600; font-size: 13px; overflow-wrap: anywhere; }
  .cancelled { font-size: 10px; color: #8b949e; border: 1px solid #30363d; border-radius: 10px; padding: 0 6px; }
  .desc, .result { font-size: 12px; white-space: pre-wrap; overflow-wrap: anywhere; margin-top: 4px; }
  .result { color: #3fb950; }
  .meta { font-size: 11px; color: #8b949e; margin-top: 4px; }
  .actions, form { display: flex; gap: 4px; margin-top: 6px; flex-wrap: wrap; }
  form input, select { background: #0d1117; border: 1px solid #30363d; border-radius: 4px; color: #c9d1d9; font-size: 11px; padding: 2px 4px; min-width: 0; }
  form input { flex: 1; }
  .act { padding: 1px 8px; font-size: 11px; background: transparent; border: 1px solid #30363d; border-radius: 4px; color: #8b949e; cursor: pointer; }
  .act:hover { color: #c9d1d9; border-color: #8b949e; }
  .ok { color: #3fb950; border-color: #238636; }
  .danger { color: #f85149; border-color: #6e2b29; }
  .err { margin-top: 6px; padding: 4px 8px; background: rgba(248,81,73,0.1); border: 1px solid #f85149; border-radius: 6px; color: #f85149; font-size: 11px; }
</style>
