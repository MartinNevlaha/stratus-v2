<script lang="ts">
  import { createSharedTask } from '$lib/api'
  import { groupTasks, describeCommsError } from '$lib/comms'
  import CommsTaskCard from './CommsTaskCard.svelte'
  import type { Peer, SharedTask } from '$lib/types'

  interface Props { tasks: SharedTask[]; peers: Peer[]; onchanged: () => void }
  let { tasks, peers, onchanged }: Props = $props()

  let title = $state('')
  let description = $state('')
  let assignee = $state('')
  let error = $state<string | null>(null)
  let handles = $derived(peers.filter((p) => p.active).map((p) => p.handle))
  let groups = $derived(groupTasks(tasks))
  const columns = [
    { key: 'open', label: 'Open' },
    { key: 'claimed', label: 'Claimed' },
    { key: 'done', label: 'Done' },
  ] as const

  async function create() {
    if (!title.trim()) return
    try {
      await createSharedTask(title.trim(), description.trim(), assignee)
      title = ''
      description = ''
      assignee = ''
      error = null
      onchanged()
    } catch (e) {
      error = describeCommsError(e)
    }
  }
</script>

<section class="col" aria-label="Shared tasks">
  <h2>Shared tasks</h2>
  <form onsubmit={(e) => { e.preventDefault(); create() }}>
    <input aria-label="Task title" placeholder="Title" bind:value={title} />
    <textarea aria-label="Task description" rows="2" placeholder="Description (optional)" bind:value={description}></textarea>
    <div class="line">
      <select aria-label="Assignee" bind:value={assignee}>
        <option value="">unassigned</option>
        {#each handles as h}<option value={h}>{h}</option>{/each}
      </select>
      <button type="submit" disabled={!title.trim()}>Create</button>
    </div>
  </form>
  {#if error}<div class="err" role="alert">{error}</div>{/if}
  <div class="board">
    {#each columns as c (c.key)}
      <div class="lane">
        <h3>{c.label} <span class="count">{groups[c.key].length}</span></h3>
        {#each groups[c.key] as t (t.id)}
          <CommsTaskCard task={t} {handles} {onchanged} />
        {/each}
      </div>
    {/each}
  </div>
</section>

<style>
  .col { display: flex; flex-direction: column; min-height: 0; background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 12px; }
  h2 { font-size: 13px; text-transform: uppercase; letter-spacing: 0.04em; color: #8b949e; margin-bottom: 8px; }
  h3 { font-size: 12px; color: #8b949e; margin-bottom: 6px; }
  .count { background: #21262d; border-radius: 10px; padding: 0 6px; font-size: 11px; }
  form { display: flex; flex-direction: column; gap: 6px; margin-bottom: 10px; }
  .line { display: flex; gap: 6px; }
  input, textarea, select { background: #0d1117; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; padding: 5px 8px; font: inherit; font-size: 13px; min-width: 0; }
  select { flex: 1; }
  textarea { resize: vertical; }
  button[type='submit'] { padding: 5px 14px; background: #238636; color: white; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; }
  button[type='submit']:disabled { opacity: 0.5; cursor: default; }
  .board { flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 12px; }
  .lane { display: flex; flex-direction: column; gap: 6px; }
  .err { margin-bottom: 8px; padding: 6px 10px; background: rgba(248,81,73,0.1); border: 1px solid #f85149; border-radius: 6px; color: #f85149; font-size: 12px; }
</style>
