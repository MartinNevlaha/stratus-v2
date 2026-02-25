<script lang="ts">
  import { onMount } from 'svelte'
  import { searchEvents, getTimeline } from '$lib/api'
  import type { Event } from '$lib/types'
  import SttButton from '../components/SttButton.svelte'

  let query = $state('')
  let results = $state<Event[]>([])
  let isRecent = $state(true)
  let timeline = $state<Event[]>([])
  let selectedId = $state<number | null>(null)
  let loading = $state(false)
  let error = $state<string | null>(null)

  onMount(async () => {
    loading = true
    try {
      const res = await searchEvents('', { limit: '5' })
      results = res.results
    } catch {
      // ignore — empty state is fine
    } finally {
      loading = false
    }
  })

  async function search() {
    if (!query.trim()) return
    loading = true
    error = null
    isRecent = false
    try {
      const res = await searchEvents(query)
      results = res.results
    } catch (e) {
      error = e instanceof Error ? e.message : 'Search failed'
    } finally {
      loading = false
    }
  }

  async function showTimeline(id: number) {
    selectedId = id
    const res = await getTimeline(id)
    timeline = res.events
  }

  function onSttTranscript(text: string) {
    query = text
    search()
  }
</script>

<div class="memory">
  <div class="search-bar">
    <input
      type="text"
      placeholder="Search memory events…"
      bind:value={query}
      onkeydown={(e) => e.key === 'Enter' && search()}
    />
    <button onclick={search} disabled={loading}>
      {loading ? '…' : 'Search'}
    </button>
    <SttButton onTranscript={onSttTranscript} />
  </div>

  {#if error}
    <div class="error">{error}</div>
  {/if}

  <div class="results-layout">
    <div class="results-list">
      {#if results.length === 0 && !loading}
        <div class="empty">No memory events found</div>
      {:else if results.length > 0}
        <div class="results-label">{isRecent ? 'Recent memories' : `Results (${results.length})`}</div>
      {/if}
      {#each results as event}
        <div
          class="event-card"
          class:selected={selectedId === event.id}
          onclick={() => showTimeline(event.id)}
          role="button"
          tabindex="0"
          onkeydown={(e) => e.key === 'Enter' && showTimeline(event.id)}
        >
          <div class="event-header">
            <span class="badge">{event.type}</span>
            <span class="ts">{new Date(event.ts).toLocaleString()}</span>
          </div>
          {#if event.title}
            <div class="title">{event.title}</div>
          {/if}
          <div class="text">{event.text.slice(0, 200)}{event.text.length > 200 ? '…' : ''}</div>
          {#if event.tags.length > 0}
            <div class="tags">
              {#each event.tags as tag}
                <span class="tag">{tag}</span>
              {/each}
            </div>
          {/if}
        </div>
      {/each}
    </div>

    {#if timeline.length > 0}
      <div class="timeline-panel">
        <div class="panel-title">Timeline (anchor #{selectedId})</div>
        {#each timeline as event}
          <div class="tl-event" class:anchor={event.id === selectedId}>
            <span class="tl-type">{event.type}</span>
            <span class="tl-text">{event.title || event.text.slice(0, 100)}</span>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .memory { display: flex; flex-direction: column; gap: 16px; }

  .search-bar { display: flex; gap: 8px; align-items: center; }
  input { flex: 1; padding: 8px 12px; background: #0d1117; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; font-size: 14px; }
  input:focus { outline: none; border-color: #58a6ff; }
  button { padding: 8px 16px; background: #21262d; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; cursor: pointer; }
  button:hover:not(:disabled) { background: #30363d; }
  button:disabled { opacity: 0.5; }

  .error { color: #f85149; font-size: 14px; }
  .empty { color: #8b949e; text-align: center; padding: 32px; }

  .results-layout { display: grid; grid-template-columns: 1fr auto; gap: 16px; }

  .results-list { display: flex; flex-direction: column; gap: 8px; }
  .results-label { font-size: 12px; font-weight: 600; color: #8b949e; text-transform: uppercase; letter-spacing: 0.05em; padding-bottom: 4px; }

  .event-card { padding: 12px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; cursor: pointer; transition: border-color 0.15s; }
  .event-card:hover, .event-card.selected { border-color: #58a6ff; }
  .event-header { display: flex; justify-content: space-between; margin-bottom: 6px; }
  .badge { font-size: 11px; background: #21262d; color: #8b949e; padding: 1px 6px; border-radius: 4px; }
  .ts { font-size: 11px; color: #8b949e; }
  .title { font-weight: 600; color: #c9d1d9; margin-bottom: 4px; }
  .text { font-size: 13px; color: #8b949e; line-height: 1.5; }
  .tags { display: flex; gap: 4px; flex-wrap: wrap; margin-top: 6px; }
  .tag { font-size: 11px; background: #1f3056; color: #58a6ff; padding: 1px 6px; border-radius: 4px; }

  .timeline-panel { width: 300px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; padding: 12px; display: flex; flex-direction: column; gap: 8px; max-height: 600px; overflow-y: auto; }
  .panel-title { font-size: 12px; font-weight: 600; color: #8b949e; text-transform: uppercase; margin-bottom: 4px; }
  .tl-event { display: flex; flex-direction: column; gap: 2px; padding: 6px; border-radius: 4px; border-left: 2px solid transparent; }
  .tl-event.anchor { border-left-color: #58a6ff; background: #1f3056; }
  .tl-type { font-size: 11px; color: #8b949e; }
  .tl-text { font-size: 12px; color: #c9d1d9; }
</style>
