<script lang="ts">
  import { onMount } from 'svelte'
  import { listPeers, listCommsMessages, listSharedTasks } from '$lib/api'
  import { wsClient } from '$lib/ws'
  import { debounce } from '$lib/comms'
  import CommsPeers from '../components/CommsPeers.svelte'
  import CommsFeed from '../components/CommsFeed.svelte'
  import CommsTasks from '../components/CommsTasks.svelte'
  import type { Peer, CommsMessage, SharedTask } from '$lib/types'

  let peers = $state<Peer[]>([])
  let messages = $state<CommsMessage[]>([])
  let tasks = $state<SharedTask[]>([])
  let selected = $state<string | null>(null)
  let showEnded = $state(false)
  let loading = $state(true)
  let error = $state<string | null>(null)
  let filterLabel = $derived(selected ? (peers.find((p) => p.session_key === selected)?.handle ?? selected) : null)

  async function load<T>(fetcher: () => Promise<T>, apply: (v: T) => void) {
    try {
      apply(await fetcher())
      error = null
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load comms data'
    }
  }
  const loadPeers = () => load(listPeers, (v) => (peers = v))
  const loadMessages = () => load(() => listCommsMessages(selected ?? undefined), (v) => (messages = v))
  const loadTasks = () => load(listSharedTasks, (v) => (tasks = v))

  const refreshPeers = debounce(loadPeers, 200)
  const refreshMessages = debounce(loadMessages, 200)
  const refreshTasks = debounce(loadTasks, 200)

  function select(key: string | null) {
    selected = key
    loadMessages()
  }

  onMount(() => {
    Promise.all([loadPeers(), loadMessages(), loadTasks()]).finally(() => (loading = false))
    const offs = [
      wsClient.on('peer_updated', refreshPeers.call),
      wsClient.on('message_sent', refreshMessages.call),
      wsClient.on('shared_task_updated', refreshTasks.call),
    ]
    return () => {
      offs.forEach((off) => off())
      for (const d of [refreshPeers, refreshMessages, refreshTasks]) d.cancel()
    }
  })
</script>

<div class="comms">
  {#if error}<div class="error-banner" role="alert">{error}</div>{/if}
  {#if loading}
    <div class="loading">Loading…</div>
  {:else}
    <div class="grid">
      <CommsPeers {peers} {selected} {showEnded} onselect={select} ontoggleended={(v) => (showEnded = v)} onchanged={loadPeers} />
      <CommsFeed {messages} {peers} {filterLabel} onclearfilter={() => select(null)} onsent={loadMessages} />
      <CommsTasks {tasks} {peers} onchanged={loadTasks} />
    </div>
  {/if}
</div>

<style>
  .comms { height: 100%; display: flex; flex-direction: column; }
  .grid { flex: 1; min-height: 0; display: grid; grid-template-columns: minmax(220px, 1fr) minmax(320px, 2fr) minmax(260px, 1.2fr); gap: 14px; }
  .loading { text-align: center; padding: 48px; color: #8b949e; }
  .error-banner { padding: 8px 14px; margin-bottom: 12px; background: rgba(248,81,73,0.1); border: 1px solid #f85149; border-radius: 6px; color: #f85149; font-size: 13px; }
  @media (max-width: 900px) {
    .grid { grid-template-columns: 1fr; grid-auto-rows: minmax(320px, auto); overflow-y: auto; }
  }
</style>
