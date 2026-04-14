import { describe, it, expect } from 'vitest'
import { flowForFinding, sanitizeTerminalPayload, buildSlashCommand } from './findingFlow'
import type { CodeFinding } from './types'

describe('flowForFinding', () => {
  it('should return bug for warning + error_handling', () => {
    expect(flowForFinding('warning', 'error_handling')).toBe('bug')
  })

  it('should return spec for info + duplication', () => {
    expect(flowForFinding('info', 'duplication')).toBe('spec')
  })

  it('should return spec for warning + duplication', () => {
    expect(flowForFinding('warning', 'duplication')).toBe('spec')
  })

  it('should return bug for error + dead_code', () => {
    expect(flowForFinding('error', 'dead_code')).toBe('bug')
  })

  it('should return bug (fallback) for info + unknown category', () => {
    expect(flowForFinding('info', 'unknown')).toBe('bug')
  })

  it('should be case-insensitive: WARNING + ERROR_HANDLING → bug', () => {
    expect(flowForFinding('WARNING', 'ERROR_HANDLING')).toBe('bug')
  })
})

describe('sanitizeTerminalPayload', () => {
  it('should remove CR and LF characters', () => {
    const result = sanitizeTerminalPayload('a\r\nb\nc\rd')
    expect(result).not.toMatch(/[\r\n]/)
  })
})

describe('buildSlashCommand', () => {
  const baseFinding: CodeFinding = {
    id: 'f1',
    run_id: 'r1',
    file_path: 'src/foo.ts',
    category: 'error_handling',
    severity: 'warning',
    title: 'Missing error check',
    description: 'The function does not check the returned error value.',
    line_start: 10,
    line_end: 10,
    confidence: 0.9,
    suggestion: 'Add error handling.',
    status: 'pending',
  }

  it('should start with /bug when flow is bug', () => {
    expect(buildSlashCommand('bug', baseFinding)).toMatch(/^\/bug /)
  })

  it('should start with /spec when flow is spec', () => {
    expect(buildSlashCommand('spec', baseFinding)).toMatch(/^\/spec /)
  })

  it('should contain no CR or LF characters', () => {
    const result = buildSlashCommand('bug', baseFinding)
    expect(result).not.toMatch(/[\r\n]/)
  })

  it('should include file_path', () => {
    expect(buildSlashCommand('bug', baseFinding)).toContain('src/foo.ts')
  })

  it('should include line number', () => {
    expect(buildSlashCommand('bug', baseFinding)).toContain(':10')
  })

  it('should include line range when start and end differ', () => {
    const f = { ...baseFinding, line_start: 5, line_end: 15 }
    expect(buildSlashCommand('bug', f)).toContain('5-15')
  })

  it('should truncate very long descriptions to 200 chars', () => {
    const longDesc = 'x'.repeat(300)
    const f = { ...baseFinding, description: longDesc }
    const result = buildSlashCommand('bug', f)
    // description is the part between ' — ' and ' After the fix'
    const afterDash = result.split(' — ')[1] ?? ''
    const descPart = afterDash.split(' After the fix')[0] ?? afterDash
    expect(descPart.length).toBeLessThanOrEqual(200)
  })

  it('should include finding_id in the command', () => {
    expect(buildSlashCommand('bug', baseFinding)).toContain('finding_id=f1')
  })

  it('should include MCP tool instruction with the finding id', () => {
    const result = buildSlashCommand('bug', baseFinding)
    expect(result).toContain("code_quality_finding_update")
    expect(result).toContain("finding_id='f1'")
    expect(result).toContain("status='applied'")
  })
})
