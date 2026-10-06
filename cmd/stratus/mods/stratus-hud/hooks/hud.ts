// What the HUD shows, worked out from Stratus API payloads: which workflow and swarm mission
// are this session's, a workflow's phases in order, and the denials and alerts worth a toast.

import type { Alert, Denial, Mission, Workflow } from '../types'

// Phase order per workflow, mirroring validTransitions in orchestration/state.go.
// A complex spec plans twice: once before discovery, once after governance.
const PHASES = {
  spec: ['plan', 'implement', 'verify', 'learn'],
  specComplex: ['plan', 'discovery', 'design', 'governance', 'plan', 'implement', 'verify', 'learn'],
  bug: ['analyze', 'fix', 'review'],
  e2e: ['setup', 'plan', 'generate', 'heal'],
}

export type Step = { name: string; isCurrent: boolean }

const isActive = (wf: Workflow) => !wf.aborted && wf.phase !== 'complete'

// A workflow is this session's when it names this session or none: one started in another
// session is shown, but its toasts and actions stay there.
export const isOwn = (wf: Workflow, sessionId: string) => !wf.session_id || wf.session_id === sessionId

// Whether a complex spec went through design, so its plan is the second one. Not every
// coordinator stores the design document, so an agent delegated in those phases counts too.
const isPastDesign = (wf: Workflow) =>
  !!wf.design_content || ['discovery', 'design', 'governance'].some(p => (wf.delegated_agents?.[p]?.length ?? 0) > 0)

// This session's active workflow, else the first active one (as the statusline does).
export function pickWorkflow(list: readonly Workflow[], sessionId: string): Workflow | null {
  const active = list.filter(isActive)
  return active.find(wf => wf.session_id === sessionId) ?? active[0] ?? null
}

export function steps(wf: Workflow): Step[] {
  const phases =
    wf.type === 'bug' ? PHASES.bug
    : wf.type === 'e2e' ? PHASES.e2e
    : wf.complexity === 'complex' ? PHASES.specComplex
    : PHASES.spec
  // accept is the legacy name of the second plan
  const phase = wf.phase === 'accept' ? 'plan' : wf.phase
  const at = isPastDesign(wf) ? phases.lastIndexOf(phase) : phases.indexOf(phase)
  return phases.map((name, i) => ({ name, isCurrent: i === at }))
}

export function taskCount(wf: Workflow): { done: number; total: number } {
  const tasks = wf.tasks ?? []
  return { done: tasks.filter(t => t.status === 'done').length, total: wf.total_tasks || tasks.length }
}

// Phases a workflow may move to, mirroring validTransitions in orchestration/state.go,
// with accept (the legacy name of plan) left out.
const NEXT: Record<string, Record<string, string[]>> = {
  spec: { design: ['governance', 'plan'], discovery: ['design'], governance: ['plan'], accept: ['implement'], implement: ['verify'], verify: ['implement', 'learn'], learn: ['complete'] },
  bug: { analyze: ['fix'], fix: ['review'], review: ['fix', 'complete'] },
  e2e: { setup: ['plan'], plan: ['generate'], generate: ['heal'], heal: ['generate', 'complete'] },
}

export function nextPhases(wf: Workflow): string[] {
  // a spec's plan leads to discovery only before a complex one's design, else to implement
  if (wf.type === 'spec' && wf.phase === 'plan') return wf.complexity === 'complex' && !isPastDesign(wf) ? ['discovery'] : ['implement']
  return NEXT[wf.type]?.[wf.phase] ?? []
}

export function agentsNow(wf: Workflow): string[] {
  return wf.delegated_agents?.[wf.phase] ?? []
}

type DenialEvent = { id: number; ts: string; refs?: Record<string, unknown> | null }

// hook_denial events as posted by hooks/audit.go, newest first.
export function denialsOf(events: readonly DenialEvent[]): Denial[] {
  return events
    .map(ev => ({
      id: ev.id,
      ts: ev.ts,
      hook: String(ev.refs?.hook ?? ''),
      tool: String(ev.refs?.tool_name ?? ''),
      reason: String(ev.refs?.reason ?? ''),
      sessionId: String(ev.refs?.session_id ?? ''),
    }))
    .sort((a, b) => b.id - a.id)
}

// Denials newer than lastSeen that belong to this session, oldest first.
export function freshDenials(denials: readonly Denial[], lastSeen: number, sessionId: string): Denial[] {
  return denials.filter(d => d.id > lastSeen && d.sessionId === sessionId).reverse()
}

const isMissionLive = (m: Mission) => ['planning', 'active', 'merging', 'verifying'].includes(m.status)

// The live swarm mission of this workflow, else the first live one.
export function pickMission(list: readonly Mission[], workflowId: string): Mission | null {
  const live = list.filter(isMissionLive)
  return live.find(m => m.workflow_id === workflowId) ?? live[0] ?? null
}

// Guardian warnings newer than lastSeen, oldest first; info alerts stay in the pane.
export function freshAlerts(alerts: readonly Alert[], lastSeen: number): Alert[] {
  return alerts.filter(a => a.id > lastSeen && a.severity === 'warning').sort((a, b) => a.id - b.id)
}

// Control characters in API strings (an escape sequence in a title, say) would reach the
// terminal through the band and toasts; newlines and tabs stay for the plan's Markdown.
const CONTROL = /[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/g

export function parseClean<T>(text: string): T {
  return JSON.parse(text, (_key, value: unknown) => (typeof value === 'string' ? value.replace(CONTROL, '') : value)) as T
}

// The /goal that keeps a workflow running to completion: the same condition the coordinator
// skills offer once their approval step is done. The evaluator only reads the conversation,
// so the condition names evidence Claude prints (get_workflow, the review verdict or test run)
// and the coordinator instructions to continue with when they are not loaded yet.
export function goalCondition(wf: Workflow, isSwarm: boolean): string {
  const evidence = wf.type === 'e2e'
    ? ', and the last Playwright test run shown passes'
    : ', and the last code review shown has verdict PASS'
  const skill = wf.type === 'e2e' ? 'e2e' : isSwarm ? 'swarm' : wf.complexity === 'complex' ? 'spec-complex' : 'resume'
  return `/goal Stratus workflow ${wf.id} is complete: the latest mcp__stratus__get_workflow output for ${wf.id} ` +
    `in this conversation shows phase "complete" and every task done${evidence}. Work through the remaining ` +
    `phases in order; if the coordinator instructions for this workflow are not in this conversation, read ` +
    `.claude/skills/${skill}/SKILL.md and continue ${wf.id} from its current phase as it describes. ` +
    `Do not skip phases or weaken tests to make them pass. When a step needs my decision, ask me with ` +
    `AskUserQuestion. Stop after 40 turns.`
}
