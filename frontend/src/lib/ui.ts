export function relativeTime(ts: string): string {
  const diff = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
  if (diff < 5) return 'just now'
  if (diff < 60) return `${diff}s ago`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  return `${Math.floor(diff / 3600)}h ago`
}

export function workerStatusColor(status: string): string {
  switch (status) {
    case 'active': return '#3fb950'
    case 'pending': return '#8b949e'
    case 'stale': return '#d29922'
    case 'done': return '#58a6ff'
    case 'failed': case 'killed': return '#f85149'
    default: return '#8b949e'
  }
}

export function ticketIcon(status: string): string {
  switch (status) {
    case 'done': return '✓'
    case 'in_progress': return '▶'
    case 'assigned': return '○'
    case 'failed': return '✗'
    case 'blocked': return '❘'
    default: return '○'
  }
}

export function toggleSet(set: Set<string>, id: string): Set<string> {
  const next = new Set(set)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  return next
}

/** Human-readable span between two ISO timestamps (end defaults to now). */
export function formatDuration(start: string, end?: string): string {
  const ms = (end ? new Date(end).getTime() : Date.now()) - new Date(start).getTime()
  if (!Number.isFinite(ms) || ms < 0) return ''
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
}

/** Compact token count: 950, 12.3k, 1.4M. */
export function formatTokens(n: number): string {
  if (n < 1000) return String(n)
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`
  return `${(n / 1_000_000).toFixed(1)}M`
}

/** A running agent that made no tool call for this long is shown as stuck. */
export const STALE_AGENT_MS = 10 * 60 * 1000

export function isAgentStale(status: string, lastActivity: string): boolean {
  return status === 'running' && Date.now() - new Date(lastActivity).getTime() > STALE_AGENT_MS
}
