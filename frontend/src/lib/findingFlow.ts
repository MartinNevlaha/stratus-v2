import type { CodeFinding } from './types'

export type FlowType = 'bug' | 'spec'

const BUG_CATEGORIES = new Set(['error_handling', 'dead_code', 'anti_pattern'])
const SPEC_CATEGORIES = new Set(['duplication', 'complexity', 'coverage_gap'])

export function flowForFinding(severity: string, category: string): FlowType {
  const sev = severity.toLowerCase()
  const cat = category.toLowerCase()
  if ((sev === 'warning' || sev === 'error') && BUG_CATEGORIES.has(cat)) return 'bug'
  if ((sev === 'info' || sev === 'warning') && SPEC_CATEGORIES.has(cat)) return 'spec'
  return 'bug'
}

export function sanitizeTerminalPayload(s: string): string {
  return s.replace(/[\r\n]+/g, ' ')
}

export function buildSlashCommand(flow: FlowType, f: CodeFinding): string {
  const lines = f.line_end && f.line_end !== f.line_start
    ? `${f.line_start}-${f.line_end}`
    : `${f.line_start}`
  const desc = (f.description ?? '').slice(0, 200)
  const instruction = `After the fix is complete and verified, call the MCP tool code_quality_finding_update with finding_id='${f.id}' and status='applied'.`
  const raw = `/${flow} finding_id=${f.id} ${f.file_path}:${lines} ${f.title} — ${desc} ${instruction}`
  return sanitizeTerminalPayload(raw)
}
