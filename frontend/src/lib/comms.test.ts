import { describe, it, expect, vi } from 'vitest'
import { kindClass, toOptions, groupTasks, describeCommsError, debounce } from './comms'
import { CommsApiError } from './api'
import type { Peer, SharedTask } from './types'

const peer = (handle: string, active: boolean): Peer => ({
  session_key: handle, runtime: 'codex', handle, project: '', started_at: '', last_seen_at: '', active,
})
const task = (id: number, status: SharedTask['status']): SharedTask => ({
  id, title: 't', description: '', status, created_by: 'user', created_by_handle: 'user', result: '', created_at: '', updated_at: '',
})

describe('kindClass', () => {
  it('should highlight done, question and task kinds when kind is special', () => {
    expect(kindClass('done')).toBe('kind-done')
    expect(kindClass('task_done')).toBe('kind-done')
    expect(kindClass('question')).toBe('kind-question')
    expect(kindClass('task_assigned')).toBe('kind-task')
    expect(kindClass('text')).toBe('kind-text')
  })
})

describe('toOptions', () => {
  it('should list active handles, runtimes and wildcard when peers include ended ones', () => {
    expect(toOptions([peer('codex-aa', true), peer('old-one', false)])).toEqual(['codex-aa', 'claude-code', 'opencode', 'codex', '*'])
  })
})

describe('groupTasks', () => {
  it('should put cancelled tasks in the done column when grouping', () => {
    const g = groupTasks([task(1, 'open'), task(2, 'claimed'), task(3, 'done'), task(4, 'cancelled')])
    expect(g.open.map((t) => t.id)).toEqual([1])
    expect(g.claimed.map((t) => t.id)).toEqual([2])
    expect(g.done.map((t) => t.id)).toEqual([3, 4])
  })
})

describe('describeCommsError', () => {
  it('should show the server text once when recipient is unknown', () => {
    const e = new CommsApiError('unknown recipient "x"; active peers: a (codex)', 404, 'UNKNOWN_RECIPIENT')
    expect(describeCommsError(e)).toBe('unknown recipient "x"; active peers: a (codex)')
  })
  it('should return the message when error is a plain conflict', () => {
    expect(describeCommsError(new CommsApiError('handle taken', 409))).toBe('handle taken')
  })
  it('should fall back when value is not an Error', () => {
    expect(describeCommsError('x')).toBe('Unexpected error')
  })
})

describe('debounce', () => {
  it('should call once when invoked repeatedly within the window', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = debounce(fn, 100)
    d.call(); d.call(); d.call()
    vi.advanceTimersByTime(100)
    expect(fn).toHaveBeenCalledTimes(1)
    vi.useRealTimers()
  })
})
