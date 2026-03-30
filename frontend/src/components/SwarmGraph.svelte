<script lang="ts">
  import type { SwarmMissionDetail, SwarmTicket, SwarmFileReservation } from '$lib/types'
  import { getMissionFiles } from '$lib/api'

  let { detail }: { detail: SwarmMissionDetail } = $props()

  let fileReservations = $state<SwarmFileReservation[]>([])
  let hoveredTicket = $state<SwarmTicket | null>(null)
  let tooltipX = $state(0)
  let tooltipY = $state(0)

  const NODE_W = 168
  const NODE_H = 50
  const H_GAP = 56
  const V_GAP = 14
  const PAD = 20

  const domainFill: Record<string, string> = {
    backend:      '#1c2d4f',
    frontend:     '#173322',
    database:     '#3a200f',
    tests:        '#1e350d',
    infra:        '#2a1a3f',
    architecture: '#361828',
    general:      '#1c2128',
  }

  const statusStroke: Record<string, string> = {
    done:        '#3fb950',
    in_progress: '#a371f7',
    assigned:    '#58a6ff',
    pending:     '#484f58',
    failed:      '#f85149',
    blocked:     '#f0883e',
  }

  function safeArr(s: string): string[] {
    if (!s || s === '[]') return []
    try { return JSON.parse(s) } catch { return [] }
  }

  interface Edge {
    fromId: string
    toId: string
    type: 'dependency' | 'backbone'
  }

  function computeLayout(tickets: SwarmTicket[]) {
    if (tickets.length === 0) {
      return { positions: new Map<string, { x: number; y: number }>(), depMap: new Map<string, string[]>(), edges: [] as Edge[], svgW: 200, svgH: 80 }
    }

    const depMap = new Map<string, string[]>()
    for (const t of tickets) depMap.set(t.id, safeArr(t.depends_on))

    const hasDeps = tickets.some(t => safeArr(t.depends_on).length > 0)

    const positions = new Map<string, { x: number; y: number }>()
    const edges: Edge[] = []

    if (hasDeps) {
      // Original topological layout
      const levelMap = new Map<string, number>()
      function getLevel(id: string, stack: Set<string>): number {
        if (levelMap.has(id)) return levelMap.get(id)!
        if (stack.has(id)) return 0
        const s2 = new Set(stack); s2.add(id)
        const deps = depMap.get(id) || []
        const lvl = deps.length === 0 ? 0 : Math.max(...deps.map(d => getLevel(d, s2))) + 1
        levelMap.set(id, lvl)
        return lvl
      }
      for (const t of tickets) getLevel(t.id, new Set())

      const byLevel = new Map<number, SwarmTicket[]>()
      for (const t of tickets) {
        const lvl = levelMap.get(t.id) ?? 0
        if (!byLevel.has(lvl)) byLevel.set(lvl, [])
        byLevel.get(lvl)!.push(t)
      }
      for (const group of byLevel.values()) {
        group.sort((a, b) => a.domain.localeCompare(b.domain) || a.priority - b.priority)
      }

      for (const [lvl, group] of byLevel) {
        const x = PAD + lvl * (NODE_W + H_GAP)
        group.forEach((t, i) => positions.set(t.id, { x, y: PAD + i * (NODE_H + V_GAP) }))
      }

      // Dependency edges
      for (const t of tickets) {
        for (const depId of safeArr(t.depends_on)) {
          if (positions.has(depId)) edges.push({ fromId: depId, toId: t.id, type: 'dependency' })
        }
      }
    } else {
      // No dependencies — group by domain into columns
      const byDomain = new Map<string, SwarmTicket[]>()
      for (const t of tickets) {
        if (!byDomain.has(t.domain)) byDomain.set(t.domain, [])
        byDomain.get(t.domain)!.push(t)
      }
      const domainKeys = Array.from(byDomain.keys()).sort()

      for (let col = 0; col < domainKeys.length; col++) {
        const group = byDomain.get(domainKeys[col])!
        group.sort((a, b) => a.priority - b.priority)
        const x = PAD + col * (NODE_W + H_GAP)
        group.forEach((t, i) => {
          positions.set(t.id, { x, y: PAD + i * (NODE_H + V_GAP) })
        })
        // Backbone edges within domain column
        for (let i = 1; i < group.length; i++) {
          edges.push({ fromId: group[i - 1].id, toId: group[i].id, type: 'backbone' })
        }
      }
    }

    // Compute SVG dimensions
    let maxX = 0, maxY = 0
    for (const p of positions.values()) {
      if (p.x + NODE_W > maxX) maxX = p.x + NODE_W
      if (p.y + NODE_H > maxY) maxY = p.y + NODE_H
    }
    const svgW = maxX + PAD
    const svgH = maxY + PAD

    return { positions, depMap, edges, svgW, svgH }
  }

  function domainProgress(tickets: SwarmTicket[]) {
    const map = new Map<string, { done: number; total: number }>()
    for (const t of tickets) {
      if (!map.has(t.domain)) map.set(t.domain, { done: 0, total: 0 })
      const e = map.get(t.domain)!
      e.total++
      if (t.status === 'done') e.done++
    }
    return Array.from(map.entries()).sort((a, b) => a[0].localeCompare(b[0])).map(([domain, v]) => ({ domain, ...v }))
  }

  let layout = $derived(computeLayout(detail.tickets))
  let domains = $derived(domainProgress(detail.tickets))
  let workerMap = $derived(new Map(detail.workers.map(w => [w.id, w])))

  $effect(() => {
    const id = detail.mission.id
    getMissionFiles(id).then(r => { fileReservations = r }).catch(() => { fileReservations = [] })
  })

  function edgePath(x1: number, y1: number, x2: number, y2: number): string {
    const cx = (x1 + x2) / 2
    return `M ${x1} ${y1} C ${cx} ${y1} ${cx} ${y2} ${x2} ${y2}`
  }

  function verticalEdgePath(x1: number, y1: number, x2: number, y2: number): string {
    const cy = (y1 + y2) / 2
    return `M ${x1} ${y1} C ${x1} ${cy} ${x2} ${cy} ${x2} ${y2}`
  }

  function truncate(s: string, n: number): string {
    return s.length > n ? s.slice(0, n - 1) + '…' : s
  }

  function onNodeEnter(e: MouseEvent, ticket: SwarmTicket) {
    hoveredTicket = ticket
    tooltipX = e.clientX
    tooltipY = e.clientY
  }

  function onNodeMove(e: MouseEvent) {
    if (hoveredTicket) { tooltipX = e.clientX; tooltipY = e.clientY }
  }

  function onNodeLeave() {
    hoveredTicket = null
  }
</script>

<div class="graph-root">
  <!-- Domain progress summary -->
  {#if domains.length > 0}
    <div class="domain-bars">
      {#each domains as d}
        <div class="domain-row">
          <span class="domain-chip" style="background:{domainFill[d.domain] || domainFill.general}; border-color:{statusStroke.done}">{d.domain}</span>
          <div class="bar-track">
            <div class="bar-fill" style="width:{d.total > 0 ? (d.done / d.total) * 100 : 0}%; background:{d.done === d.total && d.total > 0 ? statusStroke.done : statusStroke.in_progress}"></div>
          </div>
          <span class="domain-count">{d.done}/{d.total}</span>
        </div>
      {/each}
    </div>
  {/if}

  <!-- Dependency graph -->
  <div class="svg-scroll">
    <svg
      width={layout.svgW}
      height={layout.svgH}
      viewBox="0 0 {layout.svgW} {layout.svgH}"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-label="Ticket dependency graph"
    >
      <defs>
        <marker id="arr-green" markerWidth="7" markerHeight="7" refX="6" refY="3.5" orient="auto">
          <path d="M0,0 L0,7 L7,3.5 z" fill="#3fb950" />
        </marker>
        <marker id="arr-orange" markerWidth="7" markerHeight="7" refX="6" refY="3.5" orient="auto">
          <path d="M0,0 L0,7 L7,3.5 z" fill="#d29922" />
        </marker>
        <marker id="arr-purple" markerWidth="7" markerHeight="7" refX="6" refY="3.5" orient="auto">
          <path d="M0,0 L0,7 L7,3.5 z" fill="#a371f7" />
        </marker>
        <filter id="edge-glow" x="-20%" y="-20%" width="140%" height="140%">
          <feGaussianBlur in="SourceGraphic" stdDeviation="3" result="blur" />
          <feMerge>
            <feMergeNode in="blur" />
            <feMergeNode in="SourceGraphic" />
          </feMerge>
        </filter>
      </defs>

      <!-- Edges -->
      {#each layout.edges as edge, eIdx}
        {@const src = layout.positions.get(edge.fromId)}
        {@const dst = layout.positions.get(edge.toId)}
        {#if src && dst}
          {@const fromTicket = detail.tickets.find(t => t.id === edge.fromId)}
          {@const toTicket = detail.tickets.find(t => t.id === edge.toId)}
          {@const isVertical = Math.abs(src.x - dst.x) < NODE_W}
          {@const pathD = isVertical
            ? verticalEdgePath(src.x + NODE_W / 2, src.y + NODE_H, dst.x + NODE_W / 2, dst.y)
            : edgePath(src.x + NODE_W, src.y + NODE_H / 2, dst.x - 2, dst.y + NODE_H / 2)}
          {@const hasActive = fromTicket?.status === 'in_progress' || toTicket?.status === 'in_progress'}
          {@const bothDone = fromTicket?.status === 'done' && toTicket?.status === 'done'}
          {@const depSatisfied = edge.type === 'dependency' && fromTicket?.status === 'done'}
          {#if hasActive}
            <!-- Bloom glow layer -->
            <path
              d={pathD}
              stroke="#a371f7"
              stroke-width="6"
              fill="none"
              opacity="0.15"
              filter="url(#edge-glow)"
            />
            <!-- Main active edge -->
            <path
              d={pathD}
              stroke="#a371f7"
              stroke-width="1.5"
              fill="none"
              opacity="0.7"
            />
            <!-- Traveling pulse (glow) -->
            <circle r="6" fill="#a371f7" opacity="0.2">
              <animateMotion dur="2.5s" repeatCount="indefinite" begin="{(eIdx * 0.5) % 2.5}s" path={pathD} />
            </circle>
            <!-- Traveling pulse (core) -->
            <circle r="3" fill="#a371f7" opacity="0.85">
              <animateMotion dur="2.5s" repeatCount="indefinite" begin="{(eIdx * 0.5) % 2.5}s" path={pathD} />
            </circle>
          {:else if bothDone}
            <path
              d={pathD}
              stroke="#3fb950"
              stroke-width="1.5"
              fill="none"
              opacity="0.5"
            />
          {:else if depSatisfied}
            <path
              d={pathD}
              stroke="#3fb950"
              stroke-width="1.5"
              fill="none"
              opacity="0.5"
              marker-end="url(#arr-green)"
            />
          {:else}
            <path
              d={pathD}
              stroke="#484f58"
              stroke-width="1"
              fill="none"
              opacity="0.35"
            />
          {/if}
        {/if}
      {/each}

      <!-- Nodes -->
      {#each detail.tickets as ticket}
        {@const pos = layout.positions.get(ticket.id)}
        {#if pos}
          {@const stroke = statusStroke[ticket.status] || statusStroke.pending}
          {@const fill = domainFill[ticket.domain] || domainFill.general}
          {@const worker = ticket.worker_id ? workerMap.get(ticket.worker_id) : undefined}
          {@const isActive = ticket.status === 'in_progress'}
          <g
            transform="translate({pos.x},{pos.y})"
            role="img"
            aria-label={ticket.title}
            onmouseenter={(e) => onNodeEnter(e, ticket)}
            onmousemove={onNodeMove}
            onmouseleave={onNodeLeave}
          >
            {#if isActive}
              <rect class="node-heartbeat" width={NODE_W} height={NODE_H} rx="7" fill="none" stroke={stroke} stroke-width="4" />
            {/if}
            <rect width={NODE_W} height={NODE_H} rx="7" fill={fill} stroke={stroke} stroke-width={isActive ? 2 : 1} />
            <text x="10" y="19" font-size="12" fill="#c9d1d9" font-family="ui-monospace,monospace">{truncate(ticket.title, 19)}</text>
            <text x="10" y="36" font-size="10" fill="#6e7681" font-family="ui-monospace,monospace">
              {ticket.domain}{worker ? ' · ' + worker.agent_type.replace('delivery-', '') : ''}
            </text>
            <circle cx={NODE_W - 12} cy={NODE_H / 2} r="5" fill={stroke} opacity="0.9" />
          </g>
        {/if}
      {/each}
    </svg>
  </div>

  <!-- File reservations -->
  {#if fileReservations.length > 0}
    <details class="reservations">
      <summary class="res-summary">File Reservations ({fileReservations.length})</summary>
      <div class="res-list">
        {#each fileReservations as res}
          {@const w = workerMap.get(res.worker_id)}
          <div class="res-row">
            <span class="res-worker">{w ? w.agent_type.replace('delivery-', '') : res.worker_id.slice(0, 8)}</span>
            <div class="res-patterns">
              {#each safeArr(res.patterns) as p}
                <code class="res-path">{p}</code>
              {/each}
            </div>
            {#if res.reason}
              <span class="res-reason">{res.reason}</span>
            {/if}
          </div>
        {/each}
      </div>
    </details>
  {/if}
</div>

<!-- Tooltip (fixed-positioned, outside scroll) -->
{#if hoveredTicket}
  <div class="tooltip" style="left:{tooltipX + 14}px; top:{tooltipY - 8}px">
    <div class="tt-title">{hoveredTicket.title}</div>
    {#if hoveredTicket.description}
      <div class="tt-desc">{hoveredTicket.description}</div>
    {/if}
    <div class="tt-meta">
      <span class="tt-badge" style="color:{statusStroke[hoveredTicket.status] || statusStroke.pending}">{hoveredTicket.status}</span>
      <span class="tt-badge">{hoveredTicket.domain}</span>
      {#if hoveredTicket.priority > 0}
        <span class="tt-badge">p{hoveredTicket.priority}</span>
      {/if}
    </div>
  </div>
{/if}

<style>
  .graph-root {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  /* Domain progress bars */
  .domain-bars {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 16px;
    padding: 8px 0 4px;
  }
  .domain-row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 200px;
  }
  .domain-chip {
    font-size: 10px;
    font-family: ui-monospace, monospace;
    padding: 2px 7px;
    border-radius: 4px;
    border: 1px solid transparent;
    color: #8b949e;
    min-width: 80px;
    text-align: right;
  }
  .bar-track {
    flex: 1;
    height: 5px;
    background: #21262d;
    border-radius: 3px;
    min-width: 80px;
  }
  .bar-fill {
    height: 100%;
    border-radius: 3px;
    transition: width 0.4s ease;
  }
  .domain-count {
    font-size: 10px;
    color: #6e7681;
    min-width: 28px;
    text-align: right;
  }

  /* SVG scroll container */
  .svg-scroll {
    overflow-x: auto;
    overflow-y: hidden;
    border: 1px solid #21262d;
    border-radius: 8px;
    background: #0d1117;
    padding: 4px;
  }
  .svg-scroll svg {
    display: block;
  }

  /* Node heartbeat breathing glow */
  .node-heartbeat {
    opacity: 0.15;
    animation: heartbeat 2s ease-in-out infinite;
    will-change: opacity;
  }
  @keyframes heartbeat {
    0%, 100% { opacity: 0.1; stroke-width: 3; }
    50%      { opacity: 0.35; stroke-width: 5; }
  }
  @media (prefers-reduced-motion: reduce) {
    .node-heartbeat { animation: none; opacity: 0.2; }
  }

  /* File reservations */
  .reservations {
    border: 1px solid #21262d;
    border-radius: 6px;
    padding: 0;
    overflow: hidden;
  }
  .res-summary {
    padding: 6px 12px;
    font-size: 11px;
    color: #8b949e;
    cursor: pointer;
    user-select: none;
    background: #161b22;
  }
  .res-summary:hover { color: #c9d1d9; }
  .res-list {
    padding: 8px 12px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .res-row {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    flex-wrap: wrap;
  }
  .res-worker {
    font-size: 10px;
    background: #2d1f56;
    color: #a371f7;
    padding: 1px 7px;
    border-radius: 4px;
    flex-shrink: 0;
  }
  .res-patterns {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    flex: 1;
  }
  .res-path {
    font-size: 10px;
    background: #161b22;
    color: #58a6ff;
    padding: 1px 6px;
    border-radius: 3px;
    border: 1px solid #21262d;
  }
  .res-reason {
    font-size: 10px;
    color: #6e7681;
    font-style: italic;
  }

  /* Tooltip */
  .tooltip {
    position: fixed;
    z-index: 1000;
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 8px;
    padding: 10px 14px;
    pointer-events: none;
    max-width: 300px;
    box-shadow: 0 8px 24px rgba(0,0,0,0.4);
  }
  .tt-title {
    font-size: 12px;
    color: #c9d1d9;
    font-weight: 600;
    margin-bottom: 4px;
    font-family: ui-monospace, monospace;
  }
  .tt-desc {
    font-size: 11px;
    color: #8b949e;
    margin-bottom: 6px;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .tt-meta {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .tt-badge {
    font-size: 10px;
    background: #21262d;
    padding: 1px 6px;
    border-radius: 4px;
    color: #8b949e;
  }
</style>
