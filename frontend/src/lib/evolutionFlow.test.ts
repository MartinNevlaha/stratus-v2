import { describe, it, expect } from 'vitest'
import { buildEvolutionCommand } from './evolutionFlow'
import type { EvolutionHypothesis } from './types'

const baseHypothesis: EvolutionHypothesis = {
  id: 'h1',
  run_id: 'r1',
  category: 'prompt_tuning',
  description: 'Improve system prompt clarity',
  baseline_value: 'v1-prompt',
  proposed_value: 'v2-prompt',
  metric: 'success_rate',
  baseline_metric: 0.72,
  experiment_metric: 0.81,
  confidence: 0.85,
  decision: 'proposal_created',
  decision_reason: 'High confidence improvement',
  wiki_page_id: 'wp-123',
  evidence: {},
  created_at: '2026-04-14T10:00:00Z',
}

describe('buildEvolutionCommand', () => {
  it('should start with /spec ', () => {
    const result = buildEvolutionCommand(baseHypothesis)
    expect(result).toMatch(/^\/spec /)
  })

  it('should contain the category', () => {
    const result = buildEvolutionCommand(baseHypothesis)
    expect(result).toContain('prompt_tuning')
  })

  it('should contain the description', () => {
    const result = buildEvolutionCommand(baseHypothesis)
    expect(result).toContain('Improve system prompt clarity')
  })

  it('should contain baseline_value and proposed_value', () => {
    const result = buildEvolutionCommand(baseHypothesis)
    expect(result).toContain('v1-prompt')
    expect(result).toContain('v2-prompt')
  })

  it('should contain the confidence percentage', () => {
    const result = buildEvolutionCommand(baseHypothesis)
    expect(result).toContain('85%')
  })

  it('should include wiki ref when wiki_page_id is non-null', () => {
    const result = buildEvolutionCommand(baseHypothesis)
    expect(result).toContain('[wiki:wp-123]')
  })

  it('should not include wiki ref when wiki_page_id is null', () => {
    const h = { ...baseHypothesis, wiki_page_id: null }
    const result = buildEvolutionCommand(h)
    expect(result).not.toContain('[wiki:')
  })

  it('should contain no CR or LF characters', () => {
    const h = { ...baseHypothesis, description: 'line one\nline two\r\nline three' }
    const result = buildEvolutionCommand(h)
    expect(result).not.toMatch(/[\r\n]/)
  })

  it('should round confidence correctly (0.156 → 16%)', () => {
    const h = { ...baseHypothesis, confidence: 0.156 }
    const result = buildEvolutionCommand(h)
    expect(result).toContain('16%')
  })
})
