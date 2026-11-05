<script lang="ts">
  import { onMount } from 'svelte'
  import { wsClient } from '$lib/ws'
  import { debounce } from '$lib/comms'
  import { teamRequest, TeamRequestError, TeamTimeoutError } from '$lib/teams'
  import { expectedGateNames, gateChip, gateLabel, noChecksConfigured, noChecksHint } from '$lib/gates'
  import type { TeamChecks, TeamHandoff, TeamIntegrationResult } from '$lib/types'

  let { teamId, onremoved }: { teamId: string; onremoved?: () => void } = $props()

  let data = $state<TeamHandoff | null>(null)
  let busy = $state(false)
  let error = $state('')
  let notice = $state<string[]>([])
  let forceOffer = $state(false)
  let blockers = $state<string[]>([])
  let confirmForce = $state(false)
  let requestSeq = 0

  const path = (suffix: string) => `teams/${teamId}/${suffix}`
  // The server gives integration and checks up to 5 minutes; go vet on a cold cache is slow.
  const LONG_ACTION_MS = 6 * 60 * 1000

  async function load() {
    const seq = ++requestSeq
    try {
      const result = await teamRequest<TeamHandoff>(path('handoff'))
      if (seq === requestSeq) data = result
    } catch (e) {
      if (seq === requestSeq) error = e instanceof Error ? e.message : String(e)
    }
  }

  async function act(fn: () => Promise<void>) {
    if (busy) return
    busy = true
    error = ''
    notice = []
    forceOffer = confirmForce = false
    blockers = []
    try {
      await fn()
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
      // The server may have finished anyway: look once, never re-send the action.
      if (e instanceof TeamTimeoutError) await load()
      // Only the server's workspaces_blocked refusal can be forced; other conflicts would fail again.
      const blocked = e instanceof TeamRequestError && e.status === 409 && e.body.code === 'workspaces_blocked'
      forceOffer = blocked
      blockers = blocked ? (e as TeamRequestError).body.blockers ?? [] : []
    } finally {
      busy = false
    }
  }

  const integrate = () => act(async () => {
    const res = await teamRequest<TeamIntegrationResult>(path('integrate'), {}, 'POST', LONG_ACTION_MS)
    notice = [`Zlúčené: ${res.merged.length}, konflikty: ${res.conflicts.length}, zlyhania: ${(res.failed ?? []).length}, bez zmien: ${res.nothing_to_merge.length}`, ...res.warnings]
    await load()
  })

  const verify = () => act(async () => {
    const res = await teamRequest<{ checks: TeamChecks; cached: boolean }>(path('verify'), {}, 'POST', LONG_ACTION_MS)
    notice = [res.cached ? 'Kontroly sú z pamäte, revízia sa nezmenila.' : 'Kontroly sú hotové.']
    await load()
    // A dirty tree is never remembered by the server; keep what this run just reported.
    if (data && !(data.checks?.gates.length)) data = { ...data, checks: res.checks }
  })

  const remove = (force: boolean) => act(async () => {
    await teamRequest(path(force ? 'workspaces?force=true' : 'workspaces'), undefined, 'DELETE')
    onremoved?.()
  })

  const reload = () => { error = ''; notice = []; forceOffer = confirmForce = false; blockers = []; load() }

  onMount(() => {
    load()
    const d = debounce(load, 150)
    const off = wsClient.on('team_updated', d.call)
    return () => { off(); d.cancel() }
  })

  // The first line carries the absolute path and revision, the rest is the tool output; keep it whole but bounded.
  const clip = (text: string) => (text.length > 4000 ? text.slice(0, 4000) + '…' : text)
  let failedGates = $derived((data?.checks?.gates ?? []).filter(g => g.verdict === 'fail'))
  let gates = $derived(expectedGateNames(data?.checks?.expected_gates).map(name => ({ name, gate: data?.checks?.gates.find(g => g.name === name) })))
  let hasNoChecksRow = $derived((data?.checks?.gates ?? []).some(noChecksConfigured))



  function reviewState(r: NonNullable<TeamHandoff['review']>): { label: string; cls: string } {
    if (!r.current) return { label: 'Zastaralé', cls: 'stale' }
    if (r.valid) return { label: 'PASS - platné', cls: 'valid' }
    if (r.verdict === 'fail') return { label: 'FAIL', cls: 'failed' }
    return { label: 'Bez verdiktu', cls: 'none' }
  }

  const short = $derived(teamId.slice(0, 8))
</script>

<section class="team-handoff" aria-label={`Odovzdanie práce tímu ${short}`}>
  <div class="head"><h2>Odovzdanie práce</h2>
    <div class="actions">
      <button onclick={integrate} disabled={busy} aria-label={`Zlúčiť výsledky tímu ${short}`}>Zlúčiť výsledky</button>
      <button onclick={verify} disabled={busy || !data?.integration} aria-label={`Spustiť kontroly tímu ${short}`}>Spustiť kontroly</button>
      <button onclick={reload} disabled={busy} aria-label={`Obnoviť odovzdanie tímu ${short}`}>Obnoviť</button>
      <button class="danger" onclick={() => remove(false)} disabled={busy} aria-label={`Upratať pracovné priestory tímu ${short}`}>Upratať pracovné priestory</button>
    </div>
  </div>
  {#if error}<div class="error" role="alert">{#if blockers.length}<div>Upratanie je blokované (neuložená alebo nezlúčená práca):</div><ul>{#each blockers as b}<li>{b}</li>{/each}</ul>{:else}{error}{/if}</div>{/if}
  {#if forceOffer && !confirmForce}
    <button class="danger" onclick={() => (confirmForce = true)} disabled={busy}>Vynútiť upratanie</button>
  {/if}
  {#if confirmForce}
    <div class="confirm" role="alertdialog" aria-label="Potvrdenie vynúteného upratania">
      <span>Nezlúčená a neuložená práca členov sa stratí. Naozaj vynútiť upratanie?</span>
      <button class="danger" onclick={() => remove(true)} disabled={busy}>Áno, zahodiť prácu</button>
      <button onclick={() => (confirmForce = false)} disabled={busy}>Zrušiť</button>
    </div>
  {/if}
  <div class="result" aria-live="polite">
    {#if notice.length}<div class="notice" role="status">{#each notice as line}<div>{line}</div>{/each}</div>{/if}
    {#if data}
      <div class="row">
        {#if data.integration}
          <code class="rev">{data.integration.revision ? data.integration.revision.slice(0, 7) : '?'}</code>
          <span class="tree" class:dirty={data.integration.dirty}>{data.integration.dirty ? 'neuložené zmeny' : 'čistý'}</span>
          <span class="kind">{data.integration.branch}</span>
        {:else}
          <span class="kind">Zatiaľ nezlúčené</span>
        {/if}
        {#if data.diff_stat}
          <span class="diff">{data.diff_stat.files_changed} súborov
            <span class="add">+{data.diff_stat.lines_added}</span>
            <span class="del">−{data.diff_stat.lines_removed}</span></span>
        {/if}
      </div>
      {#if data.members.length}
        <table><tbody>
          {#each data.members as m (m.id)}
            <tr><td>{m.id}</td><td>{m.role}</td><td><code>{m.branch}</code></td><td>{m.ahead} commitov pred</td><td class:dirty={m.uncommitted > 0}>{m.uncommitted} neuložených súborov</td></tr>
          {/each}
        </tbody></table>
      {/if}
      {#if data.merged.length}<div class="row">Zlúčené: {data.merged.join(', ')}</div>{/if}
      {#each data.conflicts as c (c.member)}
        <details class="conflict"><summary class="del">Konflikt: {c.member} ({c.files.length} súborov)</summary>
          {#if c.reason}<div class="reason">{c.reason}</div>{/if}
          <ul>{#each c.files as f}<li><code>{f}</code></li>{/each}</ul>
        </details>
      {/each}
      {#each data.failed ?? [] as c (c.member)}
        <details class="failed"><summary class="del">Zlúčenie zlyhalo (nie konflikt): {c.member}</summary>
          {#if c.reason}<div class="reason">{c.reason}</div>{/if}
          <ul>{#each c.files as f}<li><code>{f}</code></li>{/each}</ul>
        </details>
      {/each}
      <div class="row">
        {#each gates as { name, gate: g }}
          {#if g}
            {@const chip = gateChip(g)}
            <span class="gate-chip {chip.cls}" class:outdated={chip.outdated} title={g.content}>{gateLabel(name)} {chip.text}</span>
          {:else}
            <span class="gate-chip missing">{gateLabel(name)} chýba</span>
          {/if}
        {/each}
        <span class="checks" class:ok={data.checks?.checks_ok}>{data.checks?.checks_ok ? 'Kontroly OK' : 'Kontroly neúplné'}</span>
      </div>
      {#if hasNoChecksRow}<div class="hint">{noChecksHint}</div>{/if}
      {#if failedGates.length}
        <details class="gate-detail" open={failedGates.length === 1}>
          <summary>Detail zlyhaných kontrol</summary>
          {#each failedGates as g (g.name)}
            <div class="gate-name">{gateLabel(g.name)}</div>
            <pre>{clip(g.content)}</pre>
          {/each}
        </details>
      {/if}
      {#if data.review}
        {@const rs = reviewState(data.review)}
        <div class="row">
          <span class="review-state {rs.cls}" title={data.review.reason ?? ''}>
            Review: {rs.label}
            {#if data.review.reason}<span class="reason">({data.review.reason})</span>{/if}
          </span>
        </div>
      {/if}
      {#each data.warnings as w}<div class="warning">⚠ {w}</div>{/each}
    {/if}
  </div>
</section>

<style>
  .team-handoff { background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 12px 16px; display: flex; flex-direction: column; gap: 8px; }
  .head { display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap; }
  h2 { font-size: 15px; margin: 0; }
  .actions, .confirm { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  button { background: #0d1117; color: #c9d1d9; border: 1px solid #30363d; border-radius: 6px; padding: 4px 10px; font: inherit; font-size: 12px; cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled) { border-color: #58a6ff; }
  button:disabled { opacity: .45; cursor: default; }
  .danger { color: #f85149; }
  .result { display: flex; flex-direction: column; gap: 6px; }
  .error { color: #ff7b72; background: #f8514910; padding: 8px; border-radius: 6px; font-size: 12px; white-space: pre-wrap; }
  .notice { color: #3fb950; background: #3fb95010; padding: 8px; border-radius: 6px; font-size: 12px; }
  .confirm { border: 1px solid #f85149; border-radius: 6px; padding: 8px; font-size: 12px; }
  .row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; font-size: 12px; }
  table { border-collapse: collapse; font-size: 12px; }
  td { padding: 2px 10px 2px 0; color: #8b949e; }
  td.dirty { color: #e3b341; }
  .rev { color: #c9d1d9; background: #21262d; border-radius: 4px; padding: 1px 6px; }
  .tree { color: #3fb950; }
  .tree.dirty { color: #e3b341; }
  .kind, .diff { color: #8b949e; }
  .add { color: #3fb950; }
  .del { color: #f85149; }
  details { margin: 0; font-size: 12px; }
  ul { margin: 4px 0 0; padding-left: 18px; }
  .gate-chip { border: 1px solid #30363d; border-radius: 10px; padding: 1px 8px; font-size: 11px; color: #8b949e; }
  .gate-chip.pass { color: #3fb950; border-color: #3fb95055; }
  .gate-chip.fail { color: #f85149; border-color: #f8514955; }
  .gate-chip.outdated { color: #e3b341; border-color: #e3b34155; }
  .gate-chip.missing { color: #484f58; border-style: dashed; }
  .hint { font-size: 11px; color: #8b949e; }
  .checks { font-size: 11px; color: #e3b341; }
  .checks.ok { color: #3fb950; }
  .review-state { font-size: 11px; font-weight: 600; }
  .review-state.valid { color: #3fb950; }
  .review-state.stale { color: #e3b341; }
  .review-state.failed { color: #f85149; }
  .review-state.none { color: #8b949e; }
  .gate-detail { margin: 0; font-size: 12px; }
  .gate-detail summary { cursor: pointer; color: #f85149; }
  .gate-name { font-weight: 600; color: #8b949e; margin-top: 4px; }
  .gate-detail pre { white-space: pre-wrap; overflow-wrap: anywhere; max-height: 14em; overflow: auto; margin: 2px 0 0; font: inherit; font-family: monospace; font-size: 11px; background: #0d1117; border-radius: 4px; padding: 6px; }
  .reason { font-weight: 400; color: #8b949e; }
  .warning { font-size: 11px; color: #e3b341; }
</style>
