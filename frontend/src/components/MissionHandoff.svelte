<script lang="ts">
  import { appState } from '$lib/store'
  import { getMissionHandoff } from '$lib/api'
  import type { MissionHandoff, MissionHandoffGate } from '$lib/types'

  let { missionId }: { missionId: string } = $props()

  let data = $state<MissionHandoff | null>(null)
  let requestSeq = 0

  async function load(id: string) {
    const seq = ++requestSeq
    try {
      const result = await getMissionHandoff(id)
      if (seq === requestSeq) data = result
    } catch (e) {
      console.error('[swarm] getMissionHandoff failed for', id, e)
      if (seq === requestSeq) data = null
    }
  }

  // Runs on mount, when the mission changes and on every swarm update (gate results included).
  $effect(() => {
    const _ = appState.swarmUpdateCounter
    load(missionId)
  })

  const gateLabels: Record<string, string> = { build: 'build', vet: 'vet', placeholder_scan: 'placeholder' }

  // Always the three expected gates; a gate with no row is shown as missing.
  let gates = $derived(
    Object.keys(gateLabels).map(name => ({ name, gate: data?.gates.find(g => g.name === name) })),
  )

  function reviewState(r: NonNullable<MissionHandoff['review']>): { label: string; cls: string } {
    if (!r.current) return { label: 'Zastaralé', cls: 'stale' }
    if (r.valid) return { label: 'PASS - platné', cls: 'valid' }
    if (r.verdict === 'fail') return { label: 'FAIL', cls: 'failed' }
    return { label: 'Bez verdiktu', cls: 'none' }
  }

  function gateState(g: MissionHandoffGate): 'pass' | 'fail' | 'info' {
    if (g.verdict === 'pass') return 'pass'
    if (g.verdict === 'fail') return 'fail'
    return 'info'
  }
</script>

{#if data}
  <div class="handoff detail-section">
    <div class="detail-label">Odovzdanie</div>
    <div class="row">
      <code class="rev">{data.revision ? data.revision.slice(0, 7) : '?'}</code>
      <span class="tree" class:dirty={data.dirty}>{data.dirty ? 'neuložené zmeny' : 'čistý'}</span>
      <span class="kind">{data.dir_kind === 'integration' ? 'integračný worktree' : 'koreň projektu'}</span>
      {#if data.diff_stat}
        <span class="diff">{data.diff_stat.files_changed} súborov
          <span class="add">+{data.diff_stat.lines_added}</span>
          <span class="del">−{data.diff_stat.lines_removed}</span></span>
      {/if}
    </div>
    <div class="row">
      {#each gates as { name, gate: g }}
        {#if g}
          {@const state = gateState(g)}
          <span class="gate-chip {state}" class:outdated={!g.current} title={g.content}>
            {gateLabels[name]}
            {#if !g.current}zastaralé{:else if state === 'pass'}✓{:else if state === 'fail'}✕{:else}·{/if}
          </span>
        {:else}
          <span class="gate-chip missing">{gateLabels[name]} chýba</span>
        {/if}
      {/each}
      <span class="checks" class:ok={data.checks_ok}>{data.checks_ok ? 'Kontroly OK' : 'Kontroly neúplné'}</span>
    </div>
    {#if data.review}
      {@const rs = reviewState(data.review)}
      <div class="row">
        <span class="review-state {rs.cls}" title={data.review.reason ?? ''}>
          Review: {rs.label}
          {#if data.review.reason}<span class="reason">({data.review.reason})</span>{/if}
        </span>
      </div>
    {/if}
    {#each data.warnings as w}
      <div class="warning">⚠ {w}</div>
    {/each}
  </div>
{/if}

<style>
  .detail-section { display: flex; flex-direction: column; gap: 6px; margin-top: 8px; }
  .detail-label { font-size: 12px; font-weight: 600; color: #8b949e; }
  .row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; font-size: 12px; }
  .rev { color: #c9d1d9; background: #21262d; border-radius: 4px; padding: 1px 6px; }
  .tree { color: #3fb950; }
  .tree.dirty { color: #e3b341; }
  .kind { color: #8b949e; }
  .diff { color: #8b949e; }
  .add { color: #3fb950; }
  .del { color: #f85149; }
  .gate-chip {
    border: 1px solid #30363d; border-radius: 10px; padding: 1px 8px; font-size: 11px; color: #8b949e;
  }
  .gate-chip.pass { color: #3fb950; border-color: #3fb95055; }
  .gate-chip.fail { color: #f85149; border-color: #f8514955; }
  .gate-chip.outdated { color: #e3b341; border-color: #e3b34155; }
  .gate-chip.missing { color: #484f58; border-style: dashed; }
  .checks { font-size: 11px; color: #e3b341; }
  .checks.ok { color: #3fb950; }
  .review-state { font-size: 11px; font-weight: 600; }
  .review-state.valid { color: #3fb950; }
  .review-state.stale { color: #e3b341; }
  .review-state.failed { color: #f85149; }
  .review-state.none { color: #8b949e; }
  .reason { font-weight: 400; color: #8b949e; }
  .warning { font-size: 11px; color: #e3b341; }
</style>
