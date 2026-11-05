<script lang="ts">
  import { onMount } from 'svelte'
  import { appState, setActiveTab } from '$lib/store'
  import { wsClient } from '$lib/ws'
  import { getDecisions } from '$lib/api'
  import { teamRequest } from '$lib/teams'
  import { debounce } from '$lib/comms'
  import { relativeTime } from '$lib/ui'
  import DesignReviewOverride from './DesignReviewOverride.svelte'
  import type { DecisionAction, DecisionItem, DecisionSummary } from '$lib/types'

  const LAST_SEEN_KEY = 'stratus-last-seen'
  const kinds: Record<DecisionItem['kind'], string> = {
    team_approval: 'Povolenie', team_recover: 'Obnova tímu', workflow: 'Workflow', guardian: 'Guardian', design_review: 'Review návrhu',
  }
  const labels: Record<DecisionAction['name'], string> = {
    approve: 'Povoliť raz', deny: 'Zamietnuť', recover: 'Obnoviť po kontrole', dismiss: 'Zavrieť', override: 'Schváliť bez review',
  }

  let items = $state<DecisionItem[]>([])
  let summary = $state<DecisionSummary | null>(null)
  let busy = $state(false)
  let error = $state('')
  let since = readLastSeen()

  function readLastSeen(): string | undefined {
    try { return localStorage.getItem(LAST_SEEN_KEY) ?? undefined } catch { return undefined }
  }
  function storeLastSeen(): string {
    const now = new Date().toISOString()
    try { localStorage.setItem(LAST_SEEN_KEY, now) } catch { /* not persisted; the line just reappears */ }
    return now
  }

  let loadSeq = 0
  async function load() {
    const seq = ++loadSeq
    try {
      const res = await getDecisions(since)
      if (seq !== loadSeq) return // a newer request is in flight or done
      items = res.items
      summary = res.summary
      error = ''
    } catch {
      /* the queue is advisory; stay quiet when the API is unreachable */
    }
  }

  async function run(action: DecisionAction) {
    busy = true
    error = ''
    try {
      await teamRequest(action.path.replace(/^\//, ''), action.body ?? {}, action.method)
      await load()
    } catch (e) {
      error = e instanceof Error ? e.message : 'Akcia zlyhala'
    } finally {
      busy = false
    }
  }

  function markSeen() {
    since = storeLastSeen()
    summary = null
  }

  let reload: ReturnType<typeof debounce> | undefined

  onMount(() => {
    load()
    const d = debounce(load, 150)
    reload = d
    const poll = setInterval(load, 30_000)
    const offs = [wsClient.on('team_updated', d.call), wsClient.on('guardian_alert', d.call), wsClient.on('connected', d.call)]
    // Leaving the page counts as having seen the queue; coming back reports what changed since.
    const onVisibility = () => {
      if (document.hidden) { storeLastSeen(); return }
      since = readLastSeen()
      load()
    }
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      storeLastSeen()
      clearInterval(poll)
      offs.forEach(off => off())
      document.removeEventListener('visibilitychange', onVisibility)
      d.cancel()
    }
  })

  // Only changes after mount trigger a reload; onMount already does the initial load.
  let seenActivity = appState.workflowActivityCounter
  $effect(() => {
    const counter = appState.workflowActivityCounter
    if (counter === seenActivity) return
    seenActivity = counter
    reload?.call()
  })

  let sinceLine = $derived.by(() => {
    const s = summary
    if (!s || (s.new_items_since === 0 && s.guardian_new === 0 && !s.teams_updated)) return ''
    const parts = [`${s.new_items_since} nových`]
    if (s.guardian_new > 0) parts.push(`${s.guardian_new} upozornení Guardianu`)
    if (s.teams_updated) parts.push('tím sa zmenil')
    return `Od posledného návratu: ${parts.join(', ')}`
  })
</script>

{#if items.length > 0 || sinceLine}
  <div class="queue" aria-live="polite">
    <div class="head">
      <span class="label">Čaká na tvoje rozhodnutie{items.length > 0 ? ` (${items.length})` : ''}</span>
      {#if sinceLine}
        <span class="since">{sinceLine}</span>
        <button class="ghost" onclick={markSeen}>Označiť ako videné</button>
      {/if}
    </div>
    {#if error}<div class="err">{error}</div>{/if}
    {#each items as it (it.id)}
      <div class="row" class:critical={it.severity === 'critical'}>
        <span class="chip {it.kind}">{kinds[it.kind]}</span>
        <div class="text">
          <span class="title">{it.title}</span>
          {#if it.detail}
            {#if it.kind === 'team_approval'}
              <pre class="approval-text">{it.detail}</pre>
            {:else}
              <span class="detail">{it.detail}</span>
            {/if}
          {/if}
        </div>
        {#if it.created_at}<span class="when">{relativeTime(it.created_at)}</span>{/if}
        <div class="actions">
          {#each it.actions as a (a.name)}
            {#if a.name === 'override'}
              <DesignReviewOverride path={a.path} body={a.body ?? {}} ariaLabel="{labels[a.name]}: {it.title}" ondone={load} />
            {:else}
              <button class:primary={a.name === 'approve'} disabled={busy} aria-label="{labels[a.name]}: {it.title}" onclick={() => run(a)}>{labels[a.name]}</button>
            {/if}
          {/each}
          {#if it.link}
            <a class="open" href={it.link} aria-label="Otvoriť: {it.title}">Otvoriť</a>
          {:else if it.kind === 'team_approval' || it.kind === 'team_recover'}
            <button aria-label="Otvoriť tím: {it.title}" onclick={() => setActiveTab('team')}>Otvoriť tím</button>
          {/if}
        </div>
      </div>
    {/each}
  </div>
{/if}

<style>
  .queue { display: flex; flex-direction: column; gap: 4px; background: #2d2200; border: 1px solid #5a4500; border-radius: 6px; padding: 6px 10px; font-size: 12px; }
  .head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .label { color: #d29922; font-weight: 600; }
  .since { color: #8b949e; }
  .row { display: flex; align-items: center; gap: 8px; background: #161b22; border-radius: 4px; padding: 4px 8px; color: #c9d1d9; }
  .row.critical { outline: 1px solid #f8514966; }
  .chip { font-size: 11px; padding: 0 6px; border-radius: 8px; background: #21262d; color: #8b949e; white-space: nowrap; }
  .chip.team_approval, .chip.team_recover { color: #d29922; }
  .chip.guardian { color: #f85149; }
  .text { display: flex; flex-direction: column; min-width: 0; flex: 1; }
  .title { overflow-wrap: anywhere; }
  .detail { color: #8b949e; white-space: pre-wrap; overflow-wrap: anywhere; max-height: 4.5em; overflow: hidden; }
  .approval-text { margin: 2px 0 0; font: inherit; color: #8b949e; white-space: pre-wrap; overflow-wrap: anywhere; max-height: 14em; overflow: auto; }
  .when { color: #8b949e; white-space: nowrap; }
  .actions { display: flex; gap: 6px; align-items: center; }
  button, .open { background: #0d1117; color: #c9d1d9; border: 1px solid #30363d; border-radius: 4px; padding: 2px 8px; font: inherit; cursor: pointer; white-space: nowrap; text-decoration: none; }
  button:hover:not(:disabled), .open:hover { border-color: #58a6ff; }
  button:disabled { opacity: 0.45; cursor: default; }
  button.primary { background: #238636; border-color: #2ea043; color: white; }
  button.ghost { color: #8b949e; }
  .err { color: #ff7b72; }
</style>
