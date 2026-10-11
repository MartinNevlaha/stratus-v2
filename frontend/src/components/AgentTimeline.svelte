<script lang="ts">
  import { formatDuration } from '$lib/ui'
  import type { Task, WorkflowAgent } from '$lib/types'

  interface Props {
    tasks: Task[]
    agents: WorkflowAgent[]
    now: number
  }

  let { tasks, agents, now }: Props = $props()

  interface Bar { key: string; label: string; start: number; end: number; kind: string; title: string }

  let bars = $derived.by<Bar[]>(() => {
    const out: Bar[] = []
    for (const t of tasks) {
      if (!t.started_at) continue
      const start = Date.parse(t.started_at)
      const end = t.completed_at ? Date.parse(t.completed_at) : now
      out.push({ key: `t${t.index}`, label: t.title, start, end, kind: t.status === 'done' || t.status === 'skipped' ? 'task-done' : 'task-active', title: `${t.title} · ${formatDuration(t.started_at, t.completed_at)}` })
    }
    for (const a of agents) {
      const start = Date.parse(a.started_at)
      const end = a.ended_at ? Date.parse(a.ended_at) : now
      const kind = a.status === 'failed' || a.verdict === 'FAIL' ? 'failed' : a.status === 'running' ? 'running' : 'done'
      out.push({ key: `a${a.id}`, label: a.agent_type.replace('delivery-', ''), start, end, kind, title: `${a.agent_type} · ${formatDuration(a.started_at, a.ended_at)}${a.verdict ? ' · ' + a.verdict : ''}` })
    }
    return out.filter(b => Number.isFinite(b.start))
  })

  let min = $derived(bars.length ? Math.min(...bars.map(b => b.start)) : 0)
  let max = $derived(bars.length ? Math.max(...bars.map(b => b.end), min + 1000) : 1)
  let span = $derived(Math.max(max - min, 1000))

  function left(b: Bar): number { return ((b.start - min) / span) * 100 }
  function width(b: Bar): number { return Math.max(((b.end - b.start) / span) * 100, 0.8) }
</script>

{#if bars.length > 0}
  <div class="timeline">
    <div class="head">Timeline · {formatDuration(new Date(min).toISOString(), new Date(max).toISOString())}</div>
    {#each bars as b (b.key)}
      <div class="row">
        <span class="label" title={b.label}>{b.label}</span>
        <div class="track">
          <div class="bar {b.kind}" style="left: {left(b)}%; width: {width(b)}%" title={b.title}></div>
        </div>
      </div>
    {/each}
  </div>
{/if}

<style>
  .timeline { display: flex; flex-direction: column; gap: 4px; }
  .head { font-size: 12px; font-weight: 600; color: #8b949e; }
  .row { display: flex; align-items: center; gap: 8px; }
  .label { width: 150px; flex-shrink: 0; font-size: 11px; color: #8b949e; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .track { position: relative; flex: 1; height: 10px; background: #21262d; border-radius: 3px; }
  .bar { position: absolute; top: 0; bottom: 0; border-radius: 3px; }
  .bar.task-done { background: #3fb950; }
  .bar.task-active { background: #58a6ff; }
  .bar.done { background: #6e7681; }
  .bar.running { background: #a371f7; }
  .bar.failed { background: #f85149; }
</style>
