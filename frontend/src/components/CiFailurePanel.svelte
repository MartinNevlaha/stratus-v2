<script lang="ts">
  import { getCiFailure } from '$lib/api'
  import { requestTeamTask } from '$lib/store'
  import type { CiFailureResponse } from '$lib/types'

  let loading = $state(false)
  let result = $state<CiFailureResponse | null>(null)
  let error = $state<string | null>(null)
  let showLog = $state(false)

  // Runs `gh` on the server, so it only happens on an explicit click - never on mount or a timer.
  async function check() {
    if (loading) return
    loading = true
    error = null
    showLog = false
    try {
      result = await getCiFailure()
    } catch (e) {
      result = null
      error = e instanceof Error ? e.message : 'neznáma chyba'
    } finally {
      loading = false
    }
  }

  function prepareFix() {
    if (result?.draft) requestTeamTask({ text: result.draft.task_text })
  }

  const isGithubUrl = (u: string) => u.startsWith('https://github.com/')

  function when(iso: string) {
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
  }
</script>

<div class="ci">
  <div class="head">
    <span class="label">CI</span>
    <span class="hint">neúspešný beh GitHub Actions → pripravená oprava</span>
    <button class="act" onclick={check} disabled={loading}>{loading ? 'Kontrolujem…' : 'Skontrolovať CI'}</button>
  </div>
  <div class="body" class:filled={error || result} aria-live="polite">
    {#if error || result}
      {#if error}
        <div class="err">Kontrola CI zlyhala: {error}</div>
      {:else if result && !result.available}
        <div class="muted">CI nie je dostupné: {result.reason}</div>
      {:else if result && !result.failure}
        <div class="muted">Žiadny neúspešný beh na tejto vetve.{#if result.note} {result.note}{/if}</div>
        {#each result.warnings as w}
          <div class="warn">{w}</div>
        {/each}
      {:else if result?.failure}
        <div class="run">
          {#if isGithubUrl(result.failure.url)}
            <a href={result.failure.url} target="_blank" rel="noreferrer noopener">{result.failure.title || result.failure.workflow}</a>
          {:else}
            <span>{result.failure.title || result.failure.workflow}</span>
          {/if}
          <span class="meta">{result.failure.workflow} · {result.failure.branch} · {when(result.failure.created_at)}</span>
        </div>
        {#each result.warnings as w}
          <div class="warn">{w}</div>
        {/each}
        {#if result.log_excerpt}
          <button class="link" aria-expanded={showLog} onclick={() => (showLog = !showLog)}>{showLog ? 'Skryť log' : 'Zobraziť log'} (upravený, bez tajomstiev)</button>
          {#if showLog}<pre class="log">{result.log_excerpt}</pre>{/if}
        {/if}
        {#if result.draft}
          <button class="act primary" onclick={prepareFix}>Pripraviť opravu v tíme</button>
          <span class="muted">Tím sa nespustí sám - zadanie si skontrolujete na stránke Team.</span>
        {/if}
      {/if}
    {/if}
  </div>
</div>

<style>
  .ci { background: #161b22; border: 1px solid #30363d; border-radius: 6px; font-size: 12px; }
  .head { display: flex; align-items: center; gap: 8px; padding: 6px 10px; }
  .label { font-weight: 600; }
  .hint, .muted { color: #8b949e; }
  .head .act { margin-left: auto; }
  .body { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; }
  .body.filled { padding: 0 10px 8px; }
  .act { background: #21262d; border: 1px solid #30363d; color: #c9d1d9; border-radius: 4px; padding: 3px 10px; font-size: 12px; cursor: pointer; }
  .act:hover:not(:disabled) { border-color: #58a6ff; }
  .act:disabled { opacity: 0.6; cursor: default; }
  .act.primary { background: #238636; border-color: #2ea043; color: #fff; }
  .run { display: flex; flex-direction: column; gap: 2px; }
  .run a { color: #58a6ff; text-decoration: none; }
  .meta { color: #8b949e; }
  .warn { background: #2d2200; border: 1px solid #5a4500; color: #d29922; border-radius: 4px; padding: 3px 8px; }
  .err { color: #f85149; }
  .link { background: none; border: none; color: #58a6ff; cursor: pointer; padding: 0; font-size: 12px; }
  .log { margin: 0; max-height: 240px; overflow: auto; width: 100%; box-sizing: border-box; background: #0d1117; border: 1px solid #30363d; border-radius: 4px; padding: 6px 8px; font-size: 11px; white-space: pre-wrap; word-break: break-word; }
</style>
