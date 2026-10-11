import { describe, it, expect } from 'vitest'
import { formatDuration, formatTokens, isAgentStale, toggleSet, STALE_AGENT_MS } from './ui'

describe('formatDuration', () => {
  it('should format seconds, minutes and hours between two timestamps', () => {
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T00:00:42Z')).toBe('42s')
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T00:03:05Z')).toBe('3m 5s')
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T02:10:00Z')).toBe('2h 10m')
  })

  it('should return empty string when the end precedes the start', () => {
    expect(formatDuration('2026-01-01T01:00:00Z', '2026-01-01T00:00:00Z')).toBe('')
  })
})

describe('isAgentStale', () => {
  const ago = (ms: number) => new Date(Date.now() - ms).toISOString()

  it('should flag a running agent idle longer than the threshold', () => {
    expect(isAgentStale('running', ago(STALE_AGENT_MS + 1000))).toBe(true)
  })

  it('should not flag a recently active or a finished agent', () => {
    expect(isAgentStale('running', ago(1000))).toBe(false)
    expect(isAgentStale('done', ago(STALE_AGENT_MS * 2))).toBe(false)
  })
})

describe('toggleSet', () => {
  it('should add a missing id and remove a present one without mutating the input', () => {
    const a = new Set(['x'])
    expect([...toggleSet(a, 'y')].sort()).toEqual(['x', 'y'])
    expect([...toggleSet(a, 'x')]).toEqual([])
    expect([...a]).toEqual(['x'])
  })
})

describe('formatTokens', () => {
  it('should abbreviate thousands and millions', () => {
    expect(formatTokens(950)).toBe('950')
    expect(formatTokens(1234)).toBe('1.2k')
    expect(formatTokens(48_000)).toBe('48k')
    expect(formatTokens(2_500_000)).toBe('2.5M')
  })
})
