import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { appState, pushToast, dismissToast } from './store.svelte'

describe('toasts', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    appState.toasts = []
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('should add a toast and drop it after the timeout', () => {
    pushToast('warn', 'Review FAILED', '#/workflow/x')
    expect(appState.toasts).toHaveLength(1)
    expect(appState.toasts[0]).toMatchObject({ kind: 'warn', text: 'Review FAILED', href: '#/workflow/x' })
    vi.advanceTimersByTime(8100)
    expect(appState.toasts).toHaveLength(0)
  })

  it('should keep at most five toasts, newest last', () => {
    for (let i = 0; i < 7; i++) pushToast('info', `t${i}`)
    expect(appState.toasts).toHaveLength(5)
    expect(appState.toasts[4].text).toBe('t6')
  })

  it('should dismiss a toast by id', () => {
    pushToast('info', 'a')
    pushToast('info', 'b')
    dismissToast(appState.toasts[0].id)
    expect(appState.toasts.map(t => t.text)).toEqual(['b'])
  })
})
