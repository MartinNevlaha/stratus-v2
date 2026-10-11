<script lang="ts">
  import type { Task } from '$lib/types'

  let { tasks }: { tasks: Task[] } = $props()

  const NODE_W = 170
  const NODE_H = 34
  const H_GAP = 44
  const V_GAP = 12
  const PAD = 12

  const stroke: Record<string, string> = { pending: '#484f58', in_progress: '#58a6ff', done: '#3fb950', skipped: '#6e7681' }
  const fill: Record<string, string> = { pending: '#161b22', in_progress: '#1f3056', done: '#1f2d1f', skipped: '#161b22' }

  // A task sits one column right of its deepest dependency.
  let layout = $derived.by(() => {
    const level = new Map<number, number>()
    for (const t of tasks) {
      const deps = (t.depends_on ?? []).filter(d => level.has(d))
      level.set(t.index, deps.length ? Math.max(...deps.map(d => level.get(d)!)) + 1 : 0)
    }
    const rows = new Map<number, number>()
    const pos = new Map<number, { x: number; y: number }>()
    for (const t of tasks) {
      const col = level.get(t.index) ?? 0
      const row = rows.get(col) ?? 0
      rows.set(col, row + 1)
      pos.set(t.index, { x: PAD + col * (NODE_W + H_GAP), y: PAD + row * (NODE_H + V_GAP) })
    }
    const cols = Math.max(0, ...level.values()) + 1
    const maxRows = Math.max(1, ...rows.values())
    return {
      pos,
      width: PAD * 2 + cols * NODE_W + (cols - 1) * H_GAP,
      height: PAD * 2 + maxRows * NODE_H + (maxRows - 1) * V_GAP,
    }
  })

  function truncate(s: string, n: number): string {
    return s.length > n ? s.slice(0, n - 1) + '…' : s
  }
</script>

<div class="graph">
  <svg width={layout.width} height={layout.height} viewBox="0 0 {layout.width} {layout.height}" role="img" aria-label="Task dependency graph">
    {#each tasks as t (t.index)}
      {#each t.depends_on ?? [] as d}
        {@const from = layout.pos.get(d)}
        {@const to = layout.pos.get(t.index)}
        {#if from && to}
          <path
            d="M {from.x + NODE_W} {from.y + NODE_H / 2} C {from.x + NODE_W + H_GAP / 2} {from.y + NODE_H / 2}, {to.x - H_GAP / 2} {to.y + NODE_H / 2}, {to.x} {to.y + NODE_H / 2}"
            fill="none" stroke="#30363d" stroke-width="1.5"
          />
        {/if}
      {/each}
    {/each}
    {#each tasks as t (t.index)}
      {@const p = layout.pos.get(t.index)}
      {#if p}
        <g transform="translate({p.x}, {p.y})">
          <title>{t.title}</title>
          <rect width={NODE_W} height={NODE_H} rx="6" fill={fill[t.status] ?? fill.pending} stroke={stroke[t.status] ?? stroke.pending} stroke-width="1.5" />
          <text x="10" y="21" font-size="12" fill="#c9d1d9" font-family="ui-monospace,monospace">{t.index + 1}. {truncate(t.title, 20)}</text>
        </g>
      {/if}
    {/each}
  </svg>
</div>

<style>
  .graph { overflow-x: auto; padding: 4px 0; }
</style>
