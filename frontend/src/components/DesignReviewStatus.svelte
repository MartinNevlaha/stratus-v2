<script lang="ts">
  import { onMount } from 'svelte'
  import { wsClient } from '$lib/ws'
  import { debounce } from '$lib/comms'
  import { teamRequest, teamStateLabel } from '$lib/teams'
  import DesignReviewOverride from './DesignReviewOverride.svelte'
  import type { DesignReviewState, DesignReviewStatus } from '$lib/types'

  let { workflowId, teamId }: { workflowId: string; teamId: string } = $props()

  let data = $state<DesignReviewStatus | null>(null)
  let requestSeq = 0

  async function load() {
    const seq = ++requestSeq
    try {
      const result = await teamRequest<DesignReviewStatus>(`workflows/${encodeURIComponent(workflowId)}/design-review`)
      if (seq === requestSeq) data = result
    } catch {
      // 404 (unknown workflow) or an unreachable API: the status is advisory, show nothing.
      if (seq === requestSeq) data = null
    }
  }

  onMount(() => {
    load()
    const d = debounce(load, 150)
    const off = wsClient.on('team_updated', d.call)
    return () => { off(); d.cancel() }
  })

  const labels = (s: DesignReviewStatus): Partial<Record<DesignReviewState, string>> => ({
    none: 'čaká na spustenie',
    reviewing: `prebieha (kolo ${s.round}/${s.max_rounds})`,
    reworking: `prepracovanie (kolo ${s.round}/${s.max_rounds})`,
    pass: 'PASS',
    fail: 'FAIL – na prepracovanie',
    waived: 'schválené používateľom',
    exhausted: 'vyčerpané kolá – rozhodni',
    review_failed: 'review zlyhalo',
    documents_missing: 'chýbajú dokumenty',
    documents_invalid: 'neplatné dokumenty',
  })
  let label = $derived(data?.enabled && data.state ? (data.stuck ? 'zaseknuté' : labels(data)[data.state]) : undefined)
  const tone = (s: DesignReviewStatus) => s.stuck ? 'warn' : s.state === 'pass' || s.state === 'waived' ? 'ok' : s.state === 'none' || s.state === 'reviewing' || s.state === 'reworking' ? 'wait' : 'bad'
  // Mirrors the server: failed, exhausted and failed-to-run reviews can be waived, and so can a running one that is stuck.
  let canOverride = $derived(
    !!data && (['fail', 'exhausted', 'review_failed'].includes(data.state ?? '') || (!!data.stuck && ['reviewing', 'reworking'].includes(data.state ?? ''))),
  )
</script>

{#if data && label}
  <details class="design-review">
    <summary class="chip {tone(data)}" title={data.reason || undefined} aria-label={`Review návrhu workflow ${workflowId}: ${label}`}>Review návrhu: {label}</summary>
    <div class="body">
      {#if data.stuck && data.reason}<div class="stuck">{data.reason}</div>{/if}
      {#if data.team_state}<div>Stav tímu: {teamStateLabel(data.team_state)}</div>{/if}
      {#if data.reviewer_state}<div>Stav reviewera: {teamStateLabel(data.reviewer_state)}</div>{/if}
      {#if data.reviewer}<div>Review robí: {data.reviewer.id} ({data.reviewer.role})</div>{/if}
      {#if data.author}<div>Autor: {data.author.id} ({data.author.role})</div>{/if}
      {#if data.docs?.length}
        <ul>{#each data.docs as d}<li><code>{d.path}</code> ({d.bytes} B)</li>{/each}</ul>
      {/if}
      {#if data.missing?.length}
        <div>Chýba:</div><ul>{#each data.missing as m}<li><code>{m}</code></li>{/each}</ul>
      {/if}
      {#if data.message}<div>{data.message}</div>{/if}
      {#if data.issues_excerpt}<pre>{data.issues_excerpt}</pre>{/if}
      {#if canOverride}
        <DesignReviewOverride
          path={`/teams/${teamId}/design-review/override`}
          body={{ workflow_id: workflowId, reason: 'Schválené používateľom bez úspešného review' }}
          ariaLabel={`Schváliť návrh workflow ${workflowId} bez review`}
          ondone={load}
        />
      {/if}
    </div>
  </details>
{/if}

<style>
  .design-review { display: inline-block; font-size: 11px; }
  .chip { cursor: pointer; border: 1px solid #30363d; border-radius: 10px; padding: 1px 8px; color: #8b949e; list-style: none; }
  .chip.ok { color: #3fb950; border-color: #3fb95055; }
  .chip.bad { color: #f85149; border-color: #f8514955; }
  .chip.warn { color: #d29922; border-color: #d2992255; }
  .stuck { color: #d29922; }
  .chip.wait { color: #e3b341; border-color: #e3b34155; }
  .body { margin-top: 4px; padding: 6px; background: #0d1117; border: 1px solid #30363d; border-radius: 4px; color: #8b949e; max-width: 520px; }
  ul { margin: 2px 0; padding-left: 16px; }
  pre { white-space: pre-wrap; overflow-wrap: anywhere; max-height: 12em; overflow: auto; margin: 4px 0 0; font: inherit; font-family: monospace; }
</style>
