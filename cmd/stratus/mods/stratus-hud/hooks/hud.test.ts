import { describe, expect, mock, test } from 'claude-code/testing'
import type { On } from 'claude-code'
import type { MockClock } from 'claude-code/testing'

import type { Alert, Mission, Swarm, Workflow } from '../types'
import { denialsOf, freshAlerts, freshDenials, goalCondition, isOwn, nextPhases, parseClean, pickMission, pickWorkflow, steps } from './hud'

const wf = (over: Partial<Workflow> = {}): Workflow => ({
  id: 'spec-a', type: 'spec', phase: 'implement', complexity: 'simple', title: 'Add mods',
  session_id: 'sess-1', aborted: false, tasks: [], total_tasks: 0, delegated_agents: {}, ...over,
})
const current = (w: Workflow) => steps(w).findIndex(s => s.isCurrent)

describe('hud', () => {
  test('pickWorkflow prefers this session, skips finished ones, falls back to the first active', () => {
    const mine = wf({ id: 'mine' })
    const other = wf({ id: 'other', session_id: 'sess-2' })
    const done = wf({ id: 'done', phase: 'complete' })
    const aborted = wf({ id: 'aborted', aborted: true })
    expect(pickWorkflow([other, mine], 'sess-1')?.id).toBe('mine')
    expect(pickWorkflow([done, aborted, other], 'sess-1')?.id).toBe('other')
    expect(pickWorkflow([done, aborted], 'sess-1')).toBeNull()
  })

  test('steps follow the workflow type and mark the current phase', () => {
    expect(steps(wf({ phase: 'verify' })).map(s => s.name)).toEqual(['plan', 'implement', 'verify', 'learn'])
    expect(current(wf({ phase: 'verify' }))).toBe(2)
    expect(steps(wf({ type: 'bug', phase: 'review' })).map(s => s.name)).toEqual(['analyze', 'fix', 'review'])
    expect(current(wf({ type: 'e2e', phase: 'heal' }))).toBe(3)
  })

  test('a complex spec plans first before discovery and again once the design exists', () => {
    expect(current(wf({ complexity: 'complex', phase: 'plan' }))).toBe(0)
    expect(current(wf({ complexity: 'complex', phase: 'plan', design_content: '# D' }))).toBe(4)
    expect(current(wf({ complexity: 'complex', phase: 'accept', design_content: '# D' }))).toBe(4)
  })

  test('a complex spec back in plan without a stored design still counts as its second plan', () => {
    const back = wf({ complexity: 'complex', phase: 'plan', delegated_agents: { design: ['delivery-system-architect'] } })
    expect(current(back)).toBe(4)
    expect(nextPhases(back)).toEqual(['implement'])
    expect(nextPhases(wf({ complexity: 'complex', phase: 'plan', delegated_agents: { plan: ['delivery-strategic-architect'] } }))).toEqual(['discovery'])
  })

  test('a workflow is this session’s when it names this session or none', () => {
    expect(isOwn(wf(), 'sess-1')).toBe(true)
    expect(isOwn(wf({ session_id: '' }), 'sess-1')).toBe(true)
    expect(isOwn(wf({ session_id: 'sess-2' }), 'sess-1')).toBe(false)
  })

  test('parseClean drops control characters from strings and keeps newlines and tabs', () => {
    const parsed = parseClean<{ title: string; plan: string; n: number }>(JSON.stringify({ title: 'a\u001b[2Jb\u0007', plan: '# P\n\tx', n: 1 }))
    expect(parsed).toEqual({ title: 'a[2Jb', plan: '# P\n\tx', n: 1 })
  })

  test('denials come newest first; fresh ones are this session’s, past the last seen, oldest first', () => {
    const ev = (id: number, session_id: string) => ({ id, ts: `t${id}`, refs: { hook: 'phase_guard', tool_name: 'Edit', reason: 'verify phase', session_id } })
    const denials = denialsOf([ev(3, 'sess-1'), ev(5, 'sess-1'), ev(4, 'sess-2')])
    expect(denials.map(d => d.id)).toEqual([5, 4, 3])
    expect(denials[0]).toMatchObject({ hook: 'phase_guard', tool: 'Edit', reason: 'verify phase', sessionId: 'sess-1' })
    expect(freshDenials(denials, 2, 'sess-1').map(d => d.id)).toEqual([3, 5])
    expect(freshDenials(denials, 3, 'sess-1').map(d => d.id)).toEqual([5])
  })

  test('nextPhases follows the state machine; a spec plan leads to discovery only before a complex design', () => {
    expect(nextPhases(wf({ phase: 'plan' }))).toEqual(['implement'])
    expect(nextPhases(wf({ phase: 'plan', complexity: 'complex' }))).toEqual(['discovery'])
    expect(nextPhases(wf({ phase: 'plan', complexity: 'complex', design_content: '# D' }))).toEqual(['implement'])
    expect(nextPhases(wf({ phase: 'verify' }))).toEqual(['implement', 'learn'])
    expect(nextPhases(wf({ type: 'bug', phase: 'review' }))).toEqual(['fix', 'complete'])
    expect(nextPhases(wf({ type: 'e2e', phase: 'heal' }))).toEqual(['generate', 'complete'])
  })

  test('goalCondition asks for evidence the evaluator can read, and the coordinator to continue with', () => {
    const spec = goalCondition(wf(), false)
    expect(spec.startsWith('/goal Stratus workflow spec-a is complete: ')).toBe(true)
    expect(spec).toContain('mcp__stratus__get_workflow output for spec-a')
    expect(spec).toContain('the last code review shown has verdict PASS')
    expect(spec).toContain('read .claude/skills/resume/SKILL.md and continue spec-a from its current phase')
    expect(spec).toContain('Stop after 40 turns.')
    expect(spec.length - '/goal '.length).toBeLessThanOrEqual(4000)

    const e2e = goalCondition(wf({ id: 'e2e-login', type: 'e2e' }), false)
    expect(e2e).toContain('the last Playwright test run shown passes')
    expect(e2e).toContain('skills/e2e/SKILL.md')
    expect(e2e).not.toContain('Verdict')

    expect(goalCondition(wf(), true)).toContain('.claude/skills/swarm/SKILL.md')
    expect(goalCondition(wf({ complexity: 'complex' }), false)).toContain('.claude/skills/spec-complex/SKILL.md')
  })

  test('pickMission prefers the workflow’s live mission; freshAlerts toasts only new warnings', () => {
    const m = (id: string, workflow_id: string, status = 'active'): Mission => ({ id, workflow_id, title: id, status, strategy: 'parallel' })
    expect(pickMission([m('a', 'x'), m('b', 'spec-a')], 'spec-a')?.id).toBe('b')
    expect(pickMission([m('a', 'spec-a', 'complete'), m('b', 'x', 'verifying')], 'spec-a')?.id).toBe('b')
    expect(pickMission([m('a', 'spec-a', 'aborted')], 'spec-a')).toBeNull()
    const a = (id: number, severity: string): Alert => ({ id, type: 'stale', severity, message: `m${id}`, created_at: '' })
    expect(freshAlerts([a(4, 'warning'), a(3, 'info'), a(2, 'warning')], 1).map(x => x.id)).toEqual([2, 4])
    expect(freshAlerts([a(4, 'warning'), a(2, 'warning')], 2).map(x => x.id)).toEqual([4])
  })
})

type World = {
  workflows: Workflow[]; events: object[]; missions: Mission[]; swarm: Swarm | null; alerts: Alert[]
  isDown: boolean; failWrite?: string; isOldServer?: boolean; toasts: string[]; urls: string[]; writes: string[]; opened: string[]
  filled: string[]; fillRefusal?: string; clock?: MockClock
  sessionId: string; port: string; draft: string; ended: Workflow[]; stall?: Promise<void>
}
const world = (): World => ({
  workflows: [wf()], events: [], missions: [], swarm: null, alerts: [], isDown: false, toasts: [], urls: [], writes: [], opened: [], filled: [],
  sessionId: 'sess-1', port: '41999', draft: '', ended: [],
})
const JSON_TYPE = { 'content-type': 'application/json' }
const respond = (body: unknown) => ({ value: { status: 200, ok: true, headers: JSON_TYPE, text: JSON.stringify(body) } }) as never

// The engine beneath a started interactive session: the Stratus API, the port command, toasts and the pane.
const start = async ($: { session: { start: (e: never) => Promise<unknown> } }, on: On, w: World) => {
  w.clock = mock.clock(on)
  on('session.id', () => ({ value: w.sessionId }) as never)
  on('process.run', () => ({ value: { exitCode: 0, stdout: `${w.port}\n`, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }) as never)
  on('prompt.read', () => ({ value: { text: w.draft, cursor: w.draft.length } }) as never)
  on('http.fetch', async ($, e) => {
    w.urls.push(e.url)
    if (w.stall) await w.stall
    if (w.isDown || new URL(e.url).port !== w.port) throw new Error('connect ECONNREFUSED')
    const path = new URL(e.url).pathname
    if (e.init?.method) {
      w.writes.push(`${e.init.method} ${path} ${e.init.body ?? ''}`.trim())
      if (w.failWrite) return { value: { status: 400, ok: false, headers: JSON_TYPE, text: JSON.stringify({ error: w.failWrite }) } } as never
      if (w.isOldServer) return { value: { status: 200, ok: true, headers: { 'content-type': 'text/html; charset=utf-8' }, text: '<!doctype html>' } } as never
      return respond({})
    }
    if (path === '/api/dashboard/state') return respond({ workflows: w.workflows })
    if (path === '/api/events/search') return respond({ results: w.events })
    if (path === '/api/swarm/missions') return respond(w.missions)
    if (path.startsWith('/api/swarm/missions/')) return respond(w.swarm)
    if (path === '/api/guardian/alerts') return respond(w.alerts)
    if (path.startsWith('/api/workflows/')) {
      const id = decodeURIComponent(path.slice('/api/workflows/'.length))
      const found = [...w.workflows, ...w.ended].find(x => x.id === id)
      return found ? respond(found) : ({ value: { status: 404, ok: false, headers: JSON_TYPE, text: '{"error":"not found"}' } } as never)
    }
    throw new Error(`unexpected ${e.url}`)
  })
  on('ui.toast', ($, e) => (w.toasts.push(e.text), { value: undefined }) as never)
  on('command.register', () => ({ value: { command: 'stratus' } }) as never)
  on('ui.open', ($, e) => (w.opened.push(e.id), { value: { isPlaced: true } }) as never)
  on('prompt.fill', ($, e) => (w.fillRefusal ? { isFilled: false, refusal: w.fillRefusal } : (w.filled.push(e.text), { isFilled: true })) as never)
  on('ui.render', { component: 'AbovePrompt' }, () => ({ type: 'engine', ref: 0 }) as never)
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  await $.session.start({ cwd: '/w', surface: null, isInteractive: true } as never)
  await w.clock.advance(0)
}
const FULL = { columns: 160, rows: 40, isFullscreen: true }
const band = { plugin: 'stratus-hud', surface: 'terminal', component: 'AbovePrompt', requestId: 'band', props: { hasSurvey: false, isWorking: false }, viewport: FULL } as never
const pane = {
  plugin: 'stratus-hud', surface: 'terminal', component: 'Pane', requestId: 'stratus',
  props: { title: 'Stratus', isFocused: true, bodyColumns: 80, placement: 'dock', scroll: { offset: 0, bodyRows: 30 }, view: {} },
  viewport: FULL,
} as never

test('the band shows the workflow and its phase from the port `stratus port` names', async ($, on) => {
  const w = world()
  w.workflows = [wf({ tasks: [{ index: 0, title: 'a', status: 'done' }, { index: 1, title: 'b', status: 'pending' }], total_tasks: 2, delegated_agents: { implement: ['delivery-backend-engineer'] } })]
  await start($, on, w)
  expect(w.urls[0]).toBe('http://127.0.0.1:41999/api/dashboard/state')
  const ui = await $.ui.mount(band)
  expect((await ui.find({ type: 'Text', text: 'Add mods' }))?.props).toMatchObject({ bold: true })
  expect((await ui.find({ type: 'Text', text: '▸implement' }))?.props).toMatchObject({ bold: true, color: 'suggestion' })
  expect(await ui.find({ text: / · 1\/2/ })).toBeDefined()
  expect(await ui.find({ text: /delivery-backend-engineer/ })).toBeDefined()
  await ui.unmount()
})

test('a phase change and a new denial of this session toast once; another session’s denial does not', async ($, on) => {
  const w = world()
  w.events = [{ id: 7, ts: 't7', refs: { hook: 'phase_guard', tool_name: 'Edit', reason: 'old', session_id: 'sess-1' } }]
  await start($, on, w)
  expect(w.toasts).toEqual([])

  w.workflows = [wf({ phase: 'verify' })]
  w.events = [
    { id: 9, ts: 't9', refs: { hook: 'delegation_guard', tool_name: 'Agent', reason: 'other session', session_id: 'sess-2' } },
    { id: 8, ts: 't8', refs: { hook: 'phase_guard', tool_name: 'Write', reason: 'verify is read-only', session_id: 'sess-1' } },
    ...w.events,
  ]
  await w.clock!.advance(5_000)
  expect(w.toasts).toEqual(['◈ Add mods: implement → verify', '⛔ phase_guard blocked Write: verify is read-only'])

  await w.clock!.advance(5_000)
  expect(w.toasts).toHaveLength(2)
})

test('with the API down the band says offline, and the pane says how to start it', async ($, on) => {
  const w = world()
  w.isDown = true
  await start($, on, w)
  const ui = await $.ui.mount(band)
  expect(await ui.find({ text: '◈ stratus offline' })).toBeDefined()
  await ui.unmount()
  const p = await $.ui.mount(pane)
  expect(await p.find({ text: /stratus serve/ })).toBeDefined()
  await p.unmount()
})

test('/stratus opens the pane; its tabs show the tasks, the plan and the denials', async ($, on) => {
  const w = world()
  w.workflows = [wf({ tasks: [{ index: 0, title: 'Write the mod', status: 'in_progress' }], total_tasks: 1, plan_content: '# The plan' })]
  w.events = [{ id: 1, ts: 't1', refs: { hook: 'bash_write_guard', tool_name: 'Bash', reason: 'no workflow', session_id: 'sess-1' } }]
  await start($, on, w)
  expect(await $.command.run({ command: 'stratus', args: '' } as never)).toMatchObject({ text: 'Stratus pane opened.' })
  expect(w.opened).toEqual(['stratus'])

  const p = await $.ui.mount(pane)
  expect(await p.find({ text: '▸ Write the mod' })).toBeDefined()
  await p.press({ key: 'tab-plan' })
  expect((await p.find({ type: 'Markdown', key: 'plan' }))?.props).toMatchObject({ text: '# The plan' })
  await p.press({ key: 'tab-denials' })
  expect(await p.find({ text: 'bash_write_guard' })).toBeDefined()
  expect(await p.find({ text: 'no workflow' })).toBeDefined()
  await p.unmount()
})

test('a non-interactive session registers the command but never polls', async ($, on) => {
  const w = world()
  w.clock = mock.clock(on)
  on('http.fetch', ($, e) => (w.urls.push(e.url), respond({})))
  on('command.register', () => ({ value: { command: 'stratus' } }) as never)
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  await $.session.start({ cwd: '/w', surface: null, isInteractive: false } as never)
  await w.clock.advance(10_000)
  expect(w.urls).toEqual([])
})

test('a new Guardian warning toasts once; info alerts only show in the pane', async ($, on) => {
  const w = world()
  w.alerts = [{ id: 1, type: 'stale_workflow', severity: 'warning', message: 'old warning', created_at: 'c1' }]
  await start($, on, w)
  w.alerts = [
    { id: 3, type: 'coverage', severity: 'info', message: 'just info', created_at: 'c3' },
    { id: 2, type: 'drift', severity: 'warning', message: 'governance drift', created_at: 'c2' },
    ...w.alerts,
  ]
  await w.clock!.advance(5_000)
  expect(w.toasts).toEqual(['⚠ Guardian: governance drift'])

  await $.command.run({ command: 'stratus', args: '' } as never)
  const p = await $.ui.mount(pane)
  await p.press({ key: 'tab-guardian' })
  expect(await p.find({ text: 'just info' })).toBeDefined()
  expect(await p.find({ text: 'old warning' })).toBeDefined()
  await p.unmount()
})

test('the swarm tab shows the workflow’s live mission with its workers and tickets', async ($, on) => {
  const w = world()
  const mission: Mission = { id: 'm1', workflow_id: 'spec-a', title: 'Parallel build', status: 'active', strategy: 'parallel' }
  w.missions = [mission]
  w.swarm = {
    mission,
    workers: [{ id: 'w1', agent_type: 'delivery-backend-engineer', status: 'active', branch_name: 'swarm/w1', last_heartbeat: 'h1' }],
    tickets: [{ id: 't1', title: 'API route', status: 'in_progress', domain: 'backend' }, { id: 't2', title: 'Pane', status: 'done', domain: 'frontend' }],
  }
  await start($, on, w)
  expect(w.urls).toContain('http://127.0.0.1:41999/api/swarm/missions/m1')
  const p = await $.ui.mount(pane)
  await p.press({ key: 'tab-swarm' })
  expect(await p.find({ text: 'Parallel build' })).toBeDefined()
  expect(await p.find({ text: /delivery-backend-engineer active · swarm\/w1/ })).toBeDefined()
  expect(await p.find({ text: '▸ API route [backend]' })).toBeDefined()
  expect((await p.find({ type: 'Text', text: '✓ Pane [frontend]' }))?.props).toMatchObject({ dimColor: true })
  await p.unmount()
})

test('a phase button asks first; Confirm moves the workflow, Cancel leaves it', async ($, on) => {
  const w = world()
  w.workflows = [wf({ phase: 'verify' })]
  await start($, on, w)
  const p = await $.ui.mount(pane)
  expect((await p.findAll({ type: 'Button' })).map(b => b.key)).toEqual(
    ['tab-workflow', 'tab-plan', 'tab-swarm', 'tab-guardian', 'tab-denials', 'autopilot', 'go-implement', 'go-learn', 'abort'],
  )

  await p.press({ key: 'go-learn' })
  expect(await p.find({ text: 'Move Add mods to learn?' })).toBeDefined()
  await p.press({ key: 'cancel' })
  expect(w.writes).toEqual([])
  expect(await p.find({ key: 'go-learn' })).toBeDefined()

  await p.press({ key: 'go-learn' })
  w.workflows = [wf({ phase: 'learn' })]
  await p.press({ key: 'confirm' })
  expect(w.writes).toEqual(['PUT /api/workflows/spec-a/phase {"phase":"learn"}'])
  expect(w.toasts).toEqual(['◈ Add mods: verify → learn'])
  await p.unmount()
})

test('a refused abort toasts the server’s reason', async ($, on) => {
  const w = world()
  w.failWrite = 'workflow not found'
  await start($, on, w)
  const p = await $.ui.mount(pane)
  await p.press({ key: 'abort' })
  expect(await p.find({ text: 'Abort Add mods?' })).toBeDefined()
  await p.press({ key: 'confirm' })
  expect(w.writes).toEqual(['POST /api/workflows/spec-a/abort'])
  expect(w.toasts).toEqual(['◈ abort failed: workflow not found'])
  await p.unmount()
})

test('an older server that answers the abort with its HTML page is no success', async ($, on) => {
  const w = world()
  w.isOldServer = true
  await start($, on, w)
  const p = await $.ui.mount(pane)
  await p.press({ key: 'abort' })
  await p.press({ key: 'confirm' })
  expect(w.toasts).toEqual(['◈ abort failed: no API answer; restart `stratus serve` with this Stratus version'])
  await p.unmount()
})

test('Autopilot puts the workflow’s goal in the prompt for the person to send', async ($, on) => {
  const w = world()
  const mission: Mission = { id: 'm1', workflow_id: 'spec-a', title: 'Parallel build', status: 'active', strategy: 'parallel' }
  w.missions = [mission]
  w.swarm = { mission, workers: [], tickets: [] }
  await start($, on, w)
  const p = await $.ui.mount(pane)
  await p.press({ key: 'autopilot' })
  expect(w.filled).toEqual([goalCondition(wf(), true)])
  expect(w.writes).toEqual([])
  expect(w.toasts.at(-1)).toContain('Press Enter')
  await p.unmount()
})

test('Autopilot says so when the prompt could not be filled', async ($, on) => {
  const w = world()
  w.fillRefusal = 'no_composer'
  await start($, on, w)
  const p = await $.ui.mount(pane)
  await p.press({ key: 'autopilot' })
  expect(w.filled).toEqual([])
  expect(w.toasts.at(-1)).toBe('◈ Autopilot: could not fill the prompt')
  await p.unmount()
})

test('this session’s workflow toasts once when it completes or is aborted and leaves the band', async ($, on) => {
  const w = world()
  w.workflows = [wf({ phase: 'learn' }), wf({ id: 'bug-b', type: 'bug', phase: 'fix', title: 'Fix login' })]
  await start($, on, w)

  w.ended = [wf({ phase: 'complete' })]
  w.workflows = [wf({ id: 'bug-b', type: 'bug', phase: 'fix', title: 'Fix login' })]
  await w.clock!.advance(5_000)
  expect(w.toasts).toEqual(['◈ Add mods: learn → complete'])

  const p = await $.ui.mount(pane)
  await p.press({ key: 'abort' })
  w.ended.push(wf({ id: 'bug-b', type: 'bug', phase: 'fix', title: 'Fix login', aborted: true }))
  w.workflows = []
  await p.press({ key: 'confirm' })
  expect(w.writes).toEqual(['POST /api/workflows/bug-b/abort'])
  expect(w.toasts).toEqual(['◈ Add mods: learn → complete', '◈ Fix login: aborted'])
  await w.clock!.advance(5_000)
  expect(w.toasts).toHaveLength(2)
  await p.unmount()
})

test('another session’s workflow is shown as such, without toasts or actions', async ($, on) => {
  const w = world()
  w.workflows = [wf({ session_id: 'sess-2' })]
  await start($, on, w)
  w.workflows = [wf({ session_id: 'sess-2', phase: 'verify' })]
  await w.clock!.advance(5_000)
  expect(w.toasts).toEqual([])

  const ui = await $.ui.mount(band)
  expect(await ui.find({ text: ' (another session)' })).toBeDefined()
  await ui.unmount()
  const p = await $.ui.mount(pane)
  expect((await p.findAll({ type: 'Button' })).map(b => b.key)).toEqual(['tab-workflow', 'tab-plan', 'tab-swarm', 'tab-guardian', 'tab-denials'])
  expect(await p.find({ text: /another session/ })).toBeDefined()
  await p.unmount()
})

test('after /clear the new session id picks the workflow and the denials', async ($, on) => {
  const w = world()
  w.workflows = [wf(), wf({ id: 'spec-new', title: 'New work', session_id: 'sess-9' })]
  await start($, on, w)
  w.sessionId = 'sess-9'
  w.events = [{ id: 4, ts: 't4', refs: { hook: 'phase_guard', tool_name: 'Edit', reason: 'verify is read-only', session_id: 'sess-9' } }]
  await w.clock!.advance(5_000)
  expect(w.toasts).toEqual(['⛔ phase_guard blocked Edit: verify is read-only'])
  const ui = await $.ui.mount(band)
  expect(await ui.find({ type: 'Text', text: 'New work' })).toBeDefined()
  await ui.unmount()
})

test('while the API is down the port is looked up again, so a server back on another port is found', async ($, on) => {
  const w = world()
  await start($, on, w)
  w.port = '42001'
  await w.clock!.advance(30_000)
  expect(w.urls.at(-1)).toBe('http://127.0.0.1:41999/api/dashboard/state')
  await w.clock!.advance(5_000)
  expect(w.urls.at(-1)).toMatch(/^http:\/\/127\.0\.0\.1:42001\//)
  const ui = await $.ui.mount(band)
  expect(await ui.find({ type: 'Text', text: 'Add mods' })).toBeDefined()
  await ui.unmount()
})

test('a slow API gets one poll at a time', async ($, on) => {
  const w = world()
  await start($, on, w)
  let release = () => {}
  w.stall = new Promise<void>(resolve => (release = resolve))
  const before = w.urls.length
  await w.clock!.advance(5_000)
  await w.clock!.advance(5_000)
  await w.clock!.advance(5_000)
  expect(w.urls.length - before).toBe(1)
  w.stall = undefined
  release()
  await w.clock!.advance(5_000)
  expect(w.urls.filter(u => u.endsWith('/api/dashboard/state')).length).toBeGreaterThan(2)
})

test('API strings reach the band without control characters', async ($, on) => {
  const w = world()
  w.workflows = [wf({ title: 'Add\u001b[31m mods' })]
  await start($, on, w)
  const ui = await $.ui.mount(band)
  expect(await ui.find({ type: 'Text', text: 'Add[31m mods' })).toBeDefined()
  await ui.unmount()
})

test('Autopilot leaves a draft in the prompt alone', async ($, on) => {
  const w = world()
  w.draft = 'half a thought'
  await start($, on, w)
  const p = await $.ui.mount(pane)
  await p.press({ key: 'autopilot' })
  expect(w.filled).toEqual([])
  expect(w.toasts.at(-1)).toBe('◈ Autopilot: the prompt holds a draft; send or clear it first')
  await p.unmount()
})
