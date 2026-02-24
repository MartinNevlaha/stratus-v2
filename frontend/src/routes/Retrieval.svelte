<script lang="ts">
  import { retrieve, triggerReIndex } from '$lib/api'
  import { appState } from '$lib/store'
  import SttButton from '../components/SttButton.svelte'
  import type { SearchResult } from '$lib/types'

  let query = $state('')
  let corpus = $state<'' | 'code' | 'governance'>('')
  let results = $state<SearchResult[]>([])
  let loading = $state(false)
  let indexing = $state(false)
  let error = $state<string | null>(null)

  let vexorOk = $derived(appState.dashboard?.vexor_available ?? false)

  async function search() {
    if (!query.trim()) return
    loading = true
    error = null
    try {
      const res = await retrieve(query, corpus || undefined)
      results = res.results
    } catch (e) {
      error = e instanceof Error ? e.message : 'Search failed'
    } finally {
      loading = false
    }
  }

  async function reindex() {
    indexing = true
    try {
      await triggerReIndex()
    } finally {
      setTimeout(() => (indexing = false), 2000)
    }
  }

  function onSttTranscript(text: string) {
    query = text
    search()
  }
</script>

<div class="retrieval">
  <div class="search-bar">
    <input
      type="text"
      placeholder="Search code and governance docs…"
      bind:value={query}
      onkeydown={(e) => e.key === 'Enter' && search()}
    />
    <select bind:value={corpus}>
      <option value="">Auto</option>
      <option value="code">Code</option>
      <option value="governance">Governance</option>
    </select>
    <button onclick={search} disabled={loading}>
      {loading ? '…' : 'Search'}
    </button>
    <SttButton onTranscript={onSttTranscript} />
    <button class="secondary" onclick={reindex} disabled={indexing} title="Re-index governance docs">
      {indexing ? 'Indexing…' : '↺ Re-index'}
    </button>
  </div>

  <div class="status">
    <span class="badge" class:ok={vexorOk} class:warn={!vexorOk}>
      Vexor {vexorOk ? '✓' : '✗ (governance only)'}
    </span>
  </div>

  {#if error}
    <div class="error">{error}</div>
  {/if}

  <div class="results">
    {#if results.length === 0 && !loading}
      <div class="empty">Search code and governance documents above</div>
    {/if}
    {#each results as r}
      <div class="result-card">
        <div class="result-header">
          <span class="source" class:code={r.source === 'code'}>{r.source}</span>
          {#if r.doc_type}
            <span class="doc-type">{r.doc_type}</span>
          {/if}
          <span class="file-path">{r.file_path}</span>
          <span class="score">{(r.score * -1).toFixed(3)}</span>
        </div>
        {#if r.title}
          <div class="result-title">{r.title}</div>
        {/if}
        <pre class="excerpt">{r.excerpt}</pre>
      </div>
    {/each}
  </div>
</div>

<style>
  .retrieval { display: flex; flex-direction: column; gap: 16px; }

  .search-bar { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  input { flex: 1; min-width: 200px; padding: 8px 12px; background: #0d1117; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; font-size: 14px; }
  input:focus { outline: none; border-color: #58a6ff; }
  select { padding: 8px 12px; background: #21262d; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; cursor: pointer; }
  button { padding: 8px 16px; background: #238636; border: none; border-radius: 6px; color: white; cursor: pointer; font-size: 14px; }
  button:hover:not(:disabled) { background: #2ea043; }
  button.secondary { background: #21262d; border: 1px solid #30363d; color: #c9d1d9; }
  button.secondary:hover:not(:disabled) { background: #30363d; }
  button:disabled { opacity: 0.5; }

  .status { display: flex; gap: 8px; }
  .badge { padding: 2px 8px; border-radius: 12px; font-size: 12px; background: #21262d; color: #8b949e; }
  .badge.ok { color: #3fb950; }
  .badge.warn { color: #d29922; }

  .error { color: #f85149; }
  .empty { color: #8b949e; text-align: center; padding: 32px; }

  .results { display: flex; flex-direction: column; gap: 8px; }

  .result-card { padding: 12px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; }
  .result-header { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; flex-wrap: wrap; }
  .source { font-size: 11px; padding: 1px 6px; border-radius: 4px; background: #21262d; color: #8b949e; }
  .source.code { background: #1f3056; color: #58a6ff; }
  .doc-type { font-size: 11px; background: #2d1f3d; color: #bc8cff; padding: 1px 6px; border-radius: 4px; }
  .file-path { font-size: 12px; color: #8b949e; font-family: monospace; flex: 1; }
  .score { font-size: 11px; color: #8b949e; }
  .result-title { font-weight: 600; color: #c9d1d9; margin-bottom: 6px; }
  pre.excerpt { font-size: 12px; color: #8b949e; white-space: pre-wrap; word-break: break-all; margin: 0; line-height: 1.5; background: #0d1117; padding: 8px; border-radius: 4px; max-height: 200px; overflow-y: auto; }
</style>
