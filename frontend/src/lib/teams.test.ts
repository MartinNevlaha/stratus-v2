import { describe, expect, it } from 'vitest'
import { launchMembers, usageLabel } from './teams'

describe('team launch choices', () => {
 it('keeps explicit model and complex effort without silently falling back', () => {
  const rows = [{role:'design',runtime:'codex',model:'gpt-6-astra',effort:'high',complexEffort:'xhigh'},{role:'implementation',runtime:'codex',model:'gpt-6.1-sol',effort:'high',complexEffort:'high'}]
  const members = launchMembers(rows)
  expect(members[0].complex_profile?.effort).toBe('xhigh')
  expect(members[1].profile.model).toBe('gpt-6.1-sol')
  expect(()=>launchMembers([{...rows[0],model:''}])).toThrow()
 })
 it('distinguishes unknown consumption from measured zero', () => {
  expect(usageLabel(false,0,0)).toContain('neznáma')
  expect(usageLabel(true,0,0)).toContain('0')
 })
})
