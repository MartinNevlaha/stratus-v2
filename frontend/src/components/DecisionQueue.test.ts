// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import DecisionQueue from './DecisionQueue.svelte'
import type { DecisionItem, DecisionSummary } from '$lib/types'
import { appState } from '$lib/store'

const wsHandlers = vi.hoisted(() => new Map<string, Set<() => void>>())
vi.mock('$lib/ws', () => ({
  wsClient: {
    on: (type: string, fn: () => void) => {
      if (!wsHandlers.has(type)) wsHandlers.set(type, new Set())
      wsHandlers.get(type)!.add(fn)
      return () => wsHandlers.get(type)?.delete(fn)
    },
  },
}))
const emitWs = (type: string) => wsHandlers.get(type)?.forEach(fn => fn())

let app: ReturnType<typeof mount> | undefined
let items: DecisionItem[]
let summary: DecisionSummary
let calls: { url: string; method: string; body: unknown }[]

const approval: DecisionItem = {
  id: 'team:t1:lead:ap-1', kind: 'team_approval', severity: 'warning', title: 'Tím t1 (lead): nástroj potrebuje povolenie',
  detail: 'May I write?', created_at: '2026-11-05T10:00:00Z',
  actions: [
    { name: 'approve', method: 'POST', path: '/teams/t1/approvals', body: { member_id: 'lead', approval_id: 'ap-1', allow: true } },
    { name: 'deny', method: 'POST', path: '/teams/t1/approvals', body: { member_id: 'lead', approval_id: 'ap-1', allow: false } },
  ],
}
const recover: DecisionItem = {
  id: 'team:t1:lead:recover', kind: 'team_recover', severity: 'warning', title: 'Tím t1 (lead): skontroluj výsledok a obnov',
  detail: '', created_at: '2026-11-05T10:00:00Z',
  actions: [{ name: 'recover', method: 'POST', path: '/teams/t1/recover', body: { member_id: 'lead' } }],
}
const workflow: DecisionItem = {
  id: 'workflow:spec-x:review_failed', kind: 'workflow', severity: 'warning', title: 'Spec X', detail: 'Posledná kontrola zlyhala (verify)',
  created_at: '2026-11-05T10:00:00Z', link: '#/workflow/spec-x', actions: [],
}
const guardian: DecisionItem = {
  id: 'guardian:7', kind: 'guardian', severity: 'critical', title: 'Agent failed', detail: 'agent_failed',
  created_at: '2026-11-05T10:00:00Z', actions: [{ name: 'dismiss', method: 'PUT', path: '/guardian/alerts/7/dismiss', body: {} }],
}

beforeEach(() => {
  items = []
  summary = { new_items_since: 0, guardian_new: 0, teams_updated: false }
  calls = []
  localStorage.clear()
  vi.stubGlobal('fetch', async (input: string | URL, init?: RequestInit) => {
    const url = String(input)
    calls.push({ url, method: init?.method ?? 'GET', body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (url.includes('/api/decisions')) return Response.json({ items, summary })
    return Response.json({ ok: true })
  })
})
afterEach(async () => {
  if (app) await unmount(app)
  app = undefined
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.useRealTimers()
  wsHandlers.clear()
  appState.workflowActivityCounter = 0
  appState.activeTab = 'overview'
})
async function show() {
  app = mount(DecisionQueue, { target: document.body })
  flushSync()
  await vi.waitFor(() => expect(calls.some(c => c.url.includes('/api/decisions'))).toBe(true))
  await new Promise(r => setTimeout(r, 0))
  flushSync()
}
const decisionCalls = () => calls.filter(c => c.url.includes('/api/decisions'))

it('should render nothing when the queue is empty', async () => {
  await show()
  expect(document.querySelector('.queue')).toBeNull()
})

it('should render a row with the right controls for every kind', async () => {
  items = [approval, recover, workflow, guardian]
  await show()
  const rows = document.querySelectorAll('.row')
  expect(rows.length).toBe(4)
  const text = (i: number) => rows[i].textContent ?? ''
  expect(text(0)).toContain('Povoliť raz')
  expect(text(0)).toContain('Zamietnuť')
  expect(text(1)).toContain('Obnoviť po kontrole')
  expect(rows[2].querySelector('a')?.getAttribute('href')).toBe('#/workflow/spec-x')
  expect(text(3)).toContain('Zavrieť')
})

it('should post the approve request and reload when the approve button is clicked', async () => {
  items = [approval]
  await show()
  const before = decisionCalls().length
  ;(Array.from(document.querySelectorAll('button')).find(b => b.textContent === 'Povoliť raz') as HTMLButtonElement).click()
  await vi.waitFor(() => expect(decisionCalls().length).toBe(before + 1))
  const post = calls.find(c => c.method === 'POST')!
  expect(post.url).toBe('/api/teams/t1/approvals')
  expect(post.body).toEqual({ member_id: 'lead', approval_id: 'ap-1', allow: true })
})

it('should dismiss a guardian alert through the dismiss endpoint', async () => {
  items = [guardian]
  await show()
  ;(Array.from(document.querySelectorAll('button')).find(b => b.textContent === 'Zavrieť') as HTMLButtonElement).click()
  await vi.waitFor(() => expect(calls.some(c => c.method === 'PUT' && c.url === '/api/guardian/alerts/7/dismiss')).toBe(true))
})

it('should show the since line only when the summary has non-zero values', async () => {
  items = [guardian]
  localStorage.setItem('stratus-last-seen', '2026-11-05T09:00:00.000Z')
  await show()
  expect(document.querySelector('.since')).toBeNull()
  expect(decisionCalls()[0].url).toContain('since=2026-11-05T09%3A00%3A00.000Z')
  await unmount(app!)
  app = undefined
  document.body.innerHTML = ''
  summary = { new_items_since: 2, guardian_new: 1, teams_updated: true }
  calls = []
  await show()
  expect(document.querySelector('.since')?.textContent).toContain('Od posledného návratu: 2 nových')
})

it('should clear the since line and store a new last-seen time when marked as seen', async () => {
  items = [guardian]
  summary = { new_items_since: 1, guardian_new: 1, teams_updated: false }
  localStorage.setItem('stratus-last-seen', '2026-11-05T09:00:00.000Z')
  await show()
  ;(Array.from(document.querySelectorAll('button')).find(b => b.textContent === 'Označiť ako videné') as HTMLButtonElement).click()
  flushSync()
  expect(document.querySelector('.since')).toBeNull()
  expect(localStorage.getItem('stratus-last-seen')).not.toBe('2026-11-05T09:00:00.000Z')
})

it('should show the full approval text without clipping', async () => {
  const long = Array.from({ length: 40 }, (_, i) => `line ${i}`).join('\n') + '\nrm -rf /tmp/very-last-line'
  items = [{ ...approval, detail: long }]
  await show()
  const block = document.querySelector('.row pre.approval-text') as HTMLElement
  expect(block.textContent).toBe(long)
})

it('should show the error of a failed action and clear it after a successful load', async () => {
  items = [approval]
  await show()
  const real = globalThis.fetch
  vi.stubGlobal('fetch', async (input: string | URL, init?: RequestInit) =>
    String(input).includes('/teams/') ? Response.json({ error: 'conflict' }, { status: 409 }) : real(input, init))
  ;(Array.from(document.querySelectorAll('button')).find(b => b.textContent === 'Povoliť raz') as HTMLButtonElement).click()
  await vi.waitFor(() => expect(document.querySelector('.err')?.textContent).toContain('conflict'))
  vi.stubGlobal('fetch', real)
  emitWs('guardian_alert')
  await vi.waitFor(() => expect(document.querySelector('.err')).toBeNull())
})

it('should store last-seen on unmount', async () => {
  await show()
  expect(localStorage.getItem('stratus-last-seen')).toBeNull()
  await unmount(app!)
  app = undefined
  expect(localStorage.getItem('stratus-last-seen')).not.toBeNull()
})

it('should switch to the team tab when the team button is clicked', async () => {
  items = [recover]
  await show()
  ;(Array.from(document.querySelectorAll('button')).find(b => b.textContent === 'Otvoriť tím') as HTMLButtonElement).click()
  expect(appState.activeTab).toBe('team')
})

it('should reload debounced after a team_updated event', async () => {
  await show()
  vi.useFakeTimers()
  const before = decisionCalls().length
  emitWs('team_updated')
  emitWs('team_updated')
  await vi.advanceTimersByTimeAsync(149)
  expect(decisionCalls().length).toBe(before)
  await vi.advanceTimersByTimeAsync(2)
  expect(decisionCalls().length).toBe(before + 1)
})

it('should not load twice at mount when workflow activity is already non-zero', async () => {
  appState.workflowActivityCounter = 3
  await show()
  vi.useFakeTimers()
  await vi.advanceTimersByTimeAsync(400)
  expect(decisionCalls().length).toBe(1)
})

it('should ignore an older response that arrives after a newer one', async () => {
  const pending: ((r: Response) => void)[] = []
  vi.stubGlobal('fetch', (input: string | URL) => {
    if (!String(input).includes('/api/decisions')) return Promise.resolve(Response.json({ ok: true }))
    return new Promise<Response>(resolve => pending.push(resolve))
  })
  app = mount(DecisionQueue, { target: document.body })
  flushSync()
  await vi.waitFor(() => expect(pending.length).toBe(1))
  emitWs('team_updated')
  await vi.waitFor(() => expect(pending.length).toBe(2), { timeout: 1000 })
  pending[1](Response.json({ items: [guardian], summary }))
  await vi.waitFor(() => expect(document.querySelectorAll('.row').length).toBe(1))
  pending[0](Response.json({ items: [approval, recover], summary }))
  await new Promise(r => setTimeout(r, 20))
  expect(document.querySelectorAll('.row').length).toBe(1)
})

it('should label action buttons with the item title and announce list changes politely', async () => {
  items = [approval]
  await show()
  expect(document.querySelector('[aria-live="polite"]')).not.toBeNull()
  const labels = Array.from(document.querySelectorAll('button')).map(b => b.getAttribute('aria-label'))
  expect(labels).toContain(`Povoliť raz: ${approval.title}`)
  expect(labels).toContain(`Zamietnuť: ${approval.title}`)
})
