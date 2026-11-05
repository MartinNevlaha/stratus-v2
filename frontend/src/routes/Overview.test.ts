// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import Overview from './Overview.svelte'
import type { ManagedTeam } from '$lib/teams'
import type { WorkflowState } from '$lib/types'
import { appState, setActiveTab } from '$lib/store'

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

let app: ReturnType<typeof mount> | undefined
let workflows: WorkflowState[]
let teams: ManagedTeam[]
let teamsStatus: number
let teamGets: number

const wf = (id: string): WorkflowState => ({
  id, type: 'spec', phase: 'implement', complexity: 'simple', delegated_agents: {}, tasks: [], total_tasks: 0,
  aborted: false, title: `Title ${id}`, created_at: '2026-11-05T10:00:00Z', updated_at: '2026-11-05T10:00:00Z',
})
const team = (id: string, state: string, workflowIds: string[], createdAt = '2026-11-05T10:00:00Z'): ManagedTeam => ({
  id, project_id: 'p', project_root: '/p', state, members: [], limits: { max_concurrent: 2, max_auto_rounds: 4, max_turns: 40, max_tokens: 0 },
  turns: 0, auto_wakeups: 0, input_tokens: 0, output_tokens: 0, usage_known: false, created_at: createdAt,
  tasks: workflowIds.map((w, i) => ({ id: `${id}-t${i}`, from: 'user', to: 'lead', role: 'lead', text: 'x', workflow_id: w, state: 'complete', kind: 'task', created_at: createdAt })),
} as ManagedTeam)

beforeEach(() => {
  workflows = []
  teams = []
  teamsStatus = 200
  teamGets = 0
  wsHandlers.clear()
  window.location.hash = ''
  appState.pendingTeamSelect = null
  setActiveTab('overview')
  vi.stubGlobal('fetch', async (input: string) => {
    const path = String(input).replace(/^https?:\/\/[^/]+/, '').split('?')[0]
    if (path === '/api/teams') {
      teamGets++
      return teamsStatus === 200 ? Response.json({ teams }) : Response.json({ error: 'boom' }, { status: teamsStatus })
    }
    if (path === '/api/workflows') return Response.json(workflows)
    if (path === '/api/past') return Response.json({ items: [], total: 0, offset: 0, limit: 20 })
    if (path === '/api/decisions') return Response.json({ items: [], summary: { new_items_since: 0, guardian_new: 0, teams_updated: false } })
    if (path.endsWith('/denials')) return Response.json({ denials: [] })
    if (path.endsWith('/agents') || path === '/api/workflow-agents') return Response.json({ agents: [] })
    if (path.includes('/search')) return Response.json({ results: [] })
    return Response.json([])
  })
})
afterEach(async () => {
  if (app) await unmount(app)
  app = undefined
  document.body.innerHTML = ''
  window.location.hash = ''
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

async function show() {
  app = mount(Overview, { target: document.body })
  flushSync()
  await vi.waitFor(() => expect(document.querySelector('.workflow-card, .detail')).not.toBeNull())
}
const chips = () => [...document.querySelectorAll('.team-chip')]
const openButtons = () => [...document.querySelectorAll('button')].filter(b => b.textContent?.trim() === 'Otvoriť tím')

it('shows a team chip with its state only on workflows that a team task references', async () => {
  workflows = [wf('spec-a'), wf('spec-b')]
  teams = [team('1234567890abcdef', 'paused', ['spec-a'])]
  await show()
  await vi.waitFor(() => expect(chips()).toHaveLength(1))
  expect(chips()[0].textContent).toContain('Tím 12345678')
  expect(chips()[0].textContent).toContain('Pozastavený')
  expect(chips()[0].closest('.workflow-card')?.textContent).toContain('spec-a')
  expect(openButtons()).toHaveLength(1)
  expect(openButtons()[0].getAttribute('aria-label')).toContain('1234567890abcdef')
})

it('picks the newest team when several reference the same workflow', async () => {
  workflows = [wf('spec-a')]
  teams = [team('oldoldol-1', 'stopped', ['spec-a'], '2026-11-01T10:00:00Z'), team('newnewne-2', 'running', ['spec-a'], '2026-11-04T10:00:00Z')]
  await show()
  await vi.waitFor(() => expect(chips()).toHaveLength(1))
  expect(chips()[0].textContent).toContain('Tím newnewne')
  expect(chips()[0].textContent).toContain('Pracuje')
})

it('asks the Team tab to preselect the team when Otvoriť tím is clicked', async () => {
  workflows = [wf('spec-a')]
  teams = [team('team-aaaa-1', 'running', ['spec-a'])]
  await show()
  await vi.waitFor(() => expect(openButtons()).toHaveLength(1))
  openButtons()[0].click()
  expect(appState.pendingTeamSelect).toBe('team-aaaa-1')
  expect(appState.activeTab).toBe('team')
})

it('shows the chip on the focused workflow detail too', async () => {
  window.location.hash = '#/workflow/spec-a'
  workflows = [wf('spec-a')]
  teams = [team('team-aaaa-1', 'stopped', ['spec-a'])]
  await show()
  await vi.waitFor(() => expect(document.querySelector('.detail .team-chip')).not.toBeNull())
  expect(document.querySelector('.detail .team-chip')?.textContent).toContain('Zastavený')
  expect(openButtons()).toHaveLength(1)
})

it('reloads the teams on team_updated, debounced', async () => {
  workflows = [wf('spec-a')]
  await show()
  await vi.waitFor(() => expect(teamGets).toBe(1))
  expect(chips()).toHaveLength(0)
  teams = [team('team-aaaa-1', 'running', ['spec-a'])]
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  const handlers = wsHandlers.get('team_updated')!
  handlers.forEach(fn => { fn(); fn() })
  expect(teamGets).toBe(1)
  await vi.advanceTimersByTimeAsync(200)
  vi.useRealTimers()
  await vi.waitFor(() => expect(chips()).toHaveLength(1))
  expect(teamGets).toBe(2)
})

it('keeps working without chips or errors when the teams request fails', async () => {
  workflows = [wf('spec-a')]
  teamsStatus = 500
  await show()
  await vi.waitFor(() => expect(teamGets).toBe(1))
  expect(chips()).toHaveLength(0)
  expect(document.querySelector('[role="alert"]')).toBeNull()
  expect(document.body.textContent).toContain('spec-a')
})

it('shows no chip when there are no teams', async () => {
  workflows = [wf('spec-a')]
  await show()
  await vi.waitFor(() => expect(teamGets).toBe(1))
  expect(chips()).toHaveLength(0)
  expect(openButtons()).toHaveLength(0)
})
