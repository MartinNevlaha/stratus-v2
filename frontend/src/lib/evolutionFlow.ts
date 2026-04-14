import type { EvolutionHypothesis } from './types'
import { sanitizeTerminalPayload } from './findingFlow'

export function buildEvolutionCommand(h: EvolutionHypothesis): string {
  const wikiRef = h.wiki_page_id ? ` [wiki:${h.wiki_page_id}]` : ''
  const pct = (h.confidence * 100).toFixed(0)
  const raw = `/spec Review evolution proposal — ${h.category}: ${h.description} (baseline ${h.baseline_value} → proposed ${h.proposed_value}, confidence ${pct}%)${wikiRef}`
  return sanitizeTerminalPayload(raw)
}
