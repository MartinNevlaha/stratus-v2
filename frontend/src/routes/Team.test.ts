// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import Team from './Team.svelte'
import type { ManagedTeam, TeamPreflight } from '$lib/teams'
import { appState, setActiveTab } from '$lib/store'

vi.mock('$lib/ws', () => ({ wsClient: { on: () => () => {} } }))
let app: ReturnType<typeof mount> | undefined
let team: ManagedTeam
let requests: { path: string; body: unknown }[]
let putPaths: string[]
beforeEach(() => {
 requests = []
 putPaths = []
 team = {
  id: 'team-test', project_id: 'project', project_root: '/project', state: 'running',
  limits: { max_concurrent: 2, max_auto_rounds: 4, max_turns: 40, max_tokens: 0 },
  turns: 0, auto_wakeups: 0, input_tokens: 0, output_tokens: 0, usage_known: false,
  members: [{ id: 'chat', role: 'member', profile: { runtime: 'codex', model: '', effort: '' }, state: 'ready',
   attachment: { id: 'connection', generation: 1, state: 'connected',
    identity: { runtime: 'codex', host_id: 'local', native_id: 'original-chat', project_root: '/project' },
    capabilities: { queue: true, observe_user_turns: true, correlate_results: true, wake: true } } }],
  tasks: []
 }
 vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
  if (init?.method === 'POST' || init?.method === 'PUT') {
   requests.push({ path, body: JSON.parse(String(init.body)) })
   if (init.method === 'PUT') putPaths.push(path)
   if (init.method === 'PUT') return Response.json({ profiles: JSON.parse(String(init.body)) })
   if (path.endsWith('/attachments')) team.members[0].attachment!.state = 'detached'
   return Response.json({ ok: true })
  }
  return Response.json({ teams: [team] })
 })
})
afterEach(async () => {
 if (app) await unmount(app)
 app = undefined
 document.body.innerHTML = ''
 appState.pendingTeamTask = null
 vi.unstubAllGlobals()
})
async function show() {
 app = mount(Team, { target: document.body })
 flushSync()
 await vi.waitFor(() => expect(document.querySelector('.member')).not.toBeNull())
}

function pickerCatalog(): TeamPreflight {
 const profiles = Object.fromEntries(['lead', 'implementation', 'design', 'review', 'diagnosis', 'mechanical'].map(role => [role, { runtime: 'codex', model: 'gpt-6-astra', effort: 'high' }]))
 return {
  project_id: 'project', project_root: '/project', limits: team.limits, warnings: [],
  profiles: { codex: profiles },
  runtimes: [
   { runtime: 'codex', available: true, models: [
    { id: 'gpt-6-astra', efforts: ['xhigh', 'high', 'low'] },
    { id: 'gpt-6.1-sol', efforts: ['medium', 'high'] }
   ] },
   { runtime: 'opencode', available: true, models: [{ id: 'provider/compact', efforts: ['medium'] }] }
  ]
 }
}

async function showPickers(catalog = pickerCatalog()) {
 const ordinaryFetch = globalThis.fetch
 vi.stubGlobal('fetch', (path: string, init?: RequestInit) => path === '/api/teams/preflight'
  ? Promise.resolve(Response.json(catalog)) : ordinaryFetch(path, init))
 await show()
 const openSetup = [...document.querySelectorAll('button')].find(b => b.textContent === 'Nový tím')!
 openSetup.click()
 await vi.waitFor(() => expect(document.querySelector('table')).not.toBeNull())
}

function picker(label: string): HTMLSelectElement {
 const element = document.querySelector(`[aria-label="${label}"]`)
 expect(element?.tagName).toBe('SELECT')
 return element as HTMLSelectElement
}

function choose(select: HTMLSelectElement, value: string) {
 select.value = value
 select.dispatchEvent(new Event('change', { bubbles: true }))
 flushSync()
}

it('uses model and effort dropdowns and submits the chosen profiles', async () => {
 await showPickers()
 const model = picker('Model: Vedenie postupu')
 expect([...model.options].map(option => option.value)).toEqual(['gpt-6-astra', 'gpt-6.1-sol'])
 expect(picker('Uvažovanie: Vedenie postupu').value).toBe('high')
 expect(picker('Zložitá úloha: Vedenie postupu').value).toBe('xhigh')
 choose(model, 'gpt-6.1-sol')
 const effort = picker('Uvažovanie: Vedenie postupu')
 const complex = picker('Zložitá úloha: Vedenie postupu')
 expect([...effort.options].filter(option => !option.disabled).map(option => option.value)).toEqual(['medium', 'high'])
 expect(complex.value).toBe('')
 choose(effort, 'medium')
 choose(complex, 'high')
 const start = [...document.querySelectorAll('button')].find(b => b.textContent === 'Spustiť tím')!
 start.click()
 await vi.waitFor(() => expect(requests.some(request => request.path === '/api/teams')).toBe(true))
 const body = requests.find(request => request.path === '/api/teams')!.body as { members: { profile: unknown; complex_profile: unknown }[] }
 expect(body.members[0].profile).toEqual({ runtime: 'codex', model: 'gpt-6.1-sol', effort: 'medium' })
 expect(body.members[0].complex_profile).toEqual({ runtime: 'codex', model: 'gpt-6.1-sol', effort: 'high' })
})

it('updates the catalog for another runtime and requires a supported selection', async () => {
 await showPickers()
 choose(picker('Nástroj: Vedenie postupu'), 'opencode')
 const model = picker('Model: Vedenie postupu')
 expect([...model.options].filter(option => !option.disabled).map(option => option.value)).toEqual(['provider/compact'])
 const start = [...document.querySelectorAll('button')].find(b => b.textContent === 'Spustiť tím')!
 expect(start.disabled).toBe(true)
 choose(model, 'provider/compact')
 choose(picker('Uvažovanie: Vedenie postupu'), 'medium')
 choose(picker('Zložitá úloha: Vedenie postupu'), 'medium')
 expect(start.disabled).toBe(false)
})

it('shows unavailable configured values without substituting a model', async () => {
 const catalog = pickerCatalog()
 catalog.profiles.codex.lead.model = 'custom-missing'
 await showPickers(catalog)
 const model = picker('Model: Vedenie postupu')
 expect(model.value).toBe('custom-missing')
 expect(model.selectedOptions[0].disabled).toBe(true)
 expect(model.selectedOptions[0].textContent).toContain('nedostupný')
 expect([...document.querySelectorAll('button')].find(b => b.textContent === 'Spustiť tím')!.disabled).toBe(true)
})

it.each([{ models: [] }, { models: [{ id: 'no-levels', efforts: [] }] }])('blocks launch when the runtime has no selectable model or reasoning level', async ({ models }) => {
 const catalog = pickerCatalog()
 catalog.runtimes[0].models = models
 await showPickers(catalog)
 expect([...document.querySelectorAll('button')].find(b => b.textContent === 'Spustiť tím')!.disabled).toBe(true)
 expect(picker('Uvažovanie: Vedenie postupu').disabled).toBe(true)
})

it('detaches the connection without invoking owned-session recovery or stop', async () => {
 await show()
 expect(document.querySelector('.member')?.textContent).toContain('Model určuje pôvodný chat')
 const button = [...document.querySelectorAll('button')].find(b => b.textContent === 'Odpojiť od tímu')!
 button.click()
 await vi.waitFor(() => expect(requests).toEqual([
  { path: '/api/teams/team-test/attachments', body: { member_id: 'chat', action: 'detach' } }
 ]))
 await vi.waitFor(() => expect(document.querySelector('.member')?.textContent).toContain('Na obnovenie povedz v pôvodnom chate'))
})

it.each(['expired', 'disconnected'])('gives a rejoin path for %s without offering owned recovery', async state => {
 team.members[0].attachment!.state = state
 team.members[0].state = 'needs_user'
 await show()
 expect(document.querySelector('.member')?.textContent).toContain('Pripoj tento chat k tímu Stratus')
 expect([...document.querySelectorAll('button')].some(b => b.textContent === 'Obnoviť po kontrole')).toBe(false)
 expect([...document.querySelectorAll('button')].some(b => b.textContent === 'Viesť tím z tohto chatu')).toBe(false)
})

it('explains how to recover an uncertain task even while its connection remains live', async () => {
 team.members[0].state = 'needs_user'
 await show()
 expect(document.querySelector('.member')?.textContent).toContain('skontroluj výsledok a potom sa znovu pripoj')
 expect([...document.querySelectorAll('button')].some(b => b.textContent === 'Obnoviť po kontrole')).toBe(false)
})

it('does not present manual native turn bookkeeping as team-assigned work', async () => {
 team.tasks = [
  { id: 'native', from: 'user', to: 'chat', role: 'member', text: 'Private manual bookkeeping', state: 'running', kind: 'user', origin: 'external', created_at: '2026-11-04T12:00:00Z' },
  { id: 'assigned', from: 'lead', to: 'chat', role: 'review', text: 'Review requested change', state: 'queued', kind: 'task', created_at: '2026-11-04T12:00:01Z' }
 ]
 await show()
 expect(document.querySelector('.task-list')?.textContent).toContain('Review requested change')
 expect(document.querySelector('.task-list')?.textContent).not.toContain('Private manual bookkeeping')
})

it('saves the current choices as the project profile with complex effort', async () => {
 await showPickers()
 choose(picker('Model: Návrh a plán'), 'gpt-6.1-sol')
 choose(picker('Uvažovanie: Návrh a plán'), 'medium')
 choose(picker('Zložitá úloha: Návrh a plán'), 'high')
 const save = [...document.querySelectorAll('button')].find(b => b.textContent === 'Uložiť ako profil projektu')!
 save.click()
 await vi.waitFor(() => expect(requests.some(request => request.path === '/api/teams/profiles')).toBe(true))
 const request = requests.find(request => request.path === '/api/teams/profiles') as { body: Record<string, Record<string, unknown>> }
 expect(putPaths).toEqual(['/api/teams/profiles'])
 expect(request.body.codex.design).toEqual({ model: 'gpt-6.1-sol', effort: 'medium', complex_effort: 'high' })
 expect(Object.keys(request.body.codex).sort()).toEqual(['design', 'diagnosis', 'implementation', 'lead', 'mechanical', 'review'])
 await vi.waitFor(() => expect(document.querySelector('[role="status"]')?.textContent).toContain('Profil projektu je uložený'))
 expect(requests.some(request => request.path === '/api/teams')).toBe(false)
})

it('shows a save failure and the layer each role profile came from', async () => {
 const catalog = pickerCatalog()
 catalog.profile_sources = { codex: { lead: 'project', review: 'default' } }
 await showPickers(catalog)
 expect(document.querySelector('.setup table')?.textContent).toContain('Zdroj: Profil projektu')
 expect(document.querySelector('.setup table')?.textContent).toContain('Zdroj: Predvolené')
 const original = globalThis.fetch
 vi.stubGlobal('fetch', (path: string, init?: RequestInit) => init?.method === 'PUT'
  ? Promise.resolve(Response.json({ error: 'codex/review: model is unknown' }, { status: 400 })) : original(path, init))
 ;[...document.querySelectorAll('button')].find(b => b.textContent === 'Uložiť ako profil projektu')!.click()
 await vi.waitFor(() => expect(document.querySelector('[role="alert"]')?.textContent).toContain('model is unknown'))
})

it('explains why each task got its model', async () => {
 team.tasks = [{ id: 'review', from: 'lead', to: 'chat', role: 'review', text: 'Review it', state: 'queued', kind: 'task', created_at: '2026-11-04T12:00:01Z',
  profile: { runtime: 'codex', model: 'gpt-6-astra', effort: 'xhigh' }, profile_reason: 'rola review → gpt-6-astra, náročná úloha → xhigh' }]
 await show()
 const list = document.querySelector('.task-list')!
 expect(list.textContent).toContain('Prečo tento model?')
 expect(list.textContent).toContain('gpt-6-astra / xhigh · rola review → gpt-6-astra, náročná úloha → xhigh')
})

const taskInput = () => document.querySelector('#team-input') as HTMLTextAreaElement

it('pre-fills the task textarea from a pending Risk Analyzer hand-off and clears it', async () => {
 appState.pendingTeamTask = { text: 'Add Excel export' }
 await show()
 expect(taskInput().value).toBe('Add Excel export')
 expect(appState.pendingTeamTask).toBeNull()
})

async function showWithoutTeam() {
 vi.stubGlobal('fetch', async () => Response.json({ teams: [] }))
 app = mount(Team, { target: document.body })
 flushSync()
 await vi.waitFor(() => expect(document.body.textContent).toContain('Pripraviť tím'))
}

it('shows the pending task as a read-only block in the setup when no team exists', async () => {
 appState.pendingTeamTask = { text: 'Add Excel export' }
 await showWithoutTeam()
 expect(document.querySelector('#team-input')).toBeNull()
 expect(document.querySelector('.setup .pending-task')?.textContent).toContain('Zadanie: Add Excel export')
})

it('shows no pending task block in the setup when nothing is pending', async () => {
 await showWithoutTeam()
 expect(document.querySelector('.pending-task')).toBeNull()
})

it('leaves the task textarea empty and sends only the task fields when nothing is pending', async () => {
 await show()
 expect(taskInput().value).toBe('')
 taskInput().value = 'Manual task'
 taskInput().dispatchEvent(new Event('input', { bubbles: true }))
 flushSync()
 ;(document.querySelector('form.compose') as HTMLFormElement).dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
 await vi.waitFor(() => expect(requests.some(r => r.path === '/api/teams/team-test/tasks')).toBe(true))
 const body = requests.find(r => r.path === '/api/teams/team-test/tasks')!.body as Record<string, unknown>
 expect(body).not.toHaveProperty('workflow_type')
 expect(body).not.toHaveProperty('complexity')
})

function workspaceCheckbox(): HTMLInputElement | null {
 return document.querySelector('input[aria-label="Izolované pracovné priestory"]')
}
function startButton() {
 return [...document.querySelectorAll('button')].find(b => b.textContent === 'Spustiť tím')!
}

it('sends isolated_workspaces when the isolation checkbox is ticked and it defaults to off', async () => {
 await showPickers()
 const box = workspaceCheckbox()
 expect(box).not.toBeNull()
 expect(box!.checked).toBe(false)
 expect(document.querySelector('label.isolate')!.textContent).toContain('Izolované pracovné priestory (implementátori pracujú vo vlastných git worktree)')
 box!.click()
 flushSync()
 startButton().click()
 await vi.waitFor(() => expect(requests.some(request => request.path === '/api/teams')).toBe(true))
 expect(requests.find(request => request.path === '/api/teams')!.body).toMatchObject({ isolated_workspaces: true })
})

it('keeps the create request unchanged when isolation is not chosen', async () => {
 await showPickers()
 startButton().click()
 await vi.waitFor(() => expect(requests.some(request => request.path === '/api/teams')).toBe(true))
 const body = requests.find(request => request.path === '/api/teams')!.body as Record<string, unknown>
 expect(Object.keys(body).sort()).toEqual(['limits', 'members'])
})

it('shows the handoff card only for a team with isolated workspaces', async () => {
 const handoffGets: string[] = []
 const ordinaryFetch = globalThis.fetch
 vi.stubGlobal('fetch', (path: string, init?: RequestInit) => {
  if (path.endsWith('/handoff')) { handoffGets.push(path); return Promise.resolve(Response.json({ isolated: true, members: [], merged: [], conflicts: [], warnings: [] })) }
  return ordinaryFetch(path, init)
 })
 await show()
 expect(document.querySelector('.team-handoff')).toBeNull()
 expect(handoffGets).toEqual([])
 team.isolated_workspaces = true
 await unmount(app!)
 document.body.innerHTML = ''
 await show()
 await vi.waitFor(() => expect(document.querySelector('.team-handoff')).not.toBeNull())
 expect(handoffGets).toEqual(['/api/teams/team-test/handoff'])
})

function secondTeam(): ManagedTeam {
 return { ...team, id: 'team-second', tasks: [{ id: 'k1', from: 'user', to: 'chat', role: 'lead', text: 'Hotovo', workflow_id: 'spec-demo', state: 'complete', kind: 'task', created_at: '2026-11-05T10:00:00Z' }] }
}
async function showWithTeams(list: ManagedTeam[]) {
 vi.stubGlobal('fetch', async () => Response.json({ teams: list }))
 await show()
}

it('preselects the team requested by the Overview and clears the request', async () => {
 appState.pendingTeamSelect = 'team-second'
 await showWithTeams([team, secondTeam()])
 await vi.waitFor(() => expect((document.querySelector('.team-picker select') as HTMLSelectElement).value).toBe('team-second'))
 expect(appState.pendingTeamSelect).toBeNull()
})

it('falls back to the first team when the requested team no longer exists', async () => {
 appState.pendingTeamSelect = 'gone'
 await showWithTeams([team, secondTeam()])
 await vi.waitFor(() => expect((document.querySelector('.team-picker select') as HTMLSelectElement).value).toBe('team-test'))
 expect(appState.pendingTeamSelect).toBeNull()
})

it('opens the workflow of a task in the Overview', async () => {
 window.location.hash = ''
 setActiveTab('team')
 await showWithTeams([secondTeam()])
 const link = document.querySelector('[aria-label="Otvoriť workflow spec-demo v Overview"]') as HTMLButtonElement
 expect(link).not.toBeNull()
 expect(link.textContent).toContain('Workflow: spec-demo · Otvoriť v Overview')
 link.click()
 expect(window.location.hash).toBe('#/workflow/spec-demo')
 expect(appState.activeTab).toBe('overview')
})

function reviewBox(): HTMLInputElement | null { return document.querySelector('input[aria-label="Review návrhov (voliteľné)"]') }
function reviewerSelect(): HTMLSelectElement | null { return document.querySelector('select[aria-label="Kto robí review"]') }
function roundsInput(): HTMLInputElement | null { return document.querySelector('input[aria-label="Max. kôl"]') }
function tick(box: HTMLInputElement) { box.click(); flushSync() }

it('does not add design_review to the create request unless it is chosen', async () => {
 await showPickers()
 expect(reviewBox()).not.toBeNull()
 expect(reviewBox()!.checked).toBe(false)
 expect(reviewerSelect()).toBeNull()
 startButton().click()
 await vi.waitFor(() => expect(requests.some(request => request.path === '/api/teams')).toBe(true))
 const body = requests.find(request => request.path === '/api/teams')!.body as Record<string, unknown>
 expect(Object.keys(body).sort()).toEqual(['limits', 'members'])
})

it('sends design_review with the chosen reviewer id and max rounds, ids matching the members', async () => {
 await showPickers()
 tick(reviewBox()!)
 choose(reviewerSelect()!, 'review-4')
 roundsInput()!.value = '5'
 roundsInput()!.dispatchEvent(new Event('input', { bubbles: true }))
 flushSync()
 startButton().click()
 await vi.waitFor(() => expect(requests.some(request => request.path === '/api/teams')).toBe(true))
 const body = requests.find(request => request.path === '/api/teams')!.body as { members: { id: string }[]; design_review: unknown }
 expect(body.design_review).toEqual({ enabled: true, reviewer_member_id: 'review-4', max_rounds: 5 })
 expect(body.members.map(member => member.id)).toContain('review-4')
})

it('lists every setup row except the lead as a reviewer with role label and id', async () => {
 await showPickers()
 tick(reviewBox()!)
 const values = [...reviewerSelect()!.options].map(option => option.value).filter(Boolean)
 expect(values).toEqual(['implementation-2', 'design-3', 'review-4', 'diagnosis-5', 'mechanical-6'])
 expect([...reviewerSelect()!.options].find(option => option.value === 'review-4')!.textContent).toContain('Nezávislá kontrola · review-4')
 expect(roundsInput()!.value).toBe('3')
})

it('requires a reviewer and 1-10 rounds before the team can start', async () => {
 await showPickers()
 tick(reviewBox()!)
 expect(startButton().disabled).toBe(true)
 expect(document.querySelector('.setup')!.parentElement!.textContent).toContain('Vyber, kto robí review.')
 choose(reviewerSelect()!, 'review-4')
 expect(startButton().disabled).toBe(false)
 for (const bad of ['0', '11', '']) {
  roundsInput()!.value = bad
  roundsInput()!.dispatchEvent(new Event('input', { bubbles: true }))
  flushSync()
  expect(startButton().disabled).toBe(true)
 }
})

it('shows the design review chip in the header of a team that has it enabled', async () => {
 team.design_review = { enabled: true, reviewer_member_id: 'review-2', max_rounds: 2 }
 await show()
 expect(document.querySelector('.design-chip')?.textContent).toBe('Review návrhov: review-2 · max. 2 kôl')
})

it('shows no design review chip for a team without the gate', async () => {
 await show()
 expect(document.querySelector('.design-chip')).toBeNull()
})
