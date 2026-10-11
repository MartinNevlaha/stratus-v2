<script lang="ts">
  import { renderMarkdown } from '$lib/markdown'
  import PhaseTimeline from './PhaseTimeline.svelte'
  import SwarmDetail from './SwarmDetail.svelte'
  import WorkflowActivity from './WorkflowActivity.svelte'
  import AgentTimeline from './AgentTimeline.svelte'
  import { onMount } from 'svelte'
  import { appState } from '$lib/store'
  import { abortWorkflow, getWorkflowAgents, getRunSummary, getRunComparison, searchEvents } from '$lib/api'
  import { formatTokens, formatDuration } from '$lib/ui'
  import { plannedPaths, outOfScopeFiles } from '$lib/drift'
  import type { WorkflowAgent, RunComparison, Event as StratusEvent } from '$lib/types'
  import type { WorkflowState, SwarmMission } from '$lib/types'

  interface Props {
    wf: WorkflowState
    missions: SwarmMission[]
    isSwarm: boolean
  }

  let { wf, missions, isSwarm }: Props = $props()

  let planHtml = $derived(renderMarkdown(wf.plan_content))
  let designHtml = $derived(renderMarkdown(wf.design_content))
  let cs = $derived(wf.change_summary)

  let agents = $state<WorkflowAgent[]>([])
  let now = $state(Date.now())
  let confirmAbort = $state(false)
  let active = $derived(!wf.aborted && wf.phase !== 'complete')
  let tokensIn = $derived(agents.reduce((n, a) => n + a.tokens_in, 0))
  let tokensOut = $derived(agents.reduce((n, a) => n + a.tokens_out, 0))

  let comparison = $state<RunComparison | null>(null)
  let learned = $state<StratusEvent[]>([])
  let summaryState = $state<'idle' | 'copied' | 'error'>('idle')

  // Files agents touched although the plan never named them.
  let drift = $derived(
    outOfScopeFiles([...new Set(agents.flatMap(a => a.files))], plannedPaths(wf.plan_content)),
  )

  async function copySummary() {
    try {
      const { markdown } = await getRunSummary(wf.id)
      await navigator.clipboard.writeText(markdown)
      summaryState = 'copied'
    } catch (e) {
      console.error('[workflow] copy summary failed for', wf.id, e)
      summaryState = 'error'
    }
    setTimeout(() => { summaryState = 'idle' }, 2500)
  }

  async function loadExtras() {
    try {
      comparison = await getRunComparison(wf.id)
    } catch (e) {
      console.error('[workflow] comparison failed for', wf.id, e)
    }
    try {
      const res = await searchEvents('', { type: 'learn_pipeline', limit: '50' })
      learned = res.results.filter(e => (e.title ?? '').includes(wf.id) || (e.text ?? '').includes(wf.id))
    } catch (e) {
      console.error('[workflow] learned events failed for', wf.id, e)
    }
  }

  function delta(cur: number, prev: number, unit = ''): string {
    const d = cur - prev
    if (d === 0) return 'same'
    return `${d > 0 ? '+' : '−'}${Math.abs(d)}${unit}`
  }

  async function loadAgents() {
    try {
      agents = (await getWorkflowAgents(wf.id)).agents
    } catch (e) {
      console.error('[workflow] load agents failed for', wf.id, e)
    }
  }

  async function doAbort() {
    if (!confirmAbort) { confirmAbort = true; return }
    confirmAbort = false
    try {
      await abortWorkflow(wf.id)
    } catch (e) {
      console.error('[workflow] abort failed for', wf.id, e)
    }
  }

  onMount(() => {
    loadAgents()
    loadExtras()
    const tick = setInterval(() => { now = Date.now() }, 10_000)
    return () => clearInterval(tick)
  })

  $effect(() => {
    const _ = appState.workflowActivityCounter
    if (_ > 0) loadAgents()
  })
</script>

<div class="detail">
  <div class="head">
    <span class="type">{isSwarm ? 'swarm' : wf.type === 'spec' && wf.complexity === 'complex' ? 'spec-complex' : wf.type}</span>
    <span class="id">{wf.id}</span>
    <span class="phase">{wf.aborted ? 'aborted' : wf.phase}</span>
    {#if tokensIn + tokensOut > 0}
      <span class="tokens" title="Tokens used by subagents (input incl. cache / output)">{formatTokens(tokensIn)} in · {formatTokens(tokensOut)} out</span>
    {/if}
    <button class="copy" onclick={copySummary} title="Copy a markdown summary of this run">
      {summaryState === 'copied' ? 'Copied!' : summaryState === 'error' ? 'Failed' : 'Copy summary'}
    </button>
    {#if active}
      <button class="abort" class:armed={confirmAbort} onclick={doAbort} onblur={() => (confirmAbort = false)}>
        {confirmAbort ? 'Confirm abort' : 'Abort'}
      </button>
    {/if}
  </div>
  {#if wf.title}<h2 class="title">{wf.title}</h2>{/if}
  <PhaseTimeline type={wf.type} complexity={wf.complexity} currentPhase={wf.phase} />

  {#if isSwarm}
    <SwarmDetail {wf} {missions} />
  {:else}
    <WorkflowActivity {wf} />
  {/if}

  <AgentTimeline tasks={wf.tasks} {agents} {now} />

  {#if drift.length > 0}
    <div class="drift" title={drift.join('\n')}>
      ⚠ {drift.length} file{drift.length === 1 ? '' : 's'} changed outside the plan:
      {drift.slice(0, 3).map(f => f.split('/').slice(-2).join('/')).join(', ')}{drift.length > 3 ? '…' : ''}
    </div>
  {/if}

  {#if comparison?.previous}
    {@const p = comparison.previous}
    {@const c = comparison.current}
    <div class="compare" title="Compared with the previous finished run {p.id}">
      vs previous run <code>{p.id}</code>:
      time {delta(c.duration_sec, p.duration_sec, 's')} ·
      review loops {delta(c.review_loops, p.review_loops)} ·
      tasks {delta(c.tasks, p.tasks)}
      {#if c.tokens + p.tokens > 0}· tokens {delta(c.tokens, p.tokens)}{/if}
    </div>
  {/if}

  {#if learned.length > 0}
    <details class="learned">
      <summary>Learned ({learned.length})</summary>
      {#each learned as e (e.id)}
        <div class="learn-item"><span class="learn-title">{e.title}</span><span class="learn-text">{e.text}</span></div>
      {/each}
    </details>
  {/if}

  {#if cs}
    <div class="stats">
      <span>{cs.files_changed} files</span>
      <span class="add">+{cs.lines_added}</span>
      <span class="del">−{cs.lines_removed}</span>
    </div>
  {/if}

  {#if planHtml}
    <details class="doc" open>
      <summary>Plan</summary>
      <div class="md">{@html planHtml}</div>
    </details>
  {/if}
  {#if designHtml}
    <details class="doc" open={wf.complexity === 'complex'}>
      <summary>Design</summary>
      <div class="md">{@html designHtml}</div>
    </details>
  {/if}
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 12px; background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 16px; }
  .head { display: flex; align-items: center; gap: 8px; }
  .type { font-size: 11px; background: #21262d; color: #8b949e; padding: 2px 8px; border-radius: 4px; }
  .id { font-size: 13px; font-weight: 600; color: #c9d1d9; flex: 1; }
  .phase { font-size: 12px; color: #58a6ff; }
  .copy { background: none; border: 1px solid #30363d; color: #8b949e; border-radius: 4px; font-size: 11px; padding: 1px 8px; cursor: pointer; }
  .copy:hover { color: #c9d1d9; border-color: #484f58; }
  .drift { font-size: 12px; color: #d29922; background: #2d2200; border-radius: 4px; padding: 4px 8px; }
  .compare { font-size: 12px; color: #8b949e; }
  .compare code { color: #c9d1d9; }
  .learned summary { cursor: pointer; font-size: 12px; font-weight: 600; color: #8b949e; }
  .learn-item { display: flex; flex-direction: column; gap: 2px; font-size: 12px; padding: 4px 0; }
  .learn-title { color: #c9d1d9; }
  .learn-text { color: #8b949e; }
  .tokens { font-size: 11px; color: #8b949e; }
  .abort { background: none; border: 1px solid #30363d; color: #8b949e; border-radius: 4px; font-size: 11px; padding: 1px 8px; cursor: pointer; }
  .abort:hover { color: #f85149; border-color: #f85149; }
  .abort.armed { color: #f85149; border-color: #f85149; background: #3d1f1f; }
  .title { margin: 0; font-size: 16px; font-weight: 600; color: #c9d1d9; }
  .stats { display: flex; gap: 10px; font-size: 12px; color: #8b949e; }
  .add { color: #3fb950; }
  .del { color: #f85149; }
  .doc summary { cursor: pointer; font-size: 12px; font-weight: 600; color: #8b949e; }
  .md { font-size: 13px; color: #c9d1d9; padding-top: 8px; }
</style>
