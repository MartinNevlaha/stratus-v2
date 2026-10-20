import type { WorkflowState } from './types'
import { getWorkflow } from './api'
import { formatTokens } from './ui'
export const isDashboardOnly = (search: string): boolean => new URLSearchParams(search).get('view') === 'dashboard'
export const continuationText = (wf: WorkflowState): string => wf.runtime === 'codex' ? `Continue Stratus workflow ${wf.id}` : `/resume ${wf.id}`
export const usageLabel = (available: boolean | undefined, input: number, output: number): string => available === false ? 'Usage unavailable' : `${formatTokens(input)} in · ${formatTokens(output)} out`
export async function continuationForID(id: string, active: WorkflowState[], load: (id: string) => Promise<WorkflowState> = getWorkflow): Promise<string> {
  try { return continuationText(active.find(wf => wf.id === id) ?? await load(id)) }
  catch { return `#/workflow/${encodeURIComponent(id)}` }
}
