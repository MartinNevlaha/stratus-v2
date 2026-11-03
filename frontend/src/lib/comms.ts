import type { Peer, SharedTask } from './types'

export const RUNTIMES = ['claude-code', 'opencode', 'codex'] as const
export const MESSAGE_KINDS = ['text', 'done', 'question', 'task_assigned', 'task_update', 'task_done'] as const

/** Highlight classes: done, question and task_* kinds stand out from plain text. */
export function kindClass(kind: string): string {
  if (kind === 'done' || kind === 'task_done') return 'kind-done'
  if (kind === 'question') return 'kind-question'
  if (kind.startsWith('task_')) return 'kind-task'
  return 'kind-text'
}

export function toOptions(peers: Peer[]): string[] {
  return [...peers.filter((p) => p.active).map((p) => p.handle), ...RUNTIMES, '*']
}

export function groupTasks(tasks: SharedTask[]): { open: SharedTask[]; claimed: SharedTask[]; done: SharedTask[] } {
  return {
    open: tasks.filter((t) => t.status === 'open'),
    claimed: tasks.filter((t) => t.status === 'claimed'),
    done: tasks.filter((t) => t.status === 'done' || t.status === 'cancelled'),
  }
}

/** Human-readable text for an API failure (UNKNOWN_RECIPIENT errors already list the active peers). */
export function describeCommsError(e: unknown): string {
  return e instanceof Error ? e.message : 'Unexpected error'
}

export function debounce(fn: () => void, ms: number): { call: () => void; cancel: () => void } {
  let timer: ReturnType<typeof setTimeout> | null = null
  const cancel = () => { if (timer) clearTimeout(timer); timer = null }
  return { call: () => { cancel(); timer = setTimeout(() => { timer = null; fn() }, ms) }, cancel }
}
